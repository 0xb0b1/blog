---
title: "What the API Returns vs What the Docs Claim"
date: "2026-07-27"
description: "Evaluating whether one sports-data provider could replace another. The published capability matrix and the trial API disagreed, so the analysis was rebuilt on probes against the live endpoint. On deriving your consumption from code rather than memory, citing evidence per verdict, and labelling one appendix explicitly unproven."
tags:
  [
    "architecture",
    "api",
    "vendor-evaluation",
    "decision-making",
    "backend",
  ]
---

We ingest sports data — matches, competitions, teams, live events — from a commercial provider. The question on the table was whether a different provider could supply the same thing, at lower cost.

The usual way this gets answered is a spreadsheet built from two vendors' documentation, filled in over a week, presented as a comparison. I've built that spreadsheet before. It's confidently wrong in a specific way: it compares what two vendors *say* against what you *think* you use.

Both halves of that are unreliable, so the analysis was structured to replace each with evidence.

## What we consume, derived from the code

The first user story wasn't about the new provider at all:

> **US-001** — Know exactly what we take from Opta.

Not "what do we use" as an interview question. An inventory derived from the code: the feed endpoints named in the data-access layer, the WebSocket feed constants, the enum values in the gRPC contract, the scheduler job names. Mechanically enumerated, so the list is the truth rather than a recollection.

This matters more than it sounds, and in the same way the monolith's surface inventory did. People know the feeds they personally touched. The full set includes a feed added for one competition three years ago, and one that's polled by a scheduled job nobody has looked at since it was written. Both count when you're pricing a migration, and neither will come up in conversation.

A useful side effect: the inventory is the thing you take to the new vendor. "Can you supply these fourteen specific capabilities" is a much better question than "can you replace Opta", and it produces a much better answer.

## Verdicts with citations

The second story is the comparison, and its criterion is where the discipline lives:

> **US-002** — Know whether Sportradar can supply each capability.

Every capability gets a verdict, and every verdict cites its source. Not a green tick — a reference to the documentation section, so a reader can check the claim without redoing the research.

That sounds like bureaucracy until the verdicts disagree with reality, which is exactly what happened.

## The docs and the trial API disagreed

A later spike exists because the first analysis wasn't enough:

> **US-006** — Know what the API actually returns, not what the docs claim.

The published capability matrix said certain data was available at a certain tier. Probing the trial endpoint returned something else — fields absent, coverage narrower than described, some capabilities present only for a subset of competitions.

I don't think this is vendor dishonesty. Documentation describes the product in its fullest configuration; what any particular account can reach depends on tier, region, licensing, and which competitions the vendor has rights to this season. The document isn't lying, it's just answering a more general question than the one you're asking.

So the analysis was rebuilt on probes: call the live endpoint for the capabilities we actually consume, across the competitions we actually serve, and check in the raw responses as evidence. The unit of truth stopped being "the docs say" and became "we asked, on this date, and got this."

The lesson I'd generalise: **for a vendor evaluation, documentation is a hypothesis and a trial key is the experiment.** If the vendor won't give you a trial key, that is itself information about how the relationship will go.

## Label what you couldn't prove

The detail I keep coming back to is a task titled *TheSports appendix, explicitly unproven.*

A second provider came up during the work, and we didn't have access to test it. Two tempting options: leave it out, or include it on documentation alone alongside the empirically-verified provider.

Both are bad. Omitting it loses information — someone will ask about it, and the analysis will look incomplete. Including it silently is worse, because a reader can't tell that one column of the comparison is evidence and another is marketing copy, and they'll weight them equally.

So it's included and **labelled unproven**, in its own appendix, separated from the verified analysis. The reader gets the information and its provenance.

This is the same instinct as declaring coverage gaps in a test harness, and it shows up everywhere once you notice it: **the confidence level of a claim is part of the claim.** A document that presents verified and unverified findings in the same visual weight has destroyed information that its author had.

## What I'd take from this

**Derive your consumption from code, not from memory.** The set of things you take from a vendor includes feeds nobody remembers, and those cost the same to replace as the ones you use daily.

**Take the inventory to the vendor.** Fourteen named capabilities gets a real answer; "can you replace X" gets a sales response.

**Treat documentation as a hypothesis and probe the API.** Docs describe the product; your account reaches a subset of it. The difference is the whole risk of the migration.

**Check in the raw probe responses.** Dated evidence, reviewable, re-runnable. It also lets you ask the vendor a precise question when the answer differs from the docs.

**Label unproven sections as unproven.** Including something you couldn't verify is fine. Including it at the same confidence as everything else is a way of misleading a reader with true statements.
