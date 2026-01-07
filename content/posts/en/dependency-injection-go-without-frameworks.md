---
title: "Dependency Injection in Go Without Frameworks"
date: 2025-05-14
description: "Manual DI, functional options, Wire code generation, and why Go doesn't need Spring-style containers. Practical patterns for building testable, maintainable applications."
tags:
  ["golang", "dependency-injection", "architecture", "patterns", "testing"]
---

Coming from Java or C#, you might reach for a DI container in Go. Don't. Go's simplicity makes manual dependency injection not just viable but preferable. Frameworks like Spring solve problems that Go doesn't have.

Here's how to do DI in Go the idiomatic way.

## Why Go Doesn't Need DI Containers

In Java, you need DI containers because:
- Constructors are verbose (no named parameters)
- No first-class functions
- Annotation-driven configuration is the norm
- Complex object graphs with lifecycle management

Go has none of these problems:
- Struct literals with named fields
- First-class functions
- Explicit is better than magic
- Simple object lifecycles (create, use, done)

The Go philosophy: if you can see the code, you can understand it. DI containers hide the wiring.

## Pattern 1: Constructor Injection

The foundation of DI in Go. Dependencies go in the constructor.

```go
// repository.go
type UserRepository struct {
    db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
    return &UserRepository{db: db}
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*User, error) {
    // Uses r.db
}

// service.go
type UserService struct {
    repo   *UserRepository
    cache  Cache
    mailer Mailer
}

func NewUserService(repo *UserRepository, cache Cache, mailer Mailer) *UserService {
    return &UserService{
        repo:   repo,
        cache:  cache,
        mailer: mailer,
    }
}
```

### Main as Composition Root

All wiring happens in `main()`. This is your composition root—the one place where you see how everything connects.

```go
// main.go
func main() {
    // Infrastructure
    db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    redisClient := redis.NewClient(&redis.Options{
        Addr: os.Getenv("REDIS_URL"),
    })
    defer redisClient.Close()

    smtpClient := smtp.NewClient(os.Getenv("SMTP_HOST"))

    // Repositories
    userRepo := NewUserRepository(db)
    orderRepo := NewOrderRepository(db)

    // Services
    cache := NewRedisCache(redisClient)
    mailer := NewSMTPMailer(smtpClient)

    userService := NewUserService(userRepo, cache, mailer)
    orderService := NewOrderService(orderRepo, userService)

    // HTTP handlers
    userHandler := NewUserHandler(userService)
    orderHandler := NewOrderHandler(orderService)

    // Router
    mux := http.NewServeMux()
    mux.Handle("/users", userHandler)
    mux.Handle("/orders", orderHandler)

    log.Fatal(http.ListenAndServe(":8080", mux))
}
```

Everything is explicit. No magic, no reflection, no XML files. Open `main.go` and you see exactly how your app is wired.

## Pattern 2: Interface Segregation

Define interfaces where they're consumed, not where they're implemented. This is key to testable Go code.

```go
// WRONG: Big interface defined by the implementer
// user/repository.go
type Repository interface {
    GetByID(ctx context.Context, id string) (*User, error)
    GetByEmail(ctx context.Context, email string) (*User, error)
    Create(ctx context.Context, user *User) error
    Update(ctx context.Context, user *User) error
    Delete(ctx context.Context, id string) error
    List(ctx context.Context, filter Filter) ([]*User, error)
    Count(ctx context.Context) (int, error)
}

// GOOD: Small interface defined by the consumer
// auth/service.go
type UserGetter interface {
    GetByEmail(ctx context.Context, email string) (*User, error)
}

type AuthService struct {
    users UserGetter // Only needs one method
}

func NewAuthService(users UserGetter) *AuthService {
    return &AuthService{users: users}
}
```

Benefits:
- Easy to mock in tests (one method to implement)
- Clear dependencies (you see exactly what's needed)
- Decoupled packages (no shared interface definitions)

```go
// auth/service_test.go
type mockUserGetter struct {
    user *User
    err  error
}

func (m *mockUserGetter) GetByEmail(ctx context.Context, email string) (*User, error) {
    return m.user, m.err
}

func TestAuthService_Login(t *testing.T) {
    mock := &mockUserGetter{
        user: &User{ID: "123", Email: "test@example.com"},
    }

    svc := NewAuthService(mock)
    // Test...
}
```

## Pattern 3: Functional Options

When constructors have many optional parameters, use functional options.

```go
type Server struct {
    host         string
    port         int
    timeout      time.Duration
    maxConns     int
    logger       Logger
    metrics      Metrics
    tlsConfig    *tls.Config
}

// Option is a function that configures Server
type Option func(*Server)

func WithHost(host string) Option {
    return func(s *Server) {
        s.host = host
    }
}

func WithPort(port int) Option {
    return func(s *Server) {
        s.port = port
    }
}

func WithTimeout(d time.Duration) Option {
    return func(s *Server) {
        s.timeout = d
    }
}

func WithLogger(l Logger) Option {
    return func(s *Server) {
        s.logger = l
    }
}

func WithTLS(config *tls.Config) Option {
    return func(s *Server) {
        s.tlsConfig = config
    }
}

func NewServer(opts ...Option) *Server {
    // Defaults
    s := &Server{
        host:     "localhost",
        port:     8080,
        timeout:  30 * time.Second,
        maxConns: 100,
        logger:   defaultLogger,
        metrics:  noopMetrics,
    }

    // Apply options
    for _, opt := range opts {
        opt(s)
    }

    return s
}

// Usage
server := NewServer(
    WithHost("0.0.0.0"),
    WithPort(443),
    WithTLS(tlsConfig),
    WithLogger(zapLogger),
)
```

### When to Use Functional Options

- **Many optional parameters** (3+)
- **Sensible defaults** exist for most parameters
- **Public API** where backward compatibility matters
- **Builder-like configuration** where order doesn't matter

Don't use for:
- Simple constructors with 1-3 required parameters
- Internal code where readability beats flexibility

## Pattern 4: Config Structs

For complex configuration, a config struct is clearer than many options:

```go
type DatabaseConfig struct {
    Host            string
    Port            int
    Database        string
    Username        string
    Password        string
    MaxOpenConns    int
    MaxIdleConns    int
    ConnMaxLifetime time.Duration
}

func NewDatabase(cfg DatabaseConfig) (*sql.DB, error) {
    dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
        cfg.Host, cfg.Port, cfg.Username, cfg.Password, cfg.Database)

    db, err := sql.Open("postgres", dsn)
    if err != nil {
        return nil, err
    }

    db.SetMaxOpenConns(cfg.MaxOpenConns)
    db.SetMaxIdleConns(cfg.MaxIdleConns)
    db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

    return db, nil
}

// Usage
db, err := NewDatabase(DatabaseConfig{
    Host:            "localhost",
    Port:            5432,
    Database:        "myapp",
    Username:        "app",
    Password:        os.Getenv("DB_PASSWORD"),
    MaxOpenConns:    25,
    MaxIdleConns:    5,
    ConnMaxLifetime: 5 * time.Minute,
})
```

Config structs work well with environment parsing:

```go
func LoadDatabaseConfig() DatabaseConfig {
    return DatabaseConfig{
        Host:         env.GetString("DB_HOST", "localhost"),
        Port:         env.GetInt("DB_PORT", 5432),
        Database:     env.GetString("DB_NAME", "myapp"),
        Username:     env.GetString("DB_USER", "postgres"),
        Password:     env.GetString("DB_PASSWORD", ""),
        MaxOpenConns: env.GetInt("DB_MAX_OPEN_CONNS", 25),
    }
}
```

## Pattern 5: Wire for Large Applications

For applications with many dependencies, Google's Wire generates the wiring code.

```go
// wire.go
//go:build wireinject

package main

import "github.com/google/wire"

func InitializeApp() (*App, error) {
    wire.Build(
        // Infrastructure
        NewDatabase,
        NewRedisClient,

        // Repositories
        NewUserRepository,
        NewOrderRepository,

        // Services
        NewUserService,
        NewOrderService,

        // Handlers
        NewUserHandler,
        NewOrderHandler,

        // App
        NewApp,
    )
    return nil, nil
}
```

Run `wire` and it generates `wire_gen.go`:

```go
// wire_gen.go (generated)
func InitializeApp() (*App, error) {
    db, err := NewDatabase()
    if err != nil {
        return nil, err
    }
    redisClient := NewRedisClient()
    userRepository := NewUserRepository(db)
    orderRepository := NewOrderRepository(db)
    userService := NewUserService(userRepository)
    orderService := NewOrderService(orderRepository, userService)
    userHandler := NewUserHandler(userService)
    orderHandler := NewOrderHandler(orderService)
    app := NewApp(userHandler, orderHandler)
    return app, nil
}
```

### Provider Sets for Organization

Group related providers:

```go
var DatabaseSet = wire.NewSet(
    NewDatabase,
    NewUserRepository,
    NewOrderRepository,
)

var ServiceSet = wire.NewSet(
    NewUserService,
    NewOrderService,
    NewPaymentService,
)

var HandlerSet = wire.NewSet(
    NewUserHandler,
    NewOrderHandler,
)

func InitializeApp() (*App, error) {
    wire.Build(
        DatabaseSet,
        ServiceSet,
        HandlerSet,
        NewApp,
    )
    return nil, nil
}
```

### When to Use Wire

- **Large applications** with 20+ injectable dependencies
- **Team projects** where consistent wiring matters
- **Compile-time safety** is important (Wire fails at compile time if wiring is wrong)

Don't use for:
- Small to medium applications
- Learning projects
- When explicit wiring in `main()` is still manageable

## Anti-Patterns to Avoid

### 1. Global Variables

```go
// WRONG: Global state
var db *sql.DB
var userRepo *UserRepository

func init() {
    db, _ = sql.Open("postgres", os.Getenv("DATABASE_URL"))
    userRepo = NewUserRepository(db)
}

func GetUser(id string) (*User, error) {
    return userRepo.GetByID(context.Background(), id)
}

// RIGHT: Inject dependencies
type Handler struct {
    users *UserRepository
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
    user, err := h.users.GetByID(r.Context(), id)
    // ...
}
```

### 2. Service Locator

```go
// WRONG: Service locator pattern
type Container struct {
    services map[string]interface{}
}

func (c *Container) Get(name string) interface{} {
    return c.services[name]
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    userService := container.Get("userService").(*UserService) // Runtime error if missing
    // ...
}

// RIGHT: Explicit dependencies
type Handler struct {
    users *UserService
}

func NewHandler(users *UserService) *Handler {
    return &Handler{users: users} // Compile-time error if missing
}
```

### 3. Hidden Dependencies

```go
// WRONG: Hidden dependency on time.Now
func (s *Service) CreateOrder(userID string) (*Order, error) {
    return &Order{
        ID:        uuid.New().String(),
        UserID:    userID,
        CreatedAt: time.Now(), // Hidden dependency, hard to test
    }
}

// RIGHT: Inject time function
type Service struct {
    now func() time.Time
}

func NewService(now func() time.Time) *Service {
    if now == nil {
        now = time.Now
    }
    return &Service{now: now}
}

func (s *Service) CreateOrder(userID string) (*Order, error) {
    return &Order{
        ID:        uuid.New().String(),
        UserID:    userID,
        CreatedAt: s.now(), // Testable!
    }
}
```

## Testing with DI

The payoff of proper DI is easy testing:

```go
func TestOrderService_Create(t *testing.T) {
    // Stub dependencies
    userGetter := &stubUserGetter{
        user: &User{ID: "user-1", Name: "Test"},
    }
    orderRepo := &stubOrderRepo{}
    nowFunc := func() time.Time {
        return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
    }

    // Create service with test dependencies
    svc := NewOrderService(userGetter, orderRepo, nowFunc)

    // Test
    order, err := svc.Create(context.Background(), CreateOrderRequest{
        UserID: "user-1",
        Items:  []Item{{SKU: "ABC", Qty: 2}},
    })

    require.NoError(t, err)
    assert.Equal(t, "user-1", order.UserID)
    assert.Equal(t, nowFunc(), order.CreatedAt)
}
```

## Choosing Your Approach

- **Manual DI**: Small-medium apps, clear dependencies, full control
- **Functional Options**: Many optional params, public APIs, libraries
- **Config Structs**: Complex configuration, environment-driven setup
- **Wire**: Large apps (20+ deps), team projects, compile-time safety

## Key Takeaways

1. **Go doesn't need DI containers**. Manual wiring is explicit, debuggable, and fast.

2. **Main is your composition root**. All wiring happens there—one place to see the whole picture.

3. **Define interfaces at the consumer**. Small interfaces are easy to mock and reduce coupling.

4. **Functional options for optional params**. Great for public APIs, overkill for simple constructors.

5. **Wire for large applications**. Generates correct wiring code, catches errors at compile time.

6. **Avoid globals and service locators**. They hide dependencies and make testing hard.

7. **Inject everything testable**. Time, randomness, external services—if you might want to control it in tests, inject it.

The best DI in Go is no framework at all. Just constructors, interfaces, and explicit wiring in main. When that gets unwieldy, Wire generates the boilerplate while keeping everything explicit.
