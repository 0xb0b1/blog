---
title: "Instructions Get Skipped. Commands Don't."
date: "2026-08-05"
description: "Three times in one day a step that was clearly documented in my workflow got skipped anyway. The fix was never better wording — it was folding the step into the command that already ran at that moment. On why written procedure decays, why 'same as before' is a documentation smell, and what it means to make a step unskippable."
tags:
  [
    "ai-agents",
    "workflow",
    "automation",
    "tooling",
    "developer-experience",
  ]
---

I have a workflow that takes a feature from a written spec to a set of commits, with a mechanical audit gate at the end. Most of it is a document: a long, carefully written set of instructions that an agent follows, stage by stage. It works well enough that I use it daily.

Then I spent a day watching it closely, and found the same failure three times in a row. Each time, a step was written down clearly. Each time, it was skipped anyway. And each time, the fix turned out to be the same thing — not better wording, but deleting the instruction and putting the operation inside a command that was already running.

## The failure that made it obvious

The workflow's rule for executing tasks reads, in part: *make the criterion tests pass, `onp-spec task <feature> T-xxx done`, then commit.* Two operations, both required, in a specific order.

A run went through eleven tasks. Seven of them had commits on the spec branch, correctly formatted, each closing a real task. Zero of them were marked done in `tasks.md`.

The dashboard, which reads `tasks.md`, said **0 of 9 done**. It was telling the truth. The work had landed; the record of it hadn't.

What makes this worth writing about is that nothing failed. `git commit` succeeded whether or not the engine call had happened. There was no error, no warning, no non-zero exit anywhere. The instruction to do both was right there in the document, and the only thing enforcing it was the reader's diligence over an hour-long run.

## The same shape, twice more

**Run registration.** The workflow's first action is supposed to be writing a run manifest, which is what the dashboard reads to know a run exists. One run did roughly sixty tool calls of repository and cloud discovery before registering anything. For twenty-five minutes the dashboard showed nothing at all, and I assumed it was broken. It wasn't — there was genuinely no run on disk to display.

**Clearing a mid-run question.** When the workflow stops to ask something, it writes the question into the manifest so the UI can render it as a decision card, and clears it once answered. A run parked on a question, got answered in the terminal, carried on working — and left the question on screen. The card sat there for six minutes advertising a decision that had already been made, because clearing it was step three of a three-step procedure and the run had moved on after step two.

Three different steps, three different parts of the document, one shape: **an operation that had to be remembered separately from the thing it accompanied.**

## Why the document loses

The obvious reading is that the reader was careless. I don't think that's the useful reading, because I recognise this failure from teams of humans.

A written procedure competes for attention with the work. When the work is absorbing — you are eleven tasks into a refactor, tests are red, you are holding four files in your head — the bookkeeping step is exactly what falls out. This is why deploy checklists get automated, why `git commit -n` exists and is regretted, why every incident review that ends in "we should document this better" produces the same incident eighteen months later.

The step didn't fail because it was badly described. It failed because it was *separately described.*

## Fold it into the thing that already runs

The fix in each case was to find the command that was already being invoked at that exact moment, and make the forgotten operation part of it.

For task completion, that command is the commit. So the two became one:

```bash
task-commit.sh done <feature> T-019
```

It marks the task done in `tasks.md` and commits, as a single operation. Two details matter more than the merge itself.

**The commit subject is derived from the task's own title**, read out of `tasks.md`. Not passed in as an argument. A commit therefore cannot describe something other than the task it closes, because there is no way to tell it to.

**If the commit fails, the status goes back.** The whole reason this helper exists is to prevent a `[done]` marker with no commit behind it, so a failed commit that left the marker set would recreate the original bug in mirror image. I tested it by installing a pre-commit hook that exits 1: the status was restored to `[pending]`, and the operation reported the failure rather than half-succeeding.

The same move worked for the other two. Manifest writes go through one helper, and everything that used to be a separate follow-up step — clearing a pending question, posting progress to the tracker, transitioning a card — now rides on the manifest write that was happening anyway. There is no longer a version of "advance the stage" that doesn't also do the rest.

## Make drift detectable, not just unlikely

Merging the operations stops new drift. It does nothing about drift that already exists, and it can't help when someone bypasses the helper.

So the same tool grew a second job:

```bash
task-commit.sh check <feature>     # exit 1 if tasks.md and the branch disagree
```

It compares what's marked done against what's actually committed, in both directions, and exits non-zero on any disagreement. The workflow now runs it before leaving the execute stage. A discrepancy stops being something you notice weeks later in a dashboard and becomes a failing check.

There's a third subcommand, `reconcile`, which repairs the common direction: for every task with a commit and no `[done]` marker, mark it. I want to flag one thing I got wrong while writing it, because the mistake is instructive.

`reconcile --apply` originally exited 0 after doing its work. But it can only fix one direction — it will happily add a `[done]` where a commit proves it, and it refuses to invent a commit for a task marked done with nothing behind it. So on a repository with that second kind of drift, it would report success while the disagreement remained. A green exit code on unresolved drift makes the whole check useless as a gate. It now tracks unfixable drift separately and exits 1 with an explanation.

## "Same as before" is a documentation smell

There's a related failure worth naming, because it's the mechanism by which good procedure quietly becomes no procedure.

When I looked at why the manifest step was being skipped, the document said this:

> Same schema/mechanics as before (runId `<UTCstamp>-<project>`, project, projectName, feature, sessionId, tasksDir, mode, kind, ticket, description, stage, note, createdAt/updatedAt).

Field names, and nothing else. No template, no example, no command — despite the section heading promising a specific atomic-write technique. Four other steps in the same document said "unchanged from before."

The fuller version those lines referred to existed nowhere. Not in the repository, not in git history, not in a backup. Someone — me — had condensed the document at some point and left pointers to a thing that no longer existed.

That is worse than an incomplete document, because it reads as complete. Every "as before" is a claim that the detail is available somewhere. When it isn't, the reader improvises, and improvisation is where the steps go missing. If you are tightening a runbook, the sentence "unchanged from before" is a good place to look for the next incident.

## What generalises

Anything that must happen alongside another thing should not be a separate instruction. Find the command that runs at that moment and put it inside.

If it genuinely can't be merged, make the mismatch mechanically detectable and check it at a boundary. Prose is a fine way to explain *why* a step exists. It is a bad mechanism for ensuring the step happens.

The distinction I keep coming back to: the helper scripts are reliable because they *are* the operation. "Remember to also post a comment at the audit gate" is still just prose, and prose gets skipped under load — by agents, and by me at the end of a long afternoon.
