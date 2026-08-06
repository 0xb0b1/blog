---
title: "A Store Never Waits on Our Database"
date: "2026-07-24"
description: "App Store and Google Play retry aggressively when you're slow, so a handler that writes to Postgres before acknowledging turns a slow query into a retry storm. On separating durability from processing, the decision record we had to supersede once we worked out what \"durable\" needed to mean, and why nothing may be acknowledged that isn't recorded."
tags:
  [
    "architecture",
    "aws",
    "sqs",
    "reliability",
    "backend",
  ]
---

The monolith processed store notifications inline. A webhook arrives, the handler decodes it, looks up the purchase, writes the new state, emits whatever follows, and *then* returns 200.

That works until the database is slow. Then the store's client times out, and because App Store and Google Play both retry on timeout, you get the same notification again — while the first one is still running. A slow query becomes duplicated work, which makes the database slower, which produces more retries. The failure is self-amplifying, and the trigger can be something entirely unrelated to subscriptions.

So the first criterion for the new ingest path was about who waits for whom:

> **AC-041** — The acknowledgement is fast and does not depend on processing.

## The decision we had to supersede

Our first decision record said, roughly, *be durable before acknowledging.* Obviously correct, and it turned out to be underspecified in a way that mattered. Durable *where?*

The reading we started with was: write the notification to our own database, then acknowledge. That is durable, and it still couples the acknowledgement to Postgres — the exact coupling we were trying to remove. It's a smaller write than full processing, so the window narrows, but a slow or unavailable database still means slow or failed acknowledgements.

The design record shows the correction rather than hiding it:

```
~~D-1~~ — Durability before acknowledgement — SUPERSEDED by D-1a
D-1a  — SQS is the durable record
```

The queue is the durable record. The handler authenticates the payload, puts it on SQS, and acknowledges. Nothing on the acknowledgement path touches our database at all. Processing happens later, from the queue, at whatever pace the database can sustain — and if the database is down, the queue grows and nothing is lost.

I've left `D-1` struck through rather than deleting it, and I'd do that again. The superseded version records that we considered a weaker form of durability and says why it wasn't enough. Someone arriving later with "why not just write it to the database first?" gets an answer instead of repeating the analysis.

## The other half of the rule

Fast acknowledgement, taken alone, is how you lose data. So it comes as a pair:

> **AC-043** — Nothing is acknowledged that has not been durably recorded.

Both directions are load-bearing. The first criterion says *don't make the store wait for processing.* The second says *don't tell the store you have it unless you actually do.* Without the second, "acknowledge fast" degrades into "acknowledge immediately and hope", and a failed enqueue becomes a notification that no longer exists anywhere — the store considers itself finished, and you have no record it ever arrived.

That combination is what makes the queue non-optional rather than an optimisation. The acknowledgement is a promise, and the enqueue is what makes the promise true.

## Authenticate before doing any work

> **AC-042** — An unauthenticated notification is refused before any work.

The ordering is the criterion. Authentication happens first, before decode, before enqueue, before anything is allocated on the request's behalf.

A public webhook endpoint is a free capability offered to the internet. If an unauthenticated payload gets as far as being decoded and queued, then anyone who finds the URL can fill your queue and pay nothing for it. Refusing before work turns that into an inexpensive rejection.

There's an interaction with the criterion above worth noticing: an unauthenticated request is refused, not enqueued, so "nothing is acknowledged that isn't recorded" doesn't accidentally commit you to storing garbage.

## Failure has to be visible

> **AC-050** — Repeated failure lands in the dead-letter queue and raises an alarm.

The dead-letter queue and the alarm are specified in the *same task* as the queue itself, deliberately. A DLQ with no alarm is a place where messages go to be forgotten quietly — arguably worse than no DLQ, because the queue looks healthy while notifications accumulate somewhere nobody has a dashboard for.

The pairing is the point. If a message can be moved aside, something must announce that it happened.

> **AC-051** — A store without a processor is parked visibly, not discarded.

This one comes from the phased rollout. We implemented App Store and Google Play; Stripe was planned but not built. The tempting behaviours are both bad: a notification for an unimplemented store either gets silently dropped (data loss, invisible) or crashes the handler (retry storm, from a store that will keep trying).

Parked visibly means it's recorded, not processed, and surfaced. When Stripe arrives, there's a queue of real notifications to replay through it — and in the meantime nobody is guessing whether Stripe traffic is arriving.

## What I'd take from this

**Acknowledgement latency is a coupling.** Whatever the acknowledgement waits on becomes something your webhook provider's retry policy is pointed at. Make the list of things it waits on as short as you can — ideally one durable append.

**"Be durable" is underspecified until you name where.** Our first decision record was correct and useless for the same reason.

**Fast ack and never-ack-without-recording are one rule in two halves.** Either alone is a bug: the first loses data, the second loses the latency guarantee.

**Specify the DLQ and its alarm together.** A dead-letter queue nobody is told about is a silent data-loss mechanism wearing a reliability costume.

**Supersede decisions in place rather than deleting them.** The rejected option is the cheapest way to stop the same debate recurring.
