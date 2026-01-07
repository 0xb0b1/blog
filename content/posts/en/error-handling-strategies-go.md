---
title: "Error Handling Strategies Beyond if err != nil"
date: 2025-06-22
description: "Structured errors, error wrapping done right, sentinel errors vs behavior checking, and patterns that make debugging production issues actually possible."
tags: ["golang", "best-practices", "error-handling", "patterns", "debugging"]
---

Go's error handling gets criticized as verbose, but the real problem isn't `if err != nil`—it's that most codebases handle errors without any strategy. Errors get swallowed, wrapped inconsistently, or logged multiple times. When something breaks in production, you're left guessing.

Here's how to build error handling that actually helps you debug.

## The Baseline Problem

Typical error handling in the wild:

```go
func ProcessOrder(ctx context.Context, orderID string) error {
    order, err := db.GetOrder(ctx, orderID)
    if err != nil {
        log.Printf("failed to get order: %v", err)
        return err
    }

    if err := validateOrder(order); err != nil {
        log.Printf("validation failed: %v", err)
        return err
    }

    if err := chargePayment(ctx, order); err != nil {
        log.Printf("payment failed: %v", err)
        return err
    }

    return nil
}
```

Problems:

1. Error logged at every level (log spam)
2. Context lost (which order? what user?)
3. No way to distinguish "order not found" from "database down"
4. Caller can't make decisions based on error type

## Strategy 1: Wrap Once, Log Once

Errors should be wrapped with context where they happen, then logged once at the top level.

```go
// WRONG: Log at every level
func getUser(ctx context.Context, id string) (*User, error) {
    user, err := db.Query(ctx, id)
    if err != nil {
        log.Printf("db query failed: %v", err) // Logged here
        return nil, err
    }
    return user, nil
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
    user, err := getUser(r.Context(), userID)
    if err != nil {
        log.Printf("getUser failed: %v", err) // And here (duplicate)
        http.Error(w, "error", 500)
        return
    }
}

// RIGHT: Wrap at each level, log at the top
func getUser(ctx context.Context, id string) (*User, error) {
    user, err := db.Query(ctx, id)
    if err != nil {
        return nil, fmt.Errorf("query user %s: %w", id, err)
    }
    return user, nil
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
    user, err := getUser(r.Context(), userID)
    if err != nil {
        log.Printf("handleRequest failed: %v", err) // Log once with full context
        http.Error(w, "error", 500)
        return
    }
}
// Log output: "handleRequest failed: query user abc123: connection refused"
```

## Strategy 2: Sentinel Errors vs. Behavior Checking

### Sentinel Errors (When to Use)

```go
var (
    ErrNotFound     = errors.New("not found")
    ErrUnauthorized = errors.New("unauthorized")
    ErrConflict     = errors.New("conflict")
)

func GetUser(ctx context.Context, id string) (*User, error) {
    user, err := db.Query(ctx, id)
    if err == sql.ErrNoRows {
        return nil, ErrNotFound
    }
    if err != nil {
        return nil, fmt.Errorf("query user: %w", err)
    }
    return user, nil
}

// Caller can check
user, err := GetUser(ctx, id)
if errors.Is(err, ErrNotFound) {
    // Handle not found (maybe return 404)
}
```

### Behavior Checking (Often Better)

Sentinel errors create coupling. Behavior checking is more flexible:

```go
// Define behavior interface
type NotFoundError interface {
    NotFound() bool
}

type userNotFoundError struct {
    userID string
}

func (e *userNotFoundError) Error() string {
    return fmt.Sprintf("user %s not found", e.userID)
}

func (e *userNotFoundError) NotFound() bool {
    return true
}

// Check behavior, not type
func IsNotFound(err error) bool {
    var nf NotFoundError
    return errors.As(err, &nf) && nf.NotFound()
}

// Usage
if IsNotFound(err) {
    w.WriteHeader(http.StatusNotFound)
    return
}
```

Why this is better:

- Different packages can return their own "not found" errors
- No import dependency on error definitions
- Works across package boundaries

## Strategy 3: Structured Errors

For complex systems, errors need structure:

```go
type AppError struct {
    Code    string            // Machine-readable code
    Message string            // Human-readable message
    Op      string            // Operation that failed
    Err     error             // Underlying error
    Meta    map[string]string // Additional context
}

func (e *AppError) Error() string {
    if e.Err != nil {
        return fmt.Sprintf("%s: %s: %v", e.Op, e.Message, e.Err)
    }
    return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

func (e *AppError) Unwrap() error {
    return e.Err
}

// Constructor helpers
func NewAppError(op, code, message string) *AppError {
    return &AppError{Op: op, Code: code, Message: message, Meta: make(map[string]string)}
}

func (e *AppError) WithError(err error) *AppError {
    e.Err = err
    return e
}

func (e *AppError) WithMeta(key, value string) *AppError {
    e.Meta[key] = value
    return e
}

// Usage
func ProcessPayment(ctx context.Context, orderID string, amount int) error {
    result, err := paymentGateway.Charge(ctx, amount)
    if err != nil {
        return NewAppError("ProcessPayment", "PAYMENT_FAILED", "charge failed").
            WithError(err).
            WithMeta("order_id", orderID).
            WithMeta("amount", strconv.Itoa(amount))
    }
    return nil
}
```

### Extracting Structured Data

```go
func handleError(w http.ResponseWriter, err error) {
    var appErr *AppError
    if errors.As(err, &appErr) {
        // Structured error - can extract details
        log.Printf("op=%s code=%s meta=%v err=%v",
            appErr.Op, appErr.Code, appErr.Meta, appErr.Err)

        status := mapCodeToHTTPStatus(appErr.Code)
        writeJSONError(w, status, appErr.Code, appErr.Message)
        return
    }

    // Unknown error - log and return generic message
    log.Printf("unhandled error: %v", err)
    http.Error(w, "internal error", http.StatusInternalServerError)
}

func mapCodeToHTTPStatus(code string) int {
    switch code {
    case "NOT_FOUND":
        return http.StatusNotFound
    case "UNAUTHORIZED":
        return http.StatusUnauthorized
    case "VALIDATION_FAILED":
        return http.StatusBadRequest
    default:
        return http.StatusInternalServerError
    }
}
```

## Strategy 4: Error Wrapping Done Right

### The %w Verb

Go 1.13+ introduced `%w` for error wrapping:

```go
// Creates a chain that errors.Is and errors.As can traverse
if err != nil {
    return fmt.Errorf("process order %s: %w", orderID, err)
}
```

### When NOT to Wrap

Sometimes you want to hide implementation details:

```go
// WRONG: Leaks internal error types
func GetUser(id string) (*User, error) {
    user, err := redis.Get(ctx, key)
    if err != nil {
        return nil, fmt.Errorf("get user: %w", err) // Exposes redis errors
    }
    return user, nil
}

// Caller can now do errors.Is(err, redis.ErrNil) - tight coupling!

// RIGHT: Translate to domain errors
func GetUser(id string) (*User, error) {
    user, err := redis.Get(ctx, key)
    if errors.Is(err, redis.ErrNil) {
        return nil, ErrUserNotFound // Domain error
    }
    if err != nil {
        return nil, fmt.Errorf("get user: %w", err)
    }
    return user, nil
}
```

### Error Chain Inspection

```go
func analyzeError(err error) {
    // errors.Is - checks if any error in chain matches
    if errors.Is(err, sql.ErrNoRows) {
        // Handle not found
    }

    // errors.As - extracts typed error from chain
    var appErr *AppError
    if errors.As(err, &appErr) {
        // Can access appErr.Code, appErr.Meta, etc.
    }

    // Unwrap - gets the next error in chain
    inner := errors.Unwrap(err)
}
```

## Strategy 5: Domain-Specific Error Types

Group errors by domain:

```go
// errors/payment.go
package errors

type PaymentError struct {
    Code          string
    Message       string
    TransactionID string
    Retryable     bool
    Err           error
}

func (e *PaymentError) Error() string {
    return fmt.Sprintf("payment error [%s]: %s", e.Code, e.Message)
}

func (e *PaymentError) Unwrap() error {
    return e.Err
}

// Behavior methods
func (e *PaymentError) IsRetryable() bool {
    return e.Retryable
}

// Common payment errors
func PaymentDeclined(txnID, reason string) *PaymentError {
    return &PaymentError{
        Code:          "DECLINED",
        Message:       reason,
        TransactionID: txnID,
        Retryable:     false,
    }
}

func PaymentTimeout(txnID string, err error) *PaymentError {
    return &PaymentError{
        Code:          "TIMEOUT",
        Message:       "payment gateway timeout",
        TransactionID: txnID,
        Retryable:     true,
        Err:           err,
    }
}
```

Usage:

```go
func processPayment(ctx context.Context, order Order) error {
    result, err := gateway.Charge(ctx, order.Amount)
    if err != nil {
        if isTimeout(err) {
            return errors.PaymentTimeout(order.ID, err)
        }
        return fmt.Errorf("charge failed: %w", err)
    }

    if !result.Approved {
        return errors.PaymentDeclined(result.TransactionID, result.Reason)
    }

    return nil
}

// Caller can make smart decisions
err := processPayment(ctx, order)
if err != nil {
    var payErr *errors.PaymentError
    if errors.As(err, &payErr) && payErr.IsRetryable() {
        return retryWithBackoff(ctx, func() error {
            return processPayment(ctx, order)
        })
    }
    return err
}
```

## Strategy 6: Error Aggregation

Sometimes you need to collect multiple errors:

```go
type MultiError struct {
    errors []error
}

func (m *MultiError) Add(err error) {
    if err != nil {
        m.errors = append(m.errors, err)
    }
}

func (m *MultiError) Error() string {
    if len(m.errors) == 0 {
        return ""
    }
    if len(m.errors) == 1 {
        return m.errors[0].Error()
    }

    var b strings.Builder
    fmt.Fprintf(&b, "%d errors occurred:\n", len(m.errors))
    for i, err := range m.errors {
        fmt.Fprintf(&b, "  %d: %v\n", i+1, err)
    }
    return b.String()
}

func (m *MultiError) ErrorOrNil() error {
    if len(m.errors) == 0 {
        return nil
    }
    return m
}

// Go 1.20+ has errors.Join for this
func validateOrder(order Order) error {
    var errs []error

    if order.CustomerID == "" {
        errs = append(errs, fmt.Errorf("customer ID required"))
    }
    if order.Amount <= 0 {
        errs = append(errs, fmt.Errorf("amount must be positive"))
    }
    if len(order.Items) == 0 {
        errs = append(errs, fmt.Errorf("order must have items"))
    }

    return errors.Join(errs...) // nil if errs is empty
}
```

## Production Checklist

1. **Wrap errors with context** using `fmt.Errorf("operation: %w", err)`

2. **Log once at the top level**, not at every layer

3. **Use sentinel errors sparingly**. Behavior checking is often more flexible.

4. **Don't expose implementation details** through error wrapping unless intentional

5. **Make errors actionable**. Include enough context to debug without requiring code access.

6. **Consider error types for complex domains**. Structured errors enable smart handling.

7. **Test error paths**. Your tests should verify error messages and types.

## Key Takeaways

1. **Error handling is not just `if err != nil`**. It's about building a strategy that helps you debug production issues.

2. **Wrap once, log once**. Duplicate logging wastes time and obscures the real problem.

3. **Use `%w` for wrapping**, but know when NOT to wrap (hiding implementation details).

4. **Behavior checking > type checking**. `errors.As` with interfaces is more flexible than sentinel errors.

5. **Structure enables automation**. Structured errors can drive HTTP status codes, metrics, and alerting.

6. **Domain errors clarify intent**. `PaymentDeclined` is clearer than `errors.New("payment failed")`.

The goal isn't perfect error handling—it's error handling that helps you figure out what went wrong at 3 AM when production is down.
