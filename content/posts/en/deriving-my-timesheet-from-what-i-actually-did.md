---
title: "Deriving My Timesheet From What I Actually Did"
date: "2026-08-05"
description: "Three sources — the board, my workflow's own run records, and git — merged into one line per working day. Getting the data was easy; the naive version produced 58 activities for a single Tuesday. On why each filter had to earn its place, and the four ways duplicate work arrives wearing different clothes."
tags:
  [
    "automation",
    "bash",
    "google-sheets",
    "productivity",
    "tooling",
  ]
---

I fill in an hours spreadsheet. One row per day, with a column called *Descrição das atividades* — what I worked on. I fill it in at the end of the week from memory, which means it is reconstructed rather than recorded, and the reconstruction gets thinner the further back it goes.

Everything needed to write that column accurately already exists somewhere: cards I moved on the board, runs my workflow recorded, commits I authored. So I automated it. The interesting part wasn't the plumbing — it was discovering how much of the raw signal is noise.

## Three sources

**The board.** Work items whose last change that day was made by me:

```sql
SELECT [System.Id] FROM WorkItems
WHERE [System.ChangedBy] = 'me@example.com'
  AND [System.ChangedDate] >= '2026-08-04' AND [System.ChangedDate] < '2026-08-05'
```

`ChangedBy` rather than `AssignedTo` on purpose: a card assigned to me that someone else moved is not my working day, and a card I commented on is.

**My workflow's run records.** Each spec-driven run writes a small JSON manifest — project, feature, stage, and a link to the board Story if one exists. These cover work that never got a card.

**Git.** Commit subjects I authored that day, across the repositories I work in.

Then merge, deduplicate, and append to the day's cell — only what isn't already mentioned, so anything I typed by hand survives and re-running is a no-op.

## 58 activities for a single Tuesday

That was the first real run. The cell would have been over 2,000 characters. Four separate causes, each needing its own fix.

**Container work items.** Creating a child bumps the parent's modified date and sets its `ChangedBy` to whoever created the child. I'd created Story and Task cards that morning, so an Epic and four Features I had never opened appeared as work I'd personally done. Excluded by type: `NOT IN ('Feature','Epic','Iniciativa')`.

**Task-level granularity.** A day of spec-driven work touches a dozen task cards under one Story. Listing all of them describes the mechanics of my day, not its substance. `Task` is excluded too, behind a flag for the days you want the detail.

**Commits already covered by the run records.** My commit convention is `T-019 <feature>: <title>`, so those commits describe exactly the work the run source already reports at Story level. Dropping them leaves git covering what it's actually useful for — the ad-hoc work no spec run wrapped.

**Refs that aren't work.** `git log --all` includes `refs/stash`, so `WIP on main: 0685722` showed up as an activity. `--branches --remotes` instead, plus `--no-merges`, because `Merge pull request #48 from …` is process rather than work.

After all four: **seven activities, 393 characters.** Same day, same sources.

## The duplicate that got through

Squash merges produce the same work twice, and my substring deduplication couldn't see it:

```
correct the idempotency key to (store, store_pur… (#8245)
correct the idempotency key to (store, store_purchase_id)
```

One from the branch commit, one from the PR — where GitHub had clipped the subject with an ellipsis and appended the PR number. Neither string contains the other, so neither looked like a duplicate.

Stripping the two things GitHub adds makes the clipped copy a prefix of the full one:

```python
s = re.sub(r'\s*\(#\d+\)\s*$', '', s)
s = re.sub(r'\s*[…]+\s*$', '', s)
```

Then a second fix, because getting the dedupe *direction* right matters: my original logic kept whichever variant arrived first, which meant the truncated PR subject could win over the complete one. It now replaces a shorter entry when a longer superstring arrives, so the fullest phrasing survives regardless of source order.

## The bug that credited work to the wrong day

Run records were matched on `updatedAt`. That's the field that changes on any write — including administrative ones.

Late in the day I added a field to six existing manifests. Two of those runs had happened four days earlier. Their `updatedAt` became today, so both were credited to today and vanished from the day they were actually done.

`createdAt` is the honest field. A run's work happens on the day it starts; a later edit to its record isn't a working day. Obvious in hindsight, and it only surfaced because I happened to backfill several days at once and could see history shift.

## Prefer short, reviewer-facing titles

Each source describes the same work at a different length. A board Story title is short and already written for a human — *"Phase 1 — shadow reads"*. A run manifest description is a full sentence written for me:

> Phase 1 shadow reads — catalog, entitlement and purchasing-user read paths plus the parity harness that proves the Go rewrite against r10-hub

170 characters, in a cell holding eight activities. So the run source prefers the linked Story's title where one exists, falls back to the description, and anything over 90 characters is cut on a word boundary. Board titles are already shorter than that, so the cap only bites the verbose source.

## Don't invent activity on empty days

One detail that seems trivial and isn't. Nearly every row in my sheet starts with *dailys* — the standup — so I had the tool prepend it automatically.

Which meant a Sunday with no work got a row saying `dailys`. The sheet was now claiming a standup happened on a day I didn't work. The prefix now only rides along with real activity; a day with no sources leaves the row empty, the way it should be.

The general version: a constant you add unconditionally is an assertion. If the rest of the row is empty, that assertion is the only content, and it's false.

## What I'd tell someone doing this

**Automating the doing is easy; automating the reporting is where you learn what your data contains.** Every filter above came from looking at real output and asking why something was in it. None of them were predictable from the schema.

**Every filter needs a reason, not a threshold.** "Take the top 10" would have produced a plausible cell and thrown away the wrong things silently. "Exclude containers because creating a child bumps the parent" is a rule I can still justify in six months.

**Append, never replace.** The cell contains things no system knows about — `folga remunerada 4/10`, `R10 Planning`. A tool that rewrites the cell would delete them. Appending only what's missing makes the tool safe to run repeatedly and safe to run over a row I've already edited.

**Dry run by default.** Every write goes out only with `--apply`. I ran the thing dozens of times against real days while tuning the filters, and not one of those runs touched the spreadsheet.
