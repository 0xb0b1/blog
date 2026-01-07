---
title: "Escrevendo Código Go Testável Sem Mockar Tudo"
date: 2025-10-01
description: "Implementações reais ao invés de mocks, test fixtures que funcionam, e design patterns que tornam código testável sem gerar arquivos mock para cada interface."
tags:
  [
    "golang",
    "testes",
    "boas-praticas",
    "arquitetura",
    "padroes",
  ]
---

Projetos Go frequentemente acabam com mais arquivos de mock do que código real. Cada interface ganha um mock gerado, testes ficam fortemente acoplados a detalhes de implementação, e refatoração quebra dezenas de arquivos de teste.

Existe uma forma melhor. Aqui está como escrever código testável sem recorrer a mocks por padrão.

## O Problema da Explosão de Mocks

Projeto Go enterprise típico:

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

Cada dependência é mockada. Testes verificam interações com mocks ao invés de comportamento. E quando você muda como algo funciona internamente, todas aquelas expectativas de mock quebram.

## Estratégia 1: Implementações Reais Primeiro

Antes de mockar, pergunte: posso usar a coisa real?

### SQLite para Testes de Banco

```go
// test_helpers.go
func TestDB(t *testing.T) *sql.DB {
    t.Helper()

    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil {
        t.Fatalf("open db: %v", err)
    }

    // Roda migrations
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

    // Verifica que foi realmente persistido
    found, err := repo.GetByID(context.Background(), user.ID)
    require.NoError(t, err)
    assert.Equal(t, user.Name, found.Name)
}
```

Benefícios:
- Testa queries SQL reais
- Pega problemas de schema
- Sem manutenção de mock
- Rápido o suficiente para testes unitários (SQLite em memória)

### Redis com miniredis

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

    // Testa operações Redis reais
    user := &User{ID: "123", Name: "test"}
    err := cache.Set(context.Background(), user)
    require.NoError(t, err)

    found, err := cache.Get(context.Background(), "123")
    require.NoError(t, err)
    assert.Equal(t, user.Name, found.Name)
}
```

### HTTP com httptest

```go
func TestPaymentClient_Charge(t *testing.T) {
    // Servidor HTTP real, não mocks
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Verifica request
        assert.Equal(t, "POST", r.Method)
        assert.Equal(t, "/v1/charges", r.URL.Path)

        var req ChargeRequest
        json.NewDecoder(r.Body).Decode(&req)
        assert.Equal(t, 1000, req.Amount)

        // Retorna resposta
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

## Estratégia 2: Interfaces Pequenas

Interfaces grandes levam a mocks grandes. Em vez disso, defina a menor interface que você precisa:

```go
// RUIM: Interface enorme, mock enorme
type UserRepository interface {
    Create(ctx context.Context, user *User) error
    Update(ctx context.Context, user *User) error
    Delete(ctx context.Context, id string) error
    GetByID(ctx context.Context, id string) (*User, error)
    GetByEmail(ctx context.Context, email string) (*User, error)
    List(ctx context.Context, filter Filter) ([]*User, error)
    Count(ctx context.Context) (int, error)
    // ... mais 20 métodos
}

// BOM: Define interface onde é usada
// No pacote auth:
type UserGetter interface {
    GetByEmail(ctx context.Context, email string) (*User, error)
}

func NewAuthService(users UserGetter) *AuthService {
    return &AuthService{users: users}
}

// Agora testes só precisam implementar um método
type stubUserGetter struct {
    user *User
    err  error
}

func (s *stubUserGetter) GetByEmail(ctx context.Context, email string) (*User, error) {
    return s.user, s.err
}
```

Isso é chamado de "Princípio de Segregação de Interface" mas em Go acontece naturalmente quando você define interfaces no ponto de uso.

## Estratégia 3: Functional Options para Dependências

Ao invés de dependências de interface, às vezes uma função é suficiente:

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

// Testes podem injetar funções simples
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

## Estratégia 4: Test Fixtures ao Invés de Factories

Ao invés de construir dados de teste inline, use fixtures:

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

// Builder para objetos complexos
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

Uso:

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

## Estratégia 5: Table-Driven Tests Sem Mocks

Table-driven tests funcionam muito bem com stubs:

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

// Para casos mais complexos com dependências
func TestOrderProcessor(t *testing.T) {
    tests := []struct {
        name          string
        order         Order
        inventory     map[string]int // item -> quantidade disponível
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
            // Stub simples, não um mock
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

## Estratégia 6: Testes de Integração com Docker

Para coisas que são difíceis de simular, use a coisa real com testcontainers:

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

    // Agora testa com Postgres real
    repo := NewUserRepository(db)
    // ...
}
```

## Quando Mocks São Realmente Úteis

Mocks não são do mal—apenas usados demais. Eles são bons para:

1. **Serviços externos que você não controla** (APIs de terceiros)
2. **Verificar interações** quando a interação É o comportamento
3. **Simular falhas** que são difíceis de disparar naturalmente

```go
// Bom uso de mock: verificar que um email foi enviado
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

## Pontos-Chave

1. **Implementações reais primeiro**. SQLite, miniredis, httptest—use-os antes de recorrer a mocks.

2. **Interfaces pequenas**. Defina interfaces onde são usadas, com apenas os métodos necessários.

3. **Funções ao invés de interfaces** quando você só precisa de um método.

4. **Fixtures ao invés de factories** para dados de teste consistentes.

5. **Table-driven tests** reduzem duplicação e tornam edge cases óbvios.

6. **Testes de integração com containers** para coisas que não podem ser simuladas.

7. **Mocke apenas o que você não controla** ou quando verificar interações é o requisito real.

O objetivo não é zero mocks—é testes que verificam comportamento, não detalhes de implementação. Quando testes quebram apenas porque comportamento mudou (não porque você refatorou internals), você sabe que está no caminho certo.
