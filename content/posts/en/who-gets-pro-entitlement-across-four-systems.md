---
title: "Who Gets PRO: Entitlement Across Four Systems"
date: "2026-08-08"
description: "Granting a paid tier sounds like setting a boolean. It's a predicate over subscription states nobody agrees on, applied to four systems with no transaction between them. On ordering writes by what each partial failure leaves behind, refusing to compensate, a product decision that granted access to precisely nobody, and one enum asked two questions that look like one."
tags:
  [
    "distributed-systems",
    "domain-modelling",
    "reliability",
    "identity",
    "backend",
    "subscriptions-extraction",
  ]
---

The system is a sports app with a paid tier. Someone subscribes through the App Store or Google Play, and that should unlock the paid features. We're [extracting that machinery out of a Django monolith into a Go service](/en/posts/quantify-the-failure-before-you-redesign-it), against the same shared Postgres — so for the whole transition, both implementations are live.

Before any of the interesting problems, the vocabulary, because "unlock the paid features" hides two separate questions.

A **purchase** is a row: which store, which product, which user, what state the store last told us it was in, when it's paid through. Purchases arrive two ways. The app calls a subscribe endpoint when someone buys — that's the primary write path, the origin. And the store sends **notifications** afterwards: renewed, cancelled, refunded, entered billing retry, expired. Those are the asynchronous echo, arriving for years after the sale, whether or not anyone opens the app.

**Entitlement** is the derived answer: given the purchases this user has, do they get PRO right now? That's a predicate, and it is emphatically not a column. Which brings us to the first thing that surprised me.

## Entitlement lives in four systems

I had assumed applying entitlement meant writing a role column on the user row. Reading the monolith's `update_user` showed it touches four systems:

- **The identity provider** (FusionAuth) — the registration's roles. This is what actually gates the paid features. Skip it and nothing has changed for the user, whatever else you wrote.
- **Redis** — the cached auth lookups. Skip it and the monolith keeps serving the old role from cache.
- **Postgres** — the user's role and a pointer to their current purchase. Skip it and the database disagrees with identity.
- **The notifications service**, over gRPC — the push audience. Skip it and a downgraded user keeps receiving pushes meant for subscribers.

There is no transaction across those four. There cannot be — one is an HTTP API, one is a cache, one is a gRPC service.

This mattered immediately, because I had asked where the entitlement effect should be applied and offered *extend the new service's database grants* as one of the options. That option got chosen. It was also the wrong question, and the wrongness was mine: my option assumed applying meant writing Postgres.

Extending the Postgres grants would have let our service write the **least consequential** of the four. The row would say PRO, the identity provider would still gate on the old role, the cache would keep serving it. A user who paid would remain locked out while the database claimed otherwise — the worst kind of bug, because every dashboard says it worked.

So the question was re-put with that list attached, and settled differently: the new service becomes the entitlement authority in fact, all four writes. The mechanism worth keeping isn't the decision, it's that **a decision record carries the premise its answer was given on.** Mine was written down, which is the only reason it could be found wrong and re-asked. An answer with no recorded premise is indistinguishable from an answer to the right question.

## Ordering by what a partial failure leaves behind

With no transaction available, the design question isn't "how do we make this atomic." It's "what does each partial failure leave behind, and which leftovers can we live with."

The order is Postgres, identity provider, Redis, gRPC. Each position is argued rather than conventional:

**Postgres first**, because it's the only one of the four that can be rolled back. If something in the sequence is going to fail, you want the reversible write already made.

**The identity provider second**, because until it's written, *nothing has actually changed* — it holds the role that gates access. A failure after Postgres and before it leaves a database row claiming an entitlement the provider doesn't grant: visible, wrong, and inert. That's the least harmful intermediate state on offer.

**Redis strictly after the provider**, and this is the one that's easy to get backwards. The instinct is to invalidate the cache first so nothing stale can be served. Do that and you open a window: the cache is empty, a request arrives, the old role is read from the provider — which hasn't been written yet — and cached again. You've invalidated a cache straight back into the value you were trying to remove. Invalidation must follow the write it invalidates, and no cleverness shortens the gap.

**gRPC last**, because a stale push audience is a nuisance and a wrong role is an entitlement error. If the sequence stops somewhere, stop it where the failure is mildest.

That last sentence is the whole method: order a non-atomic sequence so that every prefix of it is a state you can defend to a user.

## Not compensating, on purpose

The obvious next move is compensation — if step three fails, undo steps one and two. We deliberately don't.

A failure leaves the earlier writes standing, reports which ones landed, and does not acknowledge the queue message. Every step is idempotent, so redelivery re-runs the whole sequence and finishes the job.

The reason is narrow and I'd defend it: **unwinding an identity provider is how one bug becomes two.** A compensation path is code that runs only when something has already gone wrong, which is exactly when it's least likely to have been exercised. Its failure mode is removing access from someone who should have it, on the strength of a partial write it may be misreading. Idempotent replay has one code path — the same one that ran the first time, the one that runs constantly and is therefore actually tested.

The cost is stated rather than hidden: between the failure and the redelivery, a user can sit in a partially-applied state. The mitigation is that the list of writes that *did* land travels out with the error, because nothing is compensated and that list is the only record of what changed. A partial state whose record stays in an unjoined log line is a partial state nobody can fix.

The first design returned immediately on failure and relied on redelivery. That was amended to a bounded in-place retry with backoff, redelivery as the backstop, and the reasoning cuts both ways. Redelivery re-runs the *entire* four-system sequence, re-writing steps that already succeeded — safe only because each step is idempotent. That same property is exactly what makes an in-place retry safe. If you can tolerate one you can tolerate the other, and the in-place retry is cheaper by three writes. The bound matters as much as the retry: unbounded retry inside a queue handler holds the message invisible until the visibility timeout expires, at which point it's redelivered anyway — twice the work for the same outcome, with nothing able to fail fast.

Applying sits behind a switch that defaults to off, so all of this could land, deploy and be watched before it changed anyone's account. The first time the service touched a real user's entitlement was a decision someone made, not a deploy artefact.

One risk I can't design away: once we apply, the monolith still can too. Nothing in either codebase detects two writers. The separation lives entirely in a runbook step — switch the old path off at the repoint — and if that step is forgotten, both services write the same registration from different notifications and the last one wins, silently. That's written into the runbook as a step rather than left as an expectation, which is the most I can do from here.

## The decision that granted access to nobody

Now the predicate itself, which is where the genuinely interesting bug was.

A subscription in **billing retry** is one whose paid period has ended and whose renewal charge failed. The store keeps trying the card; the user still has the app open. Whether they keep access is a product decision, and ours had been made: billing retry and grace period both grant access.

The predicate implementing it reads, in effect:

```go
if !grantingStates.Contains(p.Status) {
    return false
}
return p.EffectiveExpiration.After(now)
```

Both halves are defensible in isolation. The state must be one that grants. The paid-through date must not be in the past.

Together they grant access to nobody in billing retry — because a subscription is in that state *precisely because* the period ended and the charge failed, so the paid-through date is behind you by definition. Of 7,684 purchases measured in that state, **zero** had a future expiration. Not few. Zero, and necessarily zero.

The decision was live, documented, referenced in meetings, and had never granted anyone anything.

Where the bug lived is the part worth sitting with. The monolith had a commit implementing the change; a pull request left it behind. Our port was faithful to the code that actually shipped — the half without the short-circuit. Two implementations, both internally consistent, both agreeing with each other, both failing to do what the decision said.

Parity had been proven over 243,318 purchases with zero disagreements. That claim was true and could not possibly have caught this. **A differential test is blind to a defect both sides share.** Every measure of quality we had was green, and the thing that found it was reading the decision and the predicate at the same time and noticing that one state in the granting set couldn't satisfy the condition below it. I don't have a mechanical method for that class of bug, and I'd rather say so than invent one.

The fix looked like a one-line short-circuit and wasn't, because of what the harness asserts. Its claim is *the two predicates agree*. Fixing ours alone doesn't repair that claim — it breaks it, into a state where the harness reports a real divergence that is entirely our doing. So the Python half went in as its own pull request and our half as a task here, landing together. The alternative was a window where the two services disagreed about who was entitled, while the alarm designed to catch exactly that reported correctly and got ignored as expected noise. An alarm you've decided to ignore for a week is off.

One detail I'd repeat: the task that shipped *before* the fix asserted the **pre-fix** behaviour, with the cross-repository dependency written into the test's name and failure message. When the fix landed, the test failed and told you why and where. An expectation that flips loudly on a known future change is a scheduled alarm; a `// TODO` is a note.

## One enum, two questions

Later, a different endpoint made the opposite point with the same state.

The purchase screen asks *who owns this receipt* before letting anyone buy — so a user presenting an old expired receipt is told there's no purchase here, rather than being shown a stale owner. Answering it needs a notion of a purchase that no longer counts. The obvious implementation reuses the entitling set: it's right there, it's tested, it already classifies statuses.

It's the wrong set. The right one is **expired and revoked only**. Billing retry is not on it.

Because these are two different questions:

- *Does this purchase entitle the user to PRO?* — a question about access. Billing retry: yes, by decision.
- *Is there a live purchase here at all?* — a question about existence. Billing retry: yes, obviously. Someone is mid-renewal.

They agree on most inputs and disagree on that one, and the reuse is tempting precisely because they overlap. Folding them together would have answered "no purchases" for a paying subscriber whose card had just bounced — which, on the purchase screen, invites them to buy a second subscription.

So: one enum, two predicates, two names, two tests. That's not duplication. The sets differ because the questions differ, and any refactor that unifies them reintroduces the bug.

The general shape of the error is worth naming. A store's subscription status describes a *billing lifecycle*. It is not a permission, an availability flag, or a stage in your product's own lifecycle — each of those is a separate function over it. And the trap is that the name describes the values rather than the question: `entitlingStates` invites reuse by anyone with a different question and similar-looking values. `statesThatGrantPro` and `statesWithNoLivePurchase` would both have made the mismatch obvious at the call site.

## What I'd take from this

**Find out how many systems hold the thing you're about to change.** I assumed one and it was four, and the option I proposed would have written the least important of them.

**Record the premise, not just the decision.** Mine was wrong; the only reason that was recoverable is that it was written next to the answer it produced.

**With no transaction, order writes by what each partial failure leaves behind.** Every prefix should be a state you can explain to a user.

**Invalidate a cache after the write it invalidates.** Invalidating first re-reads the stale value from an un-updated source and caches it again.

**Prefer idempotent replay to compensation, and return the list of what landed.** Replay uses the code path that runs daily; compensation uses one that only runs when something is already wrong.

**Check that every value in your accepting set can actually reach the accepting outcome.** A state that structurally can't is a dead branch that reads as a live one.

**Differential testing can't find a defect both implementations share.** 243,318 comparisons, zero disagreements, bug intact.

**Two implementations of one rule must be fixed in one change**, or a true parity claim becomes a false alarm you learn to ignore.

**Name a set after the question it answers, not the values it contains.** Two questions that agree on most inputs are still two questions, and the input where they differed was a paying subscriber being told they'd never bought anything.

---

*Part of [The Subscriptions Extraction](/en/posts/the-subscriptions-extraction-a-reading-order), seventeen posts on pulling the subscriptions half of a Django monolith into a Go service.*
