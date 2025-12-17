---
title: "Managing Race Conditions in Event-Driven Systems with Go"
date: 2025-11-02
description: "Building distributed systems means accepting that events won't always arrive in the order they were created. When Services communicate asynchronously through message queues and event streams, race conditions become inevitable. This guide exploires practical strategies for handling out-of-order events using Go."
tags:
  [
    "golang",
    "distributed-systems",
    "architecture",
    "microservices",
    "system-design",
    "event-driven",
    "race-conditions",
    "concurrency",
    "goroutines",
  ]
---

## The Challenge of Distributed Event Ordering

Building distributed systems means accepting that events won't always arrive in the order they were created. When services communicate asynchronously through message queues and event streams, race conditions become inevitable. This guide explores practical strategies for handling out-of-order events using Go.

## Understanding the Problem

Consider an e-commerce order processing system where multiple services handle different aspects of an order:

- Inventory service checks stock availability
- Payment service processes transactions
- Shipping service calculates delivery options
- Customer service updates loyalty points

Each service publishes events independently, but network latency, queue processing delays, and service availability can shuffle the order in which your aggregation service receives these events.

## Core Concepts: Events as Observations

In distributed systems, we need to shift our mindset:

- **Internal events** are facts we control and trust
- **External events** are observations about other systems' states
- **Temporal ordering** is relative, not absolute
- **Partial state** is normal, not exceptional

## Building Resilient Event Handlers in Go

### Event Definitions

```go
package events

import (
    "time"
)

// OrderEvent represents any event related to order processing
type OrderEvent interface {
    GetOrderID() string
    GetTimestamp() time.Time
    GetEventType() string
}

// Base event structure
type BaseEvent struct {
    OrderID   string    `json:"orderId"`
    Timestamp time.Time `json:"timestamp"`
    EventType string    `json:"eventType"`
}

func (e BaseEvent) GetOrderID() string     { return e.OrderID }
func (e BaseEvent) GetTimestamp() time.Time { return e.Timestamp }
func (e BaseEvent) GetEventType() string   { return e.EventType }

// Specific event types
type OrderCreated struct {
    BaseEvent
    CustomerID   string  `json:"customerId"`
    TotalAmount  float64 `json:"totalAmount"`
    Currency     string  `json:"currency"`
    Items        []Item  `json:"items"`
}

type InventoryReserved struct {
    BaseEvent
    ReservedItems map[string]int `json:"reservedItems"`
    WarehouseID   string         `json:"warehouseId"`
}

type PaymentProcessed struct {
    BaseEvent
    TransactionID string  `json:"transactionId"`
    Amount        float64 `json:"amount"`
    Status        string  `json:"status"`
    Method        string  `json:"method"`
}

type ShippingCalculated struct {
    BaseEvent
    ShippingCost    float64   `json:"shippingCost"`
    EstimatedDays   int       `json:"estimatedDays"`
    CarrierOptions  []Carrier `json:"carrierOptions"`
}

type LoyaltyPointsUpdated struct {
    BaseEvent
    PointsEarned int    `json:"pointsEarned"`
    TotalPoints  int    `json:"totalPoints"`
    TierStatus   string `json:"tierStatus"`
}
```

### Aggregation State Model

```go
package models

import (
    "sync"
    "time"
)

// OrderAggregate represents the combined state from all events
type OrderAggregate struct {
    mu              sync.RWMutex
    OrderID         string                 `json:"orderId"`
    CreatedAt       *time.Time            `json:"createdAt,omitempty"`
    CustomerInfo    *CustomerData         `json:"customerInfo,omitempty"`
    PaymentInfo     *PaymentData          `json:"paymentInfo,omitempty"`
    InventoryStatus *InventoryData        `json:"inventoryStatus,omitempty"`
    ShippingInfo    *ShippingData         `json:"shippingInfo,omitempty"`
    LoyaltyInfo     *LoyaltyData          `json:"loyaltyInfo,omitempty"`
    DataCompleteness float64              `json:"dataCompleteness"`
    ProcessingStatus string               `json:"processingStatus"`
    LastUpdated      time.Time            `json:"lastUpdated"`
    EventHistory     []EventRecord        `json:"eventHistory"`
}

type CustomerData struct {
    CustomerID  string    `json:"customerId"`
    OrderAmount float64   `json:"orderAmount"`
    Currency    string    `json:"currency"`
    ReceivedAt  time.Time `json:"receivedAt"`
}

type PaymentData struct {
    TransactionID string    `json:"transactionId"`
    Status        string    `json:"status"`
    Amount        float64   `json:"amount"`
    Method        string    `json:"method"`
    ProcessedAt   time.Time `json:"processedAt"`
}

type InventoryData struct {
    ReservedItems map[string]int `json:"reservedItems"`
    WarehouseID   string         `json:"warehouseId"`
    ReservedAt    time.Time      `json:"reservedAt"`
}

type ShippingData struct {
    Cost          float64    `json:"cost"`
    EstimatedDays int        `json:"estimatedDays"`
    CalculatedAt  time.Time  `json:"calculatedAt"`
}

type LoyaltyData struct {
    PointsEarned int       `json:"pointsEarned"`
    TotalPoints  int       `json:"totalPoints"`
    TierStatus   string    `json:"tierStatus"`
    UpdatedAt    time.Time `json:"updatedAt"`
}

type EventRecord struct {
    EventType   string    `json:"eventType"`
    ReceivedAt  time.Time `json:"receivedAt"`
    ProcessedAt time.Time `json:"processedAt"`
}
```

### Event Processor with Graceful Handling

```go
package processor

import (
    "context"
    "fmt"
    "log"
    "sync"
    "time"
)

type EventProcessor struct {
    store      AggregateStore
    publisher  EventPublisher
    metrics    MetricsCollector
}

type AggregateStore interface {
    Get(ctx context.Context, orderID string) (*OrderAggregate, error)
    Save(ctx context.Context, aggregate *OrderAggregate) error
}

type EventPublisher interface {
    Publish(ctx context.Context, event interface{}) error
}

type MetricsCollector interface {
    RecordEventProcessed(eventType string, success bool)
    RecordCompleteness(orderID string, percentage float64)
}

func NewEventProcessor(store AggregateStore, pub EventPublisher, metrics MetricsCollector) *EventProcessor {
    return &EventProcessor{
        store:     store,
        publisher: pub,
        metrics:   metrics,
    }
}

func (p *EventProcessor) ProcessEvent(ctx context.Context, event OrderEvent) error {
    // Retrieve or create aggregate
    aggregate, err := p.getOrCreateAggregate(ctx, event.GetOrderID())
    if err != nil {
        return fmt.Errorf("failed to get aggregate: %w", err)
    }

    // Lock for updates
    aggregate.mu.Lock()
    defer aggregate.mu.Unlock()

    // Apply event based on type
    var processed bool
    switch e := event.(type) {
    case *OrderCreated:
        processed = p.applyOrderCreated(aggregate, e)
    case *PaymentProcessed:
        processed = p.applyPaymentProcessed(aggregate, e)
    case *InventoryReserved:
        processed = p.applyInventoryReserved(aggregate, e)
    case *ShippingCalculated:
        processed = p.applyShippingCalculated(aggregate, e)
    case *LoyaltyPointsUpdated:
        processed = p.applyLoyaltyPoints(aggregate, e)
    default:
        log.Printf("Unknown event type: %s", event.GetEventType())
        processed = false
    }

    if processed {
        // Update metadata
        aggregate.LastUpdated = time.Now()
        aggregate.EventHistory = append(aggregate.EventHistory, EventRecord{
            EventType:   event.GetEventType(),
            ReceivedAt:  time.Now(),
            ProcessedAt: time.Now(),
        })

        // Calculate data completeness
        aggregate.DataCompleteness = p.calculateCompleteness(aggregate)

        // Check if we can make decisions
        if decision := p.evaluateForDecision(aggregate); decision != nil {
            if err := p.publisher.Publish(ctx, decision); err != nil {
                log.Printf("Failed to publish decision: %v", err)
            }
        }

        // Save updated aggregate
        if err := p.store.Save(ctx, aggregate); err != nil {
            return fmt.Errorf("failed to save aggregate: %w", err)
        }

        // Record metrics
        p.metrics.RecordEventProcessed(event.GetEventType(), true)
        p.metrics.RecordCompleteness(aggregate.OrderID, aggregate.DataCompleteness)
    }

    return nil
}

func (p *EventProcessor) getOrCreateAggregate(ctx context.Context, orderID string) (*OrderAggregate, error) {
    aggregate, err := p.store.Get(ctx, orderID)
    if err != nil {
        // Create new aggregate if not found
        return &OrderAggregate{
            OrderID:          orderID,
            ProcessingStatus: "pending",
            LastUpdated:      time.Now(),
            EventHistory:     []EventRecord{},
        }, nil
    }
    return aggregate, nil
}

func (p *EventProcessor) applyOrderCreated(agg *OrderAggregate, event *OrderCreated) bool {
    // Skip if already processed (idempotency)
    if agg.CustomerInfo != nil && agg.CustomerInfo.ReceivedAt.Before(event.Timestamp) {
        return false
    }

    agg.CustomerInfo = &CustomerData{
        CustomerID:  event.CustomerID,
        OrderAmount: event.TotalAmount,
        Currency:    event.Currency,
        ReceivedAt:  event.Timestamp,
    }

    now := event.Timestamp
    agg.CreatedAt = &now
    agg.ProcessingStatus = "processing"

    return true
}

func (p *EventProcessor) applyPaymentProcessed(agg *OrderAggregate, event *PaymentProcessed) bool {
    // Check if newer payment info exists
    if agg.PaymentInfo != nil && agg.PaymentInfo.ProcessedAt.After(event.Timestamp) {
        return false
    }

    agg.PaymentInfo = &PaymentData{
        TransactionID: event.TransactionID,
        Status:        event.Status,
        Amount:        event.Amount,
        Method:        event.Method,
        ProcessedAt:   event.Timestamp,
    }

    // Update processing status if payment failed
    if event.Status == "failed" {
        agg.ProcessingStatus = "payment_failed"
    }

    return true
}

func (p *EventProcessor) applyInventoryReserved(agg *OrderAggregate, event *InventoryReserved) bool {
    if agg.InventoryStatus != nil && agg.InventoryStatus.ReservedAt.After(event.Timestamp) {
        return false
    }

    agg.InventoryStatus = &InventoryData{
        ReservedItems: event.ReservedItems,
        WarehouseID:   event.WarehouseID,
        ReservedAt:    event.Timestamp,
    }

    return true
}

func (p *EventProcessor) calculateCompleteness(agg *OrderAggregate) float64 {
    total := 5.0
    complete := 0.0

    if agg.CustomerInfo != nil {
        complete++
    }
    if agg.PaymentInfo != nil {
        complete++
    }
    if agg.InventoryStatus != nil {
        complete++
    }
    if agg.ShippingInfo != nil {
        complete++
    }
    if agg.LoyaltyInfo != nil {
        complete++
    }

    return (complete / total) * 100
}

func (p *EventProcessor) evaluateForDecision(agg *OrderAggregate) interface{} {
    // Only make decisions when we have critical data
    if agg.PaymentInfo == nil || agg.InventoryStatus == nil {
        return nil
    }

    // Check for failure conditions
    if agg.PaymentInfo.Status == "failed" {
        return &OrderRejected{
            OrderID: agg.OrderID,
            Reason:  "Payment failed",
            Time:    time.Now(),
        }
    }

    // Check if we have enough data to approve
    if agg.PaymentInfo.Status == "successful" && len(agg.InventoryStatus.ReservedItems) > 0 {
        // Optional data can be filled later
        return &OrderConfirmed{
            OrderID:       agg.OrderID,
            WarehouseID:   agg.InventoryStatus.WarehouseID,
            EstimatedDays: p.getEstimatedDays(agg),
            Time:          time.Now(),
        }
    }

    return nil
}

func (p *EventProcessor) getEstimatedDays(agg *OrderAggregate) int {
    if agg.ShippingInfo != nil {
        return agg.ShippingInfo.EstimatedDays
    }
    // Default estimate if shipping not calculated yet
    return 5
}
```

### Handling Concurrent Updates

```go
package processor

import (
    "context"
    "sync"
    "time"
)

type ConcurrentEventHandler struct {
    processor    *EventProcessor
    workerPool   int
    eventBuffer  chan OrderEvent
    ctx          context.Context
    cancel       context.CancelFunc
    wg           sync.WaitGroup
}

func NewConcurrentEventHandler(processor *EventProcessor, workers int) *ConcurrentEventHandler {
    ctx, cancel := context.WithCancel(context.Background())
    return &ConcurrentEventHandler{
        processor:   processor,
        workerPool:  workers,
        eventBuffer: make(chan OrderEvent, workers*10),
        ctx:         ctx,
        cancel:      cancel,
    }
}

func (h *ConcurrentEventHandler) Start() {
    for i := 0; i < h.workerPool; i++ {
        h.wg.Add(1)
        go h.worker(i)
    }
}

func (h *ConcurrentEventHandler) Stop() {
    h.cancel()
    close(h.eventBuffer)
    h.wg.Wait()
}

func (h *ConcurrentEventHandler) HandleEvent(event OrderEvent) error {
    select {
    case h.eventBuffer <- event:
        return nil
    case <-h.ctx.Done():
        return fmt.Errorf("handler stopped")
    case <-time.After(5 * time.Second):
        return fmt.Errorf("buffer full, event dropped")
    }
}

func (h *ConcurrentEventHandler) worker(id int) {
    defer h.wg.Done()

    for {
        select {
        case event, ok := <-h.eventBuffer:
            if !ok {
                return
            }

            ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
            if err := h.processor.ProcessEvent(ctx, event); err != nil {
                log.Printf("Worker %d: failed to process event: %v", id, err)
                // Could implement retry logic here
            }
            cancel()

        case <-h.ctx.Done():
            return
        }
    }
}
```

### Storage Implementation with Optimistic Locking

```go
package storage

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "time"
)

type PostgresAggregateStore struct {
    db *sql.DB
}

func NewPostgresAggregateStore(db *sql.DB) *PostgresAggregateStore {
    return &PostgresAggregateStore{db: db}
}

func (s *PostgresAggregateStore) Get(ctx context.Context, orderID string) (*OrderAggregate, error) {
    var data []byte
    var version int

    query := `
        SELECT data, version, updated_at
        FROM order_aggregates
        WHERE order_id = $1
    `

    err := s.db.QueryRowContext(ctx, query, orderID).Scan(&data, &version, &updated)
    if err == sql.ErrNoRows {
        return nil, fmt.Errorf("aggregate not found")
    }
    if err != nil {
        return nil, err
    }

    var aggregate OrderAggregate
    if err := json.Unmarshal(data, &aggregate); err != nil {
        return nil, err
    }

    return &aggregate, nil
}

func (s *PostgresAggregateStore) Save(ctx context.Context, aggregate *OrderAggregate) error {
    data, err := json.Marshal(aggregate)
    if err != nil {
        return err
    }

    query := `
        INSERT INTO order_aggregates (order_id, data, version, updated_at)
        VALUES ($1, $2, 1, $3)
        ON CONFLICT (order_id) DO UPDATE
        SET data = $2,
            version = order_aggregates.version + 1,
            updated_at = $3
        WHERE order_aggregates.updated_at < $3
    `

    result, err := s.db.ExecContext(ctx, query, aggregate.OrderID, data, aggregate.LastUpdated)
    if err != nil {
        return err
    }

    rows, err := result.RowsAffected()
    if err != nil {
        return err
    }

    if rows == 0 {
        return fmt.Errorf("concurrent update detected")
    }

    return nil
}
```

## Testing Strategies

```go
package processor_test

import (
    "context"
    "testing"
    "time"
)

func TestOutOfOrderEventProcessing(t *testing.T) {
    store := NewInMemoryStore()
    publisher := NewMockPublisher()
    metrics := NewMockMetrics()
    processor := NewEventProcessor(store, publisher, metrics)

    orderID := "order-123"

    // Simulate events arriving out of order
    events := []OrderEvent{
        &PaymentProcessed{
            BaseEvent: BaseEvent{
                OrderID:   orderID,
                Timestamp: time.Now().Add(-3 * time.Second),
                EventType: "PaymentProcessed",
            },
            Status: "successful",
            Amount: 99.99,
        },
        &ShippingCalculated{
            BaseEvent: BaseEvent{
                OrderID:   orderID,
                Timestamp: time.Now().Add(-2 * time.Second),
                EventType: "ShippingCalculated",
            },
            ShippingCost:  10.00,
            EstimatedDays: 3,
        },
        &OrderCreated{ // This should have come first!
            BaseEvent: BaseEvent{
                OrderID:   orderID,
                Timestamp: time.Now().Add(-5 * time.Second),
                EventType: "OrderCreated",
            },
            CustomerID:  "customer-456",
            TotalAmount: 99.99,
            Currency:    "USD",
        },
        &InventoryReserved{
            BaseEvent: BaseEvent{
                OrderID:   orderID,
                Timestamp: time.Now().Add(-4 * time.Second),
                EventType: "InventoryReserved",
            },
            ReservedItems: map[string]int{"sku-1": 2},
            WarehouseID:   "warehouse-west",
        },
    }

    // Process all events
    for _, event := range events {
        err := processor.ProcessEvent(context.Background(), event)
        if err != nil {
            t.Errorf("Failed to process event: %v", err)
        }
    }

    // Verify final state
    aggregate, err := store.Get(context.Background(), orderID)
    if err != nil {
        t.Fatalf("Failed to get aggregate: %v", err)
    }

    // Check all data was captured despite order
    if aggregate.CustomerInfo == nil {
        t.Error("Customer info not captured")
    }
    if aggregate.PaymentInfo == nil || aggregate.PaymentInfo.Status != "successful" {
        t.Error("Payment info not captured correctly")
    }
    if aggregate.InventoryStatus == nil {
        t.Error("Inventory status not captured")
    }
    if aggregate.ShippingInfo == nil {
        t.Error("Shipping info not captured")
    }

    // Verify decision was made
    if len(publisher.PublishedEvents) != 1 {
        t.Errorf("Expected 1 decision event, got %d", len(publisher.PublishedEvents))
    }
}

func TestIdempotency(t *testing.T) {
    store := NewInMemoryStore()
    publisher := NewMockPublisher()
    metrics := NewMockMetrics()
    processor := NewEventProcessor(store, publisher, metrics)

    event := &OrderCreated{
        BaseEvent: BaseEvent{
            OrderID:   "order-789",
            Timestamp: time.Now(),
            EventType: "OrderCreated",
        },
        CustomerID:  "customer-999",
        TotalAmount: 150.00,
        Currency:    "EUR",
    }

    // Process same event multiple times
    for i := 0; i < 3; i++ {
        err := processor.ProcessEvent(context.Background(), event)
        if err != nil {
            t.Errorf("Failed to process event on attempt %d: %v", i+1, err)
        }
    }

    // Verify event was only applied once
    aggregate, _ := store.Get(context.Background(), "order-789")
    if len(aggregate.EventHistory) != 1 {
        t.Errorf("Expected 1 event in history, got %d", len(aggregate.EventHistory))
    }
}
```

## Best Practices and Patterns

### 1. Embrace Partial State

Design your domain models to function with incomplete data. Use nullable fields and provide sensible defaults when information is missing.

### 2. Implement Idempotency

Always check if an event has already been processed. Use timestamps, version numbers, or event IDs to detect duplicates.

### 3. Separate Read and Write Concerns

Keep your event processing logic separate from your query models. This allows you to optimize each independently.

### 4. Monitor Data Quality

Track metrics on data completeness and processing delays. This helps identify systemic issues before they become critical.

### 5. Design for Correction

Accept that some decisions might need revision when more complete data arrives. Build mechanisms to handle corrections gracefully.

### 6. Use Correlation IDs

Ensure all events carry correlation identifiers that allow you to group related events together, even when they arrive out of sequence.

## Conclusion

Race conditions in event-driven systems are not problems to eliminate but realities to manage. By building systems that gracefully handle partial data, process events idempotently, and make decisions based on available information, we create resilient architectures that thrive in distributed environments.

The key insight is that perfect ordering is often unnecessary. What matters is building systems that converge to correct states regardless of event arrival order. Go's concurrency primitives and type system make it an excellent choice for implementing these patterns safely and efficiently.

Remember: in distributed systems, eventual consistency is not a compromise—it's a feature that enables scalability and resilience.
