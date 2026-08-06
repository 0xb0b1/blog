---
title: "Quantify the Failure Before You Redesign It"
date: "2026-07-15"
description: "\"Subscribing is unreliable\" was the premise for extracting a service out of our monolith. Before designing anything I measured it in the production log group — and the failure wasn't flakiness at all. It was a state machine rejecting purchases it should have accepted, 3,995 times in seven days, behind one opaque error code."
tags:
  [
    "architecture",
    "observability",
    "aws",
    "debugging",
    "backend",
  ]
---

We were about to pull subscriptions out of a monolith. The premise, repeated in enough meetings that nobody questioned it any more, was that subscribing is unreliable — users report failures, support sees them, the code is old.

That's a description of a feeling, not of a failure. So before writing any design, I made "measure it" an acceptance criterion of its own:

> **AC-014** — The failure picture is measured, and reproducible.

Seven days of the production log group later, the picture was nothing like the premise.

## What the logs actually said

Every rejected purchase came back to the client as one error code. Over the window:

```
WRONG_SUBSCRIPTION_STATE      app_store    subscribe    3995
SUBSCRIPTION_USERS_DO_NOT_MATCH  app_store subscribe     239
PURCHASE_ALREADY_EXISTS       google_play  subscribe       1
```

So nearly four thousand failures, 94% of them one code. Not a spread of timeouts and connection resets — one branch, taken over and over.

Breaking down what the store had actually told us in those cases:

```
got=EXPIRED        expected=[ACTIVE]    3645
got=BILLING_RETRY  expected=[ACTIVE]     316
got=REVOKED        expected=[ACTIVE]      32
```

The code accepted exactly one store state and rejected every other. That's the whole bug.

## Why this changes the design

Read those three rows as product behaviour rather than as data.

**`REVOKED` — 32 cases.** Correctly rejected. The store is telling us the purchase was taken back.

**`EXPIRED` — 3,645 cases.** A subscription that has lapsed. Rejecting the *subscribe* call for this is wrong in an interesting way: this is a returning customer trying to come back, and the endpoint they'd naturally use tells them something opaque went wrong.

**`BILLING_RETRY` — 316 cases.** This is the one that changed my mind about the whole project. `BILLING_RETRY` means the store failed to charge the card and is retrying — and during that grace period **the store still grants the user access**. So we had 316 people in a week whom Apple considered entitled, being refused by us, with an error message that told them nothing.

None of that is unreliability. There's no flakiness to engineer away, no retry budget to tune, no connection pool to fix. It's a state machine written against one happy path, and the fix is a table of states with an intended behaviour for each.

Which is a completely different piece of work from the one we were about to start.

## The measurement is the artefact

The number I care about most from this exercise isn't 3,995. It's that the evidence lives in the repository:

```json
{
  "_comment": "Measured from CloudWatch Logs Insights on log group ecs/r10-rest-core-prod-backend",
  "window": { "days": 7, "endDate": "2026-08-01" },
  "logGroup": "ecs/r10-rest-core-prod-backend",
  "rejectedStoreStates": [ { "got": "EXPIRED", "expected": ["ACTIVE"], "count": 3645 }, … ]
}
```

A checked-in file with the log group, the window and the query recorded, rather than a screenshot pasted into a ticket. That matters for three reasons.

It's **reproducible** — the criterion says so, and someone can re-run it next month and see whether the fix moved the number.

It's **reviewable** — a colleague can disagree with the interpretation while accepting the data, which is a much more productive argument than disagreeing about whether subscribing "feels flaky."

And it's **dated**. Six months from now, "we measured this in a 7-day window ending 1 August 2026" is a far more useful sentence than "we measured this."

## Classify, then choose a target

The companion criterion is where the design work actually starts:

> **AC-015** — Each failure mode is classified and given a target behaviour.

Not "fix the errors." For each observed mode: is this a legitimate rejection, a bug, or a gap in the product? What should happen instead? That turns four thousand log lines into a short table of decisions, and it forces the awkward ones into the open — `EXPIRED` on a subscribe call isn't a bug so much as an unhandled product case, and pretending it's a bug hides the fact that someone has to decide what resubscribing means.

There's a matching one further down the spec that I'd now put in every design of this kind:

> **AC-019** — Every known failure mode has a queryable signal.

The reason we needed a week of log spelunking is that the original code collapsed four distinct outcomes into one error string. If each mode has its own queryable signal, the *next* person to ask "how often does this happen?" gets an answer in a minute rather than an afternoon.

## What I'd take from this

**Make measurement a gate, not a preliminary.** If the acceptance criteria for a design include "the failure picture is measured and reproducible," you cannot skip it and get on with the interesting part. We were one meeting away from designing a distributed system to fix an `if` statement.

**A single error code for multiple causes is a future outage.** Not because it fails, but because it makes the failure unmeasurable. `WRONG_SUBSCRIPTION_STATE` is honest and useless: it says the state was wrong without saying which state, so nobody can tell a bug from a policy from a store quirk.

**Check the measurement in.** The number, the window, the query, the log group. It costs nothing and it converts a claim into evidence — which is the difference between a design review that argues about interpretation and one that argues about vibes.

The extraction went ahead, for reasons that survived the measurement. But the first thing it shipped was a state table, and that would not have been true if we'd trusted the premise.
