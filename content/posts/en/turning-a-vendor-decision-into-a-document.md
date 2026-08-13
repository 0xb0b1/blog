---
title: "A Migration Spike Should Produce a Loss List, Not a Recommendation"
date: "2026-07-28"
description: "The output of an evaluation shouldn't be a verdict someone has to trust — it should be a document they can disagree with. On ranking gaps by impact instead of tabulating features, establishing the minimum tier per capability against the catalogue you actually serve, and making the cost of \"yes\" explicit."
tags:
  [
    "architecture",
    "decision-making",
    "documentation",
    "vendor-evaluation",
    "provider-migration",
  ]
---

Three spikes came out of evaluating whether we could replace a sports-data provider. The first inventoried what we consume and checked whether the alternative could supply it. The second worked out which pricing tiers were actually required. The third asked the blunt question: if we went provider-only, what would we lose?

That third one is the interesting shape, and it's the one I'd repeat. Its deliverable isn't a recommendation. It's a **loss list**.

## Gaps ranked by impact, not tabulated by feature

A feature matrix has a comforting property: it looks complete. Fourteen rows, two columns, ticks and crosses. And it's nearly useless for deciding, because it treats every row as equally important.

The criterion we used instead:

> **US-003** — See the gaps ranked, so the decision is informed.

Ranked by impact. A missing capability that one internal report depends on and a missing capability that the live match screen depends on are not the same row, and a matrix that renders them identically has thrown away the only information that matters.

Ranking forces a judgement the matrix lets you avoid. You have to say *this gap would be visible to users within a minute of a match starting* and *this one would be noticed by two people at month-end*. Those sentences are arguable in a way a cross in a cell isn't — and arguable is the point, because someone who disagrees can say so specifically.

## Tiers, against the catalogue you actually serve

The second spike is narrower and it's the one that would have been skipped:

> **US-004** — Know which tiers supply each capability.
> **US-005** — See the tiers R10's product actually works at.

Vendor pricing is tiered, and capability availability differs per tier. So "can they supply this?" is incomplete — the real question is "at what tier, and does the product work at that tier for the competitions we serve?"

That second clause is where the analysis nearly went wrong. A tier can technically supply a capability while covering only the top few leagues. If your product serves a long tail of competitions, a capability available for 5% of your catalogue is not available. Establishing the minimum tier *per capability*, then checking it against the **real competition catalogue** rather than a representative sample, is what turned a plausible answer into a defensible one.

There's a documentation detail I like here: this yardstick was later **formally superseded** by the go/no-go spike rather than quietly replaced. The earlier document remains, marked as superseded by the newer one. Anyone who finds the yardstick knows there's a later analysis; anyone reading the newer one can see what it revised. Same instinct as striking through a decision record instead of deleting it.

## The loss list is the deliverable

Here's the part I'd argue for hardest. The strongest thing an evaluation can produce is not "we recommend X." It's an explicit list of what saying yes costs.

Every migration decision has losses. Some capability is worse, some latency is higher, some data no longer arrives, some workflow needs a workaround. A document that presents only benefits and a recommendation isn't an analysis, it's advocacy — and everyone reading it knows that, which is why those documents generate suspicion rather than agreement.

Writing the losses down does three things.

**It makes the decision reviewable.** A reader can look at the list and say "we can live without those three, but not that one." That's a real conversation, and it's much faster than one where they have to reverse-engineer the costs from your enthusiasm.

**It survives the decision.** Six months in, when someone hits one of the losses, the question is "was this known?" A loss list answers yes, with a date. Without it, a known tradeoff becomes an apparent oversight, and trust in the analysis drains away exactly when you need it.

**It disciplines the analyst.** It's uncomfortable to write, which is the signal that it's the useful part. If a spike produces no losses, either the migration is free — which it isn't — or the analysis stopped early.

## Recommend anyway

None of that means being neutral. I've written before that a report should name one recommendation and its main risk, and that applies here too.

A document that lays out gaps, tiers and losses and then declines to conclude has handed the decision back to a meeting. If you did the work, you have an opinion, and withholding it isn't rigour — it's risk aversion dressed as objectivity.

The combination is what works: **one recommendation, its main risk, and the full loss list.** The recommendation gives the reader somewhere to start; the risk tells them where to attack it; the loss list tells them what they're buying. All three, and a reviewer can genuinely engage. Any one missing, and they're either trusting you or ignoring you.

## What I'd take from this

**Rank gaps by impact; never ship a bare feature matrix.** The matrix looks complete and discards the only distinction that decides anything.

**Check capability against the real catalogue, not a sample.** "Available" at 5% coverage is not available, and a representative sample hides exactly that.

**Supersede documents formally.** A marked-superseded analysis is navigable. A quietly replaced one leaves someone acting on stale conclusions.

**Make the loss list the headline deliverable.** It's the uncomfortable half, it's what makes the document trustworthy, and it's the part that's still valuable a year later when someone asks whether this was known.

**Then recommend.** With one named risk. Analysis that refuses to conclude has outsourced the decision to whoever talks most in the meeting.

---

*Part of a two-post thread on replacing a sports-data provider — alongside [The Subscriptions Extraction](/en/posts/the-subscriptions-extraction-a-reading-order), the project running at the same time.*
