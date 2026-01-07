---
title: "Entendendo Interfaces em Go"
date: 2025-01-18
description: "Um mergulho profundo em interfaces Go, duck typing e como usar interfaces para escrever código flexível e testável."
tags: ["golang", "interfaces", "design-patterns", "tutorial"]
---

Interfaces são um dos recursos mais poderosos do Go. Elas permitem escrever código flexível, desacoplado e altamente testável. Vamos explorar como funcionam e como usá-las efetivamente.

## O Que São Interfaces?

Uma interface em Go é um tipo que especifica um conjunto de métodos. Qualquer tipo que implemente esses métodos automaticamente satisfaz a interface - não há necessidade de declaração explícita.

```go
// Interface simples
type Speaker interface {
    Speak() string
}

// Tipos que implementam Speaker
type Dog struct {
    Name string
}

func (d Dog) Speak() string {
    return "Au au!"
}

type Cat struct {
    Name string
}

func (c Cat) Speak() string {
    return "Miau!"
}

// Ambos Dog e Cat implementam Speaker implicitamente
```

## Duck Typing

Go usa "duck typing": se parece um pato e faz quack como um pato, então é um pato.

```go
func MakeSound(s Speaker) {
    fmt.Println(s.Speak())
}

func main() {
    dog := Dog{Name: "Rex"}
    cat := Cat{Name: "Mimi"}

    MakeSound(dog) // Au au!
    MakeSound(cat) // Miau!
}
```

## Interface Vazia

A interface vazia `interface{}` (ou `any` no Go 1.18+) pode conter qualquer tipo:

```go
func PrintAnything(v interface{}) {
    fmt.Printf("Tipo: %T, Valor: %v\n", v, v)
}

PrintAnything(42)         // Tipo: int, Valor: 42
PrintAnything("texto")    // Tipo: string, Valor: texto
PrintAnything([]int{1,2}) // Tipo: []int, Valor: [1 2]
```

## Type Assertions

Para recuperar o tipo concreto de uma interface:

```go
func Process(v interface{}) {
    // Type assertion com verificação
    str, ok := v.(string)
    if ok {
        fmt.Println("É uma string:", str)
        return
    }

    // Type switch para múltiplos tipos
    switch val := v.(type) {
    case int:
        fmt.Println("É um int:", val)
    case float64:
        fmt.Println("É um float:", val)
    default:
        fmt.Println("Tipo desconhecido")
    }
}
```

## Interfaces da Biblioteca Padrão

Go tem várias interfaces importantes na biblioteca padrão:

### io.Reader e io.Writer

```go
type Reader interface {
    Read(p []byte) (n int, err error)
}

type Writer interface {
    Write(p []byte) (n int, err error)
}
```

Exemplo de uso:

```go
func CopyData(w io.Writer, r io.Reader) error {
    buf := make([]byte, 1024)
    for {
        n, err := r.Read(buf)
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }
        _, err = w.Write(buf[:n])
        if err != nil {
            return err
        }
    }
    return nil
}

// Funciona com qualquer Reader/Writer
// Arquivos, conexões de rede, buffers, etc.
```

### Stringer

```go
type Stringer interface {
    String() string
}

type Person struct {
    Name string
    Age  int
}

func (p Person) String() string {
    return fmt.Sprintf("%s (%d anos)", p.Name, p.Age)
}

func main() {
    p := Person{Name: "Paulo", Age: 30}
    fmt.Println(p) // Paulo (30 anos)
}
```

### error

```go
type error interface {
    Error() string
}

// Erro customizado
type ValidationError struct {
    Field   string
    Message string
}

func (e ValidationError) Error() string {
    return fmt.Sprintf("validação falhou em %s: %s", e.Field, e.Message)
}
```

## Composição de Interfaces

Interfaces podem ser compostas:

```go
type Reader interface {
    Read(p []byte) (n int, err error)
}

type Writer interface {
    Write(p []byte) (n int, err error)
}

type Closer interface {
    Close() error
}

// Composição
type ReadWriter interface {
    Reader
    Writer
}

type ReadWriteCloser interface {
    Reader
    Writer
    Closer
}
```

## Interfaces para Testabilidade

Um dos principais usos de interfaces é facilitar testes:

```go
// Interface para serviço de email
type EmailSender interface {
    Send(to, subject, body string) error
}

// Implementação real
type SMTPSender struct {
    host string
    port int
}

func (s *SMTPSender) Send(to, subject, body string) error {
    // Envia email via SMTP
    return nil
}

// Mock para testes
type MockEmailSender struct {
    SentEmails []struct {
        To, Subject, Body string
    }
}

func (m *MockEmailSender) Send(to, subject, body string) error {
    m.SentEmails = append(m.SentEmails, struct {
        To, Subject, Body string
    }{to, subject, body})
    return nil
}

// Caso de uso que usa a interface
type UserService struct {
    emailSender EmailSender
}

func (s *UserService) RegisterUser(email string) error {
    // ... lógica de registro
    return s.emailSender.Send(email, "Bem-vindo!", "Obrigado por se registrar")
}

// Teste
func TestRegisterUser(t *testing.T) {
    mock := &MockEmailSender{}
    service := &UserService{emailSender: mock}

    err := service.RegisterUser("teste@exemplo.com")
    if err != nil {
        t.Fatal(err)
    }

    if len(mock.SentEmails) != 1 {
        t.Error("Deveria ter enviado um email")
    }

    if mock.SentEmails[0].To != "teste@exemplo.com" {
        t.Error("Email enviado para destinatário errado")
    }
}
```

## Boas Práticas

### 1. Interfaces Pequenas

```go
// Bom: interfaces pequenas e focadas
type Reader interface {
    Read(p []byte) (n int, err error)
}

// Evite: interfaces grandes
type DoEverything interface {
    Read() error
    Write() error
    Delete() error
    Update() error
    List() error
    // ... muitos métodos
}
```

### 2. Aceite Interfaces, Retorne Tipos Concretos

```go
// Bom
func NewUserService(repo UserRepository) *UserService {
    return &UserService{repo: repo}
}

// Evite
func NewUserService(repo UserRepository) UserServiceInterface {
    return &UserService{repo: repo}
}
```

### 3. Defina Interfaces no Consumidor

```go
// Pacote consumer - define a interface que precisa
package consumer

type DataStore interface {
    Get(key string) (string, error)
    Set(key string, value string) error
}

type Service struct {
    store DataStore
}

// Pacote producer - implementa sem conhecer a interface
package producer

type RedisStore struct {
    // ...
}

func (r *RedisStore) Get(key string) (string, error) { /* ... */ }
func (r *RedisStore) Set(key string, value string) error { /* ... */ }
```

### 4. Use Type Embedding

```go
type Logger interface {
    Log(message string)
}

type EnhancedLogger struct {
    Logger  // Embeds Logger interface
    prefix string
}

func (e *EnhancedLogger) LogWithPrefix(message string) {
    e.Log(e.prefix + ": " + message)
}
```

## Conclusão

Interfaces em Go são poderosas por sua simplicidade:

1. **Implícitas**: Não precisa declarar que implementa
2. **Composíveis**: Combine interfaces pequenas
3. **Testáveis**: Facilita mocks e stubs
4. **Desacopladas**: Reduz dependências entre pacotes

Use interfaces para definir comportamentos, não dados. Mantenha-as pequenas e focadas. Defina-as onde são consumidas, não onde são implementadas.
