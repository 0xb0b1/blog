---
title: "A Passing Health Check Doesn't Mean It's Your Build"
date: "2026-08-05"
description: "The rebuild succeeded, the restart silently didn't, and the health endpoint kept answering — from the old binary. I reported a change as live when it wasn't. On verifying that the process you started is the one serving, and why the lsof invocation everyone copy-pastes is wrong in two independent ways."
tags:
  [
    "debugging",
    "ops",
    "unix",
    "go",
    "tooling",
  ]
---

I have a small Go server that reads local state and renders it in a browser. Nothing exotic: `go build`, run it with `nohup`, hit `/healthz` to confirm it's up.

I changed some CSS, rebuilt, restarted, probed the health endpoint, got `claude-dashboard` back, and told the person I was working with that the change was live.

It wasn't. The old binary was still serving, and had been the whole time.

## What actually happened

My restart looked like this:

```bash
OLD=$(lsof -ti :4747)
[ -n "$OLD" ] && kill $OLD
nohup ./dashboard -port 4747 >> dashboard.log 2>&1 &
```

The `kill` failed. The new process failed to bind. The old one kept running. And `/healthz` answered `claude-dashboard` throughout, because *something* was listening — just not the thing I'd built.

Every individual step reported plausibly. `go build` succeeded. The `nohup` line returned. The health check passed. The only sign was in a log file I hadn't tailed:

```
listen tcp 127.0.0.1:4747: bind: address already in use
```

## The tell

The clearest evidence, once I thought to look:

```bash
$ ls -l /proc/3813939/exe
/home/vicente/.claude/dashboard/dashboard (deleted)
```

`(deleted)` means the running process's executable no longer exists at that path — I'd replaced it with `go build`. On Linux that's the definitive signal that a process has outlived its binary. If you're chasing "my change isn't showing up," check this before you check anything else.

## Why the kill failed

This is the part worth the post. My `lsof -ti :4747` returned three PIDs, and `kill` rejected the joined string with `illegal pid`. The command is wrong in two independent ways.

**It matches clients, not just the server.** `lsof -i :4747` selects any process with a socket involving that port — including the browser tab I had open on the dashboard. My own client connection was in the kill list.

**lsof ORs its selectors.** This is the one that could have caused real damage. I "fixed" the first problem by adding a state filter:

```bash
lsof -ti -sTCP:LISTEN -iTCP:4747
```

and got two PIDs back, one of which was a **Discord renderer process**. lsof combines multiple selection criteria with OR unless you pass `-a`. So that command means *anything in LISTEN state* **or** *anything on port 4747* — a query that matches most of your desktop.

Had I not looked at what those PIDs were, `kill $(lsof -ti -sTCP:LISTEN -iTCP:$PORT)` would have killed Discord. And that exact shape — `kill $(lsof -ti :$PORT)` — was sitting in my own runbook as the documented way to stop the server.

The correct form ANDs the selectors:

```bash
lsof -ti -a -sTCP:LISTEN -iTCP:4747
```

Now `-sTCP:LISTEN` drops the browser's client connections, and `-a` means both conditions must hold. One PID, the right one.

## Verify the process you started is the one serving

The structural fix isn't a better `kill`. It's not trusting the health check to answer the question you actually have.

`/healthz` answers *is something listening on this port and healthy?* The question I had was *is my build serving this port?* Those are different, and the gap between them is exactly where a stale process hides. So the restart asserts the identity of the listener:

```bash
nohup ./dashboard -port "$PORT" >> dashboard.log 2>&1 &
NEW=$!
for _ in 1 2 3 4 5 6 7 8; do
  sleep 0.3
  if healthy && [ "$(listener)" = "$NEW" ]; then
    echo "http://localhost:$PORT"; exit 0
  fi
done
echo "did not come up — last log lines:" >&2
tail -5 dashboard.log >&2
exit 1
```

Two things matter here beyond the PID comparison. The failure path prints the log tail, because the actual reason was in a file the whole time and I hadn't looked. And it exits non-zero, so a caller can react rather than assuming success.

## The rebuild-detection trap next door

The same server embeds its HTML with `go:embed`. That means a CSS-only edit still requires a rebuild — the file on disk is irrelevant to the running process.

My start script did check for that:

```bash
if [ ! -x dashboard ] || [ main.go -nt dashboard ] || [ index.html -nt dashboard ]; then
  go build -o dashboard .
fi
```

What it didn't do is connect the rebuild to the restart. Its logic was: probe health, and if something's serving, print the URL and stop. So the sequence "sources changed → rebuild → something is already serving → report success" left the new binary sitting on disk unused. The stale-UI problem wasn't a missing rebuild; it was a rebuild with no consequence.

The script now records whether it actually rebuilt, and treats *a current build serving* as the success condition rather than *anything serving*:

- healthy and nothing was rebuilt → done, print the URL
- healthy but we rebuilt → stop the listener, start the new one, assert the PID
- nothing listening → start, assert the PID

Cold start to serving is about 400ms, so there's no reason not to run it on every entry point that needs the server.

## What I'd generalise

**A liveness check answers a narrower question than you want.** It tells you something responds. Version, build identity, and "is this the artifact I just produced" are separate questions, and if they matter, ask them separately — an endpoint that reports a build stamp is a five-minute change that would have saved me this entirely.

**A tool that selects things needs its combination semantics checked.** I'd used `lsof -ti :PORT` for years without reading how multiple selectors combine. The answer was OR, which is a reasonable default for a diagnostic tool and a dangerous one for the input to `kill`.

**When several steps each report plausibly and the outcome is wrong, suspect the seam.** Build, start, and health check were all individually honest. What nobody checked was whether the thing that started was the thing being checked.
