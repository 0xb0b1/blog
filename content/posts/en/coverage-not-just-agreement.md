---
title: "Coverage, Not Just Agreement"
date: "2026-07-23"
description: "A parity harness reporting 100% agreement tells you nothing until you know what it asked. On making coverage a gate rather than a statistic, declaring the gaps out loud, and scoping a replay to the deterministic core so the claim you publish is one you can defend."
tags:
  [
    "testing",
    "migration",
    "go",
    "python",
    "quality",
    "subscriptions-extraction",
  ]
---

I wrote about [proving a rewrite by comparing it against the system it replaced](/en/posts/proving-a-rewrite-against-real-purchases) over a full corpus. The number that came out was zero disagreements, and I said at the time that the number was the least interesting part.

This is why. A comparison result has two dimensions, and only one of them gets reported by default.

**Agreement** is what everyone measures: of the cases we compared, how many matched? **Coverage** is what makes agreement meaningful: of the cases that exist, which did we compare at all? A harness that reports only the first is describing its own diligence in the most flattering possible terms.

## The failure this prevents

Concretely: our replay harness feeds real store notifications through a new decoder and compares its output against the old one. Suppose it reports 100% agreement over 700,000 notifications.

That is compatible with the harness having only ever exercised two notification types — because those two make up 95% of volume and the other eleven types are rare. The rare ones are also the ones with the interesting logic: revocation, refund, upgrade, a subscription changing product mid-term. Perfect agreement on renewals tells you nothing about refunds, and volume-weighted metrics will never notice.

So the criterion is explicit:

> **AC-048** — Notification-type coverage is reported, and gaps are declared.

Two obligations. Report the coverage *by type*, not in aggregate. And where a type wasn't exercised, **say so, in the output.**

## Declaring gaps is the harder half

Reporting per-type coverage is a grouping. Declaring gaps is a cultural commitment, and it's the part that decays first.

The temptation with an unexercised type is silence. The corpus has no examples, nothing failed, the report is green. Every incentive points toward letting the reader assume coverage was complete — and readers do assume that, because a report that mentions no gaps reads as a report with no gaps.

Making the declaration mandatory changes what the artefact is. It stops being evidence that the port works and becomes an honest description of what has been established: *these nine types are proven against real data, these two have no examples in the corpus, this one is unimplemented.* A reviewer can act on that. They can decide the two unexercised types are acceptable risk, or go find examples, or gate the release. What they can't do is act on a green tick that quietly means "we didn't check."

The companion criterion makes the gap consequential rather than merely visible: notification-type coverage **gates** the port. An unexercised type isn't a note in a report, it's a blocked release. That's what stops gap declaration from becoming a formality that everyone scrolls past.

## Scope the claim to what you actually proved

The decision I want to highlight is the one that sounds like a weakness:

> **D-6** — The replay covers the deterministic core, not the whole handler.

A notification handler does several things: authenticate the payload, decode it, decide what changed, write it, and emit side effects. Only some of that is deterministic given the input. Writing depends on current database state. Side effects touch third parties. Timestamps and generated ids differ per run.

We could have built a harness that replays the whole handler and normalises away everything nondeterministic. That's a lot of machinery, and every normalisation is a place where the harness quietly stops testing the thing it claims to test — you end up masking a real difference because it looked like a timestamp.

Instead the replay covers the decode-and-decide core, which is where the inherited complexity lives and where a rewrite is most likely to differ. The rest is covered by ordinary tests. And crucially, the scope is written down as a decision, so the claim being published is "the deterministic core agrees over the corpus" rather than the stronger claim nobody could defend.

This is the same instinct as declaring gaps, applied to the harness's own boundary. **A narrower claim you can defend beats a broader claim you can't.** Someone reading `D-6` knows exactly what has and hasn't been established, and if they think the boundary is wrong they can argue with it — which they can't do with an unstated assumption.

## Why coverage belongs in the criteria, not the docs

None of this is novel as an idea. Every engineer nods along at "coverage matters." It gets lost anyway, and I think the reason is structural: coverage is nobody's deliverable.

Agreement has a natural owner — it's the thing the harness exists to produce, and a disagreement is a task. Coverage has no owner. It's a property of the corpus, which nobody chose, and it can only fail silently. So the way to keep it is to make it a criterion with a gate behind it, at which point it acquires an owner by force.

The general pattern, which shows up in three different places in this project: **keep the two numbers separate and gate on both.** Criteria that have tests versus criteria whose tests passed. Tasks committed versus tasks recorded. Cases compared versus cases agreed. Each time, collapsing the pair into a single reassuring figure is what lets unverified work look finished.

## What I'd take from this

**Report coverage by category, never in aggregate.** Volume-weighted coverage is dominated by the common case, which is the case you least need to check.

**Make gap declaration mandatory and put a gate behind it.** A report that mentions no gaps is read as a report with no gaps, and an unexercised branch with no consequence attached will stay unexercised.

**Write down what your harness does not cover.** A stated boundary can be argued with. An assumed one gets discovered during an incident.

**Prefer the narrower defensible claim.** "The deterministic core agrees across the corpus" is worth more than "the handler is verified," because the first one is true.

---

*Part of [The Subscriptions Extraction](/en/posts/the-subscriptions-extraction-a-reading-order), seventeen posts on pulling the subscriptions half of a Django monolith into a Go service.*
