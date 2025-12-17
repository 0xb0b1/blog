---
title: "Building an Event-Sourced Banking System in Go: From Theory to Production"
date: 2025-12-05
description: "A hands-on guide to implementing Event Sourcing and CQRS in Go, with a complete banking domain example including aggregates, projections, and snapshots."
tags: ["golang", "event-sourcing", "CQRS", "DDD", "architecture", "backend"]
---

I wrote about [CQRS and Event Sourcing patterns](/posts/cqrs-event-sourcing-go-blog-post) a while back, covering the theory and concepts. But theory only gets you so far. This time, I'm sharing something different: **a complete, working implementation** you can run, study, and extend.

The project is called [eventsource](https://github.com/0xb0b1/eventsource) — a banking system that demonstrates Event Sourcing and CQRS in action. Let's break down how it all fits together.

## Why a Banking System?

Banking is the perfect domain for Event Sourcing because:

1. **Audit trails are mandatory** — regulators want to know every transaction
2. **State changes are business events** — "money deposited" is more meaningful than "balance updated"
3. **Temporal queries matter** — "what was the balance on March 15th?"
4. **Consistency is critical** — you can't lose money to race conditions

Plus, everyone understands the domain. No need to explain what "deposit" or "withdraw" means.

## The Architecture

Here's the 10,000-foot view:

```
┌─────────────────────────────────────────────────────────────────────┐
│                           Client                                     │
└─────────────────────────────────────────────────────────────────────┘
                    │                           │
                    │ Commands                  │ Queries
                    ▼                           ▼
┌─────────────────────────────┐   ┌─────────────────────────────────┐
│      Command Handler        │   │         Query Service            │
│  (OpenAccount, Deposit,     │   │   (GetAccount, ListAccounts,    │
│   Withdraw, Transfer)       │   │    GetTransactions)              │
└─────────────────────────────┘   └─────────────────────────────────┘
           │                                    │
           ▼                                    │
┌─────────────────────────────┐                │
│     Aggregate Root          │                │
│     (BankAccount)           │                │
└─────────────────────────────┘                │
           │                                    │
           ▼                                    │
┌─────────────────────────────┐   ┌────────────┴────────────────────┐
│       Event Store           │──▶│         Projections             │
│   (Append-only log)         │   │  (AccountBalance, Transactions) │
└─────────────────────────────┘   └─────────────────────────────────┘
```

**Write side**: Commands → Aggregates → Events → Event Store

**Read side**: Events → Projections → Denormalized tables → Queries

Two different paths, optimized for their specific jobs.

## Part 1: The Event Store

Everything starts with the Event Store. It's an append-only log where every state change becomes an immutable event.

### The Interface

```go
type EventStore interface {
    // AppendEvents adds events with optimistic concurrency control
    AppendEvents(ctx context.Context, aggregateID string, expectedVersion int, events []StoredEvent) error

    // LoadEvents retrieves events for an aggregate
    LoadEvents(ctx context.Context, aggregateID string, fromVersion int) ([]StoredEvent, error)

    // LoadAllEvents for projections to catch up
    LoadAllEvents(ctx context.Context, fromPosition int64, limit int) ([]StoredEvent, error)

    // Snapshots for performance
    SaveSnapshot(ctx context.Context, snapshot Snapshot) error
    LoadSnapshot(ctx context.Context, aggregateID string) (*Snapshot, error)
}
```

### Optimistic Concurrency

The key detail is `expectedVersion`. When saving events, we check if the aggregate's current version matches what we expect:

```go
func (s *PostgresEventStore) AppendEvents(ctx context.Context, aggregateID string, expectedVersion int, events []StoredEvent) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // Check current version with row lock
    var currentVersion int
    err = tx.QueryRowContext(ctx, `
        SELECT COALESCE(MAX(version), 0) FROM events
        WHERE aggregate_id = $1 FOR UPDATE
    `, aggregateID).Scan(&currentVersion)

    if currentVersion != expectedVersion {
        return ErrConcurrencyConflict  // Someone else modified it!
    }

    // Insert events...
    return tx.Commit()
}
```

This prevents race conditions. If two processes try to modify the same account simultaneously, one will fail with a concurrency conflict. Exactly what we want in banking.

### The Schema

```sql
CREATE TABLE events (
    position BIGSERIAL PRIMARY KEY,
    id VARCHAR(36) NOT NULL UNIQUE,
    aggregate_id VARCHAR(36) NOT NULL,
    aggregate_type VARCHAR(255) NOT NULL,
    event_type VARCHAR(255) NOT NULL,
    version INTEGER NOT NULL,
    data JSONB NOT NULL,
    metadata JSONB,
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL,

    CONSTRAINT unique_aggregate_version UNIQUE (aggregate_id, version)
);
```

The `position` column is crucial — it gives us a global ordering across all events, which projections use to process events in order.

## Part 2: Domain Events

In Event Sourcing, events are the facts of your system. They describe what happened, not what the current state is.

```go
const (
    AccountOpened    = "AccountOpened"
    MoneyDeposited   = "MoneyDeposited"
    MoneyWithdrawn   = "MoneyWithdrawn"
    TransferSent     = "TransferSent"
    TransferReceived = "TransferReceived"
    AccountClosed    = "AccountClosed"
)

type MoneyDepositedData struct {
    Amount      decimal.Decimal `json:"amount"`
    Description string          `json:"description"`
}

type TransferSentData struct {
    ToAccountID string          `json:"to_account_id"`
    Amount      decimal.Decimal `json:"amount"`
    Description string          `json:"description"`
}
```

Notice the naming: past tense, describing something that **already happened**. Not `DepositMoney` (a command) but `MoneyDeposited` (a fact).

## Part 3: The Bank Account Aggregate

The aggregate is where business logic lives. It enforces invariants and produces events.

```go
type BankAccount struct {
    es.AggregateBase

    OwnerName string
    Balance   decimal.Decimal
    Status    AccountStatus
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### Business Operations

Each operation validates business rules and produces events:

```go
func (a *BankAccount) Withdraw(amount decimal.Decimal, description string) error {
    // Business rule: Can't withdraw from closed account
    if a.Status == AccountStatusClosed {
        return ErrAccountClosed
    }

    // Business rule: Amount must be positive
    if !amount.IsPositive() {
        return ErrInvalidAmount
    }

    // Business rule: Can't overdraft
    if a.Balance.LessThan(amount) {
        return ErrInsufficientFunds
    }

    // All rules pass — create event
    evt := event.NewMoneyWithdrawn(a.AggregateID(), a.Version()+1, amount, description)

    // Apply event to update state
    if err := a.ApplyEvent(evt); err != nil {
        return err
    }

    // Record for persistence
    a.RecordEvent(evt)
    return nil
}
```

This pattern — **validate, create event, apply, record** — is the heart of Event Sourcing.

### Event Application

The `ApplyEvent` method updates state based on events:

```go
func (a *BankAccount) ApplyEvent(e es.Event) error {
    eventData := e.(es.EventData)

    switch eventData.Type {
    case event.AccountOpened:
        data := eventData.Data.(event.AccountOpenedData)
        a.OwnerName = data.OwnerName
        a.Balance = data.InitialDeposit
        a.Status = AccountStatusActive

    case event.MoneyDeposited:
        data := eventData.Data.(event.MoneyDepositedData)
        a.Balance = a.Balance.Add(data.Amount)

    case event.MoneyWithdrawn:
        data := eventData.Data.(event.MoneyWithdrawnData)
        a.Balance = a.Balance.Sub(data.Amount)

    case event.AccountClosed:
        a.Status = AccountStatusClosed
    }

    a.SetVersion(e.Version())
    return nil
}
```

This same method is used both when executing commands AND when reconstructing state from stored events. One source of truth.

## Part 4: Loading Aggregates

When we need to work with an aggregate, we load it by replaying events:

```go
func (r *AccountRepository) Load(ctx context.Context, id string) (*BankAccount, error) {
    account := NewBankAccount(id)

    // Try snapshot first (optimization)
    snapshot, _ := r.eventStore.LoadSnapshot(ctx, id)
    fromVersion := 0

    if snapshot != nil {
        account.FromSnapshot(snapshot.Data)
        account.SetVersion(snapshot.Version)
        fromVersion = snapshot.Version
    }

    // Load events since snapshot (or all events if no snapshot)
    storedEvents, err := r.eventStore.LoadEvents(ctx, id, fromVersion)
    if err != nil {
        return nil, err
    }

    // Replay events to rebuild state
    for _, stored := range storedEvents {
        evt := deserializeEvent(stored)
        account.ApplyEvent(evt)
    }

    return account, nil
}
```

The aggregate's state is never stored directly — it's always derived from events. This is the core Event Sourcing principle.

## Part 5: Snapshots for Performance

If an account has 10,000 events, replaying them all on every request is slow. Snapshots solve this:

```go
type accountSnapshot struct {
    OwnerName string          `json:"owner_name"`
    Balance   decimal.Decimal `json:"balance"`
    Status    AccountStatus   `json:"status"`
    CreatedAt time.Time       `json:"created_at"`
}

func (a *BankAccount) ToSnapshot() ([]byte, error) {
    snap := accountSnapshot{
        OwnerName: a.OwnerName,
        Balance:   a.Balance,
        Status:    a.Status,
        CreatedAt: a.CreatedAt,
    }
    return json.Marshal(snap)
}
```

We save snapshots periodically (e.g., every 100 events):

```go
func (r *AccountRepository) Save(ctx context.Context, account *BankAccount) error {
    // ... save events ...

    // Snapshot every 100 events
    if account.Version() % 100 == 0 {
        r.saveSnapshot(ctx, account)
    }

    return nil
}
```

Now loading is fast: restore from snapshot, replay only recent events.

## Part 6: CQRS Projections

The write side optimizes for consistency. The read side optimizes for queries. Projections bridge them:

```go
type AccountBalanceProjection struct {
    db *sql.DB
}

func (p *AccountBalanceProjection) Handle(ctx context.Context, event StoredEvent) error {
    switch event.EventType {
    case "AccountOpened":
        var data AccountOpenedData
        json.Unmarshal(event.Data, &data)

        _, err := p.db.ExecContext(ctx, `
            INSERT INTO account_balances (account_id, owner_name, balance, status, created_at)
            VALUES ($1, $2, $3, 'active', $4)
        `, data.AccountID, data.OwnerName, data.InitialDeposit, event.Timestamp)
        return err

    case "MoneyDeposited":
        var data MoneyDepositedData
        json.Unmarshal(event.Data, &data)

        _, err := p.db.ExecContext(ctx, `
            UPDATE account_balances SET balance = balance + $1 WHERE account_id = $2
        `, data.Amount, event.AggregateID)
        return err

    // ... other events
    }
    return nil
}
```

The projection listens to events and maintains a denormalized table optimized for reads. No joins needed, no aggregate loading — just fast queries.

### The Projector

A background process feeds events to projections:

```go
func (p *Projector) processProjection(proj Projection) {
    checkpoint := p.getCheckpoint(proj.Name())

    events, err := p.eventStore.LoadAllEvents(p.ctx, checkpoint, 100)
    if err != nil {
        return
    }

    for _, event := range events {
        if err := proj.Handle(p.ctx, event); err != nil {
            log.Error("projection failed", "error", err)
            return
        }
    }

    // Update checkpoint
    p.setCheckpoint(proj.Name(), lastProcessedPosition)
}
```

The projector runs continuously, processing new events as they arrive. If it crashes, it resumes from the last checkpoint.

## Part 7: The Command Handler

Commands represent user intent. The handler orchestrates the flow:

```go
type Handler struct {
    accountRepo *AccountRepository
}

func (h *Handler) Handle(ctx context.Context, cmd Command) (*Result, error) {
    switch c := cmd.(type) {
    case Deposit:
        return h.handleDeposit(ctx, c)
    case Transfer:
        return h.handleTransfer(ctx, c)
    // ...
    }
}

func (h *Handler) handleDeposit(ctx context.Context, cmd Deposit) (*Result, error) {
    // Load aggregate
    account, err := h.accountRepo.Load(ctx, cmd.AccountID)
    if err != nil {
        return nil, err
    }

    // Execute business logic
    if err := account.Deposit(cmd.Amount, cmd.Description); err != nil {
        return nil, err
    }

    // Save (persists events)
    if err := h.accountRepo.Save(ctx, account); err != nil {
        return nil, err
    }

    return &Result{AggregateID: account.AggregateID()}, nil
}
```

The handler is thin — it loads, delegates to the aggregate, and saves. Business logic stays in the domain.

## Part 8: The API

The REST API exposes commands (write) and queries (read):

```go
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
    // Commands (Write side)
    mux.HandleFunc("POST /api/v1/accounts", h.OpenAccount)
    mux.HandleFunc("POST /api/v1/accounts/{id}/deposit", h.Deposit)
    mux.HandleFunc("POST /api/v1/accounts/{id}/withdraw", h.Withdraw)
    mux.HandleFunc("POST /api/v1/accounts/{id}/transfer", h.Transfer)

    // Queries (Read side)
    mux.HandleFunc("GET /api/v1/accounts", h.ListAccounts)
    mux.HandleFunc("GET /api/v1/accounts/{id}", h.GetAccount)
    mux.HandleFunc("GET /api/v1/accounts/{id}/transactions", h.GetTransactions)
    mux.HandleFunc("GET /api/v1/accounts/{id}/events", h.GetAccountEvents)
}
```

Notice the last endpoint — `GetAccountEvents`. This is the superpower of Event Sourcing. You can see exactly how an account got to its current state.

## Running the Project

```bash
# Clone it
git clone https://github.com/0xb0b1/eventsource
cd eventsource

# Start PostgreSQL
docker-compose up -d postgres

# Run migrations
make dev-db

# Start the server
make dev
```

### Try It Out

```bash
# Open an account
curl -X POST http://localhost:8080/api/v1/accounts \
  -H "Content-Type: application/json" \
  -d '{"owner_name": "John Doe", "initial_deposit": "1000.00"}'

# Response: {"account_id": "550e8400-e29b-41d4-a716-446655440000"}

# Deposit money
curl -X POST http://localhost:8080/api/v1/accounts/{id}/deposit \
  -d '{"amount": "500.00", "description": "Salary"}'

# Check balance (from read model)
curl http://localhost:8080/api/v1/accounts/{id}

# See all events (the audit trail)
curl http://localhost:8080/api/v1/accounts/{id}/events
```

That last call shows you every event that ever happened to the account. That's Event Sourcing in action.

## Key Takeaways

Building this project reinforced some important lessons:

1. **Events are facts, not commands** — Name them in past tense. They describe what happened, not what you want to happen.

2. **Aggregates enforce invariants** — Business rules live in the domain, not in handlers or services.

3. **Projections are disposable** — If your read model is wrong, rebuild it from events. This is liberating.

4. **Snapshots are optimization** — They're not required for correctness, just performance.

5. **Eventual consistency is a feature** — Your read models might lag by milliseconds. Design for it.

6. **The event log is your audit trail** — Free compliance! Every state change is recorded automatically.

## When to Use This Pattern

Event Sourcing + CQRS is powerful but adds complexity. Use it when:

- ✅ You need audit trails
- ✅ Read and write patterns diverge significantly
- ✅ You need time-travel queries ("what was the state on date X?")
- ✅ Domain events are meaningful to the business
- ✅ You want to derive multiple read models from the same events

Skip it when:

- ❌ Simple CRUD is enough
- ❌ Strong consistency is required everywhere
- ❌ The team is new to these patterns
- ❌ You're building a prototype

## What's Next?

The [eventsource repo](https://github.com/0xb0b1/eventsource) is a starting point. Some ideas for extension:

- Add event versioning and schema evolution
- Implement saga pattern for complex transfers
- Add WebSocket support for real-time balance updates
- Build a dashboard to visualize event streams
- Add Kafka/NATS for event distribution to other services

The beauty of Event Sourcing is that you can always add new projections later. Your events are your source of truth — everything else can be derived.

## Resources

- [eventsource on GitHub](https://github.com/0xb0b1/eventsource) — The complete code
- [My previous post on CQRS/ES theory](/posts/cqrs-event-sourcing-go-blog-post)
- [Go with the Domain - Three Dots Labs](https://threedots.tech/go-with-the-domain/) — Excellent DDD patterns in Go
- [Martin Fowler on Event Sourcing](https://martinfowler.com/eaaDev/EventSourcing.html)
