---
title: "The Hidden Costs of CQRS in Production"
date: 2025-11-09
description: "What the tutorials don't tell you about eventual consistency, debugging distributed state, and the operational complexity that comes with separating reads from writes."
tags:
  [
    "golang",
    "architecture",
    "CQRS",
    "distributed-systems",
    "production",
    "lessons-learned",
  ]
---

Every CQRS tutorial shows you the elegant separation: commands go here, queries go there, and your system scales beautifully. What they don't show you is the 3 AM incident where a customer insists they made a payment that your read model says doesn't exist.

This post isn't about whether CQRS is good or bad—it's about the costs that only become visible after you've committed to the pattern in production.

## The Eventual Consistency Tax

The most discussed trade-off is eventual consistency, but discussions rarely cover how it actually manifests in production.

### The "Where's My Data?" Problem

```go
// This looks reasonable in a tutorial
func (h *OrderHandler) CreateOrder(ctx context.Context, cmd CreateOrderCommand) error {
    // Write to command side
    if err := h.commandStore.Save(ctx, order); err != nil {
        return err
    }

    // Publish event for read model
    return h.eventBus.Publish(ctx, OrderCreatedEvent{OrderID: order.ID})
}

// But then the user immediately tries to view their order...
func (h *OrderHandler) GetOrder(ctx context.Context, orderID string) (*OrderView, error) {
    // Read from query side - might not be there yet!
    return h.queryStore.FindByID(ctx, orderID)
}
```

The gap between write and read can be milliseconds or minutes depending on your infrastructure. Here's what we learned:

### Strategy 1: Read-Your-Writes Consistency

```go
type OrderService struct {
    commandStore CommandStore
    queryStore   QueryStore
    cache        *ConsistencyCache // Short-lived write-through cache
}

func (s *OrderService) CreateOrder(ctx context.Context, cmd CreateOrderCommand) (*OrderView, error) {
    order, err := s.commandStore.Save(ctx, cmd)
    if err != nil {
        return nil, err
    }

    // Cache the view immediately for the creating user
    view := orderToView(order)
    s.cache.SetWithUserScope(ctx, userID(ctx), order.ID, view, 30*time.Second)

    // Async projection still happens
    go s.eventBus.Publish(context.Background(), OrderCreatedEvent{OrderID: order.ID})

    return view, nil
}

func (s *OrderService) GetOrder(ctx context.Context, orderID string) (*OrderView, error) {
    // Check user-scoped cache first
    if view, ok := s.cache.GetWithUserScope(ctx, userID(ctx), orderID); ok {
        return view, nil
    }

    return s.queryStore.FindByID(ctx, orderID)
}
```

### Strategy 2: Explicit Consistency Boundaries

Sometimes the answer is being honest with users:

```go
type OrderResponse struct {
    Order       *OrderView `json:"order"`
    Consistency string     `json:"consistency"` // "confirmed" or "pending"
}

func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
    order, err := h.service.CreateOrder(r.Context(), cmd)
    if err != nil {
        // handle error
    }

    json.NewEncoder(w).Encode(OrderResponse{
        Order:       order,
        Consistency: "pending", // UI can show "Processing..." indicator
    })
}
```

## The Debugging Nightmare

When your read model shows different data than your write model, where do you start looking?

### Event Replay Issues

```go
// The projection that seemed fine in development
func (p *OrderProjection) Handle(event OrderCreatedEvent) error {
    return p.db.Exec(`
        INSERT INTO order_views (id, customer_id, total, status)
        VALUES ($1, $2, $3, $4)
    `, event.OrderID, event.CustomerID, event.Total, "created")
}

// But in production, events can arrive out of order or be replayed
// What happens if OrderUpdatedEvent arrives before OrderCreatedEvent?
```

### Building Debugging Tools

We learned to build these tools early, not after the first incident:

```go
// Projection lag monitor
type ProjectionLagMonitor struct {
    commandStore CommandStore
    queryStore   QueryStore
}

func (m *ProjectionLagMonitor) CheckLag(ctx context.Context, entityID string) (*LagReport, error) {
    commandVersion, err := m.commandStore.GetVersion(ctx, entityID)
    if err != nil {
        return nil, err
    }

    queryVersion, err := m.queryStore.GetProjectedVersion(ctx, entityID)
    if err != nil {
        return nil, err
    }

    return &LagReport{
        EntityID:       entityID,
        CommandVersion: commandVersion,
        QueryVersion:   queryVersion,
        Lag:            commandVersion - queryVersion,
        Status:         lagStatus(commandVersion - queryVersion),
    }, nil
}

// Consistency checker for batch verification
func (m *ProjectionLagMonitor) VerifyConsistency(ctx context.Context) (*ConsistencyReport, error) {
    // Sample entities and compare command vs query state
    // Alert on drift beyond acceptable threshold
}
```

## The Operational Complexity

### More Infrastructure, More Problems

CQRS typically means:

- Separate databases (or at least schemas) for reads and writes
- A message broker for events
- Projection workers that need monitoring
- More complex deployment orchestration

```yaml
# Your deployment just got more complex
services:
  command-api:
    depends_on:
      - postgres-write
      - kafka

  query-api:
    depends_on:
      - postgres-read
      - elasticsearch # Maybe you added this for search

  projection-worker:
    depends_on:
      - postgres-write
      - postgres-read
      - kafka
    replicas: 3 # Needs coordination for ordered processing
```

### Projection Worker Challenges

```go
// Projection workers need careful coordination
type ProjectionWorker struct {
    consumer     kafka.Consumer
    projection   Projection
    checkpointer Checkpointer
}

func (w *ProjectionWorker) Run(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return w.gracefulShutdown()
        default:
            msg, err := w.consumer.Consume(ctx)
            if err != nil {
                return err
            }

            // What if projection fails? Retry? Dead letter?
            if err := w.projection.Handle(msg); err != nil {
                // This decision affects your consistency guarantees
                if isRetryable(err) {
                    w.consumer.Nack(msg)
                    continue
                }
                // Permanent failure - now what?
                w.deadLetter.Send(msg, err)
            }

            // Checkpoint after successful processing
            w.checkpointer.Save(msg.Offset)
        }
    }
}
```

## When CQRS Actually Pays Off

After living with these costs, here's when they're worth it:

1. **Genuinely different read/write patterns**: Your writes need strong consistency and complex validation, while reads need denormalized data across multiple aggregates.

2. **Audit requirements**: You need to answer "how did we get here?" for compliance.

3. **Scale asymmetry**: 100x more reads than writes, and you need to scale them independently.

4. **Team boundaries**: Separate teams can own the command and query sides.

## When to Avoid CQRS

- Your reads and writes look similar
- Your team is small and can't afford the operational overhead
- You don't have genuine scale asymmetry
- You're not prepared to build the debugging tools

## Key Takeaways

1. **Eventual consistency is a UX problem**, not just a technical one. Plan for it in your UI.

2. **Build observability early**. Projection lag monitoring, consistency checkers, and event replay tools should be part of your initial implementation.

3. **The complexity is front-loaded**. You pay the architectural tax regardless of scale.

4. **Start with CRUD**, move to CQRS when you have evidence you need it. "We might need to scale" isn't evidence.

The pattern is powerful when you need it. The mistake is adopting it before you do.
