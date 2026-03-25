---
title: "Shared Database Across Microservices: The Migration You're Not Ready to Make"
date: 2026-03-25
draft: false
description: "When multiple microservices share a single PostgreSQL database, schema migrations become a coordination problem. Here's how we handle it at R10 with Django, Go, and zero downtime."
tags: ["go", "python", "microservices", "database", "migrations", "architecture", "postgresql"]
---

Everyone tells you microservices should own their data. Separate databases, clear boundaries, no shared state. They're right — eventually. But "eventually" can be a long time when you're migrating a monolith to microservices and the business won't pause for your architectural ideals.

At R10 Score, we have a Python/Django monolith managing 570+ migrations on a single PostgreSQL database. Two Go microservices — notifications and odds — read from and write to that same database. This isn't the target architecture. It's the one that lets us ship features while the migration is in progress.

This post is about the part nobody writes about: what happens when three services need to evolve the same database schema, and only one of them has Django's migration framework.

## The Situation

```
┌──────────────┐  ┌──────────────────┐  ┌───────────┐
│  r10-hub     │  │ r10-notifications │  │ r10-odds  │
│  (Python)    │  │ (Go)             │  │ (Go)      │
│  Django ORM  │  │ pgx/v5           │  │ pgx/v5    │
│  570+ migr.  │  │ raw SQL migr.    │  │ no migr.  │
└──────┬───────┘  └────────┬─────────┘  └─────┬─────┘
       │                   │                   │
       └───────────────────┼───────────────────┘
                           │
                    ┌──────▼──────┐
                    │  PostgreSQL  │
                    │  (shared)    │
                    └─────────────┘
```

Three services. One database. Three different approaches to schema management:

- **r10-hub** (Python): Django migrations. 570 files in `dao/migrations/`. Every model change generates a numbered migration that Django tracks in `django_migrations`.
- **r10-notifications** (Go): Raw SQL files in `migrations/`. No framework. Applied manually or via deploy scripts.
- **r10-odds** (Go): No migrations at all. Reads from existing tables, writes to `r10_odd_company`. If the table exists, it works.

This is the shared database problem. Not "should we share a database?" — that ship has sailed. The question is: how do you keep three codebases from stepping on each other's schema?

## The Real Problem: Who Owns the Schema?

Django thinks it owns the database. It tracks every migration in `django_migrations` and will complain if reality doesn't match its state. When I needed to add a `r10_live_activity_token` table for the notifications service, I had a choice:

**Option A**: Create a Django migration in r10-hub for a table that r10-hub doesn't use.

**Option B**: Create a raw SQL migration in the Go service, outside Django's knowledge.

Both options are wrong. Option A pollutes the monolith with schema for features it doesn't own. Option B creates tables that Django doesn't know about, which is fine — until someone runs `manage.py migrate` and Django's introspection gets confused.

I went with Option B. Here's the migration:

```sql
-- migrations/001_create_live_activity_token.sql

CREATE TABLE r10_live_activity_token (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID        NOT NULL,
    match_id            UUID,
    device_token        VARCHAR(500) NOT NULL,
    push_to_start_token VARCHAR(500),
    activity_token      VARCHAR(500),
    state               VARCHAR(20)  NOT NULL DEFAULT 'registered',
    start_retry_count   INT          NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, device_token)
);
```

Notice what's missing: **no foreign keys**. `user_id` references `r10_user.id` conceptually, but there's no `REFERENCES` constraint. `match_id` references `r10_match.id`, but again — no FK.

This is intentional.

## Rule 1: No Cross-Service Foreign Keys

Foreign keys enforce referential integrity at the database level. That sounds good until you realize what it means in a shared database with multiple owners:

- **Deployment coupling.** If the notifications service adds a FK to `r10_user`, deploying a schema change that alters `r10_user` in the monolith now requires coordinating with the Go service. One service's deploy can block another's.
- **Cascade surprises.** A `DELETE FROM r10_user` with `ON DELETE CASCADE` would wipe live activity tokens. The notifications team didn't sign up for that.
- **Migration ordering.** Django's migration planner assumes it controls FK targets. An FK pointing to a table managed by a different service creates an invisible dependency that no tool tracks.

The rule: **if two services touch the same database, they communicate through UUIDs, not foreign keys.** The Go service stores `user_id` as an opaque UUID. If that user doesn't exist in `r10_user`, the query returns no results. The application handles it. The database doesn't enforce it.

Is this less safe? Yes. Orphaned rows are possible. We accept that tradeoff because the alternative — coupling every deploy across three services — is worse during a migration.

## Rule 2: New Tables Belong to the Service That Needs Them

When the notifications service needed to store live activity tokens, the migration lived in the notifications repo. Not in r10-hub. The reasoning:

- The Go service is the only consumer of `r10_live_activity_token`
- The table schema mirrors Go structs, not Django models
- The service team controls when and how the migration runs
- Django doesn't need to know this table exists

This creates a split: some `r10_*` tables are managed by Django migrations, others by raw SQL. That's messy, but it maps to reality. The monolith owns tables it created. New services own tables they create. Shared tables (like `r10_device`, `r10_notification`, `r10_topic`) remain under Django's control because the monolith created them and still writes to them.

## Rule 3: Read From Shared Tables, Don't Alter Them

The notifications Go service reads from several Django-managed tables:

```go
// Reading from Django's r10_user table
query := `SELECT id, role, language FROM r10_user WHERE id = $1`

// Reading from Django's r10_team table
query := `SELECT id FROM r10_team WHERE id = $1`

// Reading from Django's r10_notification table
query := `
    SELECT kind, is_enabled
    FROM r10_notification
    WHERE user_id = $1
`
```

The Go service also **writes** to `r10_notification` (enabling/disabling notification preferences) and `r10_device` (device registration). This used to be the messy part — two services writing to the same tables.

We fixed it. The [strangler fig migration](/posts/strangler-fig-python-to-go-rest-api) moved all notification REST endpoints from Python to Go. The mobile app now hits the Go service directly. Python no longer writes to `r10_notification`, `r10_device`, or any notification-related table. The Go service is the single writer.

But before that migration, we lived with dual writes for months. Two services writing to the same table means:

- Schema changes to shared tables **must** go through Django migrations in r10-hub, because Django tracks migration state and will complain if reality doesn't match
- The Go service must adapt to schema changes it didn't initiate
- Column renames, type changes, or constraint additions can break the Go service silently — no compiler catches a renamed column in a raw SQL query

The process is the same whether you have dual writes or not: if you need to change a shared table, you check who else reads from it. `grep` across repos for the table name. There's no tooling for this — it's discipline.

## Rule 4: Prefix Everything, Collide on Nothing

All R10 tables use the `r10_` prefix. This is a Django convention (`db_table = 'r10_notification'`), and we carry it into Go migrations. It means:

- No table name collisions between our app and PostgreSQL extensions or other schemas
- Easy to identify which tables belong to R10 vs. third-party tools
- A simple `\dt r10_*` in psql shows the full application schema

The Go migration follows the same convention: `r10_live_activity_token`. If we ever split the database, the prefix makes it trivial to identify which tables move where.

## What Actually Goes Wrong

### The Column Rename

Django migration 0400 renamed `UserDevice` to `Device`:

```python
migrations.RenameModel(
    old_name='UserDevice',
    new_name='Device',
)
```

Django handles this transparently — the table stays `r10_device`, but the model reference changes. No schema change hits PostgreSQL. But if Django had renamed the **table** (which it can), the Go service would have broken on the next query. We got lucky. The lesson: watch Django migrations for `RenameModel` and `AlterModelTable` operations.

### The Migration That Doesn't Exist

The odds service has zero migrations. It reads from `r10_match` and writes to `r10_odd_company`. Both tables were created by Django long ago. The odds service trusts they exist. If someone drops `r10_odd_company`, the service fails at runtime with a `relation does not exist` error. No migration framework catches this because the odds service doesn't have one.

This works because `r10_odd_company` is stable — its schema hasn't changed in months. For a table that changes frequently, you'd want at least a schema validation check at startup. We don't have that yet.

### The "Who Applied This?" Problem

Django tracks migrations in `django_migrations`. The Go service tracks nothing — the SQL file is applied manually or in a deploy script. If you need to know whether `001_create_live_activity_token.sql` has been applied to production, you check if the table exists:

```sql
SELECT EXISTS (
    SELECT FROM information_schema.tables
    WHERE table_name = 'r10_live_activity_token'
);
```

There's no migration history for the Go service. For one migration, this is fine. At ten migrations, you'll want golang-migrate or goose. We're at one. We'll cross that bridge when we get there.

## The Exit Strategy

The shared database is a transitional state. The target architecture has each service owning its database. The path from here to there:

1. **Identify table ownership.** Which tables does each service actually need? `r10_live_activity_token` is notifications-only. `r10_match` is shared by everyone. `r10_odd_company` is odds-only.
2. **Eliminate shared writes.** The hardest step. Two services writing to the same table is where bugs hide. We already solved this for notifications — the strangler fig migration moved all REST endpoints from Python to Go, making the Go service the single writer for notification tables. Python still reads some of them, but that's safe.
3. **Replicate read-only data.** The Go services need `r10_user` and `r10_match` for validation. These could come from a read replica, CDC stream, or an API call instead of direct table access.
4. **Split the database.** Once a service only touches its own tables, extract those tables into a dedicated database. The `r10_` prefix makes the cut obvious.

We're between steps 2 and 3. Notifications has a single writer. Odds reads from shared tables but doesn't alter their schema. The next challenge is decoupling the read dependencies — the Go services still query `r10_user` and `r10_match` directly.

## Practical Guidelines

If you're in this situation — multiple services, one database, ongoing migration — here's what's worked for us:

**Do:**
- Keep new tables in the service that owns them
- Use UUIDs as cross-service references without foreign keys
- Prefix table names consistently
- Grep across repos before changing shared table schemas
- Make Go migrations idempotent (`CREATE TABLE IF NOT EXISTS`, `ON CONFLICT`)

**Don't:**
- Add foreign keys between tables owned by different services
- Create Django migrations for tables the monolith doesn't use
- Assume Django's migration state reflects the full database schema
- Change shared table columns without checking downstream consumers
- Build elaborate migration tooling for a transitional state

The shared database is a pragmatic compromise. It lets you extract microservices incrementally without solving the distributed data problem on day one. It's not clean, it's not what the architecture diagrams show, and it works.

The important part is knowing it's temporary — and building your migrations so they're easy to untangle when you're ready for the real split.
