---
title: "An Architecture Specified in Numbers, Not Adjectives"
date: "2026-07-18"
description: "\"Scalable and reliable\" is not a specification — it's a wish with good PR. On writing a target architecture as mechanisms and budgets, requiring every design to name its tradeoffs alongside its boundaries, and giving every known failure mode a queryable signal before any of it is built."
tags:
  [
    "architecture",
    "system-design",
    "documentation",
    "backend",
  ]
---

Design documents fail in a predictable way. They describe a shape — boxes, arrows, a queue — and then assert properties: this will be scalable, this will be reliable, this will be observable. The shape is checkable. The properties aren't, so nobody checks them, and six months later you discover which of them were true.

The user story we wrote for our extraction design tried to close that gap:

> **US-008** — A target architecture specified in numbers
>
> As a reviewer of the design, I want the reliability and scalability claims expressed as concrete mechanisms and budgets, so that "best practices" is something we can build and verify rather than a wish.

The phrase I keep coming back to is *"something we can build and verify rather than a wish."* Four acceptance criteria came out of it, and each one closes a different escape hatch.

## Name the tradeoffs, not just the boundaries

> **AC-016** — The architecture names its boundaries **and its tradeoffs**.

The first half is standard: which service owns what. The second half is the one that changes how a document reads.

A design that only lists boundaries is presenting a solution. A design that lists what each boundary *costs* is presenting a decision — and a decision is something a reviewer can engage with. Moving subscriptions out means an extra network hop on a path that used to be a function call; it means two deployables to release in order; it means a period where two systems can both be right about the same row.

None of that is an argument against the boundary. Writing it down is what makes the boundary honest, and it's what stops the same conversation from being had again in three months by someone who's noticed the network hop.

The failure mode this prevents is subtle: a document with no stated tradeoffs reads as though there were none, and the first person to hit one thinks they've found a mistake rather than a known cost.

## Reliability as mechanisms and budgets

> **AC-017** — Reliability is stated as mechanisms and budgets.

Two words doing separate work.

A **mechanism** is a thing in the system: a bounded pool, a dead-letter queue, an idempotency constraint, a retry with a ceiling. "Reliable" is not a mechanism. "The queue is the durable record and acknowledgement doesn't wait on processing" is.

A **budget** is a number you can exceed: a pool ceiling, a timeout, a maximum acceptable lag. The point of a budget isn't that it's the right number — first guesses rarely are. It's that a number can be violated, and a violation is detectable. "Fast" cannot be violated, so it will never fail a test, and so it will never be true or false.

Together they make the claim testable. Once the design says *this pool has a ceiling of N connections* rather than *the service is careful about database load*, you can write an assertion. In our case the same pair produced a criterion phrased as a capability the service must **lack** — it must not be able to exhaust the monolith's connections — which is a much stronger statement than any amount of intent.

## The trace spans every hop

> **AC-018** — The trace design spans every hop.

Distributed tracing gets specified as a component: add OpenTelemetry, done. This criterion is about coverage instead — a trace that covers three of four hops is worse than no trace, because it produces confident, incomplete pictures. You look at a span tree, don't see the problem, and conclude the problem is elsewhere.

Requiring *every hop* forces the enumeration: client to gateway, gateway to service, service to queue, queue to worker, worker to database. Every one of those is a place where correlation can be dropped, and the missing one is always the one you need at 3am.

This pairs with a decision in the implementation that I'll write about separately — the tracing was wired from day one but emitted nothing, because no collector had been chosen. The design being explicit about hops is what made it safe to defer the operational half without deferring the code.

## Every failure mode gets a queryable signal

> **AC-019** — Every known failure mode has a queryable signal.

This is the criterion I'd add to every design document I write from now on, and it exists because of what the failure measurement had just taught us.

We'd spent a week working out what was wrong with the old subscribe path, and the reason it took a week is that four distinct outcomes shared one error string. The information wasn't missing — it was aggregated into uselessness. Nobody can tell a policy rejection from a bug when both arrive as `WRONG_SUBSCRIPTION_STATE`.

So: for each failure mode the design anticipates, what query answers "how often is this happening?" Not a dashboard, not a log line — a query, meaning a signal distinct enough to count. Doing this at design time costs almost nothing. Retrofitting it costs a week of log spelunking, which is exactly what we'd just paid.

There's a nice property here too: enumerating failure modes precisely enough to give each a signal tends to reveal one or two you hadn't thought about. The exercise is partly a design review disguised as an observability task.

## What I'd take from this

**Make "and its tradeoffs" a required section.** A design with no stated costs reads as though it has none, and every discovered cost then looks like an error.

**Every property claim needs a mechanism and a number.** If you can't name the thing in the system that produces the property, and a number that would count as violating it, you've written an aspiration.

**Enumerate hops for tracing, not components.** Coverage is the property that matters; a partial trace misleads with confidence.

**Give each failure mode a queryable signal before you build it.** The alternative is measuring it later from logs that were never designed to be measured — and that price is paid in whole afternoons.

The general shape: for every adjective in a design document, ask what would have to be true for it to be false. If there's no answer, the adjective isn't doing any work, and it will be quietly dropped by whoever implements the thing.
