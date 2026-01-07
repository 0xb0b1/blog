---
title: "Padrões de Propagação de Context em Grandes Codebases Go"
date: 2025-02-16
description: "Quando passar context, quando não passar, e os erros comuns que levam a goroutines vazadas, cancelamentos perdidos e pesadelos de debug."
tags:
  [
    "golang",
    "boas-praticas",
    "concorrencia",
    "arquitetura",
    "padroes",
  ]
---

"Sempre passe context como primeiro argumento" é sabedoria Go que é repetida sem explicação. Mas em grandes codebases, propagação de context se torna surpreendentemente nuançada. Quando usar `context.Background()`? O que vai em valores de context? Por que minha goroutine não está cancelando?

Vamos explorar os padrões que realmente funcionam.

## O Básico (E Por Que Não É Suficiente)

O conselho padrão:

```go
func DoSomething(ctx context.Context, args Args) error {
    // Use ctx para cancelamento, deadlines e valores de escopo de request
}
```

Isso funciona até você ter:
- Workers em background que não devem ser cancelados por context de request
- Graceful shutdown que precisa de seu próprio cancelamento
- Goroutines aninhadas com ciclos de vida complexos
- Valores que devem (ou não) cruzar fronteiras de goroutines

## Padrão 1: Context de Request vs. Context de Aplicação

O erro mais comum é usar context de request para tudo:

```go
// ERRADO: Tarefa background herda cancelamento de request
func (h *Handler) ProcessOrder(w http.ResponseWriter, r *http.Request) {
    order := parseOrder(r)

    // Esta goroutine morre quando a request HTTP completa!
    go h.sendNotification(r.Context(), order)

    w.WriteHeader(http.StatusAccepted)
}
```

A correção é entender diferentes escopos de context:

```go
func (h *Handler) ProcessOrder(w http.ResponseWriter, r *http.Request) {
    order := parseOrder(r)

    // Cria novo context para trabalho em background
    // Herda valores (trace ID, etc.) mas não cancelamento
    bgCtx := detachContext(r.Context())

    go h.sendNotification(bgCtx, order)

    w.WriteHeader(http.StatusAccepted)
}

// detachContext cria novo context que herda valores mas não cancelamento
func detachContext(ctx context.Context) context.Context {
    // Cria novo context com cancelamento de nível de aplicação
    return contextWithValues(context.Background(), ctx)
}

func contextWithValues(dst, src context.Context) context.Context {
    // Copia valores relevantes de src para dst
    if traceID := src.Value(traceIDKey); traceID != nil {
        dst = context.WithValue(dst, traceIDKey, traceID)
    }
    if requestID := src.Value(requestIDKey); requestID != nil {
        dst = context.WithValue(dst, requestIDKey, requestID)
    }
    return dst
}
```

## Padrão 2: Context de Graceful Shutdown

Sua aplicação precisa de sua própria hierarquia de cancelamento:

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
    // Todas goroutines de longa duração usam context da aplicação
    go a.backgroundWorker(a.ctx)
    go a.metricsReporter(a.ctx)

    // Servidor HTTP recebe um context derivado
    return a.httpServer.Run(a.ctx)
}

func (a *Application) Shutdown() {
    // Cancelar context da app para tudo
    a.cancel()
}

// Handlers HTTP criam contexts de request derivados do context da app
func (a *Application) handleRequest(w http.ResponseWriter, r *http.Request) {
    // Context de request herda cancelamento da app
    ctx := r.Context()

    // Se app está desligando, este context já está cancelado
    select {
    case <-ctx.Done():
        http.Error(w, "Servidor desligando", http.StatusServiceUnavailable)
        return
    default:
    }

    // Processamento normal...
}
```

## Padrão 3: Valores de Context Corretos

Valores de context são frequentemente mal usados. Aqui está o que pertence ao context vs. parâmetros de função:

```go
// RUIM: Lógica de negócio em context
func ProcessOrder(ctx context.Context) error {
    order := ctx.Value("order").(Order)  // Type assertion panic esperando acontecer
    user := ctx.Value("user").(User)     // Dependências escondidas
    // ...
}

// BOM: Context para concerns transversais apenas
func ProcessOrder(ctx context.Context, order Order, user User) error {
    // Trace ID, request ID, token de auth - estes são transversais
    traceID := tracing.TraceIDFromContext(ctx)
    log.Info("Processando pedido", "trace_id", traceID, "order_id", order.ID)
    // ...
}
```

### Chaves de Context Type-Safe

```go
// Define tipos de chave não exportados para prevenir colisões
type contextKey int

const (
    traceIDKey contextKey = iota
    requestIDKey
    userClaimsKey
)

// Fornece acessores tipados
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

## Padrão 4: Hierarquias de Timeout

Operações aninhadas precisam timeouts coordenados:

```go
func (s *Service) ProcessWithTimeout(ctx context.Context) error {
    // Timeout geral da operação
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    // Passo 1: Query de banco (deve ser rápida)
    dbCtx, dbCancel := context.WithTimeout(ctx, 5*time.Second)
    defer dbCancel()

    data, err := s.db.Query(dbCtx, query)
    if err != nil {
        return fmt.Errorf("query de banco: %w", err)
    }

    // Passo 2: API externa (mais lenta)
    // Tempo restante do context pai
    apiCtx, apiCancel := context.WithTimeout(ctx, 20*time.Second)
    defer apiCancel()

    result, err := s.api.Call(apiCtx, data)
    if err != nil {
        return fmt.Errorf("chamada de api: %w", err)
    }

    return s.save(ctx, result)
}
```

**Insight chave**: Timeouts filhos devem ser menores que o pai. Se pai tem 30s restantes e filho precisa de 25s, filho pode ser cancelado pelo pai antes de seu próprio timeout.

## Padrão 5: Gerenciamento de Ciclo de Vida de Goroutine

A goroutine vazada clássica:

```go
// ERRADO: Goroutine nunca termina
func (w *Worker) Start() {
    go func() {
        for {
            w.process()
            time.Sleep(time.Second)
        }
    }()
}

// CERTO: Ciclo de vida controlado por context
func (w *Worker) Start(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(time.Second)
        defer ticker.Stop()

        for {
            select {
            case <-ctx.Done():
                log.Info("Worker desligando")
                return
            case <-ticker.C:
                w.process(ctx)
            }
        }
    }()
}
```

### Esperando Goroutines

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
    // Espera todos workers com timeout
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

## Padrão 6: Context de Conexão de Banco

Operações de banco têm suas próprias considerações de context:

```go
func (r *Repository) GetUser(ctx context.Context, id string) (*User, error) {
    // Query respeita cancelamento de context
    row := r.db.QueryRowContext(ctx, "SELECT * FROM users WHERE id = $1", id)

    var user User
    if err := row.Scan(&user.ID, &user.Name); err != nil {
        return nil, err
    }
    return &user, nil
}

// Mas transações precisam tratamento cuidadoso
func (r *Repository) TransferFunds(ctx context.Context, from, to string, amount int) error {
    tx, err := r.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }

    // Garante cleanup mesmo se context é cancelado
    defer func() {
        if err != nil {
            tx.Rollback() // Rollback ignora cancelamento de context
        }
    }()

    // Operações dentro da transação
    if err = r.debit(ctx, tx, from, amount); err != nil {
        return err
    }
    if err = r.credit(ctx, tx, to, amount); err != nil {
        return err
    }

    return tx.Commit()
}
```

## Erros Comuns

### 1. Usando context.Background() Em Todo Lugar

```go
// RUIM: Ignora sinais de cancelamento
func (s *Service) DoWork() {
    s.db.Query(context.Background(), query)  // Não cancela no shutdown
}

// BOM: Propaga context
func (s *Service) DoWork(ctx context.Context) {
    s.db.Query(ctx, query)
}
```

### 2. Armazenando Context em Structs

```go
// RUIM: Context armazenado em struct
type Service struct {
    ctx context.Context  // Não faça isso
}

// BOM: Passe context para métodos
type Service struct{}

func (s *Service) Process(ctx context.Context) error {
    // Use ctx aqui
}
```

### 3. Não Verificando ctx.Done()

```go
// RUIM: Loop longo ignora cancelamento
func processItems(ctx context.Context, items []Item) {
    for _, item := range items {
        process(item)  // Cancelamento de context ignorado
    }
}

// BOM: Verifica cancelamento periodicamente
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

## Pontos-Chave

1. **Context de request ≠ context de aplicação**. Tarefas background não devem herdar cancelamento de request.

2. **Valores de context são para concerns transversais**: trace IDs, request IDs, claims de auth. Não dados de negócio.

3. **Chaves de context type-safe** previnem colisões e tornam dependências explícitas.

4. **Timeouts filhos devem ser menores que o pai**. Caso contrário, pai pode cancelar antes do timeout do filho.

5. **Sempre verifique ctx.Done()** em loops de longa duração.

6. **Não armazene context em structs**. Passe como primeiro parâmetro.

7. **Use defer cancel()** para prevenir vazamentos de context.

Propagação de context parece simples até sua codebase crescer. Acerte esses padrões cedo, e debugar código concorrente se torna muito mais fácil.
