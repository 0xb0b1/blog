---
title: "Construindo um Sistema Bancário Event-Sourced em Go: Da Teoria à Produção"
date: 2025-12-05
description: "Um guia prático para implementar Event Sourcing e CQRS em Go, com um exemplo completo de domínio bancário incluindo agregados, projeções e snapshots."
tags: ["golang", "event-sourcing", "CQRS", "DDD", "arquitetura", "backend"]
---

Escrevi sobre os [padrões CQRS e Event Sourcing](/pt/posts/cqrs-event-sourcing-go-blog-post) há um tempo, cobrindo a teoria e os conceitos. Mas teoria só leva você até certo ponto. Desta vez, estou compartilhando algo diferente: **uma implementação completa e funcional** que você pode executar, estudar e estender.

O projeto se chama [eventsource](https://github.com/0xb0b1/eventsource) — um sistema bancário que demonstra Event Sourcing e CQRS em ação. Vamos detalhar como tudo se encaixa.

## Por Que um Sistema Bancário?

Bancário é o domínio perfeito para Event Sourcing porque:

1. **Trilhas de auditoria são obrigatórias** — reguladores querem saber cada transação
2. **Mudanças de estado são eventos de negócio** — "dinheiro depositado" é mais significativo que "saldo atualizado"
3. **Consultas temporais importam** — "qual era o saldo em 15 de março?"
4. **Consistência é crítica** — você não pode perder dinheiro para race conditions

Além disso, todos entendem o domínio. Não é preciso explicar o que "depósito" ou "saque" significa.

## A Arquitetura

Aqui está a visão de 10.000 pés:

```
┌─────────────────────────────────────────────────────────────────────┐
│                           Cliente                                    │
└─────────────────────────────────────────────────────────────────────┘
                    │                           │
                    │ Comandos                  │ Consultas
                    ▼                           ▼
┌─────────────────────────────┐   ┌─────────────────────────────────┐
│      Command Handler        │   │         Query Service            │
│  (AbrirConta, Depositar,    │   │   (ObterConta, ListarContas,    │
│   Sacar, Transferir)        │   │    ObterTransacoes)              │
└─────────────────────────────┘   └─────────────────────────────────┘
           │                                    │
           ▼                                    │
┌─────────────────────────────┐                │
│     Aggregate Root          │                │
│     (ContaBancaria)         │                │
└─────────────────────────────┘                │
           │                                    │
           ▼                                    │
┌─────────────────────────────┐   ┌────────────┴────────────────────┐
│       Event Store           │──▶│         Projeções               │
│   (Log append-only)         │   │  (SaldoConta, Transações)       │
└─────────────────────────────┘   └─────────────────────────────────┘
```

**Lado de escrita**: Comandos → Agregados → Eventos → Event Store

**Lado de leitura**: Eventos → Projeções → Tabelas desnormalizadas → Consultas

Dois caminhos diferentes, otimizados para seus trabalhos específicos.

## Parte 1: O Event Store

Tudo começa com o Event Store. É um log append-only onde cada mudança de estado se torna um evento imutável.

### A Interface

```go
type EventStore interface {
    // AppendEvents adiciona eventos com controle de concorrência otimista
    AppendEvents(ctx context.Context, aggregateID string, expectedVersion int, events []StoredEvent) error

    // LoadEvents recupera eventos de um agregado
    LoadEvents(ctx context.Context, aggregateID string, fromVersion int) ([]StoredEvent, error)

    // LoadAllEvents para projeções se atualizarem
    LoadAllEvents(ctx context.Context, fromPosition int64, limit int) ([]StoredEvent, error)

    // Snapshots para performance
    SaveSnapshot(ctx context.Context, snapshot Snapshot) error
    LoadSnapshot(ctx context.Context, aggregateID string) (*Snapshot, error)
}
```

### Concorrência Otimista

O detalhe chave é `expectedVersion`. Ao salvar eventos, verificamos se a versão atual do agregado corresponde ao que esperamos:

```go
func (s *PostgresEventStore) AppendEvents(ctx context.Context, aggregateID string, expectedVersion int, events []StoredEvent) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // Verificar versão atual com lock de linha
    var currentVersion int
    err = tx.QueryRowContext(ctx, `
        SELECT COALESCE(MAX(version), 0) FROM events
        WHERE aggregate_id = $1 FOR UPDATE
    `, aggregateID).Scan(&currentVersion)

    if currentVersion != expectedVersion {
        return ErrConcurrencyConflict  // Alguém modificou!
    }

    // Inserir eventos...
    return tx.Commit()
}
```

Isso previne race conditions. Se dois processos tentarem modificar a mesma conta simultaneamente, um falhará com um conflito de concorrência. Exatamente o que queremos em bancário.

## Parte 2: Eventos de Domínio

Em Event Sourcing, eventos são os fatos do seu sistema. Eles descrevem o que aconteceu, não qual é o estado atual.

```go
const (
    ContaAberta       = "ContaAberta"
    DinheiroDepositado = "DinheiroDepositado"
    DinheiroSacado    = "DinheiroSacado"
    TransferenciaEnviada = "TransferenciaEnviada"
    TransferenciaRecebida = "TransferenciaRecebida"
    ContaFechada      = "ContaFechada"
)

type DinheiroDepositadoData struct {
    Valor     decimal.Decimal `json:"valor"`
    Descricao string          `json:"descricao"`
}

type TransferenciaEnviadaData struct {
    ContaDestinoID string          `json:"conta_destino_id"`
    Valor          decimal.Decimal `json:"valor"`
    Descricao      string          `json:"descricao"`
}
```

Note a nomenclatura: tempo passado, descrevendo algo que **já aconteceu**. Não `DepositarDinheiro` (um comando) mas `DinheiroDepositado` (um fato).

## Parte 3: O Agregado Conta Bancária

O agregado é onde a lógica de negócio vive. Ele impõe invariantes e produz eventos.

```go
type ContaBancaria struct {
    es.AggregateBase

    NomeTitular string
    Saldo       decimal.Decimal
    Status      StatusConta
    CriadoEm    time.Time
    AtualizadoEm time.Time
}
```

### Operações de Negócio

Cada operação valida regras de negócio e produz eventos:

```go
func (c *ContaBancaria) Sacar(valor decimal.Decimal, descricao string) error {
    // Regra de negócio: Não pode sacar de conta fechada
    if c.Status == StatusContaFechada {
        return ErrContaFechada
    }

    // Regra de negócio: Valor deve ser positivo
    if !valor.IsPositive() {
        return ErrValorInvalido
    }

    // Regra de negócio: Não pode ficar negativo
    if c.Saldo.LessThan(valor) {
        return ErrSaldoInsuficiente
    }

    // Todas as regras passaram — criar evento
    evt := event.NewDinheiroSacado(c.AggregateID(), c.Version()+1, valor, descricao)

    // Aplicar evento para atualizar estado
    if err := c.ApplyEvent(evt); err != nil {
        return err
    }

    // Registrar para persistência
    c.RecordEvent(evt)
    return nil
}
```

Este padrão — **validar, criar evento, aplicar, registrar** — é o coração do Event Sourcing.

## Parte 4: Carregando Agregados

Quando precisamos trabalhar com um agregado, carregamos ele reproduzindo eventos:

```go
func (r *ContaRepository) Load(ctx context.Context, id string) (*ContaBancaria, error) {
    conta := NovaContaBancaria(id)

    // Tentar snapshot primeiro (otimização)
    snapshot, _ := r.eventStore.LoadSnapshot(ctx, id)
    fromVersion := 0

    if snapshot != nil {
        conta.FromSnapshot(snapshot.Data)
        conta.SetVersion(snapshot.Version)
        fromVersion = snapshot.Version
    }

    // Carregar eventos desde o snapshot (ou todos se não houver snapshot)
    storedEvents, err := r.eventStore.LoadEvents(ctx, id, fromVersion)
    if err != nil {
        return nil, err
    }

    // Reproduzir eventos para reconstruir estado
    for _, stored := range storedEvents {
        evt := deserializeEvent(stored)
        conta.ApplyEvent(evt)
    }

    return conta, nil
}
```

O estado do agregado nunca é armazenado diretamente — é sempre derivado de eventos. Este é o princípio central do Event Sourcing.

## Parte 5: Snapshots para Performance

Se uma conta tem 10.000 eventos, reproduzir todos eles a cada requisição é lento. Snapshots resolvem isso:

```go
type contaSnapshot struct {
    NomeTitular string          `json:"nome_titular"`
    Saldo       decimal.Decimal `json:"saldo"`
    Status      StatusConta     `json:"status"`
    CriadoEm    time.Time       `json:"criado_em"`
}

func (c *ContaBancaria) ToSnapshot() ([]byte, error) {
    snap := contaSnapshot{
        NomeTitular: c.NomeTitular,
        Saldo:       c.Saldo,
        Status:      c.Status,
        CriadoEm:    c.CriadoEm,
    }
    return json.Marshal(snap)
}
```

Salvamos snapshots periodicamente (ex: a cada 100 eventos):

```go
func (r *ContaRepository) Save(ctx context.Context, conta *ContaBancaria) error {
    // ... salvar eventos ...

    // Snapshot a cada 100 eventos
    if conta.Version() % 100 == 0 {
        r.saveSnapshot(ctx, conta)
    }

    return nil
}
```

Agora o carregamento é rápido: restaurar do snapshot, reproduzir apenas eventos recentes.

## Parte 6: Projeções CQRS

O lado de escrita otimiza para consistência. O lado de leitura otimiza para consultas. Projeções fazem a ponte:

```go
type SaldoContaProjection struct {
    db *sql.DB
}

func (p *SaldoContaProjection) Handle(ctx context.Context, event StoredEvent) error {
    switch event.EventType {
    case "ContaAberta":
        var data ContaAbertaData
        json.Unmarshal(event.Data, &data)

        _, err := p.db.ExecContext(ctx, `
            INSERT INTO saldos_contas (conta_id, nome_titular, saldo, status, criado_em)
            VALUES ($1, $2, $3, 'ativa', $4)
        `, data.ContaID, data.NomeTitular, data.DepositoInicial, event.Timestamp)
        return err

    case "DinheiroDepositado":
        var data DinheiroDepositadoData
        json.Unmarshal(event.Data, &data)

        _, err := p.db.ExecContext(ctx, `
            UPDATE saldos_contas SET saldo = saldo + $1 WHERE conta_id = $2
        `, data.Valor, event.AggregateID)
        return err

    // ... outros eventos
    }
    return nil
}
```

A projeção escuta eventos e mantém uma tabela desnormalizada otimizada para leituras. Sem joins necessários, sem carregamento de agregado — apenas consultas rápidas.

## Principais Aprendizados

Construir este projeto reforçou algumas lições importantes:

1. **Eventos são fatos, não comandos** — Nomeie-os no passado. Eles descrevem o que aconteceu, não o que você quer que aconteça.

2. **Agregados impõem invariantes** — Regras de negócio vivem no domínio, não em handlers ou serviços.

3. **Projeções são descartáveis** — Se seu modelo de leitura está errado, reconstrua-o dos eventos. Isso é libertador.

4. **Snapshots são otimização** — Não são necessários para corretude, apenas performance.

5. **Consistência eventual é uma feature** — Seus modelos de leitura podem ter atraso de milissegundos. Projete para isso.

6. **O log de eventos é sua trilha de auditoria** — Compliance grátis! Cada mudança de estado é registrada automaticamente.

## Quando Usar Este Padrão

Event Sourcing + CQRS é poderoso mas adiciona complexidade. Use quando:

- ✅ Você precisa de trilhas de auditoria
- ✅ Padrões de leitura e escrita divergem significativamente
- ✅ Você precisa de consultas time-travel ("qual era o estado na data X?")
- ✅ Eventos de domínio são significativos para o negócio
- ✅ Você quer derivar múltiplos modelos de leitura dos mesmos eventos

Pule quando:

- ❌ CRUD simples é suficiente
- ❌ Consistência forte é necessária em todo lugar
- ❌ A equipe é nova nesses padrões
- ❌ Você está construindo um protótipo

## Recursos

- [eventsource no GitHub](https://github.com/0xb0b1/eventsource) — O código completo
- [Meu post anterior sobre teoria CQRS/ES](/pt/posts/cqrs-event-sourcing-go-blog-post)
- [Go with the Domain - Three Dots Labs](https://threedots.tech/go-with-the-domain/) — Excelentes padrões DDD em Go
- [Martin Fowler sobre Event Sourcing](https://martinfowler.com/eaaDev/EventSourcing.html)
