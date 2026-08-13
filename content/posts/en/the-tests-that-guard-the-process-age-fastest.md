---
title: "The Tests That Guard the Process Age Fastest"
date: "2026-08-11"
description: "We gate every phase of this extraction on a mechanical check: each acceptance criterion needs a passing test, and the verdict is an exit code. It has caught real defects. It has also produced three lessons entirely about itself — because a test whose subject is the shape of your codebase is invalidated by refactoring by design, and every false failure is an invitation to weaken it."
tags:
  [
    "testing",
    "process",
    "verification",
    "refactoring",
    "workflow",
    "subscriptions-extraction",
  ]
---

We're [extracting the subscriptions and payments half of a Django monolith into a Go service](/en/posts/quantify-the-failure-before-you-redesign-it), in phases — fourteen of them so far. Each phase gets a specification with numbered user stories and acceptance criteria, and the rule that makes it more than paperwork is this: **every acceptance criterion must have a passing test, and the verdict is a process exit code rather than anyone's opinion.** A phase either audits clean or it isn't done. There's no adjective available.

That has earned its place. It's caught criteria that had tests which had never actually passed, files that were written and never mapped to any task, and — repeatedly — the discovery that a phase's real state was worse than its author's summary.

It has also produced three lessons that are entirely about *itself*, and those are what I want to write down. The tests that guard a process are code. They have design flaws like any other code, and they age faster than the code they guard, because their subject is the shape of the codebase rather than its behaviour.

## Assertions that forbid shapes

Most criteria are proven by ordinary behavioural tests. A handful can't be. "The entry point goes through the shared router assembly" is not observable from outside the process. Neither is "the store client is constructed exactly when its credentials are configured." Those are proven by asserting on source text, and I still think that's right — an earlier phase shipped four endpoints that answered 404 in production precisely because nothing checked how the entry point was assembled.

Two of those assertions went wrong, the same way, in consecutive phases.

**The first asserted the source contains `cfg.X.Enabled() && writePool != nil`.** A later refactor hoisted the store clients out of the queue block, splitting that expression across two places. Nothing broke. Both halves of the condition survived, one level apart — the client is still built exactly when the credentials are enabled, the processor is still gated on the write pool.

**The second asserted the source contains no `DB: pool.Pool` anywhere in the entry point.** True when the processors were the only thing holding a pool. Then a phase added read-only components that correctly took the *reader* pool — doing exactly the right thing — and the blanket assertion failed a change it should have welcomed.

Both were narrowed rather than weakened: scoped to the block they were actually about, force unchanged. But the pattern is the point. **An assertion over a whole file forbids shapes; an assertion over a construct asserts a meaning.** The first has to be correct about code that doesn't exist yet, which isn't something a test can be. "This expression appears nowhere in this file" is a claim about every future line, and the future keeps adding lines that are fine.

It took three occurrences to see it, and the third convinced me it wasn't luck. Assert on the block, not the file.

The distinction generalises past this project. A behavioural test is stable under refactoring by design. A structural assertion is *invalidated* by refactoring by design. That's not a flaw in the technique — it's the technique's running cost, and if you adopt it you're signing up to re-scope those assertions periodically. What you must not do is weaken one to make a refactor pass, because at that moment you can't tell the difference between "this assertion is too broad" and "this refactor broke the property." The test for which one you're in: state the meaning the assertion was protecting. If you can't, you don't yet know whether the refactor was safe.

## A proof that goes stale if you fix a typo

The second lesson is smaller and cost more.

Verification records, per criterion, that a specific test passed against a specific state of the code. Change any input and the recorded proof no longer describes what's on disk, so the engine marks it stale. That's correct, and it's the whole value — a proof that survives arbitrary edits isn't a proof.

The practical consequence: **verify last, gate immediately, nothing in between.** And "nothing" turned out to include a one-line change to a test fixture.

I learned it twice. Once when a phase re-verified every earlier phase exactly as the process prescribes, then deleted a scaffold file and edited a task list, staling all eight proofs it had just refreshed. Once when a fixture was tidied after verifying, turning a clean gate into twelve errors.

Neither was a mistake in the code. Both were the ordinary instinct to fix the small thing you noticed while you were in there. The rule is trivial to state and I still needed it twice, which is a fair definition of a rule worth writing down: the sequence is verify, then gate, and *any* edit resets it to the start.

There's a related lesson underneath it, learned earlier and confirmed repeatedly: **re-verify the earlier phases before the gate, not after it turns red.** Work in a later phase touches shared files — the config package, the database package, both processors — and every earlier phase that asserts on those goes stale at once, for reasons unrelated to the current phase's correctness. One phase's first gate run produced eight stale proofs and two orphaned files; none of them meant anything was wrong. Two consecutive phases later passed on the first attempt by following that rule and the verify-last rule literally, which is about as clean a demonstration as a process lesson gets.

## A warning that fires on fixtures and must not be weakened

The third is a design question about a check that is correct and annoying.

One standing principle is that secrets never appear in code, enforced by a pattern matching shapes like `token = "…"`. It has now fired, three phases running, on test fixtures: a purchase token, another purchase token, a shared secret. All three obviously fake. All three matched.

The tempting fix is an exception — skip test files, or allow an annotation. I argued against it and still would.

A pattern that cannot tell a fixture from a real credential is right not to try. The alternative is a pattern with holes, and the holes are precisely where a real secret ends up: in a test file, during debugging, committed by accident. So every time, the fixture moved into a variable and the principle stayed untouched.

The cost is real friction, three phases running. What makes it worth paying is the alternative failure mode. One gate run passed with **eleven warnings** — six from this pattern on fixtures, five from a Markdown table in a specification being parsed as data rows. Eleven warnings is the number at which someone stops reading warnings. Both sources were noise I'd introduced, and both were removed rather than tolerated: the table became prose, the fixtures became variables.

**A warning people learn to wave through is worse than no warning.** A check that cries wolf six times has taught everyone to ignore it on the seventh, which will be the real one. So: clear to zero every time, and never by lowering the bar.

## The pressure all three share

Look at where each of these three ends up and there's one pattern.

Every failure here arrived as a *false* failure — a refactor that was fine, a fixture fix that was harmless, a warning on an obviously fake token. In every case the cheapest resolution available was to make the check quieter: broaden the regex, skip the file, ignore the warning class, re-run the gate after the edit without re-verifying. And in every case that would have removed the check's ability to catch the real thing.

The other thing they share is timing. Every one of these arrived when the gate was one step from green, at the end of a phase, when the work is done and the remaining obstacle is a technicality. That's the worst possible moment to be making a judgement call about rigour, and it's precisely when the machinery hands you one.

Which is the argument for having the verdict be an exit code in the first place. Not because a number is wiser than a person, but because at 11pm with one criterion outstanding, a number is much harder to negotiate with than a sentence.

## What I'd take from this

**Scope a source assertion to the construct it's about.** A file-wide pattern forbids shapes rather than asserting meanings, and it has to be right about code nobody has written yet.

**Narrow a false-failing assertion; never weaken it.** Same force, smaller scope — and if you can't state the meaning it protected, you don't know whether the refactor was safe.

**Verify last, gate immediately, and count a typo fix as an edit.** Any change resets the sequence, which is the property that makes a proof worth having.

**Re-verify earlier work before the gate, not after it turns red.** Shared files stale old proofs for reasons unrelated to the current change.

**Don't put exceptions in a secrets check.** The exception is exactly where a real credential ends up. Move the fixture instead.

**Take warnings to zero without lowering the bar.** Eleven warnings is the point at which the twelfth is invisible.

**Expect structural assertions to fail on good changes, and budget for re-scoping them.** That's the technique's cost, not a defect in it.

**Assume every one of these will surface when you're one step from done.** That's the case the mechanical verdict exists for.

---

*Part of [The Subscriptions Extraction](/en/posts/the-subscriptions-extraction-a-reading-order), seventeen posts on pulling the subscriptions half of a Django monolith into a Go service.*
