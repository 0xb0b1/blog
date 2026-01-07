---
title: "Clean Architecture em Go"
date: 2025-01-25
description: "Implementando Clean Architecture em aplicações Go para código mais testável, manutenível e escalável."
tags: ["golang", "arquitetura", "clean-architecture", "design-patterns"]
---

Clean Architecture é um padrão de design que separa as preocupações do seu código em camadas distintas, tornando-o mais testável, manutenível e independente de frameworks e bancos de dados.

## Os Princípios

A Clean Architecture é baseada em alguns princípios fundamentais:

1. **Independência de frameworks**: A arquitetura não depende de bibliotecas externas
2. **Testabilidade**: A lógica de negócio pode ser testada sem UI, banco de dados ou servidor
3. **Independência de UI**: A UI pode mudar sem alterar o resto do sistema
4. **Independência de banco de dados**: Você pode trocar PostgreSQL por MongoDB sem mudar regras de negócio
5. **Independência de agentes externos**: As regras de negócio não sabem nada sobre o mundo externo

## As Camadas

```
┌─────────────────────────────────────────┐
│            Frameworks & Drivers         │
│  (HTTP, gRPC, CLI, Banco de Dados)      │
├─────────────────────────────────────────┤
│           Interface Adapters            │
│    (Controllers, Gateways, Presenters)  │
├─────────────────────────────────────────┤
│          Application Business           │
│              (Use Cases)                │
├─────────────────────────────────────────┤
│         Enterprise Business             │
│              (Entities)                 │
└─────────────────────────────────────────┘
```

## Estrutura do Projeto

```
project/
├── cmd/
│   └── api/
│       └── main.go
├── internal/
│   ├── domain/           # Entidades e regras de negócio
│   │   ├── user.go
│   │   └── errors.go
│   ├── usecase/          # Casos de uso
│   │   ├── user_usecase.go
│   │   └── interfaces.go
│   ├── repository/       # Implementações de repositório
│   │   ├── postgres/
│   │   │   └── user_repository.go
│   │   └── memory/
│   │       └── user_repository.go
│   └── delivery/         # Handlers HTTP/gRPC
│       └── http/
│           └── user_handler.go
├── pkg/
│   └── database/
│       └── postgres.go
└── go.mod
```

## Camada de Domínio

A camada mais interna contém as entidades e regras de negócio:

```go
// internal/domain/user.go
package domain

import (
    "errors"
    "regexp"
    "time"
)

var (
    ErrUserNotFound    = errors.New("usuário não encontrado")
    ErrInvalidEmail    = errors.New("email inválido")
    ErrEmailExists     = errors.New("email já cadastrado")
)

type User struct {
    ID        string
    Name      string
    Email     string
    Password  string
    CreatedAt time.Time
    UpdatedAt time.Time
}

// NewUser cria um novo usuário com validação
func NewUser(name, email, password string) (*User, error) {
    if !isValidEmail(email) {
        return nil, ErrInvalidEmail
    }

    return &User{
        Name:      name,
        Email:     email,
        Password:  password,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }, nil
}

// Regra de negócio: validação de email
func isValidEmail(email string) bool {
    pattern := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`
    matched, _ := regexp.MatchString(pattern, email)
    return matched
}

// UpdatePassword atualiza a senha com validação
func (u *User) UpdatePassword(newPassword string) error {
    if len(newPassword) < 8 {
        return errors.New("senha deve ter pelo menos 8 caracteres")
    }
    u.Password = newPassword
    u.UpdatedAt = time.Now()
    return nil
}
```

## Interfaces de Repositório

Definimos interfaces para abstrair o acesso a dados:

```go
// internal/usecase/interfaces.go
package usecase

import (
    "context"
    "myapp/internal/domain"
)

// UserRepository define as operações de persistência
type UserRepository interface {
    Create(ctx context.Context, user *domain.User) error
    GetByID(ctx context.Context, id string) (*domain.User, error)
    GetByEmail(ctx context.Context, email string) (*domain.User, error)
    Update(ctx context.Context, user *domain.User) error
    Delete(ctx context.Context, id string) error
    List(ctx context.Context, limit, offset int) ([]*domain.User, error)
}

// PasswordHasher define operações de hash de senha
type PasswordHasher interface {
    Hash(password string) (string, error)
    Compare(hash, password string) bool
}
```

## Casos de Uso

A camada de casos de uso orquestra a lógica de aplicação:

```go
// internal/usecase/user_usecase.go
package usecase

import (
    "context"

    "github.com/google/uuid"
    "myapp/internal/domain"
)

type UserUseCase struct {
    userRepo UserRepository
    hasher   PasswordHasher
}

func NewUserUseCase(repo UserRepository, hasher PasswordHasher) *UserUseCase {
    return &UserUseCase{
        userRepo: repo,
        hasher:   hasher,
    }
}

type CreateUserInput struct {
    Name     string
    Email    string
    Password string
}

func (uc *UserUseCase) Create(ctx context.Context, input CreateUserInput) (*domain.User, error) {
    // Verificar se email já existe
    existing, _ := uc.userRepo.GetByEmail(ctx, input.Email)
    if existing != nil {
        return nil, domain.ErrEmailExists
    }

    // Criar entidade com validação de domínio
    user, err := domain.NewUser(input.Name, input.Email, input.Password)
    if err != nil {
        return nil, err
    }

    // Gerar ID
    user.ID = uuid.New().String()

    // Hash da senha
    hashedPassword, err := uc.hasher.Hash(input.Password)
    if err != nil {
        return nil, err
    }
    user.Password = hashedPassword

    // Persistir
    if err := uc.userRepo.Create(ctx, user); err != nil {
        return nil, err
    }

    return user, nil
}

func (uc *UserUseCase) GetByID(ctx context.Context, id string) (*domain.User, error) {
    user, err := uc.userRepo.GetByID(ctx, id)
    if err != nil {
        return nil, err
    }
    if user == nil {
        return nil, domain.ErrUserNotFound
    }
    return user, nil
}
```

## Implementação de Repositório

```go
// internal/repository/postgres/user_repository.go
package postgres

import (
    "context"
    "database/sql"

    "myapp/internal/domain"
)

type UserRepository struct {
    db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
    return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
    query := `
        INSERT INTO users (id, name, email, password, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `
    _, err := r.db.ExecContext(ctx, query,
        user.ID, user.Name, user.Email, user.Password,
        user.CreatedAt, user.UpdatedAt,
    )
    return err
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
    query := `SELECT id, name, email, password, created_at, updated_at FROM users WHERE id = $1`

    user := &domain.User{}
    err := r.db.QueryRowContext(ctx, query, id).Scan(
        &user.ID, &user.Name, &user.Email, &user.Password,
        &user.CreatedAt, &user.UpdatedAt,
    )
    if err == sql.ErrNoRows {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }
    return user, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
    query := `SELECT id, name, email, password, created_at, updated_at FROM users WHERE email = $1`

    user := &domain.User{}
    err := r.db.QueryRowContext(ctx, query, email).Scan(
        &user.ID, &user.Name, &user.Email, &user.Password,
        &user.CreatedAt, &user.UpdatedAt,
    )
    if err == sql.ErrNoRows {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }
    return user, nil
}
```

## Handler HTTP

```go
// internal/delivery/http/user_handler.go
package http

import (
    "encoding/json"
    "net/http"

    "myapp/internal/usecase"
)

type UserHandler struct {
    userUseCase *usecase.UserUseCase
}

func NewUserHandler(uc *usecase.UserUseCase) *UserHandler {
    return &UserHandler{userUseCase: uc}
}

func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
    var input usecase.CreateUserInput
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
        respondError(w, http.StatusBadRequest, "JSON inválido")
        return
    }

    user, err := h.userUseCase.Create(r.Context(), input)
    if err != nil {
        handleError(w, err)
        return
    }

    respondJSON(w, http.StatusCreated, user)
}

func (h *UserHandler) Get(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")

    user, err := h.userUseCase.GetByID(r.Context(), id)
    if err != nil {
        handleError(w, err)
        return
    }

    respondJSON(w, http.StatusOK, user)
}
```

## Injeção de Dependências

```go
// cmd/api/main.go
package main

import (
    "database/sql"
    "log"
    "net/http"

    _ "github.com/lib/pq"

    "myapp/internal/delivery/http"
    "myapp/internal/repository/postgres"
    "myapp/internal/usecase"
    "myapp/pkg/hasher"
)

func main() {
    // Conexão com banco de dados
    db, err := sql.Open("postgres", "postgres://localhost/myapp?sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Repositórios
    userRepo := postgres.NewUserRepository(db)

    // Serviços
    passwordHasher := hasher.NewBcryptHasher()

    // Casos de uso
    userUseCase := usecase.NewUserUseCase(userRepo, passwordHasher)

    // Handlers
    userHandler := http.NewUserHandler(userUseCase)

    // Rotas
    mux := http.NewServeMux()
    mux.HandleFunc("POST /api/users", userHandler.Create)
    mux.HandleFunc("GET /api/users/{id}", userHandler.Get)

    log.Println("Servidor iniciando na porta 8080...")
    http.ListenAndServe(":8080", mux)
}
```

## Benefícios

1. **Testabilidade**: Cada camada pode ser testada isoladamente
2. **Flexibilidade**: Trocar banco de dados ou framework é simples
3. **Manutenibilidade**: Código organizado e fácil de entender
4. **Escalabilidade**: Fácil adicionar novas funcionalidades
5. **Clareza**: Separação clara de responsabilidades

## Quando Usar

Clean Architecture é ideal para:
- Aplicações de médio a grande porte
- Sistemas que precisam de longevidade
- Equipes múltiplas trabalhando no mesmo código
- Sistemas com requisitos de teste rigorosos

Para aplicações pequenas ou MVPs, a complexidade adicional pode não valer a pena.
