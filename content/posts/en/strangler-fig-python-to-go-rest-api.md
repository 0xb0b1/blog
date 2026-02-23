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

The Go notifications backend already had a chi router on port 8081, a service layer (`DeviceService`, `UserSubscriptionService`, `SubscriptionService`) used by gRPC handlers, a repository layer with PostgreSQL access, and SNS integration for push delivery.

The plan: add REST handlers that call the same services the gRPC handlers call. No new service layer, no new business logic — just a new front door.

```
BEFORE:  Mobile → Python REST → gRPC → Go services → DB/SNS
AFTER:   Mobile → Go REST → Go services → DB/SNS
         Dataloader → gRPC → Go services → DB/SNS  (unchanged)
```

## JWT Authentication: JWKS Instead of API Calls

The Python service authenticated by calling FusionAuth's validation API on every request (with a 15-minute cache). The Go service does it differently: it fetches FusionAuth's public keys once from `/.well-known/jwks.json`, caches them in memory behind a `sync.RWMutex`, and refreshes hourly in a background goroutine. No HTTP call per request.

A chi middleware extracts the `Authorization` header, validates the JWT signature with the cached RSA key, and stores the authenticated user (ID, role, language) in the request context. Every handler downstream just calls `auth.UserFromContext(r.Context())`. Parse once, use everywhere.

The performance difference is meaningful: RSA signature verification takes microseconds. An HTTP round-trip to FusionAuth's validation API takes milliseconds at best — and introduces a runtime dependency on FusionAuth availability.

## New Repositories: Reading Django's Tables from Go

The Go service already had repositories for devices, subscriptions, topics, and matches. But the REST endpoints needed validation queries that the gRPC handlers never needed — because Python handled that validation before forwarding the gRPC call.

We added three new repositories (`UserRepo`, `FavoriteRepo`, `TeamRepo`) that read from existing Django-managed tables: `r10_user` for roles and heart team data, `r10_favorite_match` and `r10_subscribed_match` for subscriptions, `r10_team` for existence validation. No schema migration, no data sync — Go reads and writes the same tables Python does.

The existing `NotificationRepo` also needed write operations. Before, it only read enabled notification kinds (for gRPC subscription logic). Now it enables, disables, and bulk-updates preferences. All writes use `ON CONFLICT` for idempotency — REST is stateless, clients may retry, and the database handles it gracefully:

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

## REST Handlers: Thin Protocol Adapters

The handler struct holds references to repositories and services — no business logic, just protocol translation. Route registration puts all 11 endpoints under `/api/v1/` with JWT authentication:

```go
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

The URL paths are intentionally different from Python's `/api/v1.8/users/device/` style. New paths mean we can run both APIs simultaneously without conflicts, though it requires a coordinated mobile release.

### Sync vs. Async: A Strategic Split

The handlers follow a consistent pattern: **synchronous database writes, asynchronous SNS operations**. Users expect immediate feedback when changing preferences. SNS topic subscriptions can be eventually consistent.

```go
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

The `runAsync` method uses a channel-based semaphore (capacity 1000) — same pattern the gRPC handlers use. If the semaphore is full, the handler blocks briefly rather than spawning unbounded goroutines. Each async task gets a fresh context with a 5-minute timeout, detached from the HTTP request lifecycle.

### Declarative Bulk Updates

The Python API required the mobile app to enable/disable notification kinds one at a time. The Go API adds a declarative bulk endpoint: `PUT /api/v1/notifications` accepts `{"enabled_kinds": ["GOAL", "MATCH_START"]}`. The handler fetches the current state, computes the diff (what to enable, what to disable), writes the delta in a single transaction, then kicks off async SNS subscription updates. One request instead of five sequential calls.

### Ported Validation Logic

Several business rules that lived in Python needed to be ported to the REST handlers: guest notification limits (configurable max kinds), heart team cooldown (10-day period between changes), reminder mutual exclusion (only one reminder interval active at a time), and team existence checks before setting a heart team.

This validation lives in the REST handlers, not the service layer. Services remain transport-agnostic — they don't know about user roles, cooldowns, or HTTP status codes. The gRPC handlers have their own validation (delegated from Python before). This keeps both protocol adapters independent.

## Where gRPC and REST Meet

The critical design decision that makes this work: **both protocol adapters call the same services**.

```go
// REST handler
err := h.deviceService.RegisterDevice(r.Context(), service.RegisterDeviceRequest{
    UserID:      user.ID,
    DeviceToken: req.DeviceToken,
    HeartTeamID: parseUUID(req.HeartTeamID),
})

// gRPC handler
err := h.deps.DeviceService.RegisterDevice(ctx, service.RegisterDeviceRequest{
    UserID:      userID,
    DeviceToken: req.DeviceToken,
    HeartTeamID: heartTeamID,
})
```

Same service, same request struct, same behavior. `DeviceService.RegisterDevice` handles the complex lifecycle: deduplicating device tokens across users, creating SNS endpoints, subscribing to enabled notification kinds — all in one call. Both handlers are thin wrappers that parse their respective protocol, call the service, and format the response.

The gRPC server continues on port 50052 for internal services like the dataloader. The HTTP server on port 8081 now serves both health checks and the REST API. Same process, same services, two protocols.

## What Broke Along the Way

**Go module version split.** The Swagger UI loaded fine, but `/swagger/doc.json` returned 500. Root cause: `docs.go` imported `github.com/swaggo/swag/v2` (RC), but the HTTP handler imported `github.com/swaggo/swag` (v1). In Go modules, these are completely separate packages with independent global registries. The docs registered the spec in v2; the handler read from v1; found nothing; 500. Fix: regenerate with the v1 CLI, pin to v1.16.4.

**Wrong JWKS endpoint per environment.** The default config pointed to production FusionAuth, but dev tokens are issued by a different instance with different RSA key pairs. Same `kid` format — the signature check failed silently. Fix: make `fusionauth_url` a per-environment config variable.

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
