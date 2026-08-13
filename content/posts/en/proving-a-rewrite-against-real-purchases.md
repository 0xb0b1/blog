---
title: "Proving a Rewrite Against 243,325 Real Purchases"
date: "2026-07-22"
description: "Before the new service answered a single user, it computed entitlement for every purchase we had and its answers were compared offline against the monolith's. Zero disagreements. The engineering is entirely in what \"zero\" required: pinning the clock, writing the logic twice on purpose, and making every disagreement diagnosable without a rerun."
tags:
  [
    "go",
    "testing",
    "migration",
    "architecture",
    "backend",
    "subscriptions-extraction",
  ]
---

The riskiest part of rewriting a piece of a system isn't the new code. It's that nobody can tell you whether the new code agrees with the old one, because the old one's behaviour is only defined by what it does.

Entitlement — whether a given user currently has access — is the worst kind of logic to inherit. It's a predicate over purchase state, store state, dates, grace periods and a decade of special cases that were each individually reasonable. There's no specification. The specification *is* the Python.

So before the Go service served anything, we ran both implementations over the whole corpus and compared:

> **AC-032** — Go and Python agree on entitlement across the whole corpus.

243,325 purchases. Zero disagreements. That number is the least interesting part of this post.

## The whole corpus, not a sample

The first decision that mattered: compare everything, not a window of recent traffic.

A week of traffic is a biased sample in the specific way that hurts here. Recent purchases are disproportionately in ordinary states — active, recently renewed. The cases where two implementations diverge are the weird ones: a subscription that expired during a billing retry, a purchase reconciled from a different store, something that went through a migration three years ago. Those are rare per-day and abundant per-corpus.

"Whole corpus" also removes an argument. Nobody can ask whether the sample was representative.

## Pinning the clock

Entitlement is time-dependent, which means the comparison has a bug built in unless you handle it explicitly: run Go at 14:02 and Python at 14:03 and a subscription can expire between them. You get a disagreement that is not a disagreement, and you go looking for a logic bug that doesn't exist.

Worse, it's nondeterministic. Re-run and the disagreement moves.

The design has a section called *"Pinning the clock"* for exactly this: both implementations are given the same evaluation instant, injected rather than read from the system. A side effect is that the comparison becomes reproducible — the same corpus and the same pinned instant give the same answer forever, which is what makes a result you can put in a document.

Any comparison harness over time-dependent logic needs this, and it's the kind of thing that's obvious in retrospect and invisible in advance.

## Offline, and unable to touch anything

> **AC-049** — The comparison cannot mutate the shared tables.

The harness reads a snapshot. It has no write path, and in the later phase where the corpus grows to include notification replay, it seeds an ephemeral database rather than touching the live account at all.

This is not paranoia about the code being wrong. It's about what the harness *is*: a thing you'll want to run twenty times while you chase down disagreements, at whatever hour you're chasing them. A tool that could mutate production is a tool you'll hesitate to run, and a comparison you hesitate to run is one you'll run less often than you should.

Make the risky thing structurally impossible and you get to be careless with the tool, which is the point of the tool.

## Writing the logic twice, deliberately

The harness has a Python oracle alongside the Go implementation. That looks like duplication and it's the entire mechanism.

The old system is Python. If the harness compared Go against a *reimplementation* of the rules, it would be comparing my reading of the rules against my other reading of the rules — a test of my consistency, not of behavioural equivalence. Running the original code as the oracle means the thing being agreed with is the thing currently in production, special cases and all.

The consequence to accept: the harness is only as good as the oracle's fidelity, and the oracle has to be the real code path, not a copy of it that has since drifted.

## Coverage is a separate criterion from agreement

> **AC-034** — Grace period and the reconciled-store rule are exercised, not assumed.

This is the criterion I'd fight to keep if I could only keep one. "Zero disagreements" is compatible with "we never asked the interesting question." A corpus where 99% of rows are plainly active tells you the two implementations agree about plainly active rows.

So specific rules are named and their exercise is asserted. Grace period — the window where a store is retrying a charge but still granting access — is the case that had already burned us in production, and it's exactly the sort of branch a corpus can under-represent. Naming it means the harness reports how many rows actually took that path, and a run where the answer is zero is a failed run, not a clean one.

I've written elsewhere that "has a test" and "the test passed" must stay separate numbers. This is the same idea one level up: **agreement and coverage are separate numbers**, and a harness that reports only the first is flattering itself.

## Diagnosable without a rerun

> **AC-033** — Every disagreement is diagnosable without a rerun.

When the two disagree, the output must contain enough to understand why — the inputs, both answers, the pinned instant, the branch taken. Not a count. Not "17 mismatches."

The reason is operational. A full-corpus run isn't instant, and a harness that tells you *that* something disagreed but not *what* forces a cycle: add logging, re-run, wait, look. Do that three times and the loop has cost you more than writing the diagnostics would have.

There's a subtler benefit. A disagreement report rich enough to diagnose is also rich enough to attach to a decision — you can put it in front of someone and ask "which of these two answers is correct?", which is often a product question rather than an engineering one.

## What I'd take from this

**Compare against the running implementation, over the full corpus.** A sample is biased toward the ordinary cases, which are the ones you don't need to check.

**Pin the clock.** Any time-dependent comparison has a nondeterministic false-positive generator in it until you inject the instant.

**Make the harness structurally unable to write.** You want to run it without thinking; that requires it to be safe without thinking.

**Assert coverage separately from agreement, and name the rules that must be exercised.** Zero disagreements over a corpus that never hit the interesting branch is not evidence.

**Spend the effort on the disagreement report, not the summary.** The count tells you whether to look. The report is how you look.

The result was a rewrite that went live having already answered a quarter of a million real questions identically to the system it replaced. That's a much better position than a test suite, and it cost less than the test suite would have — because the oracle was already written, and had been in production for years.

---

*Part of [The Subscriptions Extraction](/en/posts/the-subscriptions-extraction-a-reading-order), seventeen posts on pulling the subscriptions half of a Django monolith into a Go service.*
