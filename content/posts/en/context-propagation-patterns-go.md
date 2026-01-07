---
title: "Context Propagation Patterns in Large Go Codebases"
date: 2025-02-16
description: "When to pass context, when not to, and the common mistakes that lead to leaked goroutines, missed cancellations, and debugging nightmares."
tags: ["golang", "best-practices", "concurrency", "architecture", "patterns"]
---

"Always pass context as the first argument" is Go wisdom that gets repeated without explanation. But in large codebases, context propagation becomes surprisingly nuanced. When should you use `context.Background()`? What goes in context values? Why is my goroutine not cancelling?

Let's dig into the patterns that actually work.

## The Basics (And Why They're Not Enough)

The standard advice:

```go
func DoSomething(ctx context.Context, args Args) error {
    // Use ctx for cancellation, deadlines, and request-scoped values
}
```

This works until you have:

- Background workers that shouldn't be cancelled by request context
- Graceful shutdown that needs its own cancellation
- Nested goroutines with complex lifecycles
- Values that should (or shouldn't) cross goroutine boundaries

## Pattern 1: Request Context vs. Application Context

The most common mistake is using request context for everything:

```go
// WRONG: Background task inherits request cancellation
func (h *Handler) ProcessOrder(w http.ResponseWriter, r *http.Request) {
    order := parseOrder(r)

    // This goroutine dies when the HTTP request completes!
    go h.sendNotification(r.Context(), order)

    w.WriteHeader(http.StatusAccepted)
}
```

The fix is understanding different context scopes:

```go
func (h *Handler) ProcessOrder(w http.ResponseWriter, r *http.Request) {
    order := parseOrder(r)

    // Create a new context for background work
    // Inherit values (trace ID, etc.) but not cancellation
    bgCtx := detachContext(r.Context())

    go h.sendNotification(bgCtx, order)

    w.WriteHeader(http.StatusAccepted)
}

// detachContext creates a new context that inherits values but not cancellation
func detachContext(ctx context.Context) context.Context {
    // Create new context with application-level cancellation
    return contextWithValues(context.Background(), ctx)
}

func contextWithValues(dst, src context.Context) context.Context {
    // Copy relevant values from src to dst
    if traceID := src.Value(traceIDKey); traceID != nil {
        dst = context.WithValue(dst, traceIDKey, traceID)
    }
    if requestID := src.Value(requestIDKey); requestID != nil {
        dst = context.WithValue(dst, requestIDKey, requestID)
    }
    return dst
}
```

## Pattern 2: Graceful Shutdown Context

Your application needs its own cancellation hierarchy:

```go
type Application struct {
    ctx    context.Context
    cancel context.CancelFunc
}

func NewApplication() *Application {
    ctx, cancel := context.WithCancel(context.Background())
    return &Application{ctx: ctx, cancel: cancel}
}

func (a *Application) Run() error {
    // All long-running goroutines use application context
    go a.backgroundWorker(a.ctx)
    go a.metricsReporter(a.ctx)

    // HTTP server gets a derived context
    return a.httpServer.Run(a.ctx)
}

func (a *Application) Shutdown() {
    // Cancelling app context stops everything
    a.cancel()
}

// HTTP handlers create request contexts derived from app context
func (a *Application) handleRequest(w http.ResponseWriter, r *http.Request) {
    // Request context inherits app cancellation
    ctx := r.Context()

    // If app is shutting down, this context is already cancelled
    select {
    case <-ctx.Done():
        http.Error(w, "Server shutting down", http.StatusServiceUnavailable)
        return
    default:
    }

    // Normal processing...
}
```

## Pattern 3: Context Values Done Right

Context values are often misused. Here's what belongs in context vs. function parameters:

```go
// BAD: Business logic in context
func ProcessOrder(ctx context.Context) error {
    order := ctx.Value("order").(Order)  // Type assertion panic waiting to happen
    user := ctx.Value("user").(User)     // Hidden dependencies
    // ...
}

// GOOD: Context for cross-cutting concerns only
func ProcessOrder(ctx context.Context, order Order, user User) error {
    // Trace ID, request ID, auth token - these are cross-cutting
    traceID := tracing.TraceIDFromContext(ctx)
    log.Info("Processing order", "trace_id", traceID, "order_id", order.ID)
    // ...
}
```

### Type-Safe Context Keys

```go
// Define unexported key types to prevent collisions
type contextKey int

const (
    traceIDKey contextKey = iota
    requestIDKey
    userClaimsKey
)

// Provide typed accessors
func TraceIDFromContext(ctx context.Context) string {
    if v := ctx.Value(traceIDKey); v != nil {
        return v.(string)
    }
    return ""
}

func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
    return context.WithValue(ctx, traceIDKey, traceID)
}
```

## Pattern 4: Timeout Hierarchies

Nested operations need coordinated timeouts:

```go
func (s *Service) ProcessWithTimeout(ctx context.Context) error {
    // Overall operation timeout
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    // Step 1: Database query (should be fast)
    dbCtx, dbCancel := context.WithTimeout(ctx, 5*time.Second)
    defer dbCancel()

    data, err := s.db.Query(dbCtx, query)
    if err != nil {
        return fmt.Errorf("database query: %w", err)
    }

    // Step 2: External API (slower)
    // Remaining time from parent context
    apiCtx, apiCancel := context.WithTimeout(ctx, 20*time.Second)
    defer apiCancel()

    result, err := s.api.Call(apiCtx, data)
    if err != nil {
        return fmt.Errorf("api call: %w", err)
    }

    return s.save(ctx, result)
}
```

**Key insight**: Child timeouts should be shorter than parent. If parent has 30s remaining and child needs 25s, child might get cancelled by parent before its own timeout.

## Pattern 5: Goroutine Lifecycle Management

The classic leaked goroutine:

```go
// WRONG: Goroutine never terminates
func (w *Worker) Start() {
    go func() {
        for {
            w.process()
            time.Sleep(time.Second)
        }
    }()
}

// RIGHT: Context-controlled lifecycle
func (w *Worker) Start(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(time.Second)
        defer ticker.Stop()

        for {
            select {
            case <-ctx.Done():
                log.Info("Worker shutting down")
                return
            case <-ticker.C:
                w.process(ctx)
            }
        }
    }()
}
```

### Waiting for Goroutines

```go
type WorkerPool struct {
    wg sync.WaitGroup
}

func (p *WorkerPool) StartWorker(ctx context.Context, id int) {
    p.wg.Add(1)
    go func() {
        defer p.wg.Done()

        for {
            select {
            case <-ctx.Done():
                return
            default:
                p.doWork(ctx)
            }
        }
    }()
}

func (p *WorkerPool) Shutdown(ctx context.Context) error {
    // Wait for all workers with timeout
    done := make(chan struct{})
    go func() {
        p.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

## Pattern 6: Database Connection Context

Database operations have their own context considerations:

```go
func (r *Repository) GetUser(ctx context.Context, id string) (*User, error) {
    // Query respects context cancellation
    row := r.db.QueryRowContext(ctx, "SELECT * FROM users WHERE id = $1", id)

    var user User
    if err := row.Scan(&user.ID, &user.Name); err != nil {
        return nil, err
    }
    return &user, nil
}

// But transactions need careful handling
func (r *Repository) TransferFunds(ctx context.Context, from, to string, amount int) error {
    tx, err := r.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }

    // Ensure cleanup even if context is cancelled
    defer func() {
        if err != nil {
            tx.Rollback() // Rollback ignores context cancellation
        }
    }()

    // Operations within transaction
    if err = r.debit(ctx, tx, from, amount); err != nil {
        return err
    }
    if err = r.credit(ctx, tx, to, amount); err != nil {
        return err
    }

    return tx.Commit()
}
```

## Common Mistakes

### 1. Using context.Background() Everywhere

```go
// BAD: Ignores cancellation signals
func (s *Service) DoWork() {
    s.db.Query(context.Background(), query)  // Won't cancel on shutdown
}

// GOOD: Propagate context
func (s *Service) DoWork(ctx context.Context) {
    s.db.Query(ctx, query)
}
```

### 2. Storing Context in Structs

```go
// BAD: Context stored in struct
type Service struct {
    ctx context.Context  // Don't do this
}

// GOOD: Pass context to methods
type Service struct{}

func (s *Service) Process(ctx context.Context) error {
    // Use ctx here
}
```

### 3. Not Checking ctx.Done()

```go
// BAD: Long loop ignores cancellation
func processItems(ctx context.Context, items []Item) {
    for _, item := range items {
        process(item)  // Context cancellation ignored
    }
}

// GOOD: Check cancellation periodically
func processItems(ctx context.Context, items []Item) error {
    for _, item := range items {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            process(item)
        }
    }
    return nil
}
```

## Key Takeaways

1. **Request context ≠ application context**. Background tasks should not inherit request cancellation.

2. **Context values are for cross-cutting concerns**: trace IDs, request IDs, auth claims. Not business data.

3. **Type-safe context keys** prevent collisions and make dependencies explicit.

4. **Child timeouts should be shorter than parent**. Otherwise, parent might cancel before child's timeout.

5. **Always check ctx.Done()** in long-running loops.

6. **Don't store context in structs**. Pass it as the first parameter.

7. **Use defer cancel()** to prevent context leaks.

Context propagation seems simple until your codebase grows. Get these patterns right early, and debugging concurrent code becomes much easier.
