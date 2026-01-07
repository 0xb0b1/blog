---
title: "Writing Testable Go Code Without Mocking Everything"
date: 2025-10-01
description: "Real implementations over mocks, test fixtures that work, and design patterns that make code testable without generating mock files for every interface."
tags: ["golang", "testing", "best-practices", "architecture", "patterns"]
---

Go projects often end up with more mock files than actual code. Every interface gets a generated mock, tests become tightly coupled to implementation details, and refactoring breaks dozens of test files.

There's a better way. Here's how to write testable code without reaching for mocks by default.

## The Mock Explosion Problem

Typical enterprise Go project:

```
service/
  user_service.go
  user_service_test.go
  mock_user_repository.go
  mock_notification_service.go
  mock_cache.go
  mock_logger.go
  mock_metrics.go
```

Every dependency gets mocked. Tests verify mock interactions rather than behavior. And when you change how something works internally, all those mock expectations break.

## Strategy 1: Real Implementations First

Before mocking, ask: can I use the real thing?

### SQLite for Database Tests

```go
// test_helpers.go
func TestDB(t *testing.T) *sql.DB {
    t.Helper()

    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil {
        t.Fatalf("open db: %v", err)
    }

    // Run migrations
    if err := migrate(db); err != nil {
        t.Fatalf("migrate: %v", err)
    }

    t.Cleanup(func() {
        db.Close()
    })

    return db
}

// user_repository_test.go
func TestUserRepository_Create(t *testing.T) {
    db := TestDB(t)
    repo := NewUserRepository(db)

    user := &User{Name: "test", Email: "test@example.com"}
    err := repo.Create(context.Background(), user)

    require.NoError(t, err)
    assert.NotEmpty(t, user.ID)

    // Verify it was actually persisted
    found, err := repo.GetByID(context.Background(), user.ID)
    require.NoError(t, err)
    assert.Equal(t, user.Name, found.Name)
}
```

Benefits:

- Tests actual SQL queries
- Catches schema issues
- No mock maintenance
- Fast enough for unit tests (in-memory SQLite)

### Redis with miniredis

```go
import "github.com/alicebob/miniredis/v2"

func TestCache(t *testing.T) *redis.Client {
    t.Helper()

    s := miniredis.RunT(t)

    return redis.NewClient(&redis.Options{
        Addr: s.Addr(),
    })
}

func TestUserCache_Get(t *testing.T) {
    client := TestCache(t)
    cache := NewUserCache(client)

    // Test actual Redis operations
    user := &User{ID: "123", Name: "test"}
    err := cache.Set(context.Background(), user)
    require.NoError(t, err)

    found, err := cache.Get(context.Background(), "123")
    require.NoError(t, err)
    assert.Equal(t, user.Name, found.Name)
}
```

### HTTP with httptest

```go
func TestPaymentClient_Charge(t *testing.T) {
    // Real HTTP server, not mocks
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Verify request
        assert.Equal(t, "POST", r.Method)
        assert.Equal(t, "/v1/charges", r.URL.Path)

        var req ChargeRequest
        json.NewDecoder(r.Body).Decode(&req)
        assert.Equal(t, 1000, req.Amount)

        // Return response
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(ChargeResponse{
            ID:     "ch_123",
            Status: "succeeded",
        })
    }))
    defer server.Close()

    client := NewPaymentClient(server.URL)
    resp, err := client.Charge(context.Background(), 1000)

    require.NoError(t, err)
    assert.Equal(t, "succeeded", resp.Status)
}
```

## Strategy 2: Tiny Interfaces

Big interfaces lead to big mocks. Instead, define the smallest interface you need:

```go
// BAD: Huge interface, huge mock
type UserRepository interface {
    Create(ctx context.Context, user *User) error
    Update(ctx context.Context, user *User) error
    Delete(ctx context.Context, id string) error
    GetByID(ctx context.Context, id string) (*User, error)
    GetByEmail(ctx context.Context, email string) (*User, error)
    List(ctx context.Context, filter Filter) ([]*User, error)
    Count(ctx context.Context) (int, error)
    // ... 20 more methods
}

// GOOD: Define interface where it's used
// In auth package:
type UserGetter interface {
    GetByEmail(ctx context.Context, email string) (*User, error)
}

func NewAuthService(users UserGetter) *AuthService {
    return &AuthService{users: users}
}

// Now tests only need to implement one method
type stubUserGetter struct {
    user *User
    err  error
}

func (s *stubUserGetter) GetByEmail(ctx context.Context, email string) (*User, error) {
    return s.user, s.err
}
```

This is called the "Interface Segregation Principle" but in Go it happens naturally when you define interfaces at the point of use.

## Strategy 3: Functional Options for Dependencies

Instead of interface dependencies, sometimes a function is enough:

```go
type OrderService struct {
    generateID func() string
    now        func() time.Time
    notify     func(ctx context.Context, userID, message string) error
}

type OrderOption func(*OrderService)

func WithIDGenerator(fn func() string) OrderOption {
    return func(s *OrderService) {
        s.generateID = fn
    }
}

func WithClock(fn func() time.Time) OrderOption {
    return func(s *OrderService) {
        s.now = fn
    }
}

func NewOrderService(opts ...OrderOption) *OrderService {
    s := &OrderService{
        generateID: uuid.NewString,
        now:        time.Now,
        notify:     defaultNotify,
    }
    for _, opt := range opts {
        opt(s)
    }
    return s
}

// Tests can inject simple functions
func TestOrderService_Create(t *testing.T) {
    fixedTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
    notified := false

    svc := NewOrderService(
        WithIDGenerator(func() string { return "order-123" }),
        WithClock(func() time.Time { return fixedTime }),
        WithNotifier(func(ctx context.Context, userID, msg string) error {
            notified = true
            return nil
        }),
    )

    order, err := svc.Create(context.Background(), CreateOrderRequest{})

    require.NoError(t, err)
    assert.Equal(t, "order-123", order.ID)
    assert.Equal(t, fixedTime, order.CreatedAt)
    assert.True(t, notified)
}
```

## Strategy 4: Test Fixtures Over Factories

Instead of constructing test data inline, use fixtures:

```go
// testdata/fixtures.go
package testdata

var (
    ValidUser = &User{
        ID:    "user-123",
        Name:  "Test User",
        Email: "test@example.com",
    }

    AdminUser = &User{
        ID:    "admin-456",
        Name:  "Admin",
        Email: "admin@example.com",
        Role:  RoleAdmin,
    }

    ExpiredSubscription = &Subscription{
        ID:        "sub-789",
        UserID:    "user-123",
        ExpiresAt: time.Now().Add(-24 * time.Hour),
    }
)

// Builder for complex objects
type UserBuilder struct {
    user *User
}

func NewUserBuilder() *UserBuilder {
    return &UserBuilder{
        user: &User{
            ID:    uuid.NewString(),
            Name:  "Test User",
            Email: "test@example.com",
            Role:  RoleUser,
        },
    }
}

func (b *UserBuilder) WithName(name string) *UserBuilder {
    b.user.Name = name
    return b
}

func (b *UserBuilder) WithRole(role Role) *UserBuilder {
    b.user.Role = role
    return b
}

func (b *UserBuilder) Build() *User {
    return b.user
}
```

Usage:

```go
func TestPermissions(t *testing.T) {
    admin := testdata.NewUserBuilder().
        WithRole(RoleAdmin).
        Build()

    regular := testdata.NewUserBuilder().
        WithRole(RoleUser).
        Build()

    assert.True(t, CanDeleteUsers(admin))
    assert.False(t, CanDeleteUsers(regular))
}
```

## Strategy 5: Table-Driven Tests Without Mocks

Table-driven tests work great with stubs:

```go
func TestValidateEmail(t *testing.T) {
    tests := []struct {
        name    string
        email   string
        wantErr bool
    }{
        {"valid email", "test@example.com", false},
        {"missing @", "testexample.com", true},
        {"missing domain", "test@", true},
        {"empty", "", true},
        {"unicode", "tëst@example.com", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateEmail(tt.email)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}

// For more complex cases with dependencies
func TestOrderProcessor(t *testing.T) {
    tests := []struct {
        name          string
        order         Order
        inventory     map[string]int // item -> quantity available
        expectedError string
    }{
        {
            name:      "sufficient inventory",
            order:     Order{Items: []Item{{SKU: "A", Qty: 2}}},
            inventory: map[string]int{"A": 10},
        },
        {
            name:          "insufficient inventory",
            order:         Order{Items: []Item{{SKU: "A", Qty: 10}}},
            inventory:     map[string]int{"A": 5},
            expectedError: "insufficient inventory",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Simple stub, not a mock
            inventoryCheck := func(sku string, qty int) bool {
                return tt.inventory[sku] >= qty
            }

            processor := NewOrderProcessor(inventoryCheck)
            err := processor.Process(tt.order)

            if tt.expectedError != "" {
                assert.ErrorContains(t, err, tt.expectedError)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

## Strategy 6: Integration Tests with Docker

For things that are hard to fake, use the real thing with testcontainers:

```go
import "github.com/testcontainers/testcontainers-go"

func TestWithPostgres(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    ctx := context.Background()

    postgres, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image:        "postgres:15",
            ExposedPorts: []string{"5432/tcp"},
            Env: map[string]string{
                "POSTGRES_PASSWORD": "test",
                "POSTGRES_DB":       "testdb",
            },
            WaitingFor: wait.ForLog("database system is ready to accept connections"),
        },
        Started: true,
    })
    require.NoError(t, err)
    defer postgres.Terminate(ctx)

    host, _ := postgres.Host(ctx)
    port, _ := postgres.MappedPort(ctx, "5432")

    db, err := sql.Open("postgres", fmt.Sprintf(
        "postgres://postgres:test@%s:%s/testdb?sslmode=disable",
        host, port.Port(),
    ))
    require.NoError(t, err)

    // Now test with real Postgres
    repo := NewUserRepository(db)
    // ...
}
```

## When Mocks Are Actually Useful

Mocks aren't evil—they're just overused. They're good for:

1. **External services you don't control** (third-party APIs)
2. **Verifying interactions** when the interaction IS the behavior
3. **Simulating failures** that are hard to trigger naturally

```go
// Good use of mock: verifying an email was sent
func TestOrderService_SendsConfirmation(t *testing.T) {
    var sentTo string
    var sentSubject string

    emailer := &stubEmailer{
        sendFunc: func(to, subject, body string) error {
            sentTo = to
            sentSubject = subject
            return nil
        },
    }

    svc := NewOrderService(emailer)
    svc.Complete(context.Background(), order)

    assert.Equal(t, "customer@example.com", sentTo)
    assert.Contains(t, sentSubject, "Order Confirmation")
}
```

## Key Takeaways

1. **Real implementations first**. SQLite, miniredis, httptest—use them before reaching for mocks.

2. **Tiny interfaces**. Define interfaces where they're used, with only the methods needed.

3. **Functions over interfaces** when you only need one method.

4. **Fixtures over factories** for consistent test data.

5. **Table-driven tests** reduce duplication and make edge cases obvious.

6. **Integration tests with containers** for things that can't be faked.

7. **Mock only what you can't control** or when verifying interactions is the actual requirement.

The goal isn't zero mocks—it's tests that verify behavior, not implementation details. When tests break only because behavior changed (not because you refactored internals), you know you're on the right track.
