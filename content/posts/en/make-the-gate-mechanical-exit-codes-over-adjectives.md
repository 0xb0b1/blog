---
title: "Make the Gate Mechanical: Exit Codes Over Adjectives"
date: "2026-07-29"
description: "If the definition of done is a sentence, it will be negotiated. On making acceptance criteria executable, letting a process exit code be the verdict, refusing to record a lesson without evidence behind it — and the gap I found between ten criteria that had tests and zero that had passed."
tags:
  [
    "ai-agents",
    "testing",
    "ci",
    "workflow",
    "architecture",
  ]
---

Every review process I've worked in has the same weak joint: the moment where someone declares the work finished. It's usually a sentence. "Tests pass." "This is ready." "Looks good to me."

Sentences are negotiable. Under deadline pressure, "tests pass" quietly becomes "the tests that matter pass," which becomes "the failing one was already flaky." Nobody lies; the phrase just has enough give in it to absorb the situation.

I've been running a workflow where that joint is a process exit code instead, and the difference is larger than I expected.

## Criteria that are tests, not prose

The workflow starts with a spec containing user stories and acceptance criteria, each with an id — `US-011`, `AC-022`. Ordinary enough. The part that does the work is the next step: every acceptance criterion must have a test annotated with its id.

```go
// @spec:AC-023
func TestConfigValidateNamesTheOffendingVariable(t *testing.T) {
```

That annotation is the whole trick. It means the question "is `AC-023` satisfied?" has a mechanical answer — find the test carrying that id, look at whether it passed. There's no interpretive step where someone decides whether the criterion is *basically* covered by an adjacent test.

It also makes the inverse question answerable, which turns out to matter more. "Which criteria have no test?" is a query, not a judgement call. Anything unannotated is visibly unproven rather than tacitly assumed.

Two rules keep it honest, and they're the sort of thing that has to be stated as inviolable or it erodes:

- A skipped test is not proof. It's an unproven criterion that happens to be documented.
- You never weaken, skip, or delete a test to get through the gate. If the gate is failing, the code is what's wrong.

That second one sounds obvious written down. It is exactly what gets violated at 6pm.

## The verdict is an exit code

The audit runs as a command:

```bash
onp-spec audit --ci
```

Exit 0 or the feature isn't ready. Not "the audit reports the feature is in good shape" — a number the shell hands back. The workflow is built so that no participant, human or otherwise, has the vocabulary to argue with it. There is no field anywhere that holds a prose verdict, because a field like that would eventually get filled with something optimistic.

When the gate fails, the workflow gets three attempts to fix the cause, each recorded. Still failing after three and it *parks*: the run stops, the findings are presented ranked, and it waits for a human. That bound matters as much as the gate. Without it, "fix and retry" becomes an unbounded loop that eventually finds a way to make the check pass without making the software correct — which is a strictly worse outcome than stopping.

## The gap between written and verified

Here's the finding that convinced me this was worth the machinery.

I ran the status command on a feature that had gone through the whole workflow and was sitting at `implementing`:

```
feature                     criteria  with-test  proven
phase-0-service-scaffold          10         10       0
```

Ten acceptance criteria. Ten of them had annotated tests. **Zero had passed a verification run.**

Nothing was broken. The scaffolding step had done exactly its job — it writes a failing test skeleton for every criterion up front, so the definition of done exists before the implementation does. The tests were real, correctly annotated, and red.

But read those three numbers as a sentence and you can see what they'd have become in a status meeting. "All ten criteria have tests" is true, sounds like completion, and is compatible with none of them passing. The `with-test` and `proven` columns being separate is the entire value of the tool. A single "coverage" number would have hidden it.

## Refusing evidence-free lessons

The workflow has a learning stage: after a run, it can register a lesson so future runs pick it up. This is the part I expected to be useless, and it's the part with the sharpest design decision in it.

The engine refuses to record a lesson unless there's a real signal behind it — an actual audit finding or verification failure from a recorded run. Ask it to remember something you merely believe and it fails with `LESSON_WITHOUT_EVIDENCE`.

The first time this bit me, the run had gone perfectly and I wanted to record something anyway. The engine's answer, roughly: *no signal recurred in two or more distinct features; nothing lesson-worthy for now.* And that's right. A clean run is not a missed opportunity to write down wisdom. A lessons file that accumulates plausible-sounding advice after every run becomes noise within a month, and then nobody reads the one entry that mattered.

There's a promotion rule attached: a signal has to recur across separate features before it becomes a candidate rule. One occurrence is an incident. Two is a pattern. Only patterns get promoted into the constitution — the file of principles that gets checked mechanically on every subsequent run.

## Where it doesn't reach

Two honest limits.

**A mechanical gate only covers what the criteria say.** If the spec omits a case, no exit code will notice. The gate protects you from declaring unverified work done; it doesn't protect you from an incomplete spec. The workflow's answer is to make assumptions and open questions first-class artifacts — `ASM-xxx` and `Q-xxx` codes that the audit counts and reports — so at least the omissions are visible rather than invisible.

**Recorded state can drift from real state.** This is the failure I actually hit, and it's the mirror image of everything above. A run committed seven tasks and marked none of them done in its task file. The audit had nothing to complain about, because the audit checks criteria against tests, not commits against records. The dashboard read the task file and reported no progress, truthfully.

The fix was another mechanical check, deliberately narrow: a command that compares what's recorded as done against what's actually committed, in both directions, and exits non-zero on disagreement. It runs before the workflow leaves the execute stage.

That's the general shape of the lesson. A mechanical gate is only as good as its coverage of the ways things can be wrong, and every gate you add reveals a new seam beside it. But each of those seams gets fixed once and stays fixed, which is not true of "we should be more careful about updating the task file."

## What I'd take from this

If you have a review step that depends on someone asserting readiness, the highest-leverage change is making the assertion into an executable question — even crudely. An exit code beats a checklist, a checklist beats a norm, and a norm beats nothing.

And keep the two numbers separate. "Has a test" and "the test passed" are different facts, and the moment you collapse them into one figure, you've built the thing that lets unverified work look finished.
