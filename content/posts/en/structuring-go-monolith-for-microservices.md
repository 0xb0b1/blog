---
title: "Structuring a Go Monolith So It Can Become Microservices Later"
date: 2025-01-28
description: "Practical module boundaries, package organization, and the patterns that make future decomposition possible without rewriting everything."
tags:
  ["golang", "architecture", "microservices", "monolith", "patterns", "design"]
---

"We'll extract microservices later" is the lie we tell ourselves before building a tangled monolith that can never be split apart. The problem isn't monoliths—they're often the right choice. The problem is monoliths with no internal boundaries.

Here's how to structure a Go monolith that can actually become microservices when (if) you need them.

## The Goal: Modular Monolith

A modular monolith has clear internal boundaries that look like service boundaries, but everything runs in one process. You get:

- Simple deployment and operations
- No network latency between "services"
- Easy refactoring across boundaries
- The option to extract later

```
monolith/
├── cmd/
│   └── server/
│       └── main.go           # Single entry point
├── internal/
│   ├── orders/               # Could be a service
│   │   ├── service.go
│   │   ├── repository.go
│   │   ├── handlers.go
│   │   └── events.go
│   ├── payments/             # Could be a service
│   │   ├── service.go
│   │   ├── repository.go
│   │   ├── handlers.go
│   │   └── events.go
│   ├── inventory/            # Could be a service
│   │   └── ...
│   └── shared/               # Truly shared code
│       ├── auth/
│       └── observability/
├── pkg/                      # Public contracts
│   ├── orderapi/
│   ├── paymentapi/
│   └── events/
└── go.mod
```

## Rule 1: Modules Own Their Data

Each module has its own database schema. No cross-module table access.

```go
// internal/orders/repository.go
type Repository struct {
    db *sql.DB
}

// Orders module only touches orders tables
func (r *Repository) Create(ctx context.Context, order *Order) error {
    _, err := r.db.ExecContext(ctx, `
        INSERT INTO orders.orders (id, customer_id, status, created_at)
        VALUES ($1, $2, $3, $4)
    `, order.ID, order.CustomerID, order.Status, order.CreatedAt)
    return err
}

// WRONG: Don't do this - reaching into another module's tables
func (r *Repository) GetCustomerEmail(ctx context.Context, customerID string) (string, error) {
    // This creates hidden coupling to the customers module
    var email string
    err := r.db.QueryRowContext(ctx, `
        SELECT email FROM customers.customers WHERE id = $1
    `, customerID).Scan(&email)
    return email, err
}
```

Instead, define what you need from other modules explicitly:

```go
// internal/orders/service.go
type CustomerGetter interface {
    GetCustomer(ctx context.Context, id string) (*Customer, error)
}

type Service struct {
    repo      *Repository
    customers CustomerGetter  // Explicit dependency
    payments  PaymentProcessor
}

func (s *Service) CreateOrder(ctx context.Context, req CreateOrderRequest) (*Order, error) {
    // Get customer through the interface, not direct DB access
    customer, err := s.customers.GetCustomer(ctx, req.CustomerID)
    if err != nil {
        return nil, fmt.Errorf("get customer: %w", err)
    }

    order := &Order{
        ID:         uuid.NewString(),
        CustomerID: customer.ID,
        Email:      customer.Email,  // Denormalize what you need
        Status:     StatusPending,
    }

    if err := s.repo.Create(ctx, order); err != nil {
        return nil, fmt.Errorf("create order: %w", err)
    }

    return order, nil
}
```

### Schema Isolation with PostgreSQL

```sql
-- Each module gets its own schema
CREATE SCHEMA orders;
CREATE SCHEMA payments;
CREATE SCHEMA inventory;
CREATE SCHEMA customers;

-- Tables live in their module's schema
CREATE TABLE orders.orders (
    id UUID PRIMARY KEY,
    customer_id UUID NOT NULL,  -- Reference, not FK
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

-- No cross-schema foreign keys!
-- This makes extraction possible later
```

## Rule 2: Communication Through Interfaces

Modules talk to each other through interfaces defined by the consumer:

```go
// internal/orders/dependencies.go
package orders

// Define what orders needs from other modules
type PaymentProcessor interface {
    Charge(ctx context.Context, customerID string, amount int) (*PaymentResult, error)
    Refund(ctx context.Context, paymentID string) error
}

type InventoryChecker interface {
    Reserve(ctx context.Context, items []ReservationRequest) (*Reservation, error)
    Release(ctx context.Context, reservationID string) error
}

type CustomerGetter interface {
    GetCustomer(ctx context.Context, id string) (*Customer, error)
}

// Types that cross module boundaries
type PaymentResult struct {
    PaymentID string
    Status    string
}

type Customer struct {
    ID    string
    Email string
    Name  string
}
```

The implementing module satisfies these interfaces:

```go
// internal/payments/service.go
package payments

type Service struct {
    repo    *Repository
    gateway PaymentGateway
}

// Satisfies orders.PaymentProcessor
func (s *Service) Charge(ctx context.Context, customerID string, amount int) (*orders.PaymentResult, error) {
    // Implementation
}
```

Wire it up in main:

```go
// cmd/server/main.go
func main() {
    // Initialize modules
    customerService := customers.NewService(customerRepo)
    paymentService := payments.NewService(paymentRepo, gateway)
    inventoryService := inventory.NewService(inventoryRepo)

    // Orders gets its dependencies injected
    orderService := orders.NewService(
        orderRepo,
        customerService,  // satisfies CustomerGetter
        paymentService,   // satisfies PaymentProcessor
        inventoryService, // satisfies InventoryChecker
    )
}
```

## Rule 3: Events for Loose Coupling

Some interactions shouldn't be synchronous calls. Use events:

```go
// pkg/events/events.go
package events

type OrderCreated struct {
    OrderID    string    `json:"order_id"`
    CustomerID string    `json:"customer_id"`
    Items      []Item    `json:"items"`
    Total      int       `json:"total"`
    CreatedAt  time.Time `json:"created_at"`
}

type OrderCompleted struct {
    OrderID     string    `json:"order_id"`
    CustomerID  string    `json:"customer_id"`
    CompletedAt time.Time `json:"completed_at"`
}

type PaymentFailed struct {
    OrderID   string `json:"order_id"`
    PaymentID string `json:"payment_id"`
    Reason    string `json:"reason"`
}
```

Event bus that works in-process now, can become Kafka/NATS later:

```go
// internal/shared/eventbus/bus.go
package eventbus

type Handler func(ctx context.Context, event any) error

type Bus struct {
    mu       sync.RWMutex
    handlers map[string][]Handler
}

func New() *Bus {
    return &Bus{
        handlers: make(map[string][]Handler),
    }
}

func (b *Bus) Subscribe(eventType string, handler Handler) {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.handlers[eventType] = append(b.handlers[eventType], handler)
}

func (b *Bus) Publish(ctx context.Context, event any) error {
    eventType := reflect.TypeOf(event).String()

    b.mu.RLock()
    handlers := b.handlers[eventType]
    b.mu.RUnlock()

    // In-process: run handlers directly
    // Later: serialize and send to message broker
    for _, h := range handlers {
        if err := h(ctx, event); err != nil {
            // Log error, maybe retry, but don't fail the publisher
            log.Printf("event handler error: %v", err)
        }
    }

    return nil
}
```

Usage:

```go
// internal/orders/service.go
func (s *Service) CompleteOrder(ctx context.Context, orderID string) error {
    order, err := s.repo.GetByID(ctx, orderID)
    if err != nil {
        return err
    }

    order.Status = StatusCompleted
    order.CompletedAt = time.Now()

    if err := s.repo.Update(ctx, order); err != nil {
        return err
    }

    // Publish event - other modules react asynchronously
    s.events.Publish(ctx, events.OrderCompleted{
        OrderID:     order.ID,
        CustomerID:  order.CustomerID,
        CompletedAt: order.CompletedAt,
    })

    return nil
}

// internal/notifications/handlers.go
func (s *Service) HandleOrderCompleted(ctx context.Context, event any) error {
    e := event.(events.OrderCompleted)

    return s.SendEmail(ctx, SendEmailRequest{
        To:       e.CustomerEmail,
        Template: "order-completed",
        Data:     e,
    })
}

// Wired up in main.go
eventBus.Subscribe("events.OrderCompleted", notificationService.HandleOrderCompleted)
```

## Rule 4: Public API Contracts

Define your module's public API in `pkg/`:

```go
// pkg/orderapi/api.go
package orderapi

import "context"

// This is the contract other services/modules use
type Client interface {
    CreateOrder(ctx context.Context, req CreateOrderRequest) (*Order, error)
    GetOrder(ctx context.Context, id string) (*Order, error)
    ListOrders(ctx context.Context, customerID string) ([]*Order, error)
}

type CreateOrderRequest struct {
    CustomerID string
    Items      []OrderItem
}

type Order struct {
    ID         string
    CustomerID string
    Status     string
    Items      []OrderItem
    Total      int
    CreatedAt  time.Time
}
```

In the monolith, the implementation is direct:

```go
// internal/orders/client.go
package orders

// Direct implementation - same process
type Client struct {
    service *Service
}

func NewClient(service *Service) *Client {
    return &Client{service: service}
}

func (c *Client) CreateOrder(ctx context.Context, req orderapi.CreateOrderRequest) (*orderapi.Order, error) {
    // Direct call to service
    order, err := c.service.CreateOrder(ctx, req)
    if err != nil {
        return nil, err
    }
    return toAPIOrder(order), nil
}
```

When you extract to microservices, swap in an HTTP/gRPC client:

```go
// pkg/orderapi/httpclient.go
package orderapi

// HTTP implementation - different process
type HTTPClient struct {
    baseURL    string
    httpClient *http.Client
}

func NewHTTPClient(baseURL string) *HTTPClient {
    return &HTTPClient{
        baseURL:    baseURL,
        httpClient: &http.Client{Timeout: 10 * time.Second},
    }
}

func (c *HTTPClient) CreateOrder(ctx context.Context, req CreateOrderRequest) (*Order, error) {
    // HTTP call to orders service
    body, _ := json.Marshal(req)
    resp, err := c.httpClient.Post(c.baseURL+"/orders", "application/json", bytes.NewReader(body))
    // ...
}
```

The calling code doesn't change—it just uses a different `orderapi.Client` implementation.

## Rule 5: Separate Read and Write Models Where Needed

Some queries need data from multiple modules. Instead of cross-module joins, build read models:

```go
// internal/reporting/service.go
package reporting

// Read model built from events
type OrderSummary struct {
    OrderID      string
    CustomerName string
    CustomerEmail string
    Items        []ItemSummary
    Total        int
    Status       string
    PaymentStatus string
}

type Service struct {
    repo *Repository
}

// Subscribe to events from multiple modules
func (s *Service) HandleOrderCreated(ctx context.Context, event any) error {
    e := event.(events.OrderCreated)
    return s.repo.UpsertOrderSummary(ctx, &OrderSummary{
        OrderID: e.OrderID,
        // ... populate from event
    })
}

func (s *Service) HandlePaymentCompleted(ctx context.Context, event any) error {
    e := event.(events.PaymentCompleted)
    return s.repo.UpdatePaymentStatus(ctx, e.OrderID, "completed")
}

// Queries hit the read model, not source modules
func (s *Service) GetDashboard(ctx context.Context, customerID string) (*Dashboard, error) {
    summaries, err := s.repo.GetOrderSummaries(ctx, customerID)
    // All data is local - no cross-module queries
}
```

## The Extraction Playbook

When it's time to extract a module to a service:

1. **Module is already isolated** - owns its data, communicates through interfaces
2. **Create HTTP/gRPC handlers** for the module's public API
3. **Deploy as separate service** with its own database
4. **Swap client implementation** in remaining monolith
5. **Event bus becomes real message broker** (same event types)

```go
// Before: in-process
orderClient := orders.NewClient(orderService)

// After: HTTP client to orders microservice
orderClient := orderapi.NewHTTPClient("http://orders-service:8080")

// Rest of the code unchanged - same interface
```

## What NOT to Share

```go
// DON'T share domain models across modules
// Each module has its own User, Order, etc.

// internal/orders/models.go
type Customer struct {  // Orders' view of a customer
    ID    string
    Email string
}

// internal/billing/models.go
type Customer struct {  // Billing's view - different fields
    ID            string
    PaymentMethod string
    BillingAddress Address
}

// DO share:
// - Event types (pkg/events/)
// - API contracts (pkg/orderapi/, pkg/paymentapi/)
// - Truly generic utilities (pkg/httputil/, pkg/validate/)
```

## Key Takeaways

1. **Modules own their data**. No cross-module database access. Use interfaces.

2. **Define interfaces at the consumer**. Orders defines what it needs from Payments, not the other way around.

3. **Events for cross-cutting concerns**. Notifications, analytics, audit logs—these subscribe to events.

4. **Public API in `pkg/`**. This becomes your service contract when you extract.

5. **Read models for cross-module queries**. Build denormalized views from events.

6. **Don't share domain models**. Each module has its own view of shared concepts.

7. **Wire it up in main**. Dependency injection makes swapping implementations trivial.

The goal isn't to build microservices in a monolith. It's to build a monolith that doesn't fight you when the time comes to split it. Most teams never need to—and that's fine. But if you do, you'll be ready.
