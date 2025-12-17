---
title: "Building REST APIs with Go"
date: 2025-01-20
description: "Learn how to build production-ready REST APIs using Go's standard library and best practices."
tags: ["golang", "api", "web-development", "rest"]
---

# Building REST APIs with Go

Go's standard library provides everything you need to build robust REST APIs. In this post, we'll explore how to create a production-ready API without relying on external frameworks.

## Basic HTTP Server

Let's start with a simple HTTP server:

```go
package main

import (
    "encoding/json"
    "log"
    "net/http"
)

type Response struct {
    Message string `json:"message"`
    Status  int    `json:"status"`
}

func main() {
    http.HandleFunc("/api/hello", helloHandler)
    log.Fatal(http.ListenAndServe(":8080", nil))
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    response := Response{
        Message: "Hello, World!",
        Status:  200,
    }

    json.NewEncoder(w).Encode(response)
}
```

## RESTful Routing

For a proper REST API, we need to handle different HTTP methods:

```go
func userHandler(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        getUser(w, r)
    case http.MethodPost:
        createUser(w, r)
    case http.MethodPut:
        updateUser(w, r)
    case http.MethodDelete:
        deleteUser(w, r)
    default:
        w.WriteHeader(http.StatusMethodNotAllowed)
    }
}
```

## JSON Handling

Go makes JSON encoding and decoding straightforward:

```go
type User struct {
    ID    int    `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

func createUser(w http.ResponseWriter, r *http.Request) {
    var user User

    // Decode request body
    if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    // Process user...

    // Send response
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(user)
}
```

## Middleware Pattern

Middleware allows you to add cross-cutting concerns:

```go
func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        log.Printf("%s %s", r.Method, r.URL.Path)
        next(w, r)
    }
}

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", "*")
        next(w, r)
    }
}

// Usage
http.HandleFunc("/api/users",
    loggingMiddleware(
        corsMiddleware(userHandler),
    ),
)
```

## Error Handling

Proper error handling is crucial for APIs:

```go
type APIError struct {
    Error   string `json:"error"`
    Code    int    `json:"code"`
    Details string `json:"details,omitempty"`
}

func sendError(w http.ResponseWriter, message string, code int) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(code)

    json.NewEncoder(w).Encode(APIError{
        Error: message,
        Code:  code,
    })
}
```

## Best Practices

1. **Use proper HTTP status codes**: 200, 201, 400, 404, 500, etc.
2. **Validate input**: Always validate and sanitize user input
3. **Handle errors gracefully**: Return meaningful error messages
4. **Use middleware**: For logging, authentication, CORS, etc.
5. **Version your API**: `/api/v1/users` instead of `/api/users`

## Conclusion

Go's standard library provides a solid foundation for building REST APIs. While frameworks can be helpful, understanding the fundamentals gives you more control and flexibility.

In the next post, we'll add database integration and authentication to our API!
