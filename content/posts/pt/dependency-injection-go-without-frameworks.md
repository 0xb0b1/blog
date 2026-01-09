---
title: "Injeção de Dependência em Go Sem Frameworks"
date: 2025-05-14
description: "DI manual, functional options, geração de código com Wire, e por que Go não precisa de containers estilo Spring. Padrões práticos para construir aplicações testáveis e manuteníveis."
tags:
  ["golang", "injecao-de-dependencia", "arquitetura", "padroes", "testes"]
---

Vindo de Java ou C#, você pode querer usar um container de DI em Go. Não faça isso. A simplicidade do Go torna injeção de dependência manual não apenas viável, mas preferível. Frameworks como Spring resolvem problemas que Go não tem.

Aqui está como fazer DI em Go da forma idiomática.

## Por Que Go Não Precisa de Containers de DI

Em Java, você precisa de containers de DI porque:
- Construtores são verbosos (sem parâmetros nomeados)
- Sem funções de primeira classe
- Configuração por annotations é a norma
- Grafos de objetos complexos com gerenciamento de ciclo de vida

Go não tem nenhum desses problemas:
- Struct literals com campos nomeados
- Funções de primeira classe
- Explícito é melhor que mágica
- Ciclos de vida de objetos simples (cria, usa, pronto)

A filosofia Go: se você pode ver o código, você pode entendê-lo. Containers de DI escondem a fiação.

## Padrão 1: Injeção por Construtor

A base de DI em Go. Dependências vão no construtor.

```go
// repository.go
type UserRepository struct {
    db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
    return &UserRepository{db: db}
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*User, error) {
    // Usa r.db
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

### Main como Composition Root

Toda fiação acontece em `main()`. Este é seu composition root—o único lugar onde você vê como tudo se conecta.

```go
// main.go
func main() {
    // Infraestrutura
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

Tudo é explícito. Sem mágica, sem reflection, sem arquivos XML. Abra `main.go` e você vê exatamente como sua app está conectada.

## Padrão 2: Segregação de Interface

Defina interfaces onde são consumidas, não onde são implementadas. Isso é chave para código Go testável.

```go
// ERRADO: Interface grande definida pelo implementador
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

// BOM: Interface pequena definida pelo consumidor
// auth/service.go
type UserGetter interface {
    GetByEmail(ctx context.Context, email string) (*User, error)
}

type AuthService struct {
    users UserGetter // Só precisa de um método
}

func NewAuthService(users UserGetter) *AuthService {
    return &AuthService{users: users}
}
```

Benefícios:
- Fácil de mockar em testes (um método para implementar)
- Dependências claras (você vê exatamente o que é necessário)
- Pacotes desacoplados (sem definições de interface compartilhadas)

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
    // Testa...
}
```

## Padrão 3: Functional Options

Quando construtores têm muitos parâmetros opcionais, use functional options.

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

// Option é uma função que configura Server
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
    // Valores padrão
    s := &Server{
        host:     "localhost",
        port:     8080,
        timeout:  30 * time.Second,
        maxConns: 100,
        logger:   defaultLogger,
        metrics:  noopMetrics,
    }

    // Aplica options
    for _, opt := range opts {
        opt(s)
    }

    return s
}

// Uso
server := NewServer(
    WithHost("0.0.0.0"),
    WithPort(443),
    WithTLS(tlsConfig),
    WithLogger(zapLogger),
)
```

### Quando Usar Functional Options

- **Muitos parâmetros opcionais** (3+)
- **Padrões sensatos** existem para a maioria dos parâmetros
- **API pública** onde compatibilidade retroativa importa
- **Configuração estilo builder** onde ordem não importa

Não use para:
- Construtores simples com 1-3 parâmetros obrigatórios
- Código interno onde legibilidade vence flexibilidade

## Padrão 4: Structs de Configuração

Para configuração complexa, uma struct de config é mais clara que muitas options:

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

// Uso
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

Structs de config funcionam bem com parsing de ambiente:

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

## Padrão 5: Wire para Aplicações Grandes

Para aplicações com muitas dependências, Wire do Google gera o código de fiação.

```go
// wire.go
//go:build wireinject

package main

import "github.com/google/wire"

func InitializeApp() (*App, error) {
    wire.Build(
        // Infraestrutura
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

Execute `wire` e ele gera `wire_gen.go`:

```go
// wire_gen.go (gerado)
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

### Provider Sets para Organização

Agrupe providers relacionados:

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

### Quando Usar Wire

- **Aplicações grandes** com 20+ dependências injetáveis
- **Projetos em equipe** onde fiação consistente importa
- **Segurança em tempo de compilação** é importante (Wire falha em compile time se fiação está errada)

Não use para:
- Aplicações pequenas a médias
- Projetos de aprendizado
- Quando fiação explícita em `main()` ainda é gerenciável

## Anti-Padrões para Evitar

### 1. Variáveis Globais

```go
// ERRADO: Estado global
var db *sql.DB
var userRepo *UserRepository

func init() {
    db, _ = sql.Open("postgres", os.Getenv("DATABASE_URL"))
    userRepo = NewUserRepository(db)
}

func GetUser(id string) (*User, error) {
    return userRepo.GetByID(context.Background(), id)
}

// CERTO: Injete dependências
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
// ERRADO: Padrão service locator
type Container struct {
    services map[string]any
}

func (c *Container) Get(name string) any {
    return c.services[name]
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    userService := container.Get("userService").(*UserService) // Erro em runtime se faltando
    // ...
}

// CERTO: Dependências explícitas
type Handler struct {
    users *UserService
}

func NewHandler(users *UserService) *Handler {
    return &Handler{users: users} // Erro em compile-time se faltando
}
```

### 3. Dependências Escondidas

```go
// ERRADO: Dependência escondida em time.Now
func (s *Service) CreateOrder(userID string) (*Order, error) {
    return &Order{
        ID:        uuid.New().String(),
        UserID:    userID,
        CreatedAt: time.Now(), // Dependência escondida, difícil testar
    }
}

// CERTO: Injete função de tempo
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
        CreatedAt: s.now(), // Testável!
    }
}
```

## Testando com DI

A recompensa de DI adequado é testes fáceis:

```go
func TestOrderService_Create(t *testing.T) {
    // Stub de dependências
    userGetter := &stubUserGetter{
        user: &User{ID: "user-1", Name: "Test"},
    }
    orderRepo := &stubOrderRepo{}
    nowFunc := func() time.Time {
        return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
    }

    // Cria service com dependências de teste
    svc := NewOrderService(userGetter, orderRepo, nowFunc)

    // Testa
    order, err := svc.Create(context.Background(), CreateOrderRequest{
        UserID: "user-1",
        Items:  []Item{{SKU: "ABC", Qty: 2}},
    })

    require.NoError(t, err)
    assert.Equal(t, "user-1", order.UserID)
    assert.Equal(t, nowFunc(), order.CreatedAt)
}
```

## Escolhendo Sua Abordagem

- **DI Manual**: Apps pequenas-médias, dependências claras, controle total
- **Functional Options**: Muitos params opcionais, APIs públicas, bibliotecas
- **Structs de Config**: Configuração complexa, setup baseado em ambiente
- **Wire**: Apps grandes (20+ deps), projetos em equipe, segurança compile-time

## Pontos-Chave

1. **Go não precisa de containers de DI**. Fiação manual é explícita, debugável e rápida.

2. **Main é seu composition root**. Toda fiação acontece lá—um lugar para ver o quadro completo.

3. **Defina interfaces no consumidor**. Interfaces pequenas são fáceis de mockar e reduzem acoplamento.

4. **Functional options para params opcionais**. Ótimo para APIs públicas, exagero para construtores simples.

5. **Wire para aplicações grandes**. Gera código de fiação correto, pega erros em compile time.

6. **Evite globals e service locators**. Eles escondem dependências e dificultam testes.

7. **Injete tudo que é testável**. Tempo, aleatoriedade, serviços externos—se você pode querer controlar em testes, injete.

A melhor DI em Go não é framework nenhum. Apenas construtores, interfaces e fiação explícita no main. Quando isso fica difícil de gerenciar, Wire gera o boilerplate mantendo tudo explícito.
