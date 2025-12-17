---
title: "CQRS and Event Sourcing in Go: Practical Patterns from the Trenches"
date: 2025-11-03
description: "Building scalable systems often means accepting that the traditional CRUD approach won't cut it. When audit requirements get strict, when read and write patterns diverge, or when you need to answer "
tags:
  [
    "golang",
    "architecture",
    "clean-architecture",
    "backend",
    "CQRS",
    "event-sourcing",
    "DDD",
    "system-design",
  ]
---

Building scalable systems often means accepting that the traditional CRUD approach won't cut it. When audit requirements get strict, when read and write patterns diverge, or when you need to answer "how did we get to this state?"—that's when CQRS and Event Sourcing become your best friends.

After implementing these patterns across multiple production systems and diving deep into Three Dots Labs' excellent "Go with the Domain" book, I've learned that Go's explicit nature makes it surprisingly well-suited for these architectural patterns. Let me share what actually works.

## Why CQRS and Event Sourcing Matter

Before diving into code, let's be clear about the problems these patterns solve:

**CQRS (Command Query Responsibility Segregation)** separates your write model from your read model. Your commands focus on business logic and consistency, while your queries optimize for fast, denormalized reads. No more trying to serve both masters with one model.

**Event Sourcing** stores your state as a sequence of events rather than current state snapshots. Every change becomes an immutable fact—perfect for audit trails, debugging production issues, and even time-travel debugging.

As the Three Dots Labs team puts it in their book: "Implementation details are often just details. Get the domain code right." This philosophy drives everything that follows.

## The Command Side: Where Business Logic Lives

Let's start with a practical example from a payment processing system. Here's how we structure commands:

```go
package command

import (
    "context"
    "fmt"
    "time"

    "github.com/google/uuid"
)

// ProcessPayment represents the intent to process a payment
type ProcessPayment struct {
    PaymentID   uuid.UUID
    Amount      Money
    Method      PaymentMethod
    CustomerID  string
    IdempotencyKey string
}

// ProcessPaymentHandler handles the business logic for payment processing
type ProcessPaymentHandler struct {
    repo      PaymentRepository
    eventBus  EventBus
    riskEngine RiskAssessment
}

func (h *ProcessPaymentHandler) Handle(ctx context.Context, cmd ProcessPayment) error {
    // Load aggregate (or create if new)
    payment, err := h.repo.GetByID(ctx, cmd.PaymentID)
    if err != nil {
        payment = NewPayment(cmd.PaymentID, cmd.CustomerID)
    }

    // Check idempotency
    if payment.HasProcessedIdempotencyKey(cmd.IdempotencyKey) {
        return nil // Already processed
    }

    // Business logic lives in the domain
    riskScore := h.riskEngine.Assess(cmd.CustomerID, cmd.Amount)

    events, err := payment.Process(
        cmd.Amount,
        cmd.Method,
        riskScore,
        cmd.IdempotencyKey,
    )
    if err != nil {
        return fmt.Errorf("processing payment: %w", err)
    }

    // Save events
    if err := h.repo.Save(ctx, payment); err != nil {
        return fmt.Errorf("saving payment: %w", err)
    }

    // Publish for read model updates and integration
    for _, event := range events {
        if err := h.eventBus.Publish(ctx, event); err != nil {
            // Log but don't fail - events are persisted
            log.Printf("failed to publish event: %v", err)
        }
    }

    return nil
}
```

The beauty here? The handler orchestrates, but the domain makes decisions. This separation becomes crucial as complexity grows.

## The Domain Layer: Where Events Are Born

Following Domain-Driven Design principles from "Go with the Domain," our aggregates produce events as the result of business operations:

```go
package payment

import (
    "fmt"
    "time"

    "github.com/google/uuid"
)

// Payment aggregate root
type Payment struct {
    id              uuid.UUID
    customerID      string
    status          PaymentStatus
    amount          Money
    method          PaymentMethod
    processedKeys   map[string]bool

    // Event sourcing
    version         int
    pendingEvents   []Event
    appliedEvents   []Event
}

// Process applies business rules and generates events
func (p *Payment) Process(
    amount Money,
    method PaymentMethod,
    riskScore RiskScore,
    idempotencyKey string,
) ([]Event, error) {
    // Guard against invalid state transitions
    if p.status == PaymentStatusCompleted {
        return nil, ErrPaymentAlreadyCompleted
    }

    if p.status == PaymentStatusFailed {
        return nil, ErrPaymentAlreadyFailed
    }

    // Check idempotency
    if p.processedKeys[idempotencyKey] {
        return nil, nil // Already processed
    }

    // Business rule: High risk payments need review
    if riskScore.IsHigh() && amount.GreaterThan(NewMoney(10000, "USD")) {
        event := PaymentFlaggedForReview{
            PaymentID:  p.id,
            Amount:     amount,
            RiskScore:  riskScore,
            FlaggedAt:  time.Now(),
        }
        p.apply(event)
        return []Event{event}, nil
    }

    // Business rule: Different limits per method
    if err := method.ValidateLimit(amount); err != nil {
        event := PaymentFailed{
            PaymentID: p.id,
            Reason:    err.Error(),
            FailedAt:  time.Now(),
        }
        p.apply(event)
        return []Event{event}, nil
    }

    // Process payment
    event := PaymentProcessed{
        PaymentID:      p.id,
        Amount:         amount,
        Method:         method,
        ProcessedAt:    time.Now(),
        IdempotencyKey: idempotencyKey,
    }

    p.apply(event)
    return []Event{event}, nil
}

// apply updates the aggregate state from an event
func (p *Payment) apply(event Event) {
    switch e := event.(type) {
    case PaymentProcessed:
        p.status = PaymentStatusCompleted
        p.amount = e.Amount
        p.method = e.Method
        p.processedKeys[e.IdempotencyKey] = true

    case PaymentFailed:
        p.status = PaymentStatusFailed

    case PaymentFlaggedForReview:
        p.status = PaymentStatusPendingReview
    }

    p.pendingEvents = append(p.pendingEvents, event)
    p.version++
}

// Reconstitute rebuilds aggregate from events (Event Sourcing)
func (p *Payment) Reconstitute(events []Event) {
    for _, event := range events {
        p.apply(event)
        p.appliedEvents = append(p.appliedEvents, event)
    }
    p.pendingEvents = nil // Clear pending after reconstitution
}
```

This pattern, heavily inspired by Three Dots Labs' approach, keeps business logic in the domain while maintaining a clear event trail.

## Event Storage: The Source of Truth

With Event Sourcing, your events become the source of truth. Here's a production-ready event store implementation:

```go
package eventstore

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "time"

    "github.com/google/uuid"
)

type PostgresEventStore struct {
    db *sql.DB
}

type StoredEvent struct {
    ID            uuid.UUID
    AggregateID   uuid.UUID
    AggregateType string
    EventType     string
    EventData     json.RawMessage
    EventVersion  int
    Metadata      map[string]string
    Timestamp     time.Time
}

func (s *PostgresEventStore) SaveEvents(
    ctx context.Context,
    aggregateID uuid.UUID,
    events []Event,
    expectedVersion int,
) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // Optimistic concurrency control
    var currentVersion int
    err = tx.QueryRowContext(ctx, `
        SELECT COALESCE(MAX(event_version), 0)
        FROM events
        WHERE aggregate_id = $1
    `, aggregateID).Scan(&currentVersion)

    if err != nil {
        return err
    }

    if currentVersion != expectedVersion {
        return ErrConcurrentModification
    }

    // Store each event
    for i, event := range events {
        eventData, err := json.Marshal(event)
        if err != nil {
            return err
        }

        _, err = tx.ExecContext(ctx, `
            INSERT INTO events (
                id, aggregate_id, aggregate_type,
                event_type, event_data, event_version,
                metadata, timestamp
            ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        `,
            uuid.New(),
            aggregateID,
            event.AggregateType(),
            event.EventType(),
            eventData,
            currentVersion + i + 1,
            event.Metadata(),
            event.Timestamp(),
        )

        if err != nil {
            return err
        }
    }

    // Update snapshot for read optimization (optional)
    if err := s.updateSnapshot(ctx, tx, aggregateID); err != nil {
        return err
    }

    return tx.Commit()
}

func (s *PostgresEventStore) GetEvents(
    ctx context.Context,
    aggregateID uuid.UUID,
    fromVersion int,
) ([]StoredEvent, error) {
    rows, err := s.db.QueryContext(ctx, `
        SELECT id, aggregate_id, aggregate_type,
               event_type, event_data, event_version,
               metadata, timestamp
        FROM events
        WHERE aggregate_id = $1 AND event_version > $2
        ORDER BY event_version ASC
    `, aggregateID, fromVersion)

    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var events []StoredEvent
    for rows.Next() {
        var event StoredEvent
        var metadata sql.NullString

        err := rows.Scan(
            &event.ID,
            &event.AggregateID,
            &event.AggregateType,
            &event.EventType,
            &event.EventData,
            &event.EventVersion,
            &metadata,
            &event.Timestamp,
        )

        if err != nil {
            return nil, err
        }

        if metadata.Valid {
            json.Unmarshal([]byte(metadata.String), &event.Metadata)
        }

        events = append(events, event)
    }

    return events, nil
}
```

## The Query Side: Optimized for Reads

Now for the query side. As Three Dots Labs demonstrates, your read models can be completely different from your write models:

```go
package query

import (
    "context"
    "database/sql"
    "time"
)

// PaymentSummaryView is optimized for dashboard queries
type PaymentSummaryView struct {
    PaymentID       string    `json:"payment_id"`
    CustomerID      string    `json:"customer_id"`
    CustomerName    string    `json:"customer_name"`
    Amount          float64   `json:"amount"`
    Currency        string    `json:"currency"`
    Status          string    `json:"status"`
    Method          string    `json:"method"`
    ProcessedAt     time.Time `json:"processed_at"`
    RiskScore       int       `json:"risk_score"`
    RequiresReview  bool      `json:"requires_review"`
}

type PaymentQueryService struct {
    db *sql.DB
}

func (s *PaymentQueryService) GetCustomerPayments(
    ctx context.Context,
    customerID string,
    filter PaymentFilter,
) ([]PaymentSummaryView, error) {
    query := `
        SELECT
            p.payment_id,
            p.customer_id,
            c.name as customer_name,
            p.amount,
            p.currency,
            p.status,
            p.method,
            p.processed_at,
            p.risk_score,
            p.requires_review
        FROM payment_summary p
        JOIN customers c ON c.id = p.customer_id
        WHERE p.customer_id = $1
            AND p.processed_at BETWEEN $2 AND $3
        ORDER BY p.processed_at DESC
        LIMIT $4 OFFSET $5
    `

    rows, err := s.db.QueryContext(
        ctx, query,
        customerID,
        filter.From,
        filter.To,
        filter.Limit,
        filter.Offset,
    )

    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var payments []PaymentSummaryView
    for rows.Next() {
        var payment PaymentSummaryView
        err := rows.Scan(
            &payment.PaymentID,
            &payment.CustomerID,
            &payment.CustomerName,
            &payment.Amount,
            &payment.Currency,
            &payment.Status,
            &payment.Method,
            &payment.ProcessedAt,
            &payment.RiskScore,
            &payment.RequiresReview,
        )
        if err != nil {
            return nil, err
        }
        payments = append(payments, payment)
    }

    return payments, nil
}

// GetDailyStatistics provides aggregated views
func (s *PaymentQueryService) GetDailyStatistics(
    ctx context.Context,
    date time.Time,
) (*DailyStats, error) {
    // This query would be expensive with Event Sourcing alone
    // But with CQRS, we can maintain optimized read models
    query := `
        SELECT
            COUNT(*) as total_transactions,
            SUM(amount) as total_volume,
            AVG(amount) as average_amount,
            COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed_count,
            COUNT(CASE WHEN requires_review THEN 1 END) as review_count
        FROM payment_summary
        WHERE DATE(processed_at) = DATE($1)
    `

    var stats DailyStats
    err := s.db.QueryRowContext(ctx, query, date).Scan(
        &stats.TotalTransactions,
        &stats.TotalVolume,
        &stats.AverageAmount,
        &stats.FailedCount,
        &stats.ReviewCount,
    )

    return &stats, err
}
```

## Projections: Keeping Read Models in Sync

The glue between commands and queries? Projections that listen to events and update read models:

```go
package projection

import (
    "context"
    "database/sql"
    "encoding/json"
    "log"
)

type PaymentProjection struct {
    db          *sql.DB
    customerSvc CustomerService
}

func (p *PaymentProjection) Handle(ctx context.Context, event Event) error {
    switch e := event.(type) {
    case PaymentProcessed:
        return p.handlePaymentProcessed(ctx, e)
    case PaymentFailed:
        return p.handlePaymentFailed(ctx, e)
    case PaymentFlaggedForReview:
        return p.handlePaymentFlagged(ctx, e)
    default:
        log.Printf("Unknown event type: %T", e)
        return nil
    }
}

func (p *PaymentProjection) handlePaymentProcessed(
    ctx context.Context,
    event PaymentProcessed,
) error {
    // Enrich with customer data
    customer, err := p.customerSvc.GetByID(ctx, event.CustomerID)
    if err != nil {
        return err
    }

    // Update denormalized read model
    _, err = p.db.ExecContext(ctx, `
        INSERT INTO payment_summary (
            payment_id, customer_id, customer_name,
            amount, currency, status, method,
            processed_at, risk_score, requires_review
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
        ON CONFLICT (payment_id) DO UPDATE SET
            status = EXCLUDED.status,
            processed_at = EXCLUDED.processed_at
    `,
        event.PaymentID,
        event.CustomerID,
        customer.Name,
        event.Amount.Value,
        event.Amount.Currency,
        "completed",
        event.Method,
        event.ProcessedAt,
        event.RiskScore,
        false,
    )

    return err
}

func (p *PaymentProjection) handlePaymentFlagged(
    ctx context.Context,
    event PaymentFlaggedForReview,
) error {
    _, err := p.db.ExecContext(ctx, `
        UPDATE payment_summary
        SET requires_review = true,
            status = 'pending_review',
            risk_score = $2
        WHERE payment_id = $1
    `, event.PaymentID, event.RiskScore)

    return err
}
```

## Eventual Consistency: Embracing the Reality

One of the key insights from "Go with the Domain" is that eventual consistency isn't a bug—it's a feature. Your projections might lag behind by milliseconds or seconds, but that's fine for most use cases:

```go
// Anti-pattern: Trying to read immediately after write
payment := ProcessPayment(cmd)
summary := GetPaymentSummary(payment.ID) // Might not exist yet!

// Better: Return write model data for immediate feedback
payment := ProcessPayment(cmd)
return PaymentProcessedResponse{
    ID:     payment.ID,
    Status: payment.Status,
    // Don't query read model immediately
}

// Best: Design UI for eventual consistency
// Show "Processing..." state and poll or use websockets
```

## Testing Event-Sourced Systems

Testing becomes surprisingly elegant with Event Sourcing:

```go
func TestPaymentProcessing(t *testing.T) {
    // Given: Previous events
    payment := NewPayment(uuid.New(), "customer-123")
    payment.Reconstitute([]Event{
        PaymentInitiated{
            PaymentID:  payment.ID,
            CustomerID: "customer-123",
            Amount:     NewMoney(100, "USD"),
        },
    })

    // When: Business operation
    events, err := payment.Process(
        NewMoney(100, "USD"),
        CreditCard,
        LowRisk,
        "idempotency-key-456",
    )

    // Then: Verify events
    require.NoError(t, err)
    require.Len(t, events, 1)

    processed, ok := events[0].(PaymentProcessed)
    require.True(t, ok)
    assert.Equal(t, NewMoney(100, "USD"), processed.Amount)
    assert.Equal(t, CreditCard, processed.Method)
}

// Test projections independently
func TestPaymentProjection(t *testing.T) {
    db := setupTestDB(t)
    projection := NewPaymentProjection(db, mockCustomerService())

    // Apply event
    err := projection.Handle(context.Background(), PaymentProcessed{
        PaymentID:   uuid.New(),
        CustomerID:  "customer-123",
        Amount:      NewMoney(100, "USD"),
        ProcessedAt: time.Now(),
    })

    require.NoError(t, err)

    // Verify read model updated
    var count int
    err = db.QueryRow(
        "SELECT COUNT(*) FROM payment_summary WHERE customer_id = $1",
        "customer-123",
    ).Scan(&count)

    assert.Equal(t, 1, count)
}
```

## Production Considerations

After running CQRS + Event Sourcing in production, here are the critical lessons:

### 1. Snapshot Strategy

Don't replay thousands of events on every load. Implement snapshots:

```go
func (s *EventStore) LoadAggregate(
    ctx context.Context,
    id uuid.UUID,
) (*Payment, error) {
    // Try loading from snapshot first
    snapshot, err := s.loadSnapshot(ctx, id)
    if err == nil {
        // Load events after snapshot
        events, err := s.GetEvents(ctx, id, snapshot.Version)
        if err != nil {
            return nil, err
        }

        payment := snapshot.ToAggregate()
        payment.Reconstitute(events)
        return payment, nil
    }

    // No snapshot, load all events
    events, err := s.GetEvents(ctx, id, 0)
    if err != nil {
        return nil, err
    }

    payment := NewPayment(id, "")
    payment.Reconstitute(events)

    // Create snapshot if many events
    if len(events) > 100 {
        s.saveSnapshot(ctx, payment)
    }

    return payment, nil
}
```

### 2. Event Schema Evolution

Events are immutable, but requirements change. Plan for it:

```go
type EventUpgrader struct {
    upgraders map[string]func(json.RawMessage) (Event, error)
}

func (u *EventUpgrader) Upgrade(eventType string, data json.RawMessage) (Event, error) {
    switch eventType {
    case "PaymentProcessedV1":
        // Convert old format to new
        var v1 PaymentProcessedV1
        json.Unmarshal(data, &v1)
        return PaymentProcessed{
            PaymentID: v1.ID, // Map old field names
            Amount:    NewMoney(v1.Amount, "USD"), // Add defaults
        }, nil
    default:
        // Current version
        return u.unmarshal(eventType, data)
    }
}
```

### 3. Monitoring and Observability

Track projection lag and event processing:

```go
func (p *ProjectionRunner) Run(ctx context.Context) {
    for {
        select {
        case event := <-p.events:
            start := time.Now()

            err := p.projection.Handle(ctx, event)

            p.metrics.RecordProjection(
                event.EventType(),
                time.Since(start),
                err == nil,
            )

            if err != nil {
                p.handleError(event, err)
            }

        case <-ctx.Done():
            return
        }
    }
}
```

## When to Use (and When Not To)

Three Dots Labs makes an excellent point in their book: these patterns aren't silver bullets. Use them when:

✅ **You need audit trails** - Every state change is recorded <br/> 
✅ **Complex domain logic** - Commands and domain events model it naturally   <br/> 
✅ **Read/write patterns diverge** - One size doesn't fit all <br/> 
✅ **Time travel debugging** - Replay events to any point in time <br/> 
✅ **Multiple read models** - Same events, different projections<br/>

Avoid them when:

❌ **Simple CRUD** - Don't overcomplicate <br/> 
❌ **Strong consistency required** - Eventual consistency is the default <br/> 
❌ **Team lacks experience** - Start with simpler patterns <br/> 
❌ **Low event volume** - The overhead might not be worth it <br/> 

## Conclusion

CQRS and Event Sourcing in Go provide a powerful foundation for complex business domains. The patterns force you to think about your domain, separate concerns clearly, and build systems that tell the story of how they got to their current state.

Three Dots Labs' "Go with the Domain" book brilliantly demonstrates these patterns using real, production-ready Go code. Their approach of showing the refactoring journey—from typical Go application to full CQRS + Event Sourcing—makes the concepts tangible and practical.

The key insight? Start with the domain. When you model behaviors instead of data structures, when you think in events rather than state mutations, the implementation flows naturally. Go's simplicity and explicitness make it an excellent choice for these patterns—no framework magic, just clear, testable code.

Remember: CQRS and Event Sourcing are tools. Use them when they solve real problems, not because they're trendy. But when you do need them, Go and the patterns from "Go with the Domain" will serve you well.

## Resources

- [Go with the Domain - Three Dots Labs](https://threedots.tech/go-with-the-domain/) - The book that inspired many patterns in this post
- [Wild Workouts Example](https://github.com/ThreeDotsLabs/wild-workouts-go-ddd-example) - Complete DDD + CQRS implementation
- [Watermill](https://watermill.io/) - Three Dots Labs' excellent event-driven library
- [Event Sourcing in Production](https://threedots.tech/tags/events/) - Real-world experiences from Three Dots Labs
