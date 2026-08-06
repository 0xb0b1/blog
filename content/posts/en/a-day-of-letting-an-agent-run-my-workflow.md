---
title: "A Day of Letting an Agent Run My Workflow"
date: "2026-08-05"
description: "Specs gated on a mechanical audit, progress mirrored onto a team board, a timesheet filled from what actually happened. An honest account of a day spent building and debugging that setup — including the three work items I created on a live board by accident, and the one failure that showed up in every single component."
tags:
  [
    "ai-agents",
    "workflow",
    "developer-experience",
    "automation",
    "retrospective",
  ]
---

For a while now my non-trivial features have gone through a spec-driven workflow: write the spec, derive the tasks, approve once, then execute task by task with a mechanical audit at the end. An agent drives it; I approve at one checkpoint and review at the end.

I spent a day working on the machinery itself rather than through it — wiring it to our Azure DevOps board, making it fill in my hours spreadsheet, and fixing the things that broke along the way. This is what I learned, including the parts that don't flatter me.

## The setup, briefly

Three pieces, each doing one job.

**A spec engine.** Features live in `.spec/features/<name>/`: a spec with user stories and acceptance criteria, a task list, a journal. Every acceptance criterion becomes a test annotated with its criterion id. The audit is a command that exits 0 or doesn't — the verdict is a process exit code, not a sentence a model wrote.

**A run manifest.** Each run writes a small JSON file: project, feature, stage, the linked ticket, the branch. This is the seam everything else hangs off.

**A local dashboard.** A Go server that reads the manifests and the spec files, so I can see where a run is without reading a transcript.

## One failure, five times

Here is the thing I'd tell you if you only read one paragraph. Every significant bug I found that day was the same bug wearing different clothes: **the work happened and the record of it didn't.**

- Eleven tasks executed, seven committed, **zero** marked done in `tasks.md`. The dashboard read `tasks.md` and reported 0 of 9. It was right.
- A run did sixty tool calls of discovery before registering its manifest. For twenty-five minutes the dashboard showed nothing and I assumed the dashboard was broken.
- A run answered a mid-flight question in the terminal and left the question rendered on screen for six minutes, because clearing it was a separate step.
- A run passed its audit and reached the final stage while its manifest still said `execute`, so the UI showed a stale stage next to live, correct task counts.
- Board cards were created for a finished run and every one of them was born in `To do`, because the thing that closes a card runs per-task during execution, and execution was already over.

In each case the instruction to do the bookkeeping existed and was clear. What didn't exist was any mechanism making it happen. The fix, five times running, was to stop describing the step and fold it into the command that was already being invoked at that moment — the commit, the manifest write, the stage transition.

There's a corollary I only appreciated at the end of the day. Because those operations live in helper scripts read from disk on every call, a session that had loaded the *old* instructions still got the *new* behaviour. Late in the day a run that started before the fix reached its final stage and correctly created its board Story and six task cards, complete with descriptions. Its instructions were stale; the mechanism wasn't. You cannot retrofit prose into a running session, but you can retrofit a script.

## The dashboard was telling the truth

Twice I went looking for a bug in the dashboard and found the bug in what it was being fed.

"The tasks look stale" — the tasks were stale, in `tasks.md`, because nothing had marked them. "The run looks stale" — the manifest hadn't been written for six minutes while the file-derived fields kept updating, so half the screen was live and half was frozen at a moment in the past.

Both times my instinct was that the display was wrong. Both times the display was the only thing in the system reporting accurately. That's an argument for building the boring read-only view early: it's the only component with no reason to lie to you.

The dashboard did have its own bugs, and one is worth the detour. Its change detection compared whole JSON payloads to decide whether to re-render. The payload contained `generatedAt`, a fresh timestamp on every request, so the comparison never once matched — the UI rebuilt itself every 2.5 seconds and threw me back to the top of whatever document I was reading. The guard had been there from the start and had never fired.

## Where I actually caused damage

I want to be specific about this rather than round it off.

While testing the new "create board items automatically" path, I ran it with `AZ_BOARD=dry` — a flag whose entire purpose is to print requests without sending them. It created three real work items on my team's shared board: a Story and two Tasks, both visible to colleagues, with notifications enabled because the permission needed to suppress them isn't granted to my account.

The cause was mundane. Every other write in that file went through a wrapper that honours the dry-run flag. The new creation function called the API client directly. The flag was real, the tests passed, and the code path under test was the one path that ignored it.

I deleted the three items, then fixed the function to short-circuit before any write. But the lesson isn't "add the check" — it's that a safety flag which isn't enforced at a single choke point isn't a safety flag, it's a convention. The write path now has exactly one place a request can leave from.

I also reported a font change as live when it wasn't. The rebuild had succeeded, the restart had silently failed, and the health endpoint kept answering — from the old binary. `/healthz` returning `ok` proves something is listening. It does not prove it's your build.

## What the automation is actually worth

Two integrations came out of the day, and their value is asymmetric in a way I didn't expect.

**Board mirroring** is worth it because it removes a class of nagging. Progress comments, the move to `In Progress`, the PR link, closing each task card — none of that needs a human, and all of it is the sort of thing that quietly stops happening on a busy week.

**Timesheet filling** turned out to be more interesting than useful at first. Deriving "what did I work on today" from the board, the run records and git is easy; the hard part is that the naive answer is unusable. My first run produced **58 activities for a single Tuesday**. Container work items showed up because creating a child bumps the parent's modified date. Individual task cards showed up because a day of spec-driven work touches a dozen of them. Squash-merged commits showed up twice, once clipped with an ellipsis. Getting to a usable seven entries was four rounds of filtering, each of which needed a reason rather than a guess.

The general shape: automating the *doing* is straightforward, and automating the *reporting* is where you discover what your data actually contains.

## What I'd keep

**Mechanical gates.** The audit verdict being an exit code rather than a phrase is the single most valuable property of this setup. There is no version of "the tests broadly pass" available to anyone, me included.

**A read-only view fed by the real files.** It was right every time I doubted it.

**Operations, not instructions.** If a step must happen alongside another, it belongs inside it. Documentation is for explaining why a step exists — not for ensuring it happens.

**Drift detection at the boundaries.** Merging operations prevents new drift; it does nothing about drift that already exists or that arrives when someone bypasses the tool. A `check` command that exits non-zero when the recorded state and the real state disagree turns "something we notice weeks later" into a failing build.

The thing I'd say to anyone building this kind of setup: your agent isn't unreliable in interesting ways. It's unreliable in exactly the ways a tired human is, at exactly the same steps — the bookkeeping ones, at the end of a long stretch of absorbing work. Design for that and most of the surprises go away.
