---
title: "Azure DevOps Boards with Nothing but curl and jq"
date: "2026-08-04"
description: "A complete Boards client — fetch, create, transition, link, reparent, WIQL — in one bash script with no SDK. Most of the post is the four failures that shaped it: a permissions problem that arrives as HTTP 500, parents that mark themselves as your work, a flag that silently orphans cards, and two tokens of identical length where one was revoked."
tags:
  [
    "azure-devops",
    "bash",
    "api",
    "automation",
    "tooling",
  ]
---

I needed a workflow to read a work item, create Stories and Tasks, move cards along the board, and attach PR links. The obvious paths are the `az` CLI or a language SDK. I wanted neither: the caller is a bash script, it has to work headless, and adding a Python dependency to a shell toolchain to make four HTTP calls is a bad trade.

The REST API is fine to use directly. `curl` for transport, `jq` for both parsing and request construction, roughly 200 lines. What follows is mostly the four things that went wrong, because they're the parts you can't read off the docs.

## The shape

One script, one subcommand per operation:

```bash
azure-workitem.sh fetch 6758                    # compact JSON: type, state, title, AC, repro
azure-workitem.sh create "User Story" "title" --parent 6760 --description "<p>…</p>"
azure-workitem.sh transition 6758 "Code Review"
azure-workitem.sh link 6758 "<pr-url>" "PR #42"
azure-workitem.sh parent 6793 6792
azure-workitem.sh wiql 'SELECT [System.Id] FROM WorkItems WHERE …'
```

Two structural decisions paid for themselves. **Every write goes through one function**, so `--dry-run` is enforced in one place rather than per subcommand. And **request bodies are built with `jq`, never string interpolation** — the JSON Patch format Azure wants is fiddly, and titles contain quotes and em dashes:

```bash
body="$(jq -n --arg title "$title" --arg assign "$assign" '
  [ {op:"add", path:"/fields/System.Title", value:$title} ]
  + (if $assign != "" then [{op:"add", path:"/fields/System.AssignedTo", value:$assign}] else [] end)')"
```

Project names contain spaces, so encode them rather than hand-escaping: `jq -rn --arg s "$PROJECT" '$s|@uri'`. That lets the config file hold `R10 Score Development` as written.

## Failure 1: a permissions problem arrives as HTTP 500

My first `create` returned this, with no body:

```
curl: (22) The requested URL returned error: 500
```

A 500 sends you looking for a malformed request. I spent a while checking the patch document. The actual message was there once I dropped `curl -f`, which suppresses the response body on error status:

```
VS403410: You don't have suppress notifications permission.
```

I had been appending `suppressNotifications=true` to every write, on the reasonable theory that a script mirroring progress shouldn't email the team on every transition. That parameter requires a **collection-level** permission my account doesn't have — and Azure's response to using it without permission is not a 403 on the parameter. It fails the entire write, as a 500.

Two things worth taking from this. **Drop `-f` while developing**, or you throw away the only useful part of an error response. And be suspicious of a 500 on a request you've never successfully made: it can be a permissions problem wearing a server-error costume.

The parameter is gone now. Notifications follow the project's normal rules, which is the cost of not being a collection admin.

## Failure 2: parents mark themselves as your work

I built a query for "work items I touched today" to feed a timesheet:

```sql
SELECT [System.Id] FROM WorkItems
WHERE [System.ChangedBy] = 'me@example.com'
  AND [System.ChangedDate] >= '2026-08-04'
```

`ChangedBy` rather than `AssignedTo`, deliberately — a card someone else moves isn't my working day.

It returned eleven items. Four were Features and one was an Epic that I had never opened. They were there because **creating a child bumps the parent's `ChangedDate`**, and the parent's `ChangedBy` becomes whoever created the child. I'd created Story and Task cards under them that morning, so every container above appeared as work I'd personally done.

The query now excludes container types outright:

```sql
AND [System.WorkItemType] NOT IN ('Task','Feature','Epic','Iniciativa')
```

`Task` is excluded for a different reason — a day of spec-driven work touches a dozen of them under one Story, which is too granular for a timesheet a manager reads. That took the day from 26 activities to 7.

## Failure 3: an empty flag value that silently orphans

I created two work items with `--parent "$FEAT"`, and both came back looking perfect: right type, right assignee, right tags, right state. Both were orphans.

`$FEAT` was empty — set in a previous shell invocation, and shell state doesn't persist between them. So the call was `--parent ""`, and my argument handling did this:

```bash
--parent) parent="${2:-}"; shift 2 ;;
```

Empty is a valid value, so no error. And downstream, the parent relation was only appended when the value was non-empty — a `// empty`-style guard that turned "you gave me nothing" into "you didn't ask for a parent."

The create response gave no hint, because from the API's perspective nothing was wrong. I only found it by listing the parent's children and getting zero.

```bash
--parent) parent="${2:-}"; [ -n "$parent" ] || die "--parent given an empty value"; shift 2 ;;
```

The general rule I'd write on the wall: **an optional flag that was explicitly passed with an empty value is a bug, not an omission.** Distinguish "absent" from "present but empty" whenever the difference is silent.

I also added a `parent <id> <parentId>` subcommand, since repairing the two orphans otherwise meant hand-rolling a `/relations/-` patch.

## Failure 4: two tokens, same length, one revoked

Credentials came from an environment variable first, then the OS secret store. Sensible order — env for CI, keyring for interactive use.

Every call started returning 401. The token in the environment and the token in the keyring were both 84 characters. Only one worked:

```
keyring  len=84  http=200  ✓
env      len=84  http=401  ✗
values DIFFER
```

A stale token in a shell profile had outlived the good one in the keyring. Because env was checked first, the dead one shadowed the working one, and the failure looked exactly like a permissions problem.

The order is now secret store first, environment as a fallback:

```bash
PAT="$(secret-tool lookup service azure-devops-pat 2>/dev/null || true)"
PAT="${PAT:-${AZDO_PAT:-${AZURE_DEVOPS_EXT_PAT:-}}}"
```

Headless runs have no keyring, so env still wins where it's needed. And a stale profile entry can no longer shadow the credential you actively maintain.

## Small things that helped

**Validate state names against the type before patching.** Our board has 19 states for a User Story and a different set for a Task. An invalid `System.State` returns a 400 that reads like an auth failure, so the script fetches the type's allowed states first and, on a miss, prints them:

```
azure-workitem: 'Nonexistent State' is not a state of User Story. Valid:
  New
  Business Refinement
  …
```

**Strip HTML on read.** `System.Description` and `AcceptanceCriteria` are rich text. A jq filter that removes tags and decodes the common entities makes them usable as spec input.

**`--dry-run` on every write, funnelled through one function.** This is the one I'd insist on, and it's also where I got burned: a later addition called the API client directly instead of through that funnel, which meant the dry-run flag didn't apply to it. It created three real cards on a shared board during a test. A safety flag enforced per-call-site is a convention, not a guarantee — there should be exactly one place a request can leave from.
