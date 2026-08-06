---
title: "The Best Test Fixtures Were Already in Production"
date: "2026-07-25"
description: "The monolith had been storing every raw store notification for years — 720,183 of them. That turned a rewrite from a fixtures exercise into a replay exercise. On recognising an accidental corpus, seeding an ephemeral database so the harness can never touch live data, and why storing the raw payload is the cheapest thing you will ever do."
tags:
  [
    "testing",
    "migration",
    "architecture",
    "backend",
    "go",
  ]
---

We were about to port notification handling — the code that receives a webhook from App Store or Google Play, works out what changed, and updates a purchase. Inherited logic, years of accumulated special cases, no specification beyond the implementation.

The usual approach is fixtures: write a JSON payload per notification type, assert the new code does the right thing. It's honest work and it tests your understanding of the format, which is not the same as testing against the format.

Then someone noticed that the old system had been storing every raw notification payload it ever received, alongside the processing result. Not decoded, not normalised — the raw body.

```
720,183 notifications
404,474 Google Play
315,709 App Store
```

That's not a table anymore. That's a test corpus, accumulated for free over years, containing exactly the payloads real stores actually sent — including the malformed ones, the deprecated fields, and the shapes that only appeared during some outage in 2023.

## An accidental corpus is better than a designed one

Hand-written fixtures encode what you believe the format to be. That belief comes from documentation, which describes the format the vendor intends to send.

The corpus contains what they *did* send. Those differ in the ways that matter: fields that are documented as required and are sometimes absent, enum values not in the docs, a payload shape that changed without a version bump. Every one of those is a case a fixture author doesn't think to write, because you can only write a fixture for a case you know exists.

There's a distributional argument too. Fixtures give you roughly one example per type, which flattens the frequency of everything. The corpus is skewed — hundreds of thousands of renewals, a handful of revocations — and the skew is real information. It tells you which paths matter for performance and which are rare enough that a subtle bug could sit there for a year.

## The harness cannot touch anything real

Two decisions in the design exist purely to make the corpus safe to use.

```
D-2 — The replay harness never touches stable
D-3 — The service writes to stable, the harness does not
```

The replay seeds an **ephemeral database** and runs against that. Not a read-only connection to the shared one — a separate database, created for the run, thrown away after.

The distinction matters because a replay is not a read. It has to write: processing a notification produces a purchase row, and comparing outcomes means letting both implementations produce their rows. So "read-only" isn't available as a safety mechanism here, and the alternative would be writing to the real database inside a transaction you promise to roll back. That's one `defer` away from disaster.

The related criterion:

> **AC-049** — The comparison cannot mutate the shared tables.

*Cannot*, not *does not*. The harness has no credential that reaches the shared database. Structural, rather than a rule someone follows — which matters because this is a tool you'll run repeatedly while chasing disagreements, often late, often distractedly.

`D-3` is the complement, and worth stating separately: the *service* does write to the real environment. It's not that writes are forbidden in this phase, it's that the harness specifically has no business making them. Two components, two different levels of authority, written down so nobody unifies them for convenience.

## Diagnosable, because you will re-run it a lot

> **AC-047** — Every disagreement is diagnosable without a rerun.

Same criterion as the entitlement comparison, and it earns its place twice for a specific reason: a corpus of 720,183 payloads takes real time to process. A harness that reports "1,204 disagreements" and nothing else forces you into a cycle of add-logging, re-run, wait, look — where each iteration costs minutes and you'll need several.

So the output carries the payload, both outcomes, and the branch each implementation took. Which has a second benefit: a report at that granularity is groupable. 1,204 disagreements turn out to be four distinct causes, and you fix four things rather than triaging 1,204 rows.

## Store the raw payload

The general lesson isn't about replay harnesses. It's that the monolith did one cheap thing years ago — kept the raw body alongside the parsed result — and that decision paid for the entire verification strategy of its own replacement.

Storing the raw payload is nearly free. It's a text column, it compresses well, and it's write-once. What it buys you:

- **Replay.** You can re-run new code against real history. This is the one we used.
- **Retroactive debugging.** When a purchase is in a state nobody can explain, you can read what the store actually said rather than inferring from what your parser kept.
- **Parser-bug recovery.** If you discover your decoder dropped a field for six months, you can reprocess. Without the raw body, that data is gone.

I'd now treat it as a default for any external webhook: **keep the raw body, keep it for a long time, and don't normalise it on the way in.** The cost is storage that's cheap. The benefit is optionality you cannot manufacture later, because the payloads you didn't keep are gone permanently and no amount of engineering brings them back.

## What I'd take from this

**Look for a corpus before you write fixtures.** Logs, an audit table, a raw payload column, an S3 archive of requests. If you already have real inputs, fixtures are a supplement rather than the strategy.

**Real inputs contain the cases you don't know to write.** That's the entire value, and it's not obtainable by being thorough.

**Give the harness its own ephemeral database.** A replay must write, so read-only isn't available; separation is. Make it *unable* to reach production rather than *unlikely* to.

**Store raw payloads on the way in.** It's the cheapest optionality in software, and the version of you doing the rewrite in three years will find it worth more than anything you designed on purpose.
