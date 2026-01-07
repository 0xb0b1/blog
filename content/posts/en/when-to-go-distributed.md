---
title: "When to Go Distributed: Real Talk on Building Systems with Go"
date: 2025-02-21
description: "After years building distributed systems, here's my honest take on when you should consider the complexity, and why Go excels when you do."
tags:
  [
    "golang",
    "distributed-systems",
    "architecture",
    "microservices",
    "system-design",
  ]
---

After years building distributed systems, I've learned the question isn't "how" to build them, but **WHEN** you should even consider the complexity.

Here's my real talk on using Go for distributed systems.

## 𝗪𝗵𝘆 𝗚𝗼 𝗘𝘅𝗰𝗲𝗹𝘀

### Goroutines = Game Changer

Handling 10k+ concurrent connections with minimal overhead. I migrated a Python API gateway to Go and saw **60% less memory usage** while handling **3x more traffic**.

```go
package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Example: Handling thousands of concurrent requests
func handleRequest(w http.ResponseWriter, r *http.Request) {
	// Simulate some work
	time.Sleep(100 * time.Millisecond)
	fmt.Fprintf(w, "Request handled by goroutine")
}

func main() {
	http.HandleFunc("/", handleRequest)

	// Go's HTTP server creates a goroutine per request automatically
	// Can handle 10k+ concurrent connections with ~2GB RAM
	http.ListenAndServe(":8080", nil)
}
```

**Real-world impact:**

- Python service: 2000 req/s, 4GB RAM
- Go service: 6000 req/s, 1.5GB RAM
- Same functionality, 1/10th the code

### ⚡ Network Programming

The standard library makes HTTP/gRPC services feel effortless. Built-in HTTP/2, context propagation, and native timeout support.

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Context propagation is built-in
func fetchUserData(ctx context.Context, userID string) (*User, error) {
	// Create HTTP client with timeout from context
	client := &http.Client{
	 Timeout: 5 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET",
	 fmt.Sprintf("https://api.example.com/users/%s", userID), nil)
	if err != nil {
	 return nil, err
	}

	// If parent context is cancelled, request is cancelled too
	resp, err := client.Do(req)
	if err != nil {
	 return nil, err
	}
	defer resp.Body.Close()

	// Parse response...
	return parseUser(resp.Body)
}

// Fan-out pattern with goroutines
func fetchMultipleUsers(ctx context.Context, userIDs []string) ([]*User, error) {
	results := make(chan *User, len(userIDs))
	errors := make(chan error, len(userIDs))

	for _, id := range userIDs {
	 go func(userID string) {
	  user, err := fetchUserData(ctx, userID)
	  if err != nil {
	   errors <- err
	   return
	  }
	  results <- user
	 }(id)
	}

	// Collect results
	users := make([]*User, 0, len(userIDs))
	for i := 0; i < len(userIDs); i++ {
	 select {
	 case user := <-results:
	  users = append(users, user)
	 case err := <-errors:
	  return nil, err
	 case <-ctx.Done():
	  return nil, ctx.Err()
	 }
	}

	return users, nil
}
```

### 📦 Single Binary Deployment

No dependency hell. Just ship and run.

```bash
# Build for production
CGO_ENABLED=0 GOOS=linux go build -o myservice

# Ship the binary - that's it!
# No Python virtualenvs, no Node modules, no JVM tuning
```

**Real story:** I've debugged too many Node.js services at 2 AM because of missing dependencies in production. With Go? Copy binary, run. Done.

```dockerfile
# Minimal Docker image
FROM scratch
COPY myservice /
ENTRYPOINT ["/myservice"]

# Result: 15MB image vs 500MB+ Node/Python images
```

## 𝗪𝗵𝗲𝗻 𝘁𝗼 𝗚𝗼 𝗗𝗶𝘀𝘁𝗿𝗶𝗯𝘂𝘁𝗲𝗱 ✅

### Your monolith hits genuine scalability walls

```go
// Example: Different scaling needs
type OrderService struct {
	// Needs to scale for Black Friday traffic
	// Peak: 10k orders/minute
}

type ReportService struct {
	// CPU-intensive, runs nightly
	// Can be scaled independently
}

type NotificationService struct {
	// I/O bound, different resource profile
}
```

### Different components need independent scaling

```yaml
# Kubernetes deployment example
apiVersion: apps/v1
kind: Deployment
metadata:
	 name: order-service
spec:
	 replicas: 10 # Scale for traffic
---
apiVersion: apps/v1
kind: Deployment
metadata:
	 name: report-service
spec:
	 replicas: 2 # Less instances, more CPU
```

### Teams need autonomous deployment cycles

```go
// Service A can deploy independently
// Version: 1.2.3
type ServiceA struct {
	// Owns user management
}

// Service B can deploy independently
// Version: 2.0.1
type ServiceB struct {
	// Owns order processing
}

// They communicate via well-defined APIs
// No deployment coordination needed
```

### Fault isolation is business-critical

```go
// Circuit breaker pattern
type CircuitBreaker struct {
	failureThreshold int
	resetTimeout     time.Duration
	failures         int
	lastFailureTime  time.Time
	state            string // "closed", "open", "half-open"
}

func (cb *CircuitBreaker) Call(fn func() error) error {
	if cb.state == "open" {
	 if time.Since(cb.lastFailureTime) > cb.resetTimeout {
	  cb.state = "half-open"
	 } else {
	  return fmt.Errorf("circuit breaker is open")
	 }
	}

	err := fn()
	if err != nil {
	 cb.failures++
	 cb.lastFailureTime = time.Now()

	 if cb.failures >= cb.failureThreshold {
	  cb.state = "open"
	 }
	 return err
	}

	cb.failures = 0
	cb.state = "closed"
	return nil
}

// Usage: If payment service fails, checkout still works
func processOrder(order *Order) error {
	// Save order first
	if err := saveOrder(order); err != nil {
	 return err
	}

	// Payment can fail without killing entire system
	paymentCB.Call(func() error {
	 return processPayment(order)
	})

	// Order is saved regardless
	return nil
}
```

### You have monitoring/observability in place

```go
// Distributed tracing example
import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func processOrder(ctx context.Context, order *Order) error {
	// Create span for tracing
	ctx, span := otel.Tracer("order-service").Start(ctx, "processOrder")
	defer span.End()

	// This trace ID follows the request across services
	span.AddEvent("validating order")
	if err := validateOrder(ctx, order); err != nil {
	 span.RecordError(err)
	 return err
	}

	span.AddEvent("calling payment service")
	if err := callPaymentService(ctx, order); err != nil {
	 span.RecordError(err)
	 return err
	}

	return nil
}
```

## 𝗪𝗵𝗲𝗻 𝘁𝗼 𝗦𝘁𝗮𝘆 𝗠𝗼𝗻𝗼𝗹𝗶𝘁𝗵𝗶𝗰 ❌

### Small team (<8 engineers)

You'll spend more time on infrastructure than features.

```go
// Monolith: One service, clear structure
type Application struct {
	userService    *UserService
	orderService   *OrderService
	paymentService *PaymentService
}

// Still organized, still testable
// But deployed together = simpler ops
```

### Theoretical problems, not actual bottlenecks

```bash
# Before splitting, measure!
$ wrk -t12 -c400 -d30s http://localhost:8080/api/orders
Running 30s test @ http://localhost:8080/api/orders
	 12 threads and 400 connections
	 Requests/sec: 5000  # Can your monolith handle this?
	 Latency avg: 50ms   # Is this actually a problem?
```

### Can't properly debug a single service yet

If you struggle with one service, wait until you have 20.

### Trying to fix org problems with tech

```
❌ Problem: Teams stepping on each other's code
✅ Solution: Better code reviews, ownership model

❌ Solution: Split into microservices
😱 Result: Teams stepping on each other's APIs instead
```

## 𝗠𝘆 𝗚𝗼𝗹𝗱𝗲𝗻 𝗥𝘂𝗹𝗲

**Start with a well-structured monolith. Clear boundaries, good interfaces. When you genuinely outgrow it, extraction becomes natural.**

### Well-Structured Monolith Example

```go
// internal/domain/user/service.go
package user

type Service struct {
	repo Repository
}

func (s *Service) CreateUser(ctx context.Context, email string) (*User, error) {
	// Business logic
}

// internal/domain/order/service.go
package order

type Service struct {
	repo Repository
	userService user.Service // Clear dependency
}

func (s *Service) CreateOrder(ctx context.Context, userID string) (*Order, error) {
	// When you split, this becomes an HTTP/gRPC call
	// But the interface stays the same!
}
```

### Natural Extraction

```go
// Phase 1: Monolith with interface
type UserService interface {
	GetUser(ctx context.Context, id string) (*User, error)
}

// Phase 2: Replace with HTTP client - same interface!
type HTTPUserService struct {
	baseURL string
}

func (s *HTTPUserService) GetUser(ctx context.Context, id string) (*User, error) {
	// Call external service
	resp, err := http.Get(s.baseURL + "/users/" + id)
	// Parse and return
}

// Your order service code doesn't change at all!
```

## The Sweet Spot

**8+ engineers, genuine scale needs, mature ops practices.**

Anything smaller? Optimize your monolith first.

### Optimization Checklist Before Going Distributed

```go
// 1. Add caching
type UserService struct {
	cache *redis.Client
	db    *sql.DB
}

// 2. Add connection pooling
db, _ := sql.Open("postgres", connStr)
db.SetMaxOpenConns(25)
db.SetMaxIdleConns(25)

// 3. Profile your code
import _ "net/http/pprof"
go func() {
	http.ListenAndServe("localhost:6060", nil)
}()

// 4. Add indexes
// CREATE INDEX idx_users_email ON users(email);

// 5. Use goroutines for I/O
results := make(chan Result, len(queries))
for _, query := range queries {
	go func(q Query) {
	 results <- executeQuery(q)
	}(query)
}
```

## Real-World Migration Path

```go
// Step 1: Monolith with clear boundaries
func main() {
	userSvc := user.NewService(userRepo)
	orderSvc := order.NewService(orderRepo, userSvc)

	http.HandleFunc("/users", userHandler(userSvc))
	http.HandleFunc("/orders", orderHandler(orderSvc))
}

// Step 2: Extract one service
// Service A (Users)
func main() {
	userSvc := user.NewService(userRepo)
	http.HandleFunc("/users", userHandler(userSvc))
	http.ListenAndServe(":8081", nil)
}

// Service B (Orders)
func main() {
	// Now calls users via HTTP
	userClient := user.NewHTTPClient("http://users-service:8081")
	orderSvc := order.NewService(orderRepo, userClient)
	http.HandleFunc("/orders", orderHandler(orderSvc))
	http.ListenAndServe(":8082", nil)
}
```

## Conclusion

Distributed systems are a **tool**, not a destination. Go makes them manageable, but knowing **WHEN** the complexity is worth it matters most.

### Decision Framework

```
Q: Is my monolith actually slow?
	  └─ No → Optimize first
	  └─ Yes → Continue

Q: Do I have >8 engineers?
	  └─ No → Stay monolithic
	  └─ Yes → Continue

Q: Can I monitor distributed systems?
	  └─ No → Not ready yet
	  └─ Yes → Continue

Q: Are components truly independent?
	  └─ No → Improve boundaries first
	  └─ Yes → Consider splitting
```

**Start simple. Scale when needed. Go makes both paths viable.**
