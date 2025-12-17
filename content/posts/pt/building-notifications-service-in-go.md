---
title: "Construindo um Serviço de Push Notifications de Alta Performance em Go"
date: 2025-12-17
draft: false
description: "Construir um serviço de push notifications que lida com milhões de eventos de forma confiável requer decisões arquiteturais cuidadosas. Neste post, vou mostrar como construí um backend de notificações em Go que processa eventos via gRPC e entrega push notifications através do AWS SNS."
tags: ["go", "grpc", "aws-sns", "redis", "notificacoes", "sistemas-distribuidos"]
---

Construir um serviço de push notifications que lida com milhões de eventos de forma confiável requer decisões arquiteturais cuidadosas. Neste post, vou mostrar como construí um backend de notificações em Go que processa eventos via gRPC e entrega push notifications através do AWS SNS.

## Visão Geral da Arquitetura

O serviço segue uma arquitetura simples mas eficaz:

```
Eventos gRPC → Processador de Eventos → Deduplicação → Publicador SNS → FCM/APNs
                                             ↓
                                           Redis
```

**Componentes principais:**

- **Servidor gRPC** para receber eventos de serviços upstream
- **Pool de workers** para processamento concorrente de eventos
- **Deduplicação baseada em Redis** para prevenir notificações duplicadas
- **AWS SNS** para entrega cross-platform (FCM para Android, APNs para iOS)

## Estrutura do Projeto

Organizei o codebase seguindo boas práticas de Go:

```
├── cmd/
│   ├── server/          # Ponto de entrada da aplicação
│   └── tasks/           # CLI para tarefas agendadas
├── internal/
│   ├── config/          # Gerenciamento de configuração
│   ├── dedup/           # Lógica de deduplicação
│   ├── grpc/            # Handlers gRPC
│   ├── processor/       # Processamento de eventos
│   ├── repository/      # Acesso a banco de dados
│   ├── sns/             # Cliente AWS SNS
│   └── tasks/           # Implementações de tarefas agendadas
├── pkg/
│   └── proto/           # Definições de Protocol Buffer
└── scripts/
    └── benchmark/       # Ferramentas de teste de performance
```

## Deduplicação Distribuída com Redis

Um dos requisitos críticos era prevenir notificações duplicadas. Usuários recebendo a mesma notificação várias vezes cria uma experiência ruim.

O desafio: com múltiplas instâncias do serviço atrás de um load balancer, deduplicação tradicional em memória não funciona. O mesmo evento pode chegar em instâncias diferentes.

**Solução:** Operações atômicas no Redis usando scripts Lua.

```go
type Deduplicator struct {
    redis       redis.UniversalClient
    ttl         time.Duration
    dedupScript *redis.Script
}

func New(redisClient redis.UniversalClient, ttl time.Duration, logger *zap.Logger) *Deduplicator {
    // Script Lua para check-and-set atômico
    // Retorna: 0 = duplicado, 1 = novo, 2 = correção
    script := redis.NewScript(`
        local existing = redis.call('GET', KEYS[1])
        if existing then
            local data = cjson.decode(existing)
            if data.h == ARGV[1] then
                return 0  -- Duplicado: mesmo hash
            else
                -- Correção: hash diferente, atualizar e permitir
                redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
                return 2
            end
        else
            -- Novo: definir e permitir
            redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
            return 1
        end
    `)

    return &Deduplicator{
        redis:       redisClient,
        ttl:         ttl,
        dedupScript: script,
    }
}
```

O script Lua roda atomicamente no Redis, garantindo que mesmo com requisições concorrentes de múltiplas instâncias, apenas uma notificação é enviada por evento único.

**Três estados de deduplicação:**

1. **Novo (1):** Primeira vez vendo este evento, permitir notificação
2. **Duplicado (0):** Mesmo evento com mesmo conteúdo, bloquear
3. **Correção (2):** Mesmo evento mas conteúdo mudou (correção de placar), permitir

## Padrão Worker Pool

Para processar eventos concorrentemente mantendo controle sobre uso de recursos, implementei um worker pool:

```go
type EventProcessor struct {
    eventChan   chan EventJob
    workerCount int
    wg          sync.WaitGroup
    // ... outros campos
}

func (p *EventProcessor) Start(ctx context.Context) {
    p.eventChan = make(chan EventJob, p.workerCount*10)

    for i := 0; i < p.workerCount; i++ {
        p.wg.Add(1)
        go p.worker(ctx, i)
    }
}

func (p *EventProcessor) worker(ctx context.Context, id int) {
    defer p.wg.Done()

    for {
        select {
        case <-ctx.Done():
            return
        case job, ok := <-p.eventChan:
            if !ok {
                return
            }
            p.processSingleEvent(job.ctx, &job.event)
        }
    }
}
```

Este padrão fornece:

- **Concorrência limitada:** Controle exatamente quantas goroutines processam eventos
- **Backpressure:** Canal com buffer previne sobrecarregar o sistema
- **Shutdown gracioso:** WaitGroup garante que todo trabalho em andamento completa

## Handler gRPC com Fire-and-Forget

O handler gRPC recebe eventos e retorna imediatamente, processando assincronamente:

```go
func (h *EventsHandler) NotifyEvents(ctx context.Context, req *pb.EventsRequest) (*emptypb.Empty, error) {
    events := make([]models.EventRequest, 0, len(req.Events))
    for _, e := range req.Events {
        events = append(events, convertProtoToModel(e))
    }

    // Fire and forget - processar em background
    go func() {
        processCtx := context.Background()
        if err := h.processor.ProcessEvents(processCtx, events); err != nil {
            h.logger.Error("falha ao processar eventos", zap.Error(err))
        }
    }()

    return &emptypb.Empty{}, nil
}
```

Esta escolha de design prioriza:

- **Baixa latência:** Clientes não esperam pelo processamento
- **Confiabilidade:** Processamento continua mesmo se cliente desconectar
- **Throughput:** Servidor pode aceitar mais requisições enquanto processa anteriores

## Resultados de Performance

Benchmark em uma máquina de desenvolvimento padrão:

```
╔══════════════════════════════════════════════════════════════╗
║                    RESULTADOS DO BENCHMARK                    ║
╠══════════════════════════════════════════════════════════════╣
║  Total de eventos:          5000                              ║
║  Concorrência:                10 workers                      ║
║  Tempo total:               265ms                             ║
╠══════════════════════════════════════════════════════════════╣
║                         THROUGHPUT                            ║
╠══════════════════════════════════════════════════════════════╣
║  Eventos/segundo:           18,879                            ║
╠══════════════════════════════════════════════════════════════╣
║                          LATÊNCIA                             ║
╠══════════════════════════════════════════════════════════════╣
║  Média:                     525µs                             ║
║  P50 (mediana):             414µs                             ║
║  P95:                       1.16ms                            ║
║  P99:                       2.72ms                            ║
╠══════════════════════════════════════════════════════════════╣
║                        CONFIABILIDADE                         ║
╠══════════════════════════════════════════════════════════════╣
║  Taxa de sucesso:           100%                              ║
╚══════════════════════════════════════════════════════════════╝
```

## Principais Aprendizados

1. **Scripts Lua no Redis** fornecem operações atômicas essenciais para deduplicação distribuída
2. **Worker pools** dão controle refinado sobre concorrência
3. **Handlers gRPC fire-and-forget** maximizam throughput para workloads assíncronos
4. **SNS abstrai complexidade de plataforma** - uma publicação, entrega para iOS e Android
5. **Configuração estruturada** torna deploy entre ambientes direto

## Próximos Passos

Melhorias futuras que estou considerando:

- **Métricas com Prometheus** para melhor observabilidade
- **Rate limiting** por dispositivo para prevenir spam de notificações
- **Batching de mensagens** para SNS para reduzir chamadas de API
- **Dead letter queue** para notificações falhas

---

A arquitetura completa lida com os requisitos do mundo real de um app de notificações esportivas: alto throughput durante partidas ao vivo, entrega confiável e nenhuma notificação duplicada. As primitivas de concorrência do Go e o ecossistema de bibliotecas tornaram a construção deste serviço direta.
