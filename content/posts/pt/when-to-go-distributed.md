---
title: "Quando Ir para Sistemas Distribuídos"
date: 2025-03-01
description: "Um guia para decidir quando migrar de um monolito para arquitetura distribuída, e como fazer isso corretamente."
tags: ["golang", "sistemas-distribuidos", "arquitetura", "microsserviços"]
---

Sistemas distribuídos estão na moda, mas nem sempre são a resposta certa. Vamos explorar quando faz sentido distribuir sua aplicação e quando um monolito bem estruturado é a melhor escolha.

## O Atrativo dos Sistemas Distribuídos

Microsserviços e sistemas distribuídos prometem:

- Escalabilidade independente de componentes
- Deploy independente de equipes
- Isolamento de falhas
- Flexibilidade tecnológica

Mas a realidade é mais complexa.

## Custos Ocultos da Distribuição

### 1. Complexidade Operacional

```
Monolito:
- 1 processo para monitorar
- 1 deploy para gerenciar
- 1 log para analisar

10 Microsserviços:
- 10 processos para monitorar
- 10 deploys para coordenar
- 10 streams de logs para correlacionar
- Service discovery, load balancing, circuit breakers...
```

### 2. Latência de Rede

```go
// Monolito: chamada de função
resultado := servico.Processar(dados)
// ~1 microsegundo

// Microsserviços: chamada HTTP
resp, err := http.Post("http://servico/processar", dados)
// ~1-100 milissegundos (1000x mais lento!)
```

### 3. Consistência de Dados

```go
// Monolito: transação única
tx.Begin()
criarPedido(tx, pedido)
atualizarEstoque(tx, itens)
tx.Commit()

// Distribuído: consistência eventual
criarPedido(pedido)         // Serviço de Pedidos
publicarEvento("pedido_criado")
// ... algum tempo depois...
atualizarEstoque(itens)     // Serviço de Estoque
// E se falhar no meio?
```

## Quando Distribuir?

### Sinais de que Você PRECISA Distribuir

1. **Escalabilidade Divergente**
```
API Gateway: 1000 req/s
Processamento de Relatórios: 10 req/s
Notificações: 10000 mensagens/s

→ Escalar tudo junto é desperdício
```

2. **Equipes Independentes**
```
Equipe A: Checkout (2 deploys/dia)
Equipe B: Recomendações (1 deploy/semana)
Equipe C: Relatórios (1 deploy/mês)

→ Bloqueio de deploy afeta produtividade
```

3. **Isolamento de Falhas Crítico**
```
Serviço de Pagamento caiu
→ Não deve derrubar catálogo de produtos
```

4. **Requisitos Tecnológicos Diferentes**
```
Busca: Elasticsearch
Filas: RabbitMQ
Cache: Redis
Análise: Python + Pandas

→ Polyglot faz sentido
```

### Sinais de que Você NÃO Precisa Distribuir

1. **Equipe Pequena (< 10 pessoas)**
2. **Tráfego Gerenciável (< 10k req/s)**
3. **Modelo de Dados Fortemente Acoplado**
4. **Sem Experiência em Operações Distribuídas**

## O Caminho do Meio: Modular Monolith

```
┌────────────────────────────────────────────────┐
│              Modular Monolith                   │
│                                                 │
│  ┌─────────┐  ┌─────────┐  ┌─────────────┐    │
│  │ Pedidos │  │ Estoque │  │ Notificações│    │
│  │         │  │         │  │             │    │
│  │ - API   │  │ - API   │  │ - Workers   │    │
│  │ - Lógica│  │ - Lógica│  │ - Filas     │    │
│  │ - Dados │  │ - Dados │  │ - Templates │    │
│  └────┬────┘  └────┬────┘  └──────┬──────┘    │
│       │            │              │            │
│       └────────────┼──────────────┘            │
│                    │                           │
│            ┌───────┴───────┐                   │
│            │   Banco de    │                   │
│            │    Dados      │                   │
│            └───────────────┘                   │
└────────────────────────────────────────────────┘
```

Benefícios:
- Limites claros entre módulos
- Ainda é um deploy único
- Fácil de extrair para microsserviço depois
- Simplicidade operacional mantida

### Implementação em Go

```go
// Estrutura de diretórios
project/
├── cmd/
│   └── api/
│       └── main.go
├── internal/
│   ├── pedidos/
│   │   ├── handler.go
│   │   ├── service.go
│   │   ├── repository.go
│   │   └── domain/
│   ├── estoque/
│   │   ├── handler.go
│   │   ├── service.go
│   │   └── domain/
│   └── notificacoes/
│       ├── worker.go
│       └── templates/
├── pkg/
│   └── database/
└── go.mod
```

```go
// Comunicação entre módulos via interfaces
package pedidos

type EstoqueService interface {
    ReservarItens(itens []Item) error
    LiberarReserva(reservaID string) error
}

type NotificacaoService interface {
    EnviarConfirmacao(pedido Pedido) error
}

type PedidoService struct {
    repo        PedidoRepository
    estoque     EstoqueService
    notificacao NotificacaoService
}
```

## Estratégias de Migração

### 1. Strangler Fig Pattern

Gradualmente substituir funcionalidades:

```
Fase 1: Monolito atende tudo
                    ┌──────────────┐
Cliente → Proxy → │   Monolito   │
                    └──────────────┘

Fase 2: Novo serviço para funcionalidade específica
                    ┌──────────────┐
Cliente → Proxy → │   Monolito   │
             ↓      └──────────────┘
             ↓      ┌──────────────┐
             └────→ │   Busca (Go) │
                    └──────────────┘

Fase 3: Mais serviços extraídos
                    ┌──────────────┐
Cliente → Proxy → │   Monolito   │ (menor)
             ↓      └──────────────┘
             ↓      ┌──────────────┐
             ├────→ │    Busca     │
             │      └──────────────┘
             │      ┌──────────────┐
             └────→ │   Checkout   │
                    └──────────────┘
```

### 2. Branch by Abstraction

```go
// Interface que abstrai implementação
type PagamentoProcessor interface {
    Processar(pagamento Pagamento) error
}

// Implementação monolítica atual
type PagamentoLocal struct {
    db *sql.DB
}

// Implementação distribuída futura
type PagamentoRemoto struct {
    client *http.Client
    url    string
}

// Toggle de feature para migração gradual
func NewPagamentoProcessor(usarRemoto bool) PagamentoProcessor {
    if usarRemoto {
        return &PagamentoRemoto{...}
    }
    return &PagamentoLocal{...}
}
```

## Ferramentas para Distribuição em Go

### gRPC para Comunicação

```go
// Definição do serviço
service PedidoService {
    rpc CriarPedido(CriarPedidoRequest) returns (Pedido);
    rpc ObterPedido(ObterPedidoRequest) returns (Pedido);
}

// Cliente
conn, _ := grpc.Dial("pedido-service:50051", grpc.WithInsecure())
client := pb.NewPedidoServiceClient(conn)
pedido, err := client.CriarPedido(ctx, &pb.CriarPedidoRequest{...})
```

### Event Sourcing para Consistência

```go
// Publicar evento
err := eventBus.Publish(ctx, PedidoCriado{
    PedidoID: pedido.ID,
    Itens:    pedido.Itens,
    Total:    pedido.Total,
})

// Consumir em outro serviço
eventBus.Subscribe("pedido.criado", func(evento PedidoCriado) {
    estoqueService.ReservarItens(evento.Itens)
})
```

## Conclusão

1. **Comece monolítico**: É mais simples e funciona na maioria dos casos
2. **Modularize primeiro**: Prepare seu código para extração futura
3. **Distribua quando necessário**: Não antes
4. **Migre gradualmente**: Strangler Fig é seu amigo
5. **Invista em observabilidade**: Logs, métricas, traces são obrigatórios

A pergunta não é "devemos usar microsserviços?" mas sim "quais problemas específicos a distribuição resolve para nós, e o custo vale a pena?"

Na maioria dos casos, um monolito bem estruturado em Go vai mais longe do que você imagina. Escale quando precisar, não quando for moda.
