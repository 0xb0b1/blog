---
title: "Strangler Fig in Practice: Moving a REST API from Python to Go Without Downtime"
date: 2026-02-23
draft: false
description: "
How we eliminated the Python middleman by adding REST endpoints directly to an existing Go microservice — reusing its service layer, repositories, and async patterns — while keeping the monolith running for 100+ other endpoints.
"
tags: ["go", "python", "strangler-fig", "microservices", "rest-api", "architecture", "jwt", "chi"]
---

Migrating from a monolith to microservices is rarely a clean break. You can't rewrite everything at once, and you can't afford downtime. The strangler fig pattern gives you a way to do it incrementally — peel off one bounded context at a time, route traffic to the new service, and let the old code wither naturally.

This post walks through a real example: extracting the notification domain from a Python/FastAPI monolith into a Go service by adding REST handlers that reuse the same service layer the gRPC handlers already call. No new services, no new containers — just a new protocol adapter on an existing codebase.

## The Problem: Python as a Middleman

Our mobile app (R10 Score) has a Python/FastAPI monolith that does everything: authentication, match data, competitions, player stats, in-app purchases, and push notifications. The notification-related endpoints look like this:

```
PUT    /api/v1/users/device/              → register device token
POST   /api/v1/users/devices/delete/      → unregister device
GET    /api/v1/notifications/             → list notification preferences
PUT    /api/v1/notifications/{kind}/      → enable a notification kind
DELETE /api/v1/notifications/{kind}/      → disable a notification kind
GET    /api/v1/users/heart-team/          → get favorite team
POST   /api/v1/users/heart-team/{id}/     → set favorite team
DELETE /api/v1/users/heart-team/          → remove favorite team
POST   /api/v1/matches/{id}/notification/ → subscribe to match notifications
DELETE /api/v1/matches/{id}/notification/ → unsubscribe from match
```

The problem: the Go service already handled all the notification business logic via gRPC. Python was just a proxy. Every mobile request went through a pointless hop:

```
Mobile → Python REST → gRPC → Go → DB/SNS
```

Two services, two languages, two ORMs hitting the same PostgreSQL tables. Python validated the request, converted it to a gRPC call, and forwarded it. The actual work happened in Go. We were paying for serialization, network latency, and operational complexity in exchange for... nothing.

## The Architecture Shift

The Go notifications backend already had:
- A **chi router** serving health checks on port 8081
- A **service layer** (`DeviceService`, `UserSubscriptionService`, `SubscriptionService`) used by gRPC handlers
- A **repository layer** with PostgreSQL access via `pgxpool`
- **SNS integration** for push notification delivery

The plan: add REST handlers that call the same services the gRPC handlers call. No new service layer, no new business logic — just a new front door.

```
BEFORE:  Mobile → Python REST → gRPC → Go services → DB/SNS
AFTER:   Mobile → Go REST → Go services → DB/SNS
         Dataloader → gRPC → Go services → DB/SNS  (unchanged)
```

## Phase 1: JWT Authentication

The Python service authenticated by calling FusionAuth's validation API on every request (with a 15-minute cache). The Go service needed to authenticate directly.

We chose **local JWT validation via JWKS** — fetch FusionAuth's public keys once, cache them in memory, refresh hourly. No HTTP call per request.

```go
// internal/auth/jwt.go
type JWKSProvider struct {
    jwksURL         string
    refreshInterval time.Duration
    mu              sync.RWMutex
    keys            map[string]*rsa.PublicKey // kid → public key
}

func (p *JWKSProvider) Keyfunc(token *jwt.Token) (any, error) {
    kid, ok := token.Header["kid"].(string)
    if !ok {
        return nil, fmt.Errorf("token missing kid header")
    }

    p.mu.RLock()
    key, exists := p.keys[kid]
    p.mu.RUnlock()

    if !exists {
        return nil, fmt.Errorf("unknown kid: %s", kid)
    }
    return key, nil
}
```

The middleware extracts the JWT, validates it with the cached public key, and stuffs the authenticated user into the request context:

```go
// internal/auth/middleware.go
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        authHeader := r.Header.Get("Authorization")
        tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

        token, err := jwt.Parse(tokenStr, m.jwks.Keyfunc,
            jwt.WithValidMethods([]string{"RS256"}),
        )
        if err != nil {
            writeError(w, http.StatusUnauthorized, "invalid token")
            return
        }

        claims := token.Claims.(jwt.MapClaims)
        userID, _ := uuid.Parse(claims["sub"].(string))

        user := &AuthUser{
            ID:       userID,
            Role:     extractRole(claims),
            Language: extractLanguage(claims),
        }

        ctx := ContextWithUser(r.Context(), user)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

Every handler downstream just calls `auth.UserFromContext(r.Context())` to get the authenticated user. Parse once, use everywhere.

The performance difference is meaningful: RSA signature verification takes microseconds. An HTTP round-trip to FusionAuth's validation API takes milliseconds at best — and introduces a runtime dependency on FusionAuth availability.

## Phase 2: New Repositories for Validation

The Go service already had repositories for devices, subscriptions, topics, and matches. But the REST endpoints needed validation queries that the gRPC handlers never needed — because Python handled that validation before forwarding the gRPC call.

Three new repositories, all reading from existing Django-managed tables:

```go
// internal/repository/user.go — heart team queries
type UserRepo interface {
    GetByID(ctx context.Context, id uuid.UUID) (*User, error)
    GetHeartTeam(ctx context.Context, userID uuid.UUID) (*HeartTeamInfo, error)
    SetHeartTeam(ctx context.Context, userID, teamID uuid.UUID) error
    ClearHeartTeam(ctx context.Context, userID uuid.UUID) error
    GetLastHeartTeamDeletion(ctx context.Context, userID uuid.UUID) (*time.Time, error)
    SetLastHeartTeamDeletion(ctx context.Context, userID uuid.UUID, t time.Time) error
}

// internal/repository/favorite.go — match subscription queries
type FavoriteRepo interface {
    AddSubscribedMatch(ctx context.Context, userID, matchID uuid.UUID) error
    RemoveSubscribedMatch(ctx context.Context, userID, matchID uuid.UUID) error
    IsSubscribedMatch(ctx context.Context, userID, matchID uuid.UUID) (bool, error)
}

// internal/repository/team.go — team existence validation
type TeamRepo interface {
    Exists(ctx context.Context, teamID uuid.UUID) (bool, error)
    GetByID(ctx context.Context, teamID uuid.UUID) (*Team, error)
}
```

The existing `NotificationRepo` also needed write operations. Before, it only read enabled kinds (for gRPC subscription logic). Now it needs to enable, disable, and bulk-update preferences:

```go
// internal/repository/notification.go — added for REST
EnableKind(ctx context.Context, userID uuid.UUID, kind TopicKind) error
DisableKind(ctx context.Context, userID uuid.UUID, kind TopicKind) error
BulkSetKinds(ctx context.Context, userID uuid.UUID, toEnable, toDisable []TopicKind) error
DisableReminderKindsExcept(ctx context.Context, userID uuid.UUID, except TopicKind) error
```

All write operations use `ON CONFLICT` clauses for idempotency — REST is stateless, clients may retry, and the database should handle it gracefully:

```go
func (r *NotificationRepository) EnableKind(ctx context.Context, userID uuid.UUID, kind models.TopicKind) error {
    query := `
        INSERT INTO r10_notification (id, user_id, kind, is_enabled, created_at, updated_at)
        VALUES (gen_random_uuid(), $1, $2, true, NOW(), NOW())
        ON CONFLICT (user_id, kind) DO UPDATE SET is_enabled = true, updated_at = NOW()
    `
    _, err := r.pool.Exec(ctx, query, userID, string(kind))
    return err
}
```

## Phase 3: REST Handlers — Thin Protocol Adapters

The handler struct holds references to repositories and services. No business logic here — just protocol translation:

```go
// internal/rest/handlers.go
type Handler struct {
    userRepo      repository.UserRepo
    favoriteRepo  repository.FavoriteRepo
    teamRepo      repository.TeamRepo
    notifRepo     repository.NotificationRepo
    deviceService *service.DeviceService
    userSubSvc    *service.UserSubscriptionService
    subSvc        *service.SubscriptionService
    logger        *zap.Logger
    cfg           config.RESTConfig
    asyncSem      chan struct{}
}
```

Route registration is straightforward — all endpoints under `/api/v1/` with JWT authentication:

```go
// internal/rest/routes.go
func RegisterRoutes(r chi.Router, h *Handler, authMW *auth.Middleware) {
    r.Route("/api/v1", func(r chi.Router) {
        r.Use(authMW.Authenticate)

        r.Put("/devices", h.RegisterDevice)
        r.Delete("/devices", h.UnregisterDevice)

        r.Get("/notifications", h.ListNotifications)
        r.Put("/notifications", h.BulkUpdateNotifications)
        r.Put("/notifications/{kind}", h.EnableNotification)
        r.Delete("/notifications/{kind}", h.DisableNotification)

        r.Post("/matches/{matchId}/subscribe", h.SubscribeMatch)
        r.Delete("/matches/{matchId}/subscribe", h.UnsubscribeMatch)

        r.Post("/heart-team/{teamId}", h.SetHeartTeam)
        r.Delete("/heart-team", h.RemoveHeartTeam)
        r.Get("/heart-team", h.GetHeartTeam)
    })
}
```

Note the URL paths are new — not matching Python's `/api/v1.8/users/device/` style. This was intentional. New paths mean we can run both APIs simultaneously without path conflicts, though it does require a coordinated mobile release.

### Sync vs. Async: A Strategic Split

The handlers follow a consistent pattern: **synchronous database writes, asynchronous SNS operations**. Users expect immediate feedback when changing preferences. SNS topic subscriptions can be eventually consistent.

Device registration is fully synchronous — the mobile app reads the device record immediately after:

```go
// internal/rest/device.go
func (h *Handler) RegisterDevice(w http.ResponseWriter, r *http.Request) {
    user := auth.UserFromContext(r.Context())

    var req registerDeviceRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    err := h.deviceService.RegisterDevice(r.Context(), service.RegisterDeviceRequest{
        UserID:      user.ID,
        DeviceToken: req.DeviceToken,
        HeartTeamID: parseUUID(req.HeartTeamID),
    })
    if err != nil {
        writeError(w, http.StatusInternalServerError, "device registration failed")
        return
    }

    writeOK(w, map[string]string{"status": "ok"})
}
```

Notification kind changes write to the database synchronously, then fire-and-forget the SNS subscription update:

```go
// internal/rest/notification.go (simplified)
func (h *Handler) EnableNotification(w http.ResponseWriter, r *http.Request) {
    user := auth.UserFromContext(r.Context())
    kind := models.TopicKind(strings.ToUpper(chi.URLParam(r, "kind")))

    // Sync: write to database
    if err := h.notifRepo.EnableKind(r.Context(), user.ID, kind); err != nil {
        writeError(w, http.StatusInternalServerError, "failed to enable notification")
        return
    }

    // Async: update SNS subscriptions
    userID := user.ID
    h.runAsync(func(ctx context.Context) {
        h.userSubSvc.SubscribeToNotificationKind(ctx, userID, kind)
    })

    writeOK(w, map[string]string{"status": "ok"})
}
```

The `runAsync` pattern uses a channel-based semaphore — same pattern the gRPC handlers use — to bound concurrent goroutines:

```go
func (h *Handler) runAsync(fn func(ctx context.Context)) {
    h.asyncSem <- struct{}{}
    go func() {
        defer func() { <-h.asyncSem }()
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
        defer cancel()
        fn(ctx)
    }()
}
```

This prevents goroutine explosion under load. If the semaphore is full (1000 concurrent async tasks), the handler blocks briefly on send rather than spawning unbounded goroutines.

### Declarative Bulk Updates

The Python API required the mobile app to enable/disable kinds one at a time. The Go API adds a declarative bulk endpoint — send the desired set of enabled kinds, let the server compute the diff:

```go
// PUT /api/v1/notifications
// Body: {"enabled_kinds": ["GOAL", "MATCH_START", "REMINDER_15_MINUTES_BEFORE"]}

func (h *Handler) BulkUpdateNotifications(w http.ResponseWriter, r *http.Request) {
    user := auth.UserFromContext(r.Context())

    var req bulkNotificationsRequest
    json.NewDecoder(r.Body).Decode(&req)

    // Parse desired state
    desiredSet := make(map[models.TopicKind]bool)
    for _, raw := range req.EnabledKinds {
        desiredSet[models.TopicKind(strings.ToUpper(raw))] = true
    }

    // Fetch current state
    currentKinds, _ := h.notifRepo.GetEnabledKindsByUser(r.Context(), user.ID)
    currentSet := make(map[models.TopicKind]bool)
    for _, k := range currentKinds {
        currentSet[k] = true
    }

    // Compute diff
    var toEnable, toDisable []models.TopicKind
    for kind := range desiredSet {
        if !currentSet[kind] {
            toEnable = append(toEnable, kind)
        }
    }
    for kind := range currentSet {
        if !desiredSet[kind] {
            toDisable = append(toDisable, kind)
        }
    }

    // Single transaction
    h.notifRepo.BulkSetKinds(r.Context(), user.ID, toEnable, toDisable)

    // Async: SNS subscriptions follow
    h.runAsync(func(ctx context.Context) {
        for _, kind := range toEnable {
            h.userSubSvc.SubscribeToNotificationKind(ctx, userID, kind)
        }
        for _, kind := range toDisable {
            h.userSubSvc.UnsubscribeFromNotificationKind(ctx, userID, kind)
        }
    })

    writeOK(w, bulkNotificationsResponse{Enabled: toEnable, Disabled: toDisable})
}
```

This eliminates multiple round-trips on the mobile side. Instead of 5 sequential enable/disable calls, the client sends one request with the desired state.

### Ported Validation Logic

Several business rules lived in Python that needed to be ported:

**Guest notification limit** — guests can only enable N notification kinds:

```go
if user.Role == "GUEST" && h.cfg.MaxGuestNotificationKinds > 0 {
    count, _ := h.notifRepo.CountEnabledByUser(r.Context(), user.ID)
    if count >= h.cfg.MaxGuestNotificationKinds {
        writeError(w, http.StatusForbidden, "guest notification limit reached")
        return
    }
}
```

**Heart team cooldown** — users can't change their favorite team more often than every 10 days:

```go
if h.cfg.HeartTeamDeletionCooldown > 0 {
    lastDeletion, _ := h.userRepo.GetLastHeartTeamDeletion(r.Context(), user.ID)
    if lastDeletion != nil {
        cooldownEnd := lastDeletion.Add(h.cfg.HeartTeamDeletionCooldown)
        if time.Now().Before(cooldownEnd) {
            remaining := time.Until(cooldownEnd)
            writeError(w, http.StatusTooManyRequests,
                fmt.Sprintf("heart team deletion on cooldown, %d days remaining",
                    int(remaining.Hours()/24)+1))
            return
        }
    }
}
```

**Reminder mutual exclusion** — only one reminder interval can be active at a time (15min, 30min, 1h, 2h):

```go
if reminderKinds[kind] {
    h.notifRepo.DisableReminderKindsExcept(r.Context(), user.ID, kind)
}
```

This validation lives in the REST handlers, not the service layer. Services remain transport-agnostic — they don't know about user roles, cooldowns, or HTTP status codes. The gRPC handlers have their own validation (delegated from Python before). This keeps both protocol adapters independent.

## Phase 4: Wiring It Together

The main function initializes the JWKS provider, creates the new repositories, and registers REST routes on the existing chi router:

```go
// cmd/server/main.go
func main() {
    // ... existing setup (config, logger, db pool, services) ...

    // New: JWKS provider for JWT auth
    jwksProvider := auth.NewJWKSProvider(
        cfg.Auth.FusionAuthURL,
        cfg.Auth.JWKSRefreshInterval,
        logger,
    )
    jwksProvider.Start(ctx)
    authMiddleware := auth.NewMiddleware(jwksProvider, logger)

    // New: repositories for REST validation
    userRepo := repository.NewUserRepository(dbPool, logger)
    favoriteRepo := repository.NewFavoriteRepository(dbPool, logger)
    teamRepo := repository.NewTeamRepository(dbPool, logger)

    // New: REST handler (reuses existing services)
    restHandler := rest.NewHandler(
        userRepo, favoriteRepo, teamRepo, notificationRepo,
        deviceService, userSubscriptionService, subscriptionService,
        logger, cfg.REST,
    )

    // Register on existing chi router
    rest.RegisterRoutes(router, restHandler, authMiddleware)
}
```

The gRPC server continues running on port 50052 for internal services. The HTTP server on port 8081 now serves both health checks and REST endpoints. Same process, same services, two protocols.

## The Service Layer: Where gRPC and REST Meet

The critical design decision that makes this work: **both protocol adapters call the same services**.

Here's how device registration looks from each side:

```go
// REST handler (internal/rest/device.go)
err := h.deviceService.RegisterDevice(r.Context(), service.RegisterDeviceRequest{
    UserID:      user.ID,
    DeviceToken: req.DeviceToken,
    HeartTeamID: parseUUID(req.HeartTeamID),
})

// gRPC handler (internal/grpc/handlers/event_handler.go)
err := h.deps.DeviceService.RegisterDevice(ctx, service.RegisterDeviceRequest{
    UserID:      userID,
    DeviceToken: req.DeviceToken,
    HeartTeamID: heartTeamID,
})
```

Same service, same request struct, same behavior. The `DeviceService.RegisterDevice` method handles the complex lifecycle: deduplicating device tokens across users, creating SNS endpoints, subscribing to enabled notification kinds, subscribing to the heart team — all in one call. Both gRPC and REST handlers are thin wrappers that parse their respective protocol, call the service, and format the response.

## What Broke Along the Way

### Go module version split

The Swagger docs loaded fine (`/swagger/index.html` → 200), but `/swagger/doc.json` returned 500. Root cause: `docs.go` imported `github.com/swaggo/swag/v2` (RC), but the HTTP handler imported `github.com/swaggo/swag` (v1). In Go modules, these are completely separate packages with independent global registries. The docs registered the spec in v2; the handler read from v1; found nothing; 500.

Fix: regenerate docs with the v1 CLI, pin `swag` to v1.16.4.

### Wrong JWKS endpoint per environment

The Go service validated JWTs by fetching public keys from FusionAuth's `/.well-known/jwks.json`. The default config pointed to production FusionAuth, but dev tokens are issued by a different FusionAuth instance with different RSA key pairs. Same `kid` format — the signature check failed silently.

Fix: make `fusionauth_url` a per-environment config variable.

## The Strangler Fig Pattern

Here's what the architecture looks like during the migration:

```
                    ┌───────────────────┐
                    │   Mobile Clients   │
                    └───┬───────────┬───┘
                        │           │
          Notifications │           │ Everything else
          (11 endpoints)│           │ (100+ endpoints)
                        │           │
                   ┌────▼───┐  ┌───▼──────────┐
                   │   Go   │  │ Python/FastAPI│
                   │  REST  │  │   Monolith    │
                   │  API   │  └──────┬────────┘
                   └────┬───┘         │
                        │             │ (gRPC, being deprecated)
                   ┌────▼─────────────▼───┐
                   │   Go Service Layer    │
                   │   DeviceService       │
                   │   SubscriptionService │
                   │   UserSubService      │
                   └────┬─────────┬───────┘
                        │         │
                   ┌────▼──┐ ┌───▼──┐
                   │  RDS  │ │  SNS │
                   └───────┘ └──────┘
```

Both protocol paths converge at the Go service layer. During migration, both are active. Once mobile clients switch to the Go REST endpoints, the Python→gRPC path becomes dead code.

### What the Go service owns now

```
| Method   | Path                                  | Purpose                             |
|----------|---------------------------------------|-------------------------------------|
| PUT      | /api/v1/devices                       | Register device for push            |
| DELETE   | /api/v1/devices                       | Unregister device                   |
| GET      | /api/v1/notifications                 | List notification preferences       |
| PUT      | /api/v1/notifications                 | Bulk update preferences             |
| PUT      | /api/v1/notifications/{kind}          | Enable a notification kind          |
| DELETE   | /api/v1/notifications/{kind}          | Disable a notification kind         |
| GET      | /api/v1/heart-team                    | Get favorite team                   |
| POST     | /api/v1/heart-team/{teamId}           | Set favorite team                   |
| DELETE   | /api/v1/heart-team                    | Remove favorite team                |
| POST     | /api/v1/matches/{matchId}/subscribe   | Subscribe to match                  |
| DELETE   | /api/v1/matches/{matchId}/subscribe   | Unsubscribe from match              |
```

### What the Python monolith still owns

Authentication, user management, match data (15+ endpoints), competitions, teams, players, in-app purchases, training modules, and dozens of other features. The Go service is a focused vertical slice — one bounded context, completely owned.

## Key Takeaways

**1. The service layer is the real API.** gRPC and REST are just protocol adapters. If your services are transport-agnostic, adding a new protocol is a matter of writing thin handlers. We added 11 REST endpoints without touching a single line of business logic.

**2. Validation belongs in the handler, not the service.** Guest limits, heart team cooldowns, reminder mutual exclusion — these are transport-layer concerns that need the authenticated user context. Keeping them out of the service layer means gRPC handlers can have their own validation rules without conflict.

**3. Synchronous writes, asynchronous effects.** Users want immediate feedback when they change a preference. SNS subscriptions can be eventually consistent. This split keeps response times fast while handling the expensive fan-out work in bounded background goroutines.

**4. JWKS beats API validation.** Caching RSA public keys locally and doing signature verification in-process is faster (microseconds vs. milliseconds) and removes a runtime dependency. The Python service called FusionAuth's HTTP validation endpoint on every request. The Go service doesn't depend on FusionAuth being available after startup.

**5. Same database, no migration.** The Go repositories read from and write to Django-managed tables (`r10_user`, `r10_notification`, `r10_team`, `r10_subscribed_match`). No schema migration, no data sync, no dual-write problem. Both services share a database — which is fine for a bounded migration window.

**6. The monolith isn't the enemy.** After this migration, the Python monolith still serves 100+ endpoints. That's fine. The strangler fig pattern doesn't require killing the host tree — it just lets the new growth take over the parts that benefit from it. Notifications needed Go's concurrency model and gRPC integration. Match details and user management work perfectly fine in Python.

## Migration Strategy

1. Deploy Go service with REST endpoints alongside existing Python service
2. Mobile team updates the app to point notification endpoints to the Go service
3. Monitor both services for parity (log diffs, error rates)
4. Once mobile adoption reaches 100%, disable notification endpoints in Python
5. Remove gRPC client code from Python; remove notification-related controllers
6. Eventually retire Python entirely — when all endpoints are migrated

No big bang migration. No flag day. Both services coexist, and the old path withers as traffic shifts to the new one.

The fig keeps growing.
