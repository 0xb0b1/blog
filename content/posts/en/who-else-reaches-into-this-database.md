---
title: "Who Else Reaches Into This Database?"
date: "2026-07-17"
description: "The blocking question for any extraction. On enumerating every service that touches a shared database with direction and evidence, discovering which entities are keyed to an external provider — and why a single-provider key quietly makes your history depend on a vendor you might replace."
tags:
  [
    "architecture",
    "databases",
    "data-ownership",
    "microservices",
    "backend",
  ]
---

I've written before that a service boundary is about data ownership: a service owns a slice of data and is the only thing that writes it. That's the principle. This post is about the survey you have to do before you can apply it to a database that predates the principle.

The question is simple and the answer is never in one person's head: **who else reaches into this database, and for what?**

## Couplings need direction and evidence

The criterion we wrote:

> **AC-034** — Every coupling names its service, tables, direction and evidence.

Four fields, and each one is there because of a way this survey goes wrong.

**Service** is obvious. **Tables** matters because "service X uses the subscriptions database" is not actionable — three tables is a different problem from thirty.

**Direction** is the one people skip, and it's the one that decides the plan. A service that only reads a table can be handled with a view, a replica, or an API, and it can keep working through a cutover. A service that *writes* a table you're about to own is a blocker: two writers to one table means you don't have a boundary, you have a shared mutable variable between two deployables.

**Evidence** is the field that keeps the document honest. Not "I think the notifications service reads this" but the query, the file, the line. Without it, a coupling survey becomes a collection of recollections, and the couplings people misremember are precisely the ones that cause the incident.

## Provider-keyed entities, and the trap in them

The part I hadn't anticipated:

> **AC-031** — Every provider-keyed entity is in the inventory with its providers.
> **AC-033** — Every single-provider-keyed entity is flagged as history-exposed.

We ingest sports data from external providers. Many entities are keyed by the provider's identifier rather than by an id we mint — a match, a competition, a team is *their* id in our column.

That's normal, and mostly harmless, right up to the moment you consider changing provider. Then an entity keyed to exactly one provider means all your history for that entity is expressed in a vocabulary you no longer have a subscription to. You can keep the rows. You just can't join them to anything new, and you can't re-fetch what's missing.

The criterion calls that **history-exposed**, and flagging it is the whole value. In parallel we were evaluating whether one sports-data provider could replace another; the entities flagged here are exactly the ones where that migration stops being an integration exercise and becomes a data-migration exercise. Two features in different repositories, and the second one's hardest risk was written down in the first one's inventory.

If your schema keys anything by a vendor's identifier, that's a coupling to the vendor as real as an API call — and much less visible, because it doesn't appear in any dependency list.

## Options evaluated against the couplings

This is the criterion I'd steal for any design document:

> **AC-035** — Every option states its effect on every recorded coupling.

Not "here are three architectures and their general tradeoffs." Each option, crossed against each coupling you actually found. Option A leaves service X reading directly; option B forces X onto an API and costs X a release; option C moves the table and breaks X until it's updated.

That turns architecture selection into something closer to arithmetic. The abstract argument — shared database versus API versus event stream — has no end, because everyone's right in general. The concrete argument has an end, because the number of couplings is finite and the effect on each is checkable.

It also surfaces the option people don't propose out loud: *leave this part where it is.* Once every option is scored against real couplings, "extract everything" frequently stops being the best-scoring one, and you find that the sensible boundary is narrower than the one in the original proposal.

## One recommendation, and its main risk

> **AC-037** — The report names one recommendation and its main risk.

Both halves are deliberate.

**One** recommendation, because a report that presents three options neutrally and stops has outsourced the decision back to the reader — usually to a meeting, where the person who talks most decides. If you did the analysis, you have an opinion; say it.

**Its main risk**, singular, because a recommendation with no stated risk reads like advocacy and gets treated as such. And a list of nine risks is a way of not committing. Naming the one thing most likely to make this the wrong call is what makes a reviewer able to engage with it — they can attack that risk specifically, and either it survives or you've learned something cheaply.

## What I'd take from this

**Survey before you design.** The set of services touching a shared database is discoverable, finite, and not knowable from memory. It's a few hours of work and it changes what you propose.

**Direction and evidence, per coupling.** Readers can be handled; writers are boundaries. And "evidence" is what stops the survey from being a poll.

**Look for vendor identifiers in your keys.** A column holding someone else's id is a coupling to that vendor with none of the visibility of an API dependency. If exactly one provider can supply that key, your history is exposed to that provider's contract.

**Score options against the couplings you found, not in the abstract.** It ends the argument, and it makes "don't extract this" a visible option rather than an unspeakable one.
