---
title: "The Subscriptions Extraction: A Reading Order"
date: "2026-08-13"
description: "Seventeen posts on pulling the subscriptions and payments half of a Django monolith into a Go service — measured before it was designed, phased so every step could be undone, and proved against 243,325 real purchases before it answered a single user. What each one adds, and the order that makes them make sense."
tags:
  [
    "architecture",
    "migration",
    "backend",
    "subscriptions-extraction",
  ]
---

Over about a month I wrote seventeen posts about one project. They were written as the work happened, which means each one starts from wherever things had got to rather than from the beginning.

This is the beginning.

The premise: a sports app with a paid tier. Users subscribe through Apple's App Store, Google Play, or Stripe on the web, and a successful subscription grants a role — internally PRO — that unlocks the paid features. All of it lived in one Django monolith: the purchase endpoints the mobile app calls, the webhook endpoints the stores call, the catalog of what's purchasable, and the logic deciding whether a purchase entitles someone to PRO.

We pulled the subscriptions half into a new Go service, and made the most consequential decision before any code: **the data does not move.** The Go service connects to the monolith's existing Postgres cluster and writes the same tables. No new database, no migration, no dual-write layer.

That was judged the lesser risk, and its cost is that for the whole transition **two independently-written implementations write the same rows.** Which changes the definition of done. A rewrite succeeds when it works; a port in this position succeeds only when it *agrees*. Almost everything below follows from that one constraint.

## Measure and scope

- [Quantify the Failure Before You Redesign It](/en/posts/quantify-the-failure-before-you-redesign-it) — "subscribing is unreliable" was the premise for the whole project. Measured in the production log group, the failure wasn't flakiness at all: a state machine rejecting purchases it should have accepted, 3,995 times in seven days. **Start here.**
- [Inventorying Every Surface a Monolith Exposes](/en/posts/inventorying-every-surface-a-monolith-exposes) — you cannot rebuild what nobody has listed. Deriving the promises from the code rather than from memory, and making the inventory something tests enforce.
- [Who Else Reaches Into This Database?](/en/posts/who-else-reaches-into-this-database) — the blocking question for any extraction. Every coupling with a direction and evidence attached, plus the discovery that entities keyed to a single provider quietly make your history depend on a vendor.

## Design and commit

- [An Architecture Specified in Numbers, Not Adjectives](/en/posts/an-architecture-specified-in-numbers) — "scalable and reliable" is a wish with good PR. Writing the target as mechanisms and budgets, and giving every known failure mode a queryable signal before anything is built.
- [A Cutover That Can Be Reversed](/en/posts/a-cutover-that-can-be-reversed) — reversibility as an acceptance criterion rather than a rollback plan written the night before, and why "we can revert the deploy" stops being true the moment data moves.
- [A Database Connection That Cannot Hurt the Monolith](/en/posts/a-database-connection-that-cannot-hurt-the-monolith) — during the shadow phase the new service reads the system that serves every user, so its criteria are phrased as capabilities it must **lack**.
- [Tracing: Wired but Silent](/en/posts/tracing-wired-but-silent) — instrumented with OpenTelemetry on day one and configured to emit nothing, because no collector had been chosen. Separating a code decision from an operational one.

## Prove it

- [Proving a Rewrite Against 243,325 Real Purchases](/en/posts/proving-a-rewrite-against-real-purchases) — before it answered a single user, the new service computed entitlement for every purchase we had, compared offline against the monolith's answers. Zero disagreements; the engineering is entirely in what "zero" required.
- [Coverage, Not Just Agreement](/en/posts/coverage-not-just-agreement) — a parity harness reporting 100% agreement tells you nothing until you know what it asked. Making coverage a gate rather than a statistic.
- [A Store Never Waits on Our Database](/en/posts/a-store-never-waits-on-our-database) — the stores retry aggressively when you're slow, so acknowledging after the write turns one slow query into a self-amplifying retry storm.
- [The Best Test Fixtures Were Already in Production](/en/posts/replaying-real-store-notifications) — the monolith had been storing every raw store notification for years. All 720,183 of them turned a fixtures exercise into a replay exercise.
- [Idempotency Belongs in the Database](/en/posts/idempotency-belongs-in-the-database) — application-level duplicate checks are necessary and insufficient. Under concurrent delivery the only thing that reliably holds is a constraint.

## Port and land it

- [Fidelity Beats Tidiness: Porting a Payments Service](/en/posts/fidelity-beats-tidiness-porting-a-payments-service) — two ugly quirks reproduced on purpose, one deliberate divergence, and why the pressure to tidy arrives disguised as competence.
- [Who Gets PRO: Entitlement Across Four Systems](/en/posts/who-gets-pro-entitlement-across-four-systems) — granting a paid tier sounds like setting a boolean. It's a predicate applied to four systems with no transaction between them.
- [The Repository Is Not the Running System](/en/posts/the-repository-is-not-the-running-system) — three times, code that was correct, reviewed, merged and deployed did nothing, because a value never crossed one of the nine boundaries between declaration and use.
- [Measuring Production and Believing the Wrong Thing](/en/posts/measuring-production-and-believing-the-wrong-thing) — a log query matched 0 of 292,932,577 records and read as a definitive answer. The format it searched for was 100% of live traffic.
- [The Tests That Guard the Process Age Fastest](/en/posts/the-tests-that-guard-the-process-age-fastest) — the mechanical gate caught real defects, then produced three lessons entirely about itself.

## The adjacent thread

Running alongside this, in a different repository, was an evaluation of whether one sports-data provider could replace another. The two projects met exactly once, in the coupling survey above: entities keyed to a single provider are the ones where a provider migration stops being an integration exercise and becomes a data-migration exercise.

- [What the API Returns vs What the Docs Claim](/en/posts/what-the-api-returns-vs-what-the-docs-claim)
- [A Migration Spike Should Produce a Loss List, Not a Recommendation](/en/posts/turning-a-vendor-decision-into-a-document)
