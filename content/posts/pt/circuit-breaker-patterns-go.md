---
title: "Por Que Nosso Microserviço Precisou de um Circuit Breaker (E Como Construímos)"
date: 2025-04-06
description: "Cascatas de latência reais que encontramos, como derrubaram nosso sistema, e a implementação de circuit breaker que nos salvou."
tags:
  [
    "golang",
    "microservices",
    "resiliencia",
    "sistemas-distribuidos",
    "padroes",
    "producao",
  ]
---

Começou com uma query lenta de banco de dados em um serviço downstream. Em minutos, cada serviço na nossa plataforma estava dando timeout. Thread pools esgotados, memória disparando, usuários vendo erros em todo lugar. Uma dependência lenta tinha cascateado em uma falha completa do sistema.

Esta é a história daquele incidente, por que aconteceu, e como construímos um circuit breaker para prevenir que acontecesse de novo.

## A Cascata Que Quebrou Tudo

Aqui está como nossa arquitetura se parecia:

```
Request do Usuário → API Gateway → Order Service → Payment Service → Bank API
                                                → Inventory Service → Database
                                                → Notification Service → Email Provider
```

A Bank API começou a responder lentamente (2-3 segundos ao invés de 200ms). Aqui está o que aconteceu:

1. Threads do Payment Service esperaram pela Bank API
2. Requests do Order Service acumularam esperando pelo Payment Service
3. Conexões do API Gateway esgotaram esperando pelo Order Service
4. Usuários começaram a dar retry, multiplicando a carga
5. Outros serviços compartilhando a mesma infraestrutura começaram a falhar

Tudo porque não tínhamos uma forma de dizer "essa dependência está quebrada, pare de tentar."

## A Abordagem Ingênua de Retry (Não Faça Isso)

Nosso primeiro instinto foi retries com timeouts:

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

Isso piorou as coisas. Quando a Bank API estava lenta, estávamos:

- Fazendo 3x as requests
- Segurando conexões 3x mais tempo
- Adicionando delays de backoff que acumulavam

## Entendendo Circuit Breakers

Um circuit breaker tem três estados:

```
     ┌─────────────────────────────────────────────────────┐
     │                                                     │
     ▼                                                     │
┌─────────┐  falhas > threshold    ┌──────────┐           │
│ CLOSED  │ ────────────────────► │  OPEN    │           │
│ (normal)│                        │ (falha)  │           │
└─────────┘                        └──────────┘           │
     ▲                                  │                  │
     │                                  │ após timeout     │
     │                                  ▼                  │
     │                           ┌────────────┐            │
     │         sucesso           │ HALF-OPEN  │            │
     └───────────────────────────│ (testando) │────────────┘
                                 └────────────┘  falha
```

- **Closed**: Operação normal. Rastreia falhas.
- **Open**: Dependência está quebrada. Falha rápido sem chamar.
- **Half-Open**: Testando se dependência se recuperou. Deixa uma request passar.

## Construindo Nosso Circuit Breaker

Aqui está a implementação que nos salvou:

```go
package circuitbreaker

import (
    "context"
    "errors"
    "sync"
    "time"
)

var (
    ErrCircuitOpen = errors.New("circuit breaker está aberto")
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

    // Configuração
    failureThreshold  int
    successThreshold  int
    timeout           time.Duration
    onStateChange     func(name string, from, to State)
}

type Config struct {
    Name              string
    FailureThreshold  int           // Falhas antes de abrir
    SuccessThreshold  int           // Sucessos em half-open antes de fechar
    Timeout           time.Duration // Quanto tempo ficar aberto
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
        // Verifica se timeout passou
        if time.Since(cb.lastFailure) > cb.timeout {
            cb.transitionTo(StateHalfOpen)
            return true
        }
        return false

    case StateHalfOpen:
        // Em half-open, permitimos requests para testar
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
        // Qualquer falha em half-open volta para open
        cb.transitionTo(StateOpen)
    }
}

func (cb *CircuitBreaker) recordSuccess() {
    cb.successes++

    switch cb.state {
    case StateClosed:
        cb.failures = 0 // Reseta contagem de falhas no sucesso

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
        // Chama assincronamente para não segurar o lock
        go cb.onStateChange(cb.name, oldState, newState)
    }
}
```

## Usando o Circuit Breaker

Aqui está como integramos no nosso cliente de pagamento:

```go
type PaymentClient struct {
    httpClient     *http.Client
    baseURL        string
    circuitBreaker *circuitbreaker.CircuitBreaker
}

func NewPaymentClient(baseURL string) *PaymentClient {
    cb := circuitbreaker.New(circuitbreaker.Config{
        Name:             "payment-service",
        FailureThreshold: 5,                // Abre após 5 falhas
        SuccessThreshold: 2,                // Fecha após 2 sucessos em half-open
        Timeout:          30 * time.Second, // Tenta de novo após 30s
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
        // Retorna erro significativo para o chamador
        return nil, fmt.Errorf("serviço de pagamento indisponível: %w", err)
    }

    return resp, err
}
```

## Padrões Avançados

### Circuit Breakers por Endpoint

Nem todos os endpoints falham juntos. Um endpoint `/payments` lento não significa que `/refunds` está quebrado:

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

### Combinando com Retries

Circuit breakers e retries podem trabalhar juntos, mas ordem importa:

```go
func (c *Client) DoWithResilience(ctx context.Context, fn func() error) error {
    // Retry envolve circuit breaker
    return c.retrier.Do(ctx, func() error {
        return c.circuitBreaker.Execute(ctx, fn)
    })
}
```

Quando o circuito está aberto, retries param imediatamente—sem tentativas desperdiçadas.

### Estratégias de Fallback

Quando o circuito está aberto, o que você retorna?

```go
func (c *PaymentClient) ProcessPayment(ctx context.Context, req PaymentRequest) (*PaymentResponse, error) {
    resp, err := c.doWithCircuitBreaker(ctx, req)

    if errors.Is(err, circuitbreaker.ErrCircuitOpen) {
        // Opção 1: Retorna dados em cache/stale se aceitável
        if cached, ok := c.cache.Get(req.OrderID); ok {
            return cached, nil
        }

        // Opção 2: Enfileira para processamento posterior
        if err := c.queue.Enqueue(req); err == nil {
            return &PaymentResponse{Status: "queued"}, nil
        }

        // Opção 3: Degrada graciosamente
        return nil, fmt.Errorf("serviço de pagamento indisponível, tente novamente mais tarde")
    }

    return resp, err
}
```

## Monitoramento e Alertas

Circuit breakers são inúteis se você não sabe que estão disparando:

```go
func setupMetrics() {
    // Métricas Prometheus
    circuitState := prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "circuit_breaker_state",
            Help: "Estado atual do circuit breaker (0=closed, 1=open, 2=half-open)",
        },
        []string{"name"},
    )

    circuitTrips := prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "circuit_breaker_trips_total",
            Help: "Número total de vezes que circuit breaker disparou",
        },
        []string{"name"},
    )

    prometheus.MustRegister(circuitState, circuitTrips)
}
```

Alertar em:

- Circuito abrindo (notificação imediata)
- Circuito ficando aberto mais que o esperado
- Alta frequência de disparo (indica problema subjacente)

## Pontos-Chave

1. **Timeouts não são suficientes**. Eles limitam duração de request individual mas não previnem falhas em cascata.

2. **Falhar rápido é uma feature**. Retornar erro imediatamente é melhor que esperar 30 segundos por um timeout inevitável.

3. **Circuit breakers protegem nos dois sentidos**. Eles protegem seu serviço de dependências lentas E protegem dependências lentas de serem sobrecarregadas.

4. **Monitore seus circuitos**. Um circuit breaker disparando frequentemente é um sintoma, não o problema.

5. **Tenha uma estratégia de fallback**. O que acontece quando o circuito está aberto? Dados em cache? Enfileirar para depois? Erro gracioso?

6. **Teste cenários de falha**. Chaos engineering não é opcional para sistemas distribuídos.

Aquela falha em cascata nos ensinou uma lição cara. Circuit breakers transformaram uma queda de 2 horas em uma degradação de 30 segundos. Vale cada linha de código.
