---
title: "Your Change Detection Never Fires If the Payload Carries a Clock"
date: "2026-08-05"
description: "A guard compared whole JSON payloads to avoid needless re-renders. The payload included a generated timestamp, so it never matched once — the UI rebuilt every 2.5 seconds and threw you back to the top of any document you were reading. On volatile fields, and why gating an event on 'did it change' can lose that event permanently."
tags:
  [
    "frontend",
    "debugging",
    "javascript",
    "ui",
    "observability",
  ]
---

I have a small dashboard that polls a local endpoint every 2.5 seconds and re-renders. It had a guard against pointless work, written the obvious way:

```js
const raw = await res.text();
if (raw === lastRaw) return;      // nothing changed, skip the render
lastRaw = raw;
render(JSON.parse(raw));
```

Reasonable. Cheap. Wrong from the first commit, and silently — because the endpoint's response looks like this:

```json
{ "generatedAt": "2026-08-01T00:58:01-04:00", "active": [...], "history": [...] }
```

`generatedAt` is a fresh timestamp on every request. The comparison could never match. That guard had been in place for the life of the project and had never once returned early.

## The symptom didn't look like this at all

The bug reached me as: *"when I try to scroll on any page the page automatically sends me back to the start and I can't read to the end."*

Which is what a re-render every 2.5 seconds does to a scrolled document. The render replaced the detail pane's children, and the document pane briefly showed a `loading…` placeholder while it re-fetched — collapsing the container's height to almost nothing, which clamps the scroll offset to zero. By the time the content came back, the position was gone.

So the reported symptom was scroll behaviour; the actual bug was a change-detection predicate that always said "changed"; and the connecting mechanism was a height collapse I'd never have guessed from the description.

## Confirming it, cheaply

Three polls, hashing the raw body and then a version with the volatile fields stripped:

```
poll1  raw=79e69fea  signature=3d5491ba
poll2  raw=c550664c  signature=3d5491ba
poll3  raw=70708ad5  signature=3d5491ba
```

The raw digest differs every time; the stripped signature is identical. That's the whole bug in three lines, and it took two minutes to produce. Worth doing before touching any code — I'd have gone looking at scroll handlers otherwise.

There were three volatile fields, not one:

- `generatedAt` — a server timestamp per request
- `durationSec` — derived from `now - startedAt`, so it increments on every poll for any running item
- `session.updatedAtMs` — a heartbeat, refreshed every few seconds

Each on its own would have been enough to defeat the comparison.

## The fix, and its trap

Compare a signature that excludes fields which change without meaning:

```js
const VOLATILE = new Set(["generatedAt", "durationSec", "updatedAtMs"]);
const signature = s => JSON.stringify(s, (k, v) => (VOLATILE.has(k) ? undefined : v));
```

`JSON.stringify`'s replacer runs at every depth, so nested occurrences are handled without walking the tree yourself.

The trap is over-stripping. A guard that suppresses real changes is worse than no guard, because the UI now silently lies. So I checked it by mutating a real payload and asserting which mutations should trigger a render:

```
skip    volatile only (generatedAt + durationSec + heartbeat)
RENDER  stage advance · note added · pending decision appears
RENDER  session goes idle · session dies · task progress · new item appears
```

That's the test I'd insist on for this kind of predicate. It's easy to verify that the noisy case is suppressed and forget to verify that the meaningful cases aren't.

I also kept the "updated HH:MM:SS" label refreshing on every poll, outside the guard. It's a text node write with no layout consequence, and without it a working dashboard looks frozen.

## The second lesson, which cost more

The same pattern bit me again in a different component, and this one is the more interesting design point.

Elsewhere in the system, stage transitions get mirrored to an external tracker. The obvious implementation is to fire when the stage *changes*:

```js
if (stage_before !== stage_after) { mirror(stage_after); }
```

I wrote that, and then tested what happens when the tracker is unreachable. The write is best-effort by design — an outage must never fail a run — so it warns and continues. Which means: with the tracker down at the moment of the transition, that milestone is lost **permanently**. The stage never changes again, so the condition never holds again, and nothing ever retries.

The fix was to stop gating on change at all, and gate only on a record of what's already been sent:

```js
if (!alreadyPosted(event)) { if (mirror(event)) markPosted(event); }
```

Now every subsequent write of any kind is a retry opportunity, and the ledger prevents duplicates. Verified by taking the tracker offline across two stage writes, restoring it, and confirming an unrelated field update caught both missed milestones up.

There's a subtle version of the same mistake I made twice before getting it right: an event gated on "the ticket field just became non-empty" has exactly the same flaw as one gated on "the stage just changed." Both are edge-triggered, and an edge you miss is gone. **Level-triggered with an idempotency record survives an outage; edge-triggered doesn't.**

## What I'd take from it

**Compare meaning, not bytes.** Any payload with a server timestamp, a computed duration, or a heartbeat cannot be compared wholesale. If you have a guard like this, check that it has ever fired — mine hadn't, for the project's entire life, and nothing was going to tell me.

**Then check it doesn't over-fire.** A change detector needs tests in both directions.

**Prefer level-triggered.** "Has this been done?" survives failure. "Did this just change?" doesn't. The ledger is a few lines and it turns a lost milestone into a delayed one.

And the symptom is rarely the bug. The report was about scrolling. The cause was a string comparison against a timestamp, three components away.
