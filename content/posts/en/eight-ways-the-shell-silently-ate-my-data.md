---
title: "Eight Ways the Shell Silently Ate My Data"
date: "2026-08-05"
description: "Every one of these produced no error, no warning, and a plausible-looking wrong result. Collected from a single day of writing bash, jq and git glue: heredocs that steal stdin, tab as IFS whitespace, NUL bytes vanishing in command substitution, jq's dot rebinding after a pipe, and four more."
tags:
  [
    "bash",
    "shell",
    "jq",
    "zsh",
    "debugging",
    "tooling",
  ]
---

I spent a day writing glue — bash calling `curl`, piping into `jq`, reading `git log`, shelling out to Python for the parsing bash shouldn't do. Ordinary work. By the end I had eight distinct bugs, and what they had in common is the thing worth writing down: **not one of them produced an error.**

No non-zero exit, no warning on stderr, no crash. Each one produced a result that looked entirely reasonable and was wrong. That is the specific danger of shell glue: the failure mode is quiet, and the output is plausible enough to ship.

Here they are, with the symptom first, because that's how you'll meet them.

## 1. A heredoc steals stdin from the program it feeds

**Symptom.** A pipeline produces nothing. No error. The Python script at the end appears to run and outputs an empty result.

```bash
collect_activities | python3 - <<'PY'
import sys
for line in sys.stdin:      # never sees a single line
    print(line.strip())
PY
```

**Cause.** `python3 -` means *read the program from stdin*. The heredoc is attached to stdin, so Python reads its own source from there and reaches EOF. The pipe on the left is discarded entirely.

**Fix.** Put the program on another descriptor and leave stdin for the data:

```bash
collect_activities | python3 /dev/fd/3 3<<'PY'
```

This one cost me twice, because I hit it again in an ad-hoc verification command an hour after fixing it in the script.

## 2. Tab is IFS *whitespace*, so leading empty fields collapse

**Symptom.** Every field shifts one position left, but only for records whose first field is empty. A run with no linked ticket reported its internal slug where a human-readable description belonged.

```bash
# line is: "\tSome description\tfeature-slug"
while IFS=$'\t' read -r id desc slug; do
  # id="Some description", desc="feature-slug", slug=""
```

**Cause.** Space, tab and newline are *IFS whitespace*, which bash treats specially: leading and trailing runs are stripped and consecutive occurrences collapse into one. Setting `IFS=$'\t'` does not opt out of that behaviour, because tab is still whitespace.

**Fix.** Use a delimiter that isn't whitespace. The unit separator is what it's for:

```bash
while IFS=$'\x1f' read -r id desc slug; do
```

## 3. Command substitution strips NUL bytes

**Symptom.** A warning you'd be forgiven for ignoring — `command substitution: ignored null byte in input` — and a variable that's missing its delimiter.

I'd used `\0` to separate two values in one captured string, on the reasoning that no real content would contain it. Correct, and useless: `$( )` discards NUL bytes, so the delimiter was the one thing guaranteed not to survive.

**Fix.** Don't invent delimiters for structured data. Emit JSON and let `jq` read it back:

```bash
merged="$(build | python3 …)"     # prints {"value": "...", "added": [...]}
value="$(jq -r .value <<<"$merged")"
```

That also fixed the second half of the bug, which was `grep '^\x00'` — not a valid basic regex, and `grep` says so with a mild warning about a "stray \ before x" rather than failing.

## 4. jq's `-e` reflects the value, not validity

**Symptom.** A helper rejected `null` as invalid JSON. `null` is valid JSON, and in my case it was the meaningful value — clearing a field.

```bash
echo "$val" | jq -e . >/dev/null || die "invalid JSON: $val"
```

**Cause.** `-e` sets the exit status from the *output*: `null` and `false` give exit 1. It is not a syntax check.

**Fix.** `jq empty` validates syntax and outputs nothing:

```bash
echo "$val" | jq empty >/dev/null 2>&1 || die "invalid JSON: $val"
```

## 5. In jq, `.` rebinds after a pipe

**Symptom.** `jq: error: Cannot index array with string ("stage")`, from an expression that reads perfectly well.

```jq
sort_by([ (["done","failed"] | index(.stage)) != null, .updatedAt ])
```

**Cause.** Inside `["done","failed"] | index(.stage)`, the dot has already rebound to that literal array. `.stage` is asking an array for a string key.

**Fix.** Bind before the pipe:

```jq
sort_by([ ((.stage // "") as $s | (["done","failed"] | index($s))) != null, .updatedAt ])
```

What made this one expensive was that the surrounding shell had `2>/dev/null || true` on the call, so the error never surfaced. The function just returned empty and everything downstream treated that as "no match found." An identical expression elsewhere in the same codebase was correct, which is why I hadn't suspected the pattern.

## 6. `@csv` quotes strings, and APIs reject the quotes

**Symptom.** HTTP 400 from a request whose ids parameter looked fine.

```jq
[.relations[] | (.url | split("/") | last)] | @csv    # → "6764","6765"
```

**Cause.** `@csv` quotes string values, correctly, because that's what CSV requires. The API wanted `6764,6765`. An almost identical call elsewhere worked, because there the ids came out of the JSON as *numbers*, and `@csv` doesn't quote numbers.

**Fix.** `join(",")` when you want a bare list — or convert to numbers first if you specifically want `@csv`'s escaping.

## 7. zsh reads `:x` after a bare `$var` as a modifier

**Symptom.** Three identical `curl` calls in a loop return 404. The same URL, pasted by hand, works.

```bash
for r in 3 5 6; do
  curl ... "https://…/values/Sheet%21L$r:clear"
done
```

**Cause.** zsh supports history-style modifiers on parameter expansion, and `$r:clear` gets parsed as `$r` followed by a modifier rather than as `$r` followed by a literal `:clear`.

**Fix.** Brace the expansion — `${r}:clear`. Worth knowing if you write scripts under `#!/usr/bin/env bash` but paste them into an interactive zsh, which is exactly the mismatch I was in.

I want to note the diagnostic error I made here, since it's more instructive than the bug: I saw 404 and assumed the URL encoding of `!` was at fault, because that's the interesting-looking character. I spent two attempts encoding and re-encoding it. The character that was actually broken was the boring one.

## 8. `git log --all` includes refs/stash

**Symptom.** A report of "what I worked on today" containing `WIP on main: 0685722` and `index on main: 0685722`.

**Cause.** `--all` means all refs, and `refs/stash` is a ref. Every stash you've ever made is a commit with a generated subject, and it lands in the output looking like work.

**Fix.** Be explicit about what you want:

```bash
git log --branches --remotes --no-merges --author="$email" --since=…
```

`--no-merges` goes in for a related reason: `Merge pull request #48 from …` is process, not activity, and it was outnumbering the real subjects.

## The pattern

Seven of these eight were caused by a tool doing something reasonable that I hadn't asked about. `@csv` quotes strings because CSV needs quotes. `-e` reports truthiness because that's its documented job. Tab collapses because tab is whitespace. None of it is a bug in the tool.

The one lesson I'd actually generalise is about the diagnostics, not the tools. Two of these were expensive purely because an error was being swallowed — `2>/dev/null || true` on a jq call, and a `head -1` on captured output that hid everything after the first line. Both of those swallows were deliberate, and both were reasonable in isolation: I didn't want a noisy warning failing a run.

If you're going to discard a tool's stderr, discard it at the point where you've already decided the failure is survivable — and make sure something still says *that a failure happened.* An empty result and a silenced error are indistinguishable from a legitimate empty result, and you will spend an hour on the difference.
