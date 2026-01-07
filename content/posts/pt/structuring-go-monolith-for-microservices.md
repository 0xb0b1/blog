---
title: "Estruturando um Monolito Go Para Que Possa Virar Microserviços Depois"
date: 2025-01-28
description: "Fronteiras de módulos práticas, organização de pacotes, e os padrões que tornam decomposição futura possível sem reescrever tudo."
tags:
  ["golang", "arquitetura", "microservices", "monolito", "padroes", "design"]
---

"Vamos extrair microserviços depois" é a mentira que contamos para nós mesmos antes de construir um monolito emaranhado que nunca pode ser dividido. O problema não são monolitos—frequentemente são a escolha certa. O problema são monolitos sem fronteiras internas.

Aqui está como estruturar um monolito Go que pode realmente virar microserviços quando (se) você precisar.

## O Objetivo: Monolito Modular

Um monolito modular tem fronteiras internas claras que parecem fronteiras de serviço, mas tudo roda em um processo. Você ganha:

- Deploy e operações simples
- Sem latência de rede entre "serviços"
- Refatoração fácil através de fronteiras
- A opção de extrair depois

```
monolith/
├── cmd/
│   └── server/
│       └── main.go           # Ponto de entrada único
├── internal/
│   ├── orders/               # Poderia ser um serviço
│   │   ├── service.go
│   │   ├── repository.go
│   │   ├── handlers.go
│   │   └── events.go
│   ├── payments/             # Poderia ser um serviço
│   │   ├── service.go
│   │   ├── repository.go
│   │   ├── handlers.go
│   │   └── events.go
│   ├── inventory/            # Poderia ser um serviço
│   │   └── ...
│   └── shared/               # Código verdadeiramente compartilhado
│       ├── auth/
│       └── observability/
├── pkg/                      # Contratos públicos
│   ├── orderapi/
│   ├── paymentapi/
│   └── events/
└── go.mod
```

## Regra 1: Módulos São Donos de Seus Dados

Cada módulo tem seu próprio schema de banco. Sem acesso a tabelas entre módulos.

```go
// internal/orders/repository.go
type Repository struct {
    db *sql.DB
}

// Módulo de orders só toca em tabelas de orders
func (r *Repository) Create(ctx context.Context, order *Order) error {
    _, err := r.db.ExecContext(ctx, `
        INSERT INTO orders.orders (id, customer_id, status, created_at)
        VALUES ($1, $2, $3, $4)
    `, order.ID, order.CustomerID, order.Status, order.CreatedAt)
    return err
}

// ERRADO: Não faça isso - acessando tabelas de outro módulo
func (r *Repository) GetCustomerEmail(ctx context.Context, customerID string) (string, error) {
    // Isso cria acoplamento escondido com o módulo de customers
    var email string
    err := r.db.QueryRowContext(ctx, `
        SELECT email FROM customers.customers WHERE id = $1
    `, customerID).Scan(&email)
    return email, err
}
```

Em vez disso, defina o que você precisa de outros módulos explicitamente:

```go
// internal/orders/service.go
type CustomerGetter interface {
    GetCustomer(ctx context.Context, id string) (*Customer, error)
}

type Service struct {
    repo      *Repository
    customers CustomerGetter  // Dependência explícita
    payments  PaymentProcessor
}

func (s *Service) CreateOrder(ctx context.Context, req CreateOrderRequest) (*Order, error) {
    // Obtém customer através da interface, não acesso direto ao DB
    customer, err := s.customers.GetCustomer(ctx, req.CustomerID)
    if err != nil {
        return nil, fmt.Errorf("get customer: %w", err)
    }

    order := &Order{
        ID:         uuid.NewString(),
        CustomerID: customer.ID,
        Email:      customer.Email,  // Desnormaliza o que você precisa
        Status:     StatusPending,
    }

    if err := s.repo.Create(ctx, order); err != nil {
        return nil, fmt.Errorf("create order: %w", err)
    }

    return order, nil
}
```

### Isolamento de Schema com PostgreSQL

```sql
-- Cada módulo ganha seu próprio schema
CREATE SCHEMA orders;
CREATE SCHEMA payments;
CREATE SCHEMA inventory;
CREATE SCHEMA customers;

-- Tabelas vivem no schema do seu módulo
CREATE TABLE orders.orders (
    id UUID PRIMARY KEY,
    customer_id UUID NOT NULL,  -- Referência, não FK
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

-- Sem foreign keys entre schemas!
-- Isso torna extração possível depois
```

## Regra 2: Comunicação Através de Interfaces

Módulos conversam entre si através de interfaces definidas pelo consumidor:

```go
// internal/orders/dependencies.go
package orders

// Define o que orders precisa de outros módulos
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

// Tipos que cruzam fronteiras de módulos
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

O módulo que implementa satisfaz essas interfaces:

```go
// internal/payments/service.go
package payments

type Service struct {
    repo    *Repository
    gateway PaymentGateway
}

// Satisfaz orders.PaymentProcessor
func (s *Service) Charge(ctx context.Context, customerID string, amount int) (*orders.PaymentResult, error) {
    // Implementação
}
```

Conecte tudo no main:

```go
// cmd/server/main.go
func main() {
    // Inicializa módulos
    customerService := customers.NewService(customerRepo)
    paymentService := payments.NewService(paymentRepo, gateway)
    inventoryService := inventory.NewService(inventoryRepo)

    // Orders recebe suas dependências injetadas
    orderService := orders.NewService(
        orderRepo,
        customerService,  // satisfaz CustomerGetter
        paymentService,   // satisfaz PaymentProcessor
        inventoryService, // satisfaz InventoryChecker
    )
}
```

## Regra 3: Eventos para Acoplamento Fraco

Algumas interações não devem ser chamadas síncronas. Use eventos:

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

Event bus que funciona in-process agora, pode virar Kafka/NATS depois:

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

    // In-process: roda handlers diretamente
    // Depois: serializa e envia para message broker
    for _, h := range handlers {
        if err := h(ctx, event); err != nil {
            // Loga erro, talvez retry, mas não falha o publisher
            log.Printf("event handler error: %v", err)
        }
    }

    return nil
}
```

Uso:

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

    // Publica evento - outros módulos reagem assincronamente
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

// Conectado no main.go
eventBus.Subscribe("events.OrderCompleted", notificationService.HandleOrderCompleted)
```

## Regra 4: Contratos de API Pública

Defina a API pública do seu módulo em `pkg/`:

```go
// pkg/orderapi/api.go
package orderapi

import "context"

// Este é o contrato que outros serviços/módulos usam
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

No monolito, a implementação é direta:

```go
// internal/orders/client.go
package orders

// Implementação direta - mesmo processo
type Client struct {
    service *Service
}

func NewClient(service *Service) *Client {
    return &Client{service: service}
}

func (c *Client) CreateOrder(ctx context.Context, req orderapi.CreateOrderRequest) (*orderapi.Order, error) {
    // Chamada direta ao service
    order, err := c.service.CreateOrder(ctx, req)
    if err != nil {
        return nil, err
    }
    return toAPIOrder(order), nil
}
```

Quando você extrair para microserviços, troque por um cliente HTTP/gRPC:

```go
// pkg/orderapi/httpclient.go
package orderapi

// Implementação HTTP - processo diferente
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
    // Chamada HTTP para orders service
    body, _ := json.Marshal(req)
    resp, err := c.httpClient.Post(c.baseURL+"/orders", "application/json", bytes.NewReader(body))
    // ...
}
```

O código que chama não muda—ele só usa uma implementação diferente de `orderapi.Client`.

## Regra 5: Separe Modelos de Leitura e Escrita Onde Necessário

Algumas queries precisam de dados de múltiplos módulos. Ao invés de joins entre módulos, construa modelos de leitura:

```go
// internal/reporting/service.go
package reporting

// Modelo de leitura construído a partir de eventos
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

// Subscreve eventos de múltiplos módulos
func (s *Service) HandleOrderCreated(ctx context.Context, event any) error {
    e := event.(events.OrderCreated)
    return s.repo.UpsertOrderSummary(ctx, &OrderSummary{
        OrderID: e.OrderID,
        // ... popula do evento
    })
}

func (s *Service) HandlePaymentCompleted(ctx context.Context, event any) error {
    e := event.(events.PaymentCompleted)
    return s.repo.UpdatePaymentStatus(ctx, e.OrderID, "completed")
}

// Queries consultam o modelo de leitura, não módulos fonte
func (s *Service) GetDashboard(ctx context.Context, customerID string) (*Dashboard, error) {
    summaries, err := s.repo.GetOrderSummaries(ctx, customerID)
    // Todos os dados são locais - sem queries entre módulos
}
```

## O Playbook de Extração

Quando for hora de extrair um módulo para um serviço:

1. **Módulo já está isolado** - é dono de seus dados, comunica através de interfaces
2. **Crie handlers HTTP/gRPC** para a API pública do módulo
3. **Faça deploy como serviço separado** com seu próprio banco de dados
4. **Troque a implementação do client** no monolito restante
5. **Event bus vira message broker real** (mesmos tipos de evento)

```go
// Antes: in-process
orderClient := orders.NewClient(orderService)

// Depois: HTTP client para orders microservice
orderClient := orderapi.NewHTTPClient("http://orders-service:8080")

// Resto do código inalterado - mesma interface
```

## O Que NÃO Compartilhar

```go
// NÃO compartilhe modelos de domínio entre módulos
// Cada módulo tem seu próprio User, Order, etc.

// internal/orders/models.go
type Customer struct {  // Visão de orders de um customer
    ID    string
    Email string
}

// internal/billing/models.go
type Customer struct {  // Visão de billing - campos diferentes
    ID            string
    PaymentMethod string
    BillingAddress Address
}

// COMPARTILHE:
// - Tipos de evento (pkg/events/)
// - Contratos de API (pkg/orderapi/, pkg/paymentapi/)
// - Utilitários verdadeiramente genéricos (pkg/httputil/, pkg/validate/)
```

## Pontos-Chave

1. **Módulos são donos de seus dados**. Sem acesso a banco entre módulos. Use interfaces.

2. **Defina interfaces no consumidor**. Orders define o que precisa de Payments, não o contrário.

3. **Eventos para concerns transversais**. Notificações, analytics, audit logs—estes subscrevem eventos.

4. **API pública em `pkg/`**. Isso vira seu contrato de serviço quando você extrair.

5. **Modelos de leitura para queries entre módulos**. Construa views desnormalizadas a partir de eventos.

6. **Não compartilhe modelos de domínio**. Cada módulo tem sua própria visão de conceitos compartilhados.

7. **Conecte tudo no main**. Injeção de dependência torna trocar implementações trivial.

O objetivo não é construir microserviços em um monolito. É construir um monolito que não lute contra você quando chegar a hora de dividir. A maioria das equipes nunca precisa—e tudo bem. Mas se precisar, você estará pronto.
