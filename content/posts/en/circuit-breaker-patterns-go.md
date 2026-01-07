---
title: "Why Our Microservice Needed a Circuit Breaker (And How We Built It)"
date: 2025-04-06
description: "Real latency cascades we encountered, how they took down our system, and the circuit breaker implementation that saved us."
tags:
  [
    "golang",
    "microservices",
    "resilience",
    "distributed-systems",
    "patterns",
    "production",
  ]
---

It started with a slow database query in a downstream service. Within minutes, every service in our platform was timing out. Thread pools exhausted, memory spiking, users seeing errors everywhere. One slow dependency had cascaded into a complete system failure.

This is the story of that incident, why it happened, and how we built a circuit breaker to prevent it from happening again.

## The Cascade That Broke Everything

Here's what our architecture looked like:

```
User Request → API Gateway → Order Service → Payment Service → Bank API
                                          → Inventory Service → Database
                                          → Notification Service → Email Provider
```

The Bank API started responding slowly (2-3 seconds instead of 200ms). Here's what happened:

1. Payment Service threads waited for Bank API
2. Order Service requests backed up waiting for Payment Service
3. API Gateway connections exhausted waiting for Order Service
4. Users started retrying, multiplying the load
5. Other services sharing the same infrastructure started failing

All because we didn't have a way to say "this dependency is broken, stop trying."

## The Naive Retry Approach (Don't Do This)

Our first instinct was retries with timeouts:

```go
func (c *PaymentClient) ProcessPayment(ctx context.Context, req PaymentRequest) (*PaymentResponse, error) {
    var lastErr error

    for i := 0; i < 3; i++ {
        ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
        defer cancel()

        resp, err := c.httpClient.Post(ctx, "/payments", req)
        if err == nil {
            return resp, nil
        }

        lastErr = err
        time.Sleep(time.Duration(i+1) * time.Second) // Exponential backoff
    }

    return nil, lastErr
}
```

This made things worse. When the Bank API was slow, we were:

- Making 3x the requests
- Holding connections 3x longer
- Adding backoff delays that accumulated

## Understanding Circuit Breakers

A circuit breaker has three states:

```
     ┌─────────────────────────────────────────────────────┐
     │                                                     │
     ▼                                                     │
┌─────────┐  failures > threshold  ┌──────────┐           │
│ CLOSED  │ ───────────────────► │  OPEN    │           │
│ (normal)│                       │ (failing)│           │
└─────────┘                       └──────────┘           │
     ▲                                 │                  │
     │                                 │ after timeout    │
     │                                 ▼                  │
     │                          ┌────────────┐            │
     │         success          │ HALF-OPEN  │            │
     └──────────────────────────│ (testing)  │────────────┘
                                └────────────┘  failure
```

- **Closed**: Normal operation. Track failures.
- **Open**: Dependency is broken. Fail fast without calling.
- **Half-Open**: Testing if dependency recovered. Let one request through.

## Building Our Circuit Breaker

Here's the implementation that saved us:

```go
package circuitbreaker

import (
    "context"
    "errors"
    "sync"
    "time"
)

var (
    ErrCircuitOpen = errors.New("circuit breaker is open")
)

type State int

const (
    StateClosed State = iota
    StateOpen
    StateHalfOpen
)

type CircuitBreaker struct {
    mu sync.RWMutex

    name          string
    state         State
    failures      int
    successes     int
    lastFailure   time.Time

    // Configuration
    failureThreshold  int
    successThreshold  int
    timeout           time.Duration
    onStateChange     func(name string, from, to State)
}

type Config struct {
    Name              string
    FailureThreshold  int           // Failures before opening
    SuccessThreshold  int           // Successes in half-open before closing
    Timeout           time.Duration // How long to stay open
    OnStateChange     func(name string, from, to State)
}

func New(cfg Config) *CircuitBreaker {
    return &CircuitBreaker{
        name:             cfg.Name,
        state:            StateClosed,
        failureThreshold: cfg.FailureThreshold,
        successThreshold: cfg.SuccessThreshold,
        timeout:          cfg.Timeout,
        onStateChange:    cfg.OnStateChange,
    }
}

func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
    if !cb.canExecute() {
        return ErrCircuitOpen
    }

    err := fn()

    cb.recordResult(err)
    return err
}

func (cb *CircuitBreaker) canExecute() bool {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    switch cb.state {
    case StateClosed:
        return true

    case StateOpen:
        // Check if timeout has passed
        if time.Since(cb.lastFailure) > cb.timeout {
            cb.transitionTo(StateHalfOpen)
            return true
        }
        return false

    case StateHalfOpen:
        // In half-open, we allow requests through to test
        return true

    default:
        return false
    }
}

func (cb *CircuitBreaker) recordResult(err error) {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    if err != nil {
        cb.recordFailure()
    } else {
        cb.recordSuccess()
    }
}

func (cb *CircuitBreaker) recordFailure() {
    cb.failures++
    cb.successes = 0
    cb.lastFailure = time.Now()

    switch cb.state {
    case StateClosed:
        if cb.failures >= cb.failureThreshold {
            cb.transitionTo(StateOpen)
        }

    case StateHalfOpen:
        // Any failure in half-open goes back to open
        cb.transitionTo(StateOpen)
    }
}

func (cb *CircuitBreaker) recordSuccess() {
    cb.successes++

    switch cb.state {
    case StateClosed:
        cb.failures = 0 // Reset failure count on success

    case StateHalfOpen:
        if cb.successes >= cb.successThreshold {
            cb.failures = 0
            cb.transitionTo(StateClosed)
        }
    }
}

func (cb *CircuitBreaker) transitionTo(newState State) {
    if cb.state == newState {
        return
    }

    oldState := cb.state
    cb.state = newState

    if cb.onStateChange != nil {
        // Call asynchronously to avoid holding the lock
        go cb.onStateChange(cb.name, oldState, newState)
    }
}

func (cb *CircuitBreaker) State() State {
    cb.mu.RLock()
    defer cb.mu.RUnlock()
    return cb.state
}
```

## Using the Circuit Breaker

Here's how we integrated it into our payment client:

```go
type PaymentClient struct {
    httpClient     *http.Client
    baseURL        string
    circuitBreaker *circuitbreaker.CircuitBreaker
}

func NewPaymentClient(baseURL string) *PaymentClient {
    cb := circuitbreaker.New(circuitbreaker.Config{
        Name:             "payment-service",
        FailureThreshold: 5,               // Open after 5 failures
        SuccessThreshold: 2,               // Close after 2 successes in half-open
        Timeout:          30 * time.Second, // Try again after 30s
        OnStateChange: func(name string, from, to circuitbreaker.State) {
            log.Printf("Circuit breaker %s: %v -> %v", name, from, to)
            metrics.CircuitBreakerState.WithLabelValues(name).Set(float64(to))
        },
    })

    return &PaymentClient{
        httpClient:     &http.Client{Timeout: 5 * time.Second},
        baseURL:        baseURL,
        circuitBreaker: cb,
    }
}

func (c *PaymentClient) ProcessPayment(ctx context.Context, req PaymentRequest) (*PaymentResponse, error) {
    var resp *PaymentResponse

    err := c.circuitBreaker.Execute(ctx, func() error {
        var err error
        resp, err = c.doRequest(ctx, req)
        return err
    })

    if errors.Is(err, circuitbreaker.ErrCircuitOpen) {
        // Return a meaningful error to the caller
        return nil, fmt.Errorf("payment service unavailable: %w", err)
    }

    return resp, err
}
```

## Advanced Patterns

### Per-Endpoint Circuit Breakers

Not all endpoints fail together. A slow `/payments` endpoint doesn't mean `/refunds` is broken:

```go
type PaymentClient struct {
    circuits map[string]*circuitbreaker.CircuitBreaker
}

func (c *PaymentClient) getCircuit(endpoint string) *circuitbreaker.CircuitBreaker {
    c.mu.Lock()
    defer c.mu.Unlock()

    if cb, ok := c.circuits[endpoint]; ok {
        return cb
    }

    cb := circuitbreaker.New(circuitbreaker.Config{
        Name:             fmt.Sprintf("payment-%s", endpoint),
        FailureThreshold: 5,
        SuccessThreshold: 2,
        Timeout:          30 * time.Second,
    })

    c.circuits[endpoint] = cb
    return cb
}
```

### Combining with Retries

Circuit breakers and retries can work together, but order matters:

```go
func (c *Client) DoWithResilience(ctx context.Context, fn func() error) error {
    // Retry wraps circuit breaker
    return c.retrier.Do(ctx, func() error {
        return c.circuitBreaker.Execute(ctx, fn)
    })
}
```

When the circuit is open, retries stop immediately—no wasted attempts.

### Fallback Strategies

When the circuit is open, what do you return?

```go
func (c *PaymentClient) ProcessPayment(ctx context.Context, req PaymentRequest) (*PaymentResponse, error) {
    resp, err := c.doWithCircuitBreaker(ctx, req)

    if errors.Is(err, circuitbreaker.ErrCircuitOpen) {
        // Option 1: Return cached/stale data if acceptable
        if cached, ok := c.cache.Get(req.OrderID); ok {
            return cached, nil
        }

        // Option 2: Queue for later processing
        if err := c.queue.Enqueue(req); err == nil {
            return &PaymentResponse{Status: "queued"}, nil
        }

        // Option 3: Degrade gracefully
        return nil, fmt.Errorf("payment service unavailable, please try again later")
    }

    return resp, err
}
```

## Monitoring and Alerting

Circuit breakers are useless if you don't know they're tripping:

```go
func setupMetrics() {
    // Prometheus metrics
    circuitState := prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "circuit_breaker_state",
            Help: "Current state of circuit breaker (0=closed, 1=open, 2=half-open)",
        },
        []string{"name"},
    )

    circuitTrips := prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "circuit_breaker_trips_total",
            Help: "Total number of times circuit breaker has tripped",
        },
        []string{"name"},
    )

    prometheus.MustRegister(circuitState, circuitTrips)
}
```

Alert on:

- Circuit opening (immediate notification)
- Circuit staying open longer than expected
- High trip frequency (indicates underlying issue)

## Key Takeaways

1. **Timeouts aren't enough**. They limit individual request duration but don't prevent cascade failures.

2. **Fail fast is a feature**. Returning an error immediately is better than waiting 30 seconds for an inevitable timeout.

3. **Circuit breakers protect both ways**. They protect your service from slow dependencies AND protect slow dependencies from being overwhelmed.

4. **Monitor your circuits**. A frequently-tripping circuit breaker is a symptom, not the problem.

5. **Have a fallback strategy**. What happens when the circuit is open? Cached data? Queue for later? Graceful error?

6. **Test failure scenarios**. Chaos engineering isn't optional for distributed systems.

That cascade failure taught us an expensive lesson. Circuit breakers turned a 2-hour outage into a 30-second degradation. Worth every line of code.
