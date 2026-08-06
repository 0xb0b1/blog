---
title: "Idempotency Belongs in the Database"
date: "2026-07-26"
description: "A replayed store notification must not duplicate a purchase. Application-level checks are necessary and insufficient — under concurrent delivery the only thing that reliably holds is a constraint. On choosing the key, the correction we had to make to it mid-run, and why the criterion names the enforcement mechanism rather than the outcome."
tags:
  [
    "databases",
    "postgres",
    "idempotency",
    "reliability",
    "backend",
  ]
---

Stores retry. App Store and Google Play both resend a notification if they don't get a prompt acknowledgement, and both will occasionally deliver the same event twice for reasons entirely their own. Anything that ingests webhooks will see duplicates; the only question is what happens when it does.

Two criteria, and the interesting one is the second:

> **AC-044** — The same notification processed twice yields one row and one outcome.
> **AC-045** — Idempotency is enforced by the database, not only by application logic.

The first states the outcome. The second states the mechanism, and states it as a *floor* — "not only by application logic." That phrasing is deliberate, because the natural implementation satisfies the first criterion in testing and fails it in production.

## Why the application check isn't enough

The obvious code is:

```go
existing, err := repo.FindPurchase(ctx, storePurchaseID)
if existing != nil {
    return nil          // already processed
}
return repo.InsertPurchase(ctx, p)
```

That passes any test you write for it. Send the notification twice sequentially and you get one row.

Then two workers pull two copies of the same notification off the queue at the same time. Both call `FindPurchase`, both get nothing, both call `InsertPurchase`, and you have two rows. The window is small — microseconds — and duplicate delivery is precisely the condition that makes concurrent processing likely, since a retry arrives while the original is still in flight.

The check isn't wrong. It's an optimisation: it avoids an insert attempt in the common case. What it cannot do is provide a guarantee, because between the read and the write it holds nothing.

A unique constraint holds. It's evaluated by the database at write time, under whatever isolation you're running, across every connection and every worker. That's not a better version of the same technique — it's the only version that's a guarantee rather than a probability.

So the handler keeps the lookup for the common path, and treats a unique-violation error as success rather than as failure. Which is the shape I'd write for any idempotent insert: **attempt, and interpret the collision as "someone else already did this."**

## Choosing the key, and getting it wrong first

The key we settled on is `(store, store_purchase_id)`, and we corrected it during the run — the commit says so plainly: *correct the idempotency key to (store, store_purchase_id)*.

The instinct is to key on the store's purchase identifier alone. It's what the store considers the identity of the thing, it's unique within that store, and it feels canonical.

It's unique *within that store*. We ingest from App Store, Google Play, and soon Stripe, and there's no rule that says three independent vendors won't mint colliding identifiers. Probably they won't. "Probably" is the wrong strength for a uniqueness constraint, because the failure mode is silently discarding a real purchase from one store because another store issued the same id — a bug you would find months later, from a support ticket, with no way to reconstruct what happened.

Including the store makes the key say what it means: *this purchase, at this store.* The correction was one line of migration. Finding it later would not have been one line of anything.

The general form: **an identifier from an external system is only unique within that system.** If you ingest from more than one, the source belongs in the key. This is the same lesson as provider-keyed entities in a shared schema, arriving from a different direction.

## Name the mechanism in the criterion

What I'd most like to keep from this is how AC-045 is written.

A criterion that says *the same notification twice yields one row* is satisfiable by the buggy code. Someone writes the lookup, writes a test that sends the notification twice in sequence, watches it pass, and moves on — honestly, having met the stated requirement.

A criterion that says *enforced by the database, not only by application logic* cannot be satisfied that way. It names the mechanism, so review has something concrete to check: is there a constraint? The reviewer doesn't have to reason about interleaving to evaluate compliance.

That runs against the usual advice to specify outcomes rather than implementations, and I think the exception is principled. Where the outcome is only observable under concurrency, a test cannot reliably demonstrate it and a reviewer cannot easily reason about it. In that situation naming the mechanism is the honest way to make the requirement checkable — you're not over-specifying, you're specifying the part that's verifiable.

The same reasoning appears elsewhere in this project: a pool "bounded by configuration" rather than "careful", a queue that "is the durable record" rather than "durability before acknowledgement". Each time, naming the mechanism is what made the property reviewable.

## What I'd take from this

**A read-then-write check is an optimisation, not a guarantee.** It holds nothing between the two statements, and duplicate delivery is exactly the condition that makes the gap matter.

**Let the constraint be the guarantee and treat the violation as success.** Attempt the insert; a unique violation means someone else already did the work.

**Put the source in the key when you ingest from multiple systems.** Their identifiers are unique in their namespace, not yours.

**Where correctness is only visible under concurrency, name the mechanism in the requirement.** "Enforced by the database" is checkable in review. "Yields one row" is checkable by a test that passes for the wrong reason.
