---
title: "CQRS e Event Sourcing em Go"
date: 2025-11-03
description: "Entendendo os padrões CQRS e Event Sourcing e como implementá-los em Go para sistemas escaláveis e auditáveis."
tags: ["golang", "cqrs", "event-sourcing", "arquitetura", "ddd"]
---

CQRS (Command Query Responsibility Segregation) e Event Sourcing são padrões arquiteturais poderosos que, quando combinados, permitem construir sistemas altamente escaláveis, auditáveis e flexíveis. Vamos explorar esses conceitos e ver como implementá-los em Go.

## O Que é CQRS?

CQRS separa as operações de leitura (Query) das operações de escrita (Command):

```
Abordagem Tradicional:
┌─────────────────────────────────────────┐
│              Modelo Único               │
│                                         │
│    Create  Read  Update  Delete         │
│      ↓       ↓      ↓       ↓           │
│    ┌───────────────────────────┐        │
│    │       Banco de Dados      │        │
│    └───────────────────────────┘        │
└─────────────────────────────────────────┘

CQRS:
┌─────────────────────────────────────────┐
│                 CQRS                     │
│                                         │
│   Commands              Queries         │
│   (Create, Update,      (Read)          │
│    Delete)                              │
│      ↓                    ↓             │
│ ┌──────────┐        ┌──────────┐        │
│ │  Write   │───────→│   Read   │        │
│ │  Model   │  Sync  │  Model   │        │
│ └──────────┘        └──────────┘        │
└─────────────────────────────────────────┘
```

## O Que é Event Sourcing?

Em vez de armazenar o estado atual, Event Sourcing armazena a sequência de eventos que levaram a esse estado:

```
Abordagem Tradicional:
┌───────────────────────────────────────┐
│ Conta: { id: 1, saldo: 1000 }        │
└───────────────────────────────────────┘

Event Sourcing:
┌───────────────────────────────────────┐
│ Evento 1: ContaCriada { id: 1 }       │
│ Evento 2: DepositoRealizado { 500 }   │
│ Evento 3: DepositoRealizado { 700 }   │
│ Evento 4: SaqueRealizado { 200 }      │
│                                       │
│ Estado Atual = replay de eventos      │
│ 0 + 500 + 700 - 200 = 1000           │
└───────────────────────────────────────┘
```

## Por Que Usar Esses Padrões?

### Benefícios do Event Sourcing

1. **Auditoria Completa**: Histórico completo de todas as mudanças
2. **Time Travel**: Reconstruir estado em qualquer ponto no tempo
3. **Debug**: Entender exatamente como o sistema chegou ao estado atual
4. **Replay de Eventos**: Corrigir bugs e re-processar dados
5. **Múltiplas Projeções**: Diferentes visões dos mesmos dados

### Benefícios do CQRS

1. **Escalabilidade Independente**: Escalar leituras e escritas separadamente
2. **Otimização**: Modelos otimizados para cada operação
3. **Simplicidade**: Cada lado é mais simples que um modelo único

## Implementação em Go

### Event Store

```go
// domain/events.go
package domain

import "time"

type Event interface {
    EventType() string
    AggregateID() string
    Version() int
    Timestamp() time.Time
}

type BaseEvent struct {
    ID          string
    AggregateId string
    Ver         int
    Type        string
    OccurredAt  time.Time
}

func (e BaseEvent) EventType() string    { return e.Type }
func (e BaseEvent) AggregateID() string  { return e.AggregateId }
func (e BaseEvent) Version() int         { return e.Ver }
func (e BaseEvent) Timestamp() time.Time { return e.OccurredAt }

// Eventos específicos
type ContaCriada struct {
    BaseEvent
    NomeTitular string
    SaldoInicial float64
}

type DepositoRealizado struct {
    BaseEvent
    Valor     float64
    Descricao string
}

type SaqueRealizado struct {
    BaseEvent
    Valor     float64
    Descricao string
}
```

### Event Store Interface

```go
// eventstore/store.go
package eventstore

import (
    "context"
    "myapp/domain"
)

type EventStore interface {
    // Append adiciona eventos com controle de concorrência otimista
    Append(ctx context.Context, aggregateID string, expectedVersion int, events []domain.Event) error

    // Load carrega eventos de um agregado
    Load(ctx context.Context, aggregateID string, fromVersion int) ([]domain.Event, error)

    // LoadAll carrega todos os eventos (para projeções)
    LoadAll(ctx context.Context, fromPosition int64, limit int) ([]domain.Event, error)
}
```

### Implementação PostgreSQL

```go
// eventstore/postgres.go
package eventstore

import (
    "context"
    "database/sql"
    "encoding/json"

    "myapp/domain"
)

type PostgresEventStore struct {
    db *sql.DB
}

func (s *PostgresEventStore) Append(ctx context.Context, aggregateID string, expectedVersion int, events []domain.Event) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // Verificar versão atual (controle de concorrência)
    var currentVersion int
    err = tx.QueryRowContext(ctx, `
        SELECT COALESCE(MAX(version), 0) FROM events
        WHERE aggregate_id = $1 FOR UPDATE
    `, aggregateID).Scan(&currentVersion)

    if currentVersion != expectedVersion {
        return ErrConcurrencyConflict
    }

    // Inserir eventos
    for i, event := range events {
        data, _ := json.Marshal(event)

        _, err = tx.ExecContext(ctx, `
            INSERT INTO events (aggregate_id, version, event_type, data, timestamp)
            VALUES ($1, $2, $3, $4, $5)
        `, aggregateID, expectedVersion+i+1, event.EventType(), data, event.Timestamp())

        if err != nil {
            return err
        }
    }

    return tx.Commit()
}

func (s *PostgresEventStore) Load(ctx context.Context, aggregateID string, fromVersion int) ([]domain.Event, error) {
    rows, err := s.db.QueryContext(ctx, `
        SELECT event_type, data FROM events
        WHERE aggregate_id = $1 AND version > $2
        ORDER BY version
    `, aggregateID, fromVersion)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var events []domain.Event
    for rows.Next() {
        var eventType string
        var data []byte

        if err := rows.Scan(&eventType, &data); err != nil {
            return nil, err
        }

        event := deserializeEvent(eventType, data)
        events = append(events, event)
    }

    return events, nil
}
```

### Agregado (Write Model)

```go
// domain/conta.go
package domain

import "errors"

type Conta struct {
    id      string
    titular string
    saldo   float64
    versao  int
    eventos []Event
}

func NovaConta(id, titular string, saldoInicial float64) (*Conta, error) {
    if saldoInicial < 0 {
        return nil, errors.New("saldo inicial não pode ser negativo")
    }

    conta := &Conta{}
    conta.aplicar(ContaCriada{
        BaseEvent: BaseEvent{AggregateId: id, Type: "ContaCriada"},
        NomeTitular:  titular,
        SaldoInicial: saldoInicial,
    })

    return conta, nil
}

func (c *Conta) Depositar(valor float64, descricao string) error {
    if valor <= 0 {
        return errors.New("valor deve ser positivo")
    }

    c.aplicar(DepositoRealizado{
        BaseEvent: BaseEvent{AggregateId: c.id, Type: "DepositoRealizado"},
        Valor:     valor,
        Descricao: descricao,
    })

    return nil
}

func (c *Conta) Sacar(valor float64, descricao string) error {
    if valor <= 0 {
        return errors.New("valor deve ser positivo")
    }
    if c.saldo < valor {
        return errors.New("saldo insuficiente")
    }

    c.aplicar(SaqueRealizado{
        BaseEvent: BaseEvent{AggregateId: c.id, Type: "SaqueRealizado"},
        Valor:     valor,
        Descricao: descricao,
    })

    return nil
}

func (c *Conta) aplicar(evento Event) {
    c.aplicarEvento(evento)
    c.eventos = append(c.eventos, evento)
}

func (c *Conta) aplicarEvento(evento Event) {
    switch e := evento.(type) {
    case ContaCriada:
        c.id = e.AggregateId
        c.titular = e.NomeTitular
        c.saldo = e.SaldoInicial
    case DepositoRealizado:
        c.saldo += e.Valor
    case SaqueRealizado:
        c.saldo -= e.Valor
    }
    c.versao++
}

// CarregarDeEventos reconstrói o agregado a partir de eventos
func CarregarContaDeEventos(eventos []Event) *Conta {
    conta := &Conta{}
    for _, evento := range eventos {
        conta.aplicarEvento(evento)
    }
    return conta
}
```

### Command Handler

```go
// usecase/conta_commands.go
package usecase

import (
    "context"

    "myapp/domain"
    "myapp/eventstore"
)

type ContaCommandHandler struct {
    store eventstore.EventStore
}

type DepositarCommand struct {
    ContaID   string
    Valor     float64
    Descricao string
}

func (h *ContaCommandHandler) Depositar(ctx context.Context, cmd DepositarCommand) error {
    // 1. Carregar eventos
    eventos, err := h.store.Load(ctx, cmd.ContaID, 0)
    if err != nil {
        return err
    }

    // 2. Reconstruir agregado
    conta := domain.CarregarContaDeEventos(eventos)

    // 3. Executar comando (gera novos eventos)
    if err := conta.Depositar(cmd.Valor, cmd.Descricao); err != nil {
        return err
    }

    // 4. Persistir novos eventos
    return h.store.Append(ctx, cmd.ContaID, conta.Versao(), conta.EventosPendentes())
}
```

### Projeções (Read Model)

```go
// projections/saldo_projection.go
package projections

import (
    "context"
    "database/sql"

    "myapp/domain"
)

type SaldoProjection struct {
    db *sql.DB
}

func (p *SaldoProjection) Handle(ctx context.Context, evento domain.Event) error {
    switch e := evento.(type) {
    case domain.ContaCriada:
        _, err := p.db.ExecContext(ctx, `
            INSERT INTO conta_saldos (conta_id, titular, saldo)
            VALUES ($1, $2, $3)
        `, e.AggregateID(), e.NomeTitular, e.SaldoInicial)
        return err

    case domain.DepositoRealizado:
        _, err := p.db.ExecContext(ctx, `
            UPDATE conta_saldos SET saldo = saldo + $1 WHERE conta_id = $2
        `, e.Valor, e.AggregateID())
        return err

    case domain.SaqueRealizado:
        _, err := p.db.ExecContext(ctx, `
            UPDATE conta_saldos SET saldo = saldo - $1 WHERE conta_id = $2
        `, e.Valor, e.AggregateID())
        return err
    }

    return nil
}
```

### Query Service

```go
// query/conta_queries.go
package query

import (
    "context"
    "database/sql"
)

type ContaQueryService struct {
    db *sql.DB
}

type ContaSaldo struct {
    ContaID string
    Titular string
    Saldo   float64
}

func (s *ContaQueryService) ObterSaldo(ctx context.Context, contaID string) (*ContaSaldo, error) {
    var saldo ContaSaldo
    err := s.db.QueryRowContext(ctx, `
        SELECT conta_id, titular, saldo FROM conta_saldos WHERE conta_id = $1
    `, contaID).Scan(&saldo.ContaID, &saldo.Titular, &saldo.Saldo)

    if err == sql.ErrNoRows {
        return nil, nil
    }
    return &saldo, err
}

func (s *ContaQueryService) ListarContas(ctx context.Context) ([]ContaSaldo, error) {
    rows, err := s.db.QueryContext(ctx, `
        SELECT conta_id, titular, saldo FROM conta_saldos ORDER BY titular
    `)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var contas []ContaSaldo
    for rows.Next() {
        var c ContaSaldo
        rows.Scan(&c.ContaID, &c.Titular, &c.Saldo)
        contas = append(contas, c)
    }
    return contas, nil
}
```

## Quando Usar

### Use CQRS + Event Sourcing Quando:

- ✅ Você precisa de auditoria completa
- ✅ Requisitos de leitura e escrita são muito diferentes
- ✅ Você precisa de múltiplas visões dos mesmos dados
- ✅ Eventos são significativos para o negócio
- ✅ Time-travel queries são necessárias

### Não Use Quando:

- ❌ CRUD simples é suficiente
- ❌ Consistência forte é obrigatória em todo lugar
- ❌ A equipe não tem experiência com esses padrões
- ❌ É um projeto pequeno ou protótipo

## Conclusão

CQRS e Event Sourcing são ferramentas poderosas, mas adicionam complexidade. Use-os quando os benefícios claramente superam os custos. Para muitas aplicações, um modelo tradicional bem estruturado é mais apropriado.

Quando usados corretamente, esses padrões permitem construir sistemas que são não apenas escaláveis, mas também compreensíveis e auditáveis - características valiosas em sistemas de missão crítica.
