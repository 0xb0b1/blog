---
title: "Reescrevendo um Serviço de Notificações Python em Go: 5x Throughput, 10x Menor"
date: 2025-12-10
description: "Um mergulho profundo em reescrever um backend de notificações Python asyncio em Go, alcançando 5x de melhoria em throughput, deduplicação distribuída adequada e imagem Docker 10x menor."
tags: ["golang", "python", "performance", "arquitetura", "aws-sns", "redis", "grpc"]
---

# Reescrevendo um Serviço de Notificações Python em Go: 5x Throughput, 10x Menor

Recentemente reescrevi um backend de notificações esportivas de Python para Go. O serviço lida com eventos de partidas (gols, cartões, pênaltis) e entrega push notifications via AWS SNS para milhões de dispositivos móveis através de FCM e APNs.

Os resultados me surpreenderam:

```
| Métrica            | Python            | Go                | Melhoria    |
|--------------------|-------------------|-------------------|-------------|
| Throughput         | ~1.000 eventos/s  | ~5.000 eventos/s  | 5x          |
| Cold start         | 3 segundos        | 100ms             | 30x         |
| Imagem Docker      | 500MB             | 50MB              | 10x         |
| Memória baseline   | 100MB             | 30MB              | 3x          |
```

Mas a verdadeira vitória não foi performance — foi **corretude**. A versão Python tinha um bug sutil de deduplicação que causava notificações duplicadas. Deixe-me explicar.

## O Problema: Deduplicação Distribuída

Notificações esportivas são críticas em tempo. Quando o Messi faz um gol, milhões de usuários precisam saber _imediatamente_. Mas devem ser notificados **apenas uma vez**.

A implementação Python usava um dicionário em memória para deduplicação:

```python
# Deduplicação por instância (quebrada!)
class EventProcessor:
    def __init__(self):
        self._seen_events = {}  # Dict em memória

    async def process_event(self, event):
        if event.key in self._seen_events:
            return  # Pular duplicado
        self._seen_events[event.key] = True
        await self.send_notification(event)
```

**O bug**: Cada instância do servidor tem seu próprio dict `_seen_events`. Com 3 instâncias atrás de um load balancer, o mesmo evento pode atingir instâncias diferentes e bypass completamente a deduplicação.

```
Evento chega → Load Balancer → Instância A (envia notificação)
Evento chega → Load Balancer → Instância B (envia notificação) ← DUPLICADO!
Evento chega → Load Balancer → Instância C (envia notificação) ← DUPLICADO!
```

Usuários recebiam 3 notificações para o mesmo gol. Não é bom.

## A Solução: Redis + Scripts Lua

A versão Go usa Redis com scripts Lua atômicos. O insight chave é que toda a operação de check-and-set deve acontecer atomicamente:

```lua
-- Deduplicação atômica no Redis
local existing = redis.call('GET', key)
if existing then
    return 0  -- DUPLICADO
end
redis.call('SET', key, value, 'EX', ttl)
return 1  -- NOVO
```

**Por que scripts Lua?** A operação inteira acontece atomicamente dentro do Redis. Sem race conditions possíveis:

```
Instância A: EVAL script → Retorna 1 (NOVO) → Envia notificação
Instância B: EVAL script → Retorna 0 (DUPLICADO) → Pula
Instância C: EVAL script → Retorna 0 (DUPLICADO) → Pula
```

Uma notificação. Como pretendido.

### Três Estados de Deduplicação

O script Lua retorna três estados possíveis:

```
| Estado         | Significado                  | Ação              |
|----------------|------------------------------|-------------------|
| NOVO (1)       | Nunca visto antes            | Enviar notificação|
| DUPLICADO (0)  | Payload exatamente igual     | Pular silenciosamente|
| CORREÇÃO (2)   | Mesmo evento, dados diferentes| Enviar atualização|
```

O estado `CORREÇÃO` lida com cenários do mundo real como: "Gol atribuído ao Jogador A" seguido de "Correção VAR: Gol atribuído ao Jogador B". Usuários devem receber ambas notificações.

## Arquitetura: De Async para Worker Pool

### Python: Event Loop Single-Threaded

```python
async def process_events(events):
    # Parece paralelo, mas não é!
    await asyncio.gather(*[process_event(e) for e in events])
    # Todas as tasks compartilham UMA thread
```

O `asyncio` do Python é _concorrente_ mas não _paralelo_. Todas as coroutines rodam em uma única thread, compartilhando um núcleo de CPU. Quando você faz `await` em uma operação I/O, outras tasks podem rodar — mas trabalho CPU-bound bloqueia tudo.

### Go: Paralelismo Real

```go
// Padrão worker pool
for i := 0; i < workerCount; i++ {
    go func() {
        for event := range eventChan {
            processEvent(ctx, event)
        }
    }()
}
```

Goroutines do Go são multiplexadas entre threads do OS pelo runtime. Com 10 workers em uma máquina de 8 núcleos, você tem paralelismo real — trabalho CPU-bound e I/O-bound escalam.

### Benefícios do Worker Pool

O padrão worker pool fornece:

1. **Concorrência controlada**: Número fixo de workers previne exaustão de recursos
2. **Backpressure**: Quando o canal enche, produtores bloqueiam
3. **Shutdown gracioso**: Fechar canal, esperar workers terminarem

## Tratamento de Erros: Explícito vs Implícito

Python facilita perder erros:

```python
async def process_event(event):
    try:
        await sns.publish(...)
    except Exception as e:
        logger.error(e)
        # Evento perdido. Sem retry. Sem alerta.
```

Go força você a tratar cada erro:

```go
if err := sns.Publish(ctx, topic, payload); err != nil {
    // Deve tratar - código não compila sem isso
    return fmt.Errorf("publish failed: %w", err)
}
```

O compilador Go é sua rede de segurança. Você não pode esquecer de tratar um erro — tem que ignorá-lo explicitamente com `_`.

### Estratégia Fail-Open

Para deduplicação, escolhemos uma estratégia fail-open. Se o Redis estiver indisponível, permitir a notificação.

**Por que fail-open?** Em um sistema de notificações esportivas, uma notificação de gol perdida é pior que uma duplicada. Usuários podem ignorar duplicados mas não podem recuperar eventos perdidos.

## Deploy: De 500MB para 50MB

Python requer o interpretador, pacotes pip e dependências do sistema:

```dockerfile
# Python: ~500MB de imagem
FROM python:3.11-slim
RUN pip install -r requirements.txt
COPY . .
CMD ["python", "-m", "app"]
```

Go compila para um binário estático:

```dockerfile
# Go: ~50MB de imagem
FROM golang:1.23-alpine AS builder
RUN CGO_ENABLED=0 go build -o /app ./cmd/server

FROM alpine:3.19
COPY --from=builder /app /app
CMD ["/app"]
```

**Benefícios operacionais:**

- Imagens 10x menores = deploys mais rápidos, menos storage
- Startup 30x mais rápido = melhor auto-scaling
- Sem conflitos de dependência = debugging mais simples
- Binário estático = copie e rode em qualquer lugar

## Principais Aprendizados

1. **Sistemas distribuídos precisam de estado distribuído**. Deduplicação em memória não funciona com múltiplas instâncias. Use Redis com operações atômicas.

2. **asyncio ≠ paralelismo**. O event loop do Python é concorrente mas single-threaded. Para paralelismo real, você precisa de múltiplos processos ou uma linguagem diferente.

3. **Explícito é melhor que implícito**. O tratamento de erros e injeção de dependência do Go tornam código mais fácil de testar e debugar.

4. **Fail-open para notificações críticas**. Melhor duplicar ocasionalmente do que perder uma notificação de gol.

5. **Meça, não assuma**. A melhoria de 5x em throughput veio de profiling e entendimento de onde o tempo realmente era gasto.

## Você Deveria Reescrever?

Não necessariamente. Reescritas são caras e arriscadas. Mas considere Go quando:

- Você precisa de paralelismo real (não apenas concorrência)
- Você está deployando para containers em escala
- Você tem estado distribuído que precisa de operações atômicas
- Sua equipe está confortável com linguagens estaticamente tipadas

A versão Python funcionava bem em escala menor. Mas para lidar com milhares de eventos por segundo com garantia de entrega exactly-once, Go foi a escolha certa.
