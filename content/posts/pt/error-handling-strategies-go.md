---
title: "Estratégias de Error Handling Além de if err != nil"
date: 2025-06-22
description: "Erros estruturados, wrapping de erros feito corretamente, sentinel errors vs verificação de comportamento, e padrões que tornam debug de produção realmente possível."
tags:
  [
    "golang",
    "boas-praticas",
    "tratamento-de-erros",
    "padroes",
    "debugging",
  ]
---

O tratamento de erros do Go é criticado como verboso, mas o problema real não é `if err != nil`—é que a maioria das codebases trata erros sem nenhuma estratégia. Erros são engolidos, wrapped inconsistentemente, ou logados múltiplas vezes. Quando algo quebra em produção, você fica adivinhando.

Aqui está como construir tratamento de erros que realmente ajuda a debugar.

## O Problema Base

Tratamento de erros típico encontrado por aí:

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

Problemas:
1. Erro logado em cada nível (spam de log)
2. Contexto perdido (qual pedido? qual usuário?)
3. Sem forma de distinguir "pedido não encontrado" de "banco de dados caiu"
4. Chamador não consegue tomar decisões baseadas no tipo de erro

## Estratégia 1: Wrap Uma Vez, Log Uma Vez

Erros devem ser wrapped com contexto onde acontecem, depois logados uma vez no nível mais alto.

```go
// ERRADO: Log em cada nível
func getUser(ctx context.Context, id string) (*User, error) {
    user, err := db.Query(ctx, id)
    if err != nil {
        log.Printf("db query failed: %v", err) // Logado aqui
        return nil, err
    }
    return user, nil
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
    user, err := getUser(r.Context(), userID)
    if err != nil {
        log.Printf("getUser failed: %v", err) // E aqui (duplicado)
        http.Error(w, "error", 500)
        return
    }
}

// CERTO: Wrap em cada nível, log no topo
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
        log.Printf("handleRequest failed: %v", err) // Log uma vez com contexto completo
        http.Error(w, "error", 500)
        return
    }
}
// Saída do log: "handleRequest failed: query user abc123: connection refused"
```

## Estratégia 2: Sentinel Errors vs. Verificação de Comportamento

### Sentinel Errors (Quando Usar)

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

// Chamador pode verificar
user, err := GetUser(ctx, id)
if errors.Is(err, ErrNotFound) {
    // Trata não encontrado (talvez retorne 404)
}
```

### Verificação de Comportamento (Frequentemente Melhor)

Sentinel errors criam acoplamento. Verificação de comportamento é mais flexível:

```go
// Define interface de comportamento
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

// Verifica comportamento, não tipo
func IsNotFound(err error) bool {
    var nf NotFoundError
    return errors.As(err, &nf) && nf.NotFound()
}

// Uso
if IsNotFound(err) {
    w.WriteHeader(http.StatusNotFound)
    return
}
```

Por que isso é melhor:
- Diferentes pacotes podem retornar seus próprios erros "não encontrado"
- Sem dependência de import nas definições de erro
- Funciona através de fronteiras de pacotes

## Estratégia 3: Erros Estruturados

Para sistemas complexos, erros precisam de estrutura:

```go
type AppError struct {
    Code    string            // Código legível por máquina
    Message string            // Mensagem legível por humanos
    Op      string            // Operação que falhou
    Err     error             // Erro subjacente
    Meta    map[string]string // Contexto adicional
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

// Helpers construtores
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

// Uso
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

### Extraindo Dados Estruturados

```go
func handleError(w http.ResponseWriter, err error) {
    var appErr *AppError
    if errors.As(err, &appErr) {
        // Erro estruturado - pode extrair detalhes
        log.Printf("op=%s code=%s meta=%v err=%v",
            appErr.Op, appErr.Code, appErr.Meta, appErr.Err)

        status := mapCodeToHTTPStatus(appErr.Code)
        writeJSONError(w, status, appErr.Code, appErr.Message)
        return
    }

    // Erro desconhecido - loga e retorna mensagem genérica
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

## Estratégia 4: Error Wrapping Feito Corretamente

### O Verbo %w

Go 1.13+ introduziu `%w` para wrapping de erros:

```go
// Cria uma cadeia que errors.Is e errors.As podem percorrer
if err != nil {
    return fmt.Errorf("process order %s: %w", orderID, err)
}
```

### Quando NÃO Fazer Wrap

Às vezes você quer esconder detalhes de implementação:

```go
// ERRADO: Vaza tipos de erro internos
func GetUser(id string) (*User, error) {
    user, err := redis.Get(ctx, key)
    if err != nil {
        return nil, fmt.Errorf("get user: %w", err) // Expõe erros do redis
    }
    return user, nil
}

// Chamador pode agora fazer errors.Is(err, redis.ErrNil) - acoplamento forte!

// CERTO: Traduz para erros de domínio
func GetUser(id string) (*User, error) {
    user, err := redis.Get(ctx, key)
    if errors.Is(err, redis.ErrNil) {
        return nil, ErrUserNotFound // Erro de domínio
    }
    if err != nil {
        return nil, fmt.Errorf("get user: %w", err)
    }
    return user, nil
}
```

### Inspeção de Cadeia de Erros

```go
func analyzeError(err error) {
    // errors.Is - verifica se algum erro na cadeia corresponde
    if errors.Is(err, sql.ErrNoRows) {
        // Trata não encontrado
    }

    // errors.As - extrai erro tipado da cadeia
    var appErr *AppError
    if errors.As(err, &appErr) {
        // Pode acessar appErr.Code, appErr.Meta, etc.
    }

    // Unwrap - pega o próximo erro na cadeia
    inner := errors.Unwrap(err)
}
```

## Estratégia 5: Tipos de Erro Específicos de Domínio

Agrupe erros por domínio:

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

// Métodos de comportamento
func (e *PaymentError) IsRetryable() bool {
    return e.Retryable
}

// Erros de pagamento comuns
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

Uso:

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

// Chamador pode tomar decisões inteligentes
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

## Estratégia 6: Agregação de Erros

Às vezes você precisa coletar múltiplos erros:

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

// Go 1.20+ tem errors.Join para isso
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

    return errors.Join(errs...) // nil se errs está vazio
}
```

## Checklist de Produção

1. **Faça wrap de erros com contexto** usando `fmt.Errorf("operation: %w", err)`

2. **Log uma vez no nível mais alto**, não em cada camada

3. **Use sentinel errors com moderação**. Verificação de comportamento é frequentemente mais flexível.

4. **Não exponha detalhes de implementação** através de error wrapping a menos que seja intencional

5. **Torne erros acionáveis**. Inclua contexto suficiente para debugar sem precisar acessar o código.

6. **Considere tipos de erro para domínios complexos**. Erros estruturados permitem tratamento inteligente.

7. **Teste caminhos de erro**. Seus testes devem verificar mensagens e tipos de erro.

## Pontos-Chave

1. **Tratamento de erro não é só `if err != nil`**. É sobre construir uma estratégia que ajuda a debugar problemas de produção.

2. **Wrap uma vez, log uma vez**. Logging duplicado desperdiça tempo e obscurece o problema real.

3. **Use `%w` para wrapping**, mas saiba quando NÃO fazer wrap (escondendo detalhes de implementação).

4. **Verificação de comportamento > verificação de tipo**. `errors.As` com interfaces é mais flexível que sentinel errors.

5. **Estrutura permite automação**. Erros estruturados podem direcionar códigos HTTP, métricas e alertas.

6. **Erros de domínio clarificam intenção**. `PaymentDeclined` é mais claro que `errors.New("payment failed")`.

O objetivo não é tratamento de erro perfeito—é tratamento de erro que ajuda você a descobrir o que deu errado às 3 da manhã quando produção está caída.
