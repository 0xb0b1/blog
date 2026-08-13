---
title: "A Cutover That Can Be Reversed"
date: "2026-07-19"
description: "Reversibility as an acceptance criterion rather than a rollback plan written the night before. On phasing an extraction so each step can be undone, why \"we can revert the deploy\" stops being true the moment data moves, and the criterion that what stays behind must be as explicit as what moves."
tags:
  [
    "architecture",
    "deployment",
    "migration",
    "risk",
    "backend",
    "subscriptions-extraction",
  ]
---

Every migration plan has a rollback section. Most of them say some version of "revert the deploy and route traffic back."

That's true for a stateless service and false for almost everything else. The moment the new system has accepted a write the old one doesn't know about, reverting the deploy doesn't restore the previous state — it restores the previous *code*, pointed at data that has moved on without it. You are now in a state neither system was designed for, at the worst possible moment.

So we made it a criterion:

> **AC-020** — The cutover is phased, reversible, and covers the data.

Three requirements in one sentence, and the third is the one that does the work.

## Phased, so each step is small enough to undo

Reversibility isn't a property you add at the end. It's a property of how you cut the work up, and it has to be decided before the first phase ships.

Ours went: stand up the service reading only; prove its answers match the old system offline; then move a write path. Each phase is independently reversible because each one leaves the previous system fully authoritative. Turning off a service that has only ever read is free. Turning off a service that has been the sole writer for a week is a data-recovery exercise.

The general rule I'd extract: **order the phases by how expensive their reversal is, cheapest first.** That ordering isn't automatic — the tempting order is by how interesting the work is, or by dependency convenience, and both of those put the expensive-to-reverse step earlier than it needs to be.

There's a bonus that isn't obvious in advance. Phasing this way means the earliest phases produce evidence rather than risk. Shadow reads told us the new entitlement logic agreed with the old one on a quarter of a million real purchases before it served a single user. That evidence was only available because the phase was designed to be discardable.

## "Covers the data" is the hard part

This is the clause that separates a real rollback plan from a paragraph.

For every phase, three questions:

**What data does this phase create or change?** If the answer is "none," reversal is a deploy. If it's anything else, keep going.

**If we reverse, what happens to that data?** Discard it? Migrate it back? Leave it and reconcile later? All three are legitimate answers; not having one is not.

**Can the old system tolerate data the new one wrote?** This is the question people miss. Reverting to the old code means reverting to the old code's assumptions — about schema, about which states are valid, about who writes which column. If the new service introduced a row shape the old one rejects, then reverting the deploy hands your old code a database it will fail against.

Writing these down per phase is unglamorous and it's where the actual plan comes from. Twice it changed the phase boundary: it turned out to be cheaper to move a piece of schema in its own step than to have a phase whose reversal required a data migration.

## What stays behind, stated as explicitly as what moves

> **AC-013** — What stays behind is as explicit as what moves.

At first read this belongs to the inventory work rather than the cutover. It's here because of what a partial extraction leaves you with.

An extraction produces two systems, and the interesting bugs live in what neither one claims. If the plan enumerates what moves and treats the remainder as "everything else," the remainder is undefined — and undefined means each engineer draws the line where their own change happens to need it. That's how you get two services both writing the same table, each believing the other stopped.

Writing the remainder explicitly also makes reversal cheaper, because you know exactly what the old system is still responsible for. "Revert to the old system" is only a meaningful instruction if you can say what the old system still owns.

## Infrastructure written, applied deliberately

One implementation choice fell out of this, and I like it more the longer I sit with it: the first phase of the new service included its Terraform, **written and validated but not applied**.

That sounds like a half-measure. It's the opposite. The infrastructure definition is reviewed alongside the code that needs it, when the context is fresh — but nothing exists yet, so there's nothing to reverse. Applying it is a separate, deliberate act taken when the phase that needs it is actually starting.

The alternative I've done before is applying infrastructure early "so it's ready," which quietly means the reversal surface of phase one now includes cloud resources, and the phase you thought was free is not.

## The part that isn't reversible

Honesty compels a limit. Some things genuinely can't be undone, and the plan should say which.

Once you have acknowledged a store notification, you cannot un-acknowledge it — the store considers itself finished. Once you've told a payment provider you accepted a purchase, that's a commitment to a third party. No phase design makes those reversible; the most you can do is make them *late*, so that the irreversible steps sit behind as much proven behaviour as possible.

Which is another way of stating the ordering rule: irreversible steps go last, after the evidence has accumulated. And it's a reason to be suspicious of any plan whose first phase touches a third party.

## What I'd take from this

**Make reversibility a criterion, not a section.** A criterion gets checked per phase. A section gets written once, at the end, by whoever is most optimistic.

**Order phases by cost of reversal, cheapest first.** The tempting orders — by interest, by dependency — both front-load the expensive ones.

**Answer "what happens to the data" per phase, in writing.** This is the whole difference between a rollback plan and a rollback paragraph, and it will move your phase boundaries.

**State what stays behind.** The undefined remainder is where two systems end up writing the same row.

**Name the irreversible steps and put them last.** You can't fix them; you can make sure they happen after you've learned everything cheaper.

---

*Part of [The Subscriptions Extraction](/en/posts/the-subscriptions-extraction-a-reading-order), seventeen posts on pulling the subscriptions half of a Django monolith into a Go service.*
