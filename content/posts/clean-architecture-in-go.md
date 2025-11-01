---
title: "Simplified Clean Architecture in Go"
date: 2025-01-25
description: "A practical guide to implementing Clean Architecture in Golang with real examples and best practices for building modular, testable backend services."
tags: ["golang", "architecture", "clean-architecture", "backend"]
---

# Simplified Clean Architecture in Go

One of the best things I've learned while building backend services in Golang is how Clean Architecture can make your codebase more modular, testable, and resilient to change — if done right.

Here's a simplified breakdown I wish I had when I started:

## 🔹 Handler Layer (Presentation)

This is the entry point of your application. It handles HTTP/gRPC/GraphQL requests, converts them to domain-friendly inputs, and forwards them to your service layer.

Think of it as a **guard** — it validates input, deals with framework-specific quirks (like Fiber, Echo, Gin, etc.), and keeps your domain logic untouched.

### Example Handler

```go
package handlers

import (
	"net/http"
	"encoding/json"
)

type UserHandler struct {
	userService UserService
}

func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	// 1. Parse and validate input
	var input CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
	 http.Error(w, "Invalid request", http.StatusBadRequest)
	 return
	}

	// 2. Call service layer
	user, err := h.userService.CreateUser(r.Context(), input.Email, input.Name)
	if err != nil {
	 http.Error(w, err.Error(), http.StatusInternalServerError)
	 return
	}

	// 3. Return response
	json.NewEncoder(w).Encode(user)
}
```

**❌ Never put core logic here.** Otherwise, changing frameworks becomes a nightmare.

## 🔹 Service Layer (Application)

This is where the actual **use cases** live. It orchestrates logic, talks to repositories, and implements business processes.

This is your **brain** 🧠. It's okay to have a bit of technical logic here, but try to keep it minimal so business rules remain portable and clear.

### Example Service

```go
package services

import (
	"context"
	"errors"
	"strings"
)

type UserService struct {
	userRepo UserRepository
	emailService EmailService
}

func (s *UserService) CreateUser(ctx context.Context, email, name string) (*User, error) {
	// Business validation
	if !isValidEmail(email) {
	 return nil, errors.New("invalid email format")
	}

	// Check if user already exists
	existing, _ := s.userRepo.FindByEmail(ctx, email)
	if existing != nil {
	 return nil, errors.New("user already exists")
	}

	// Create user entity
	user := &User{
	 Email: strings.ToLower(email),
	 Name:  name,
	 Status: "active",
	}

	// Persist
	if err := s.userRepo.Create(ctx, user); err != nil {
	 return nil, err
	}

	// Send welcome email (async)
	go s.emailService.SendWelcome(user.Email)

	return user, nil
}

func isValidEmail(email string) bool {
	return strings.Contains(email, "@")
}
```

## 🔹 Repository Layer (Data Access)

This layer deals with **persistence**: databases, message queues, external APIs.

It's your **adapter to the outside world**. Keep it focused — just CRUD, no decision-making or calculations. That belongs in the service layer.

### Example Repository

```go
package repositories

import (
	"context"
	"database/sql"
)

type PostgresUserRepository struct {
	db *sql.DB
}

func (r *PostgresUserRepository) Create(ctx context.Context, user *User) error {
	query := `
	 INSERT INTO users (id, email, name, status, created_at)
	 VALUES ($1, $2, $3, $4, $5)
	`
	_, err := r.db.ExecContext(ctx, query,
	 user.ID,
	 user.Email,
	 user.Name,
	 user.Status,
	 user.CreatedAt,
	)
	return err
}

func (r *PostgresUserRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
	query := `SELECT id, email, name, status, created_at FROM users WHERE email = $1`

	var user User
	err := r.db.QueryRowContext(ctx, query, email).Scan(
	 &user.ID,
	 &user.Email,
	 &user.Name,
	 &user.Status,
	 &user.CreatedAt,
	)

	if err == sql.ErrNoRows {
	 return nil, nil
	}

	return &user, err
}
```

## 🔹 Model Layer (Domain)

At the heart of it all lies the **domain** — pure, clean Go structs representing your business concepts.

**No dependencies. Just business rules.**

This layer knows nothing about HTTP, SQL, Redis, or JSON. And that's the point.

### Example Domain Model

```go
package domain

import (
	"time"
	"github.com/google/uuid"
)

type User struct {
	ID        string
	Email     string
	Name      string
	Status    string
	CreatedAt time.Time
}

func NewUser(email, name string) *User {
	return &User{
	 ID:        uuid.New().String(),
	 Email:     email,
	 Name:      name,
	 Status:    "active",
	 CreatedAt: time.Now(),
	}
}

// Business logic methods
func (u *User) Deactivate() {
	u.Status = "inactive"
}

func (u *User) IsActive() bool {
	return u.Status == "active"
}
```

## 📌 Dependency Rule

Everything points **inward**:

```
Handlers → Services → Repositories → Models
```

**Inner layers don't know about outer layers.**

This means:

- Models have zero imports from other layers
- Services only import models
- Repositories only import models
- Handlers import everything

## 📎 Adapter Roles

- **Handlers** = Primary adapter (driving port) - they drive your application
- **Repositories** = Secondary adapter (driven port) - they're driven by your application

## Project Structure

Here's how I typically organize a Clean Architecture project:

```
project/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── domain/
│   │   └── user.go
│   ├── services/
│   │   └── user_service.go
│   ├── repositories/
│   │   ├── user_repository.go
│   │   └── postgres/
│   │       └── user_repo_impl.go
│   └── handlers/
│       └── http/
│           └── user_handler.go
├── pkg/
│   └── database/
│       └── postgres.go
└── go.mod
```

## Wiring It All Together

```go
package main

import (
	"database/sql"
	"log"
	"net/http"
)

func main() {
	// Infrastructure
	db, err := sql.Open("postgres", "connection_string")
	if err != nil {
	 log.Fatal(err)
	}

	// Repositories
	userRepo := repositories.NewPostgresUserRepository(db)

	// Services
	emailService := services.NewEmailService()
	userService := services.NewUserService(userRepo, emailService)

	// Handlers
	userHandler := handlers.NewUserHandler(userService)

	// Routes
	http.HandleFunc("/users", userHandler.CreateUser)

	log.Println("Server starting on :8080")
	http.ListenAndServe(":8080", nil)
}
```

## Benefits of This Approach

✅ **Testability**: Each layer can be tested independently with mocks
✅ **Maintainability**: Clear separation of concerns
✅ **Flexibility**: Easy to swap databases or frameworks
✅ **Scalability**: Add new features without touching existing code

## Common Pitfalls to Avoid

❌ **Leaking abstractions**: Don't let SQL errors bubble up to handlers
❌ **Fat services**: Keep services focused on orchestration, not data transformation
❌ **Anemic models**: Add behavior to your domain models, not just data
❌ **Over-abstraction**: Don't create interfaces for everything "just in case"

## Conclusion

💡 **Clean Architecture isn't about overengineering** — it's about protecting your core business logic from tech churn. Whether you switch frameworks, protocols, or databases, your core stays clean.

If you're building with Go and want to future-proof your codebase, this pattern is worth mastering.

## Testing Example

Here's how easy it becomes to test your service:

```go
package services_test

import (
	"context"
	"testing"
)

type MockUserRepository struct {
	users map[string]*User
}

func (m *MockUserRepository) Create(ctx context.Context, user *User) error {
	m.users[user.Email] = user
	return nil
}

func (m *MockUserRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
	return m.users[email], nil
}

func TestCreateUser(t *testing.T) {
	// Arrange
	mockRepo := &MockUserRepository{users: make(map[string]*User)}
	mockEmail := &MockEmailService{}
	service := NewUserService(mockRepo, mockEmail)

	// Act
	user, err := service.CreateUser(context.Background(), "test@example.com", "Test User")

	// Assert
	if err != nil {
	 t.Fatalf("expected no error, got %v", err)
	}
	if user.Email != "test@example.com" {
	 t.Errorf("expected email test@example.com, got %s", user.Email)
	}
}
```
