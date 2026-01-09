---
title: "Construindo APIs REST com Go"
date: 2025-01-20
description: "Um guia prático para construir APIs RESTful robustas usando Go e a biblioteca padrão."
tags: ["golang", "api", "rest", "backend"]
---

Go é uma escolha excelente para construir APIs REST devido à sua simplicidade, performance e suporte robusto a concorrência. Neste post, vamos construir uma API REST completa do zero usando apenas a biblioteca padrão do Go.

## Por que Go para APIs?

1. **Performance**: APIs em Go são extremamente rápidas
2. **Baixo uso de memória**: Ideal para containerização e serverless
3. **Concorrência nativa**: Goroutines tornam o handling de requisições eficiente
4. **Biblioteca padrão robusta**: `net/http` é poderoso o suficiente para produção
5. **Compilação estática**: Um único binário para deploy

## Estrutura do Projeto

Vamos criar uma API de gerenciamento de usuários:

```
api/
├── main.go
├── handlers/
│   └── users.go
├── models/
│   └── user.go
├── middleware/
│   └── logging.go
└── go.mod
```

## O Servidor HTTP

Começamos com o servidor básico:

```go
// main.go
package main

import (
    "log"
    "net/http"
)

func main() {
    mux := http.NewServeMux()

    // Rotas
    mux.HandleFunc("GET /api/users", listUsers)
    mux.HandleFunc("POST /api/users", createUser)
    mux.HandleFunc("GET /api/users/{id}", getUser)
    mux.HandleFunc("PUT /api/users/{id}", updateUser)
    mux.HandleFunc("DELETE /api/users/{id}", deleteUser)

    log.Println("Servidor iniciando na porta 8080...")
    if err := http.ListenAndServe(":8080", mux); err != nil {
        log.Fatal(err)
    }
}
```

## Modelo de Dados

```go
// models/user.go
package models

import "time"

type User struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    Email     string    `json:"email"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type CreateUserRequest struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}

type UpdateUserRequest struct {
    Name  string `json:"name,omitempty"`
    Email string `json:"email,omitempty"`
}
```

## Handlers

```go
// handlers/users.go
package handlers

import (
    "encoding/json"
    "net/http"
    "sync"
    "time"

    "github.com/google/uuid"
    "myapi/models"
)

var (
    users = make(map[string]models.User)
    mu    sync.RWMutex
)

// ListUsers retorna todos os usuários
func ListUsers(w http.ResponseWriter, r *http.Request) {
    mu.RLock()
    defer mu.RUnlock()

    userList := make([]models.User, 0, len(users))
    for _, user := range users {
        userList = append(userList, user)
    }

    respondJSON(w, http.StatusOK, userList)
}

// CreateUser cria um novo usuário
func CreateUser(w http.ResponseWriter, r *http.Request) {
    var req models.CreateUserRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respondError(w, http.StatusBadRequest, "JSON inválido")
        return
    }

    // Validação
    if req.Name == "" || req.Email == "" {
        respondError(w, http.StatusBadRequest, "Nome e email são obrigatórios")
        return
    }

    user := models.User{
        ID:        uuid.New().String(),
        Name:      req.Name,
        Email:     req.Email,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }

    mu.Lock()
    users[user.ID] = user
    mu.Unlock()

    respondJSON(w, http.StatusCreated, user)
}

// GetUser retorna um usuário específico
func GetUser(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")

    mu.RLock()
    user, exists := users[id]
    mu.RUnlock()

    if !exists {
        respondError(w, http.StatusNotFound, "Usuário não encontrado")
        return
    }

    respondJSON(w, http.StatusOK, user)
}

// UpdateUser atualiza um usuário existente
func UpdateUser(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")

    mu.Lock()
    defer mu.Unlock()

    user, exists := users[id]
    if !exists {
        respondError(w, http.StatusNotFound, "Usuário não encontrado")
        return
    }

    var req models.UpdateUserRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respondError(w, http.StatusBadRequest, "JSON inválido")
        return
    }

    if req.Name != "" {
        user.Name = req.Name
    }
    if req.Email != "" {
        user.Email = req.Email
    }
    user.UpdatedAt = time.Now()

    users[id] = user
    respondJSON(w, http.StatusOK, user)
}

// DeleteUser remove um usuário
func DeleteUser(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")

    mu.Lock()
    defer mu.Unlock()

    if _, exists := users[id]; !exists {
        respondError(w, http.StatusNotFound, "Usuário não encontrado")
        return
    }

    delete(users, id)
    w.WriteHeader(http.StatusNoContent)
}

// Helpers
func respondJSON(w http.ResponseWriter, status int, data any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
    respondJSON(w, status, map[string]string{"error": message})
}
```

## Middleware

```go
// middleware/logging.go
package middleware

import (
    "log"
    "net/http"
    "time"
)

type responseWriter struct {
    http.ResponseWriter
    status int
}

func (rw *responseWriter) WriteHeader(code int) {
    rw.status = code
    rw.ResponseWriter.WriteHeader(code)
}

func Logging(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()

        rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
        next.ServeHTTP(rw, r)

        log.Printf(
            "%s %s %d %v",
            r.Method,
            r.URL.Path,
            rw.status,
            time.Since(start),
        )
    })
}

func CORS(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

        if r.Method == "OPTIONS" {
            w.WriteHeader(http.StatusOK)
            return
        }

        next.ServeHTTP(w, r)
    })
}
```

## Testando a API

Com a API rodando, você pode testar usando curl:

```bash
# Criar usuário
curl -X POST http://localhost:8080/api/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Paulo", "email": "paulo@exemplo.com"}'

# Listar usuários
curl http://localhost:8080/api/users

# Buscar usuário específico
curl http://localhost:8080/api/users/{id}

# Atualizar usuário
curl -X PUT http://localhost:8080/api/users/{id} \
  -H "Content-Type: application/json" \
  -d '{"name": "Paulo Vicente"}'

# Deletar usuário
curl -X DELETE http://localhost:8080/api/users/{id}
```

## Boas Práticas

1. **Sempre valide entrada**: Nunca confie em dados do cliente
2. **Use códigos HTTP corretos**: 200, 201, 400, 404, 500
3. **Trate erros graciosamente**: Retorne mensagens úteis
4. **Use middleware**: Logging, CORS, autenticação
5. **Documente sua API**: Use OpenAPI/Swagger

## Próximos Passos

- Adicionar autenticação JWT
- Conectar a um banco de dados (PostgreSQL)
- Adicionar testes unitários e de integração
- Implementar paginação
- Adicionar rate limiting

A biblioteca padrão do Go é poderosa o suficiente para construir APIs de produção. Para casos mais complexos, considere frameworks como Gin, Echo ou Chi.
