---
title: "Inventorying Every Surface a Monolith Exposes"
date: "2026-07-16"
description: "You cannot rebuild a service until you know exactly what it promises the outside world. On deriving that list from the code rather than from memory — REST routes, deployed applications, real-time channels, notification triggers — and making the inventory an artefact that tests enforce rather than a document that rots."
tags:
  [
    "architecture",
    "microservices",
    "api",
    "contracts",
    "backend",
    "subscriptions-extraction",
  ]
---

The first honest question when extracting a service from a monolith isn't *what should the new service look like*. It's *what does the old one currently promise?*

Everyone thinks they know. In practice what people know is the surfaces they personally work on, and the union of those is not the whole set. The parts nobody remembers are exactly the parts that break a consumer at cutover — a scheduled job, an event nobody subscribes to any more except one mobile release from eighteen months ago, a REST route added for a partner integration that still gets traffic.

So we wrote the inventory as a feature, with acceptance criteria, before designing anything.

## Four kinds of surface, not one

The mistake I'd have made unprompted is treating "the API" as the interface. It isn't. The criteria enumerate four separate categories, and each was found by a different method:

> **AC-022** — Every REST route in the app is in the inventory.
> **AC-023** — Every deployed API application is in the inventory.
> **AC-024** — Every real-time channel is in the inventory.
> **AC-025** — Every notification trigger is in the inventory.

REST routes are the easy one — they're in a router, enumerable by walking the code. **Deployed applications** is the criterion that saved us: the monolith isn't one deployable, and a rebuild that reproduces the routes of the main API while missing a second deployed app is a rebuild that breaks in production only.

**Real-time channels** and **notification triggers** are the ones that get forgotten in every migration I've seen, because they aren't request/response. Nobody calls them, so nobody notices them missing until a user doesn't get a push notification, which is a bug report that arrives days late and is nearly impossible to trace back to a cutover.

## Two criteria that keep an inventory honest

Any inventory drifts. These two are why this one is worth keeping:

> **AC-026** — The inventory claims nothing the code does not have.

The direction of that sentence matters. It's easy to check that everything in the code appears in the inventory — that's completeness, and it's what you'd write first. This is the *other* direction: nothing in the inventory may be aspirational. A document listing a route that was deleted last year is worse than an incomplete one, because it makes the rebuild reproduce something that no longer exists.

Both directions are enforced by tests, which is what makes this an artefact rather than a document. A new route added tomorrow without an inventory entry fails the check.

> **AC-029** — The report's counts match the inventory.

This looks like bureaucracy and isn't. The report is what people read; the inventory is the data. The moment a human writes "we expose around forty endpoints" in a summary, that number starts diverging from the list. Deriving the count and asserting it means the summary can't quietly become wrong.

## The two questions that turn a list into a plan

An inventory on its own is just a list. Two more criteria make it decision-useful:

> **AC-027** — Every surface names the consumers that depend on it.
> **AC-028** — Every surface states whether the rebuild must reproduce it.

The first is what lets you sequence a cutover. If you know which surfaces the mobile app depends on versus which ones only an internal batch job touches, you can move the internal ones first and learn on the surfaces where a mistake is cheap.

The second is the one that shrinks the work. Not every surface must be reproduced. Some exist for a client that no longer ships. Some were built for an experiment. Writing "must reproduce: no" against a surface, with a named reason, converts an argument that would otherwise happen during the migration into a decision made while nobody is under pressure.

And it's a genuinely load-bearing distinction. The size of a rebuild isn't the size of the old system — it's the size of the part that still has consumers.

## The report is grouped by consumer, not by surface

One small structural choice with disproportionate value:

> **AC-030** — The report groups the must-reproduce surfaces by consumer.

The natural way to present an inventory is by surface type: here are the routes, here are the events, here are the jobs. That's the right shape for the data and the wrong shape for the reader, because nobody's question is "what routes exist." Their question is "if I'm the mobile team, what changes for me?"

Grouped by consumer, the report answers that directly, and a cutover plan more or less falls out of it — each consumer is a conversation, and each conversation has a list.

## What I'd take from this

**Enumerate surfaces by kind, from the code.** Routes, deployables, real-time channels, scheduled work, published events. Four different searches, because they live in four different places and no single grep finds them all.

**Assert both directions.** Everything in the code is in the inventory; nothing in the inventory is absent from the code. The second half is the one that keeps it true a year later.

**Decide "must reproduce" per surface, in advance.** This is where a rebuild's scope actually gets set, and doing it before the migration means doing it calmly.

**Group the summary by consumer.** The data wants to be organised by surface type. The reader wants to know what breaks for them.

The whole thing took a fraction of the time the extraction did, and it was the artefact I referred back to most — including twice to answer "does anything still use this?" with a citation rather than a guess.

---

*Part of [The Subscriptions Extraction](/en/posts/the-subscriptions-extraction-a-reading-order), seventeen posts on pulling the subscriptions half of a Django monolith into a Go service.*
