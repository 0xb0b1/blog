---
title: "Tracing: Wired but Silent"
date: "2026-07-21"
description: "We instrumented a new Go service with OpenTelemetry on day one and configured it to emit nothing, because no collector had been chosen. On separating a code decision from an operational one, why that beats both \"add it later\" and \"pick a backend now\", and the small print that makes it honest."
tags:
  [
    "go",
    "observability",
    "opentelemetry",
    "logging",
    "backend",
  ]
---

Phase 0 of our new service had a design section with a title I liked enough to keep: **"Tracing: present but silent."**

The service is fully instrumented with OpenTelemetry. Spans are created, context propagates, every request produces a trace carrying the correlation id. And by default it exports none of it, because when we built the scaffold nobody had decided where traces should go.

That sounds like the worst of both worlds. I think it's the best available, and the reasoning generalises past tracing.

## Two decisions that get treated as one

"Should this service be traced?" and "which tracing backend do we run?" arrive together and have almost nothing in common.

The first is a **code** decision. It's about whether handlers take a context, whether that context crosses your queue boundary, whether the database layer participates. It touches every function signature on the request path. It is expensive to retrofit and cheap to do while you're already writing the code.

The second is an **operational** decision — vendor, cost, retention, who administers it, whether it's self-hosted. It touches no application code at all. It's expensive to get wrong and there's no reason to rush it.

Bundling them produces the two bad outcomes I've seen repeatedly. Either you block the scaffold on a procurement conversation, or you defer both and "add tracing later" — which means threading context through forty call sites in a service that now has traffic, and doing it under the pressure of an incident that made you want traces in the first place.

Separating them means the expensive-to-retrofit half ships with the code, and the reversible half stays open.

## What "silent" has to mean to be honest

A deferral is only safe if it's genuinely inert, and there are three ways this goes wrong.

**The instrumentation must be exercised, not merely present.** Our criterion is that *each request produces a trace carrying the correlation id* — asserted in tests against a recording exporter in memory. Code that constructs spans nobody ever verifies is code that will be wrong when you switch the exporter on, and you'll be debugging it during the outage you wanted it for.

**Silent must mean zero network cost.** A no-op exporter, not a real exporter pointed at a dead endpoint. The second version retries, buffers, times out, and eventually appears in your latency percentiles — a "disabled" feature that costs milliseconds is worse than an absent one.

**Turning it on must be configuration, not a release.** If enabling traces requires a code change, you haven't deferred a decision, you've scheduled work. One environment variable, no rebuild.

## The consolation prize is most of the value

Here's the part that made the deferral comfortable rather than merely defensible: the correlation id flows through the **logs** from day one.

The question that actually gets asked during an incident is "what happened to this user's request?" With a correlation id in every structured log line, and that id returned to the caller, that's a query — and it works with no tracing backend at all. Grep-equivalent across structured logs answers most of what a trace answers, minus the timing waterfall.

Which reframes the tracing decision honestly. Traces buy you *span timing* and cross-service topology. Correlated logs buy you *what happened, in order*. The second is 80% of incident debugging and needs no vendor. So the thing we deferred was the smaller half, and the thing we shipped immediately was the half that answers the common question.

If you're weighing this trade in your own scaffold, I'd put it this way: correlated structured logging is not the poor cousin of tracing. It's the load-bearing one. Tracing is what you add when you need to know *why it was slow* rather than *what it did*.

## The design has to say why

The design section is two paragraphs, and it says that tracing is instrumented, that export is off, that turning it on is one variable, and that the collector decision is open with no date attached.

That last part matters more than it looks. An undocumented disabled feature is indistinguishable from a broken one. Six months on, the next engineer finds tracing code that produces nothing and has two possible readings: someone deliberately deferred this, or someone left it half-finished. Without the design note they'll assume the second, and either rip it out or "fix" it by pointing it at whatever backend they happen to like.

Writing "present but silent" as a heading rather than a comment is what converts an absence into a decision. It also means the deferral is visible in review — a reviewer can push back on it, which they can't do with a thing that simply wasn't mentioned.

## What I'd take from this

**Split code decisions from operational ones and ship the code half early.** Threading context is expensive to retrofit; choosing a vendor isn't.

**Test the instrumentation with a recording exporter.** Uninspected span construction is a latent bug that surfaces exactly when you need it not to.

**Silent means a no-op exporter and a config flag**, not a real exporter aimed at nothing and a future code change.

**Document the deferral as a decision, with its reason.** Otherwise the next person reads a disabled feature as an unfinished one — and they're not being unreasonable.

**Get the correlation id into your logs first.** It answers the question people actually ask, and it doesn't need anyone to pick a product.
