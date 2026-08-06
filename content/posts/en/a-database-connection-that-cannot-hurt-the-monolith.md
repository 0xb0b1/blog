---
title: "A Database Connection That Cannot Hurt the Monolith"
date: "2026-07-20"
description: "The new service reads the old service's database during the shadow phase, which makes it a liability unless the connection is constrained by construction. On acceptance criteria phrased as capabilities a service must lack, why readiness and liveness must not agree about the database, and configuration that fails loudly at startup."
tags:
  [
    "go",
    "databases",
    "postgres",
    "reliability",
    "backend",
  ]
---

The first phase of extracting a service produced no features. It produced a process that starts, answers a health check, logs in a shape the platform understands, and connects to the monolith's database without being able to hurt it.

That last clause is the whole phase. During the shadow period the new service reads the old system's production data. If it misbehaves — a leaked connection, a runaway query loop, a bad deploy — the blast radius is the system that currently serves every user. A new service that can degrade the old one is worse than no new service.

The interesting thing about the criteria we wrote is how many of them are phrased as things the service must **not** be able to do.

## Bounded by configuration, not by intention

> **AC-028** — The pool is bounded by configuration.

Not "the service is careful about connections." A ceiling, in config, with a number. The distinction matters because the failure mode isn't a developer deciding to open a thousand connections — it's a retry loop, or a handler that forgets to release, or traffic ten times what anyone modelled. Care doesn't survive those. A hard ceiling does.

Alongside it, the grant is read-only. Between the two, the worst a bug in the new service can do is *fail*: it exhausts its own bounded pool and starts returning errors for its own requests, while the monolith's connection budget is untouched and its data can't be written.

I've come to think of this as the useful shape for a criterion at a trust boundary: **describe the capability the component must lack.** "Bounded and read-only" is checkable. "Doesn't interfere with the monolith" is a hope.

A later phase needed to write, and the notable thing is how it got there — a *separate* write-capable role added as its own task, rather than widening the read-only one. Widening a grant is a one-line change that silently deletes the guarantee. Adding a second role keeps the read path provably read-only and makes the write path a thing you can point at.

## Readiness and liveness must disagree

> **AC-027** — Readiness reflects the database; liveness does not.

This is the criterion I'd most like to see copied, because getting it wrong is so common and the consequence is so bad.

If liveness depends on the database, then a database blip kills your containers. The orchestrator sees a failing liveness probe, restarts the process, the new process also can't reach the database, and you have a crash loop layered on top of an outage — plus you've thrown away every in-flight request and any connection pool warmth, at the exact moment the system is under stress.

The split is: **liveness** answers *is this process functioning?* — it should fail only when a restart would help. **Readiness** answers *should this instance receive traffic right now?* — it should fail when a dependency is unavailable, so the load balancer stops sending work, without anything being killed.

Same underlying check, two different consumers, opposite correct behaviours.

## Fail loudly at startup

> **AC-023** — Misconfiguration fails at startup, loudly.

The service refuses to start on bad configuration, and the error names the offending variable.

Both halves earn their place. Failing at startup rather than at first use means a misconfigured deploy dies in the rollout, where the platform notices and rolls back, rather than at 3am on the first request that touches the misconfigured thing. And naming the variable is the difference between a five-second fix and a bisect through a config file, especially for whoever gets paged who didn't write the deploy.

The general form: validate the whole configuration at once, at boot, and say exactly what's missing. `Validate()` returning "database URL is required" is worth more than any amount of documentation about which variables to set.

## Structured logs and a correlation id that comes back

> **AC-024** — Every log line is structured JSON with the platform's fields.
> **AC-025** — A correlation id exists on every request and comes back to the caller.

The second half of AC-025 is the part people leave out. An id you generate internally lets *you* trace a request. An id returned to the caller lets a user's support ticket become a query — someone pastes an identifier, and you find the exact request rather than guessing from a timestamp and a route.

There's a naming decision in the design worth mentioning: this deliberately isn't called `request_id`. A single user action can cross several requests across services, and calling it a request id encourages code to mint a fresh one per hop — which is precisely how a correlation id stops correlating. Naming it for what it does rather than for where it first appeared makes the propagation rule obvious.

And the field names come from the existing platform convention rather than being invented, because logs are only queryable if every service agrees what the fields are called. Inheriting conventions from a sibling service is unglamorous and pays off the first time you need one query across both.

## What I'd take from this

**Phrase trust-boundary criteria as capabilities the component must lack.** Bounded, read-only, cannot exhaust, cannot write. Those are testable; "careful" isn't.

**Never let liveness depend on a dependency.** A database blip becomes a crash loop, and you lose in-flight work at the worst moment. Readiness is the probe that should care.

**Add a second grant rather than widening the first.** Widening is one line and it silently removes the guarantee you built the phase around.

**Validate all configuration at boot and name what's wrong.** The alternative is discovering it on the request that happens to need it.

None of this shipped a feature. It's the phase that made every later phase safe to attempt, and the criteria are worth more to me than the code they produced — the code is a few hundred lines and would be written differently in another language, but "readiness reflects the database, liveness does not" transfers everywhere.
