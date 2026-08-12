---
title: "The Repository Is Not the Running System"
date: "2026-08-09"
description: "Three times in one project, code that was correct, reviewed, merged and deployed did nothing at all — because the value it needed never crossed one of the nine boundaries between where it's declared and where it's read. On a symptom four independent bugs produced, a permission error that arrived disguised as absence, and four endpoints that passed every test and answered 404."
tags:
  [
    "operations",
    "deployment",
    "configuration",
    "testing",
    "aws",
  ]
---

Context first. We're extracting the subscriptions and payments half of a Django monolith into a Go service — App Store and Google Play webhooks, the purchase endpoints the mobile app calls, the logic that decides who gets the paid tier. The new service runs on ECS Fargate. Its configuration is environment variables only, validated at startup, sourced from Secrets Manager and injected by a GitHub Actions deploy that writes an ECS task definition. Infrastructure is Terraform. There are two AWS accounts with deliberately different topologies.

That's an ordinary setup, and I want to count something about it. For a database credential to reach the code that opens a connection, it crosses: Terraform, Secrets Manager, an IAM policy, the deploy workflow, the task definition, the container environment, the config loader, the DSN builder, the connection pool. **Nine boundaries.** At each one, two individually-correct artefacts can disagree.

Three times in this project, work that was correct in the repository did nothing in production. Each time the code was right, the tests passed, the review found nothing, and the deploy was green. I've stopped treating these as carelessness, because reading the code more carefully would not have found any of them.

## One symptom, four independent causes

The service had a write pool. It had a writer type carrying a marker method, so handing a read-only pool to something that writes is a compile error rather than a runtime one. Processors mounted only when the write pool existed. Tests passed.

Every store write it made in production would have been rejected, and there were four reasons for that simultaneously. Each was sufficient on its own.

**One: the function was never called.** `db.ConnectWrite` existed, was tested, and had exactly two references — its declaration and its test. Nothing in the startup path invoked it. The write pool every processor demanded was never constructed.

**Two: the hostname pointed at a replica.** The configured database host was the Aurora *reader* endpoint. Correct for the read pool, which is bounded and runs `default_transaction_read_only = on` on purpose. But the write DSN was built from the same configuration field, so the write pool — once it was constructed — would connect to a read-only replica and fail one layer down with the same error.

**Three: the write role wasn't configured.** The Postgres role existed. Its secret existed. Both had existed for phases. Nothing in the task definition referenced either.

**Four: the deploy copied the old task definition forward.** The workflow reads the running task definition and adds to it, so anything Terraform introduces never reaches the service. Terraform created revision 22; the running service stayed on 21, because the ECS service carries `ignore_changes = [task_definition]`. Both of those are individually reasonable. Together they mean a correct `terraform apply` changes nothing about what the process reads.

Four mechanisms, one symptom: `cannot execute INSERT in a read-only transaction`, or its equivalent a layer up, or nothing at all because the processor declined to mount.

**A symptom that four causes produce is not a clue.** Fix one and the system behaves identically, which reads as "that wasn't it" and sends you somewhere else. You get no signal that you were right. The only way through is to enumerate candidate causes before fixing any of them, and to expect the list to have more than one entry.

Notice where all four live: at boundaries between artefacts that are each correct. The pool constructor and its call site are in different files and the constructor's tests pass. The hostname is right for one consumer and wrong for another, and it's one field. The role exists in two places and a third doesn't mention it. Terraform's state is correct, the service's state is correct, and their disagreement is the bug.

Three of the four were found by querying the live service, not by reading the repository.

## What made it debuggable

The fix that mattered was cheap, and I'd reach for it first next time: **make the startup log name which precondition is missing, per component.**

    entitlement applying is not configured; effects are derived and recorded only
    app store notification processing is not enabled  … credentials:true  write_pool:false
    google play notification processing is not enabled … credentials:false write_pool:false

Two lines, and they turn "the service isn't processing anything" into "the App Store has credentials and no write pool; Google Play has neither." That's the difference between a symptom and a location.

Before those lines existed, this state had been live for two phases. Nobody was wrong to miss it — the service was up, health checks passed, and both queues sat at depth 0, which looks exactly like "no traffic yet" because that also happened to be true.

The compile-time guard did earn its keep, and it's worth separating from the rest. Gating the processors on a non-nil write pool meant the failure was *refusal to mount* rather than an error on the first real notification. That converts a production incident into a startup message. It just needed the message to say why.

## A permission error dressed as absence

Two later phases shipped — reviewed, merged, deployed, green — and the running service had none of what they configured. Its task definition carried no write credentials, no identity configuration, and still carried a value the second phase had removed.

The deploy workflow looks up each secret before injecting it:

```bash
arn=$(aws secretsmanager describe-secret --secret-id "$name" \
        --query ARN --output text 2>/dev/null || true)
if [ -z "$arn" ]; then
  echo "no $name secret; skipping"
else
  ...
fi
```

The deploy role had `DescribeSecret` on two secret ARNs and none of the three these phases introduced. So the call returned `AccessDenied`, `2>/dev/null` discarded the message, `|| true` discarded the exit status, and the variable came back empty — which the script reports, in good faith, as *the secret does not exist.*

The secrets existed. Terraform had created them. The workflow printed a sentence saying otherwise, in green, and the deploy succeeded.

Two distinct failures are being conflated here, and they call for opposite responses. **The secret is genuinely absent** — expected in some environments, and skipping is correct. **The secret exists and we may not read it** — a misconfiguration of the deploy role itself, where skipping is the worst available action, because it produces a service that starts, passes its health check, and does nothing.

`2>/dev/null || true` collapses those into one. The shape is seductive: it reads as defensive programming, like you thought about failure. What it actually says is "I don't care why this failed," which is a much stronger claim than the author intends.

There's a second layer specific to cloud APIs. Several deliberately return "not found" for "not allowed", to avoid leaking the existence of resources to callers who can't see them — `GetQueueUrl` reports a missing permission as `NonExistentQueue`. So even without the redirection, the two cases aren't reliably distinguishable from the response. You have to *decide* which you'll assume, and the safe assumption isn't the convenient one.

Here's the part that changed how I think about this. **The same hazard was documented three sections above the bug, in the same file.** A comment in the deploy role's Terraform explains the `GetQueueUrl` behaviour, notes that someone had been caught by it, and says what to watch for. Clear, correct, written by someone who paid for the knowledge. The secret lookups were written with the same shape, months later, a few dozen lines below. The note didn't travel.

I don't read that as inattention any more. A comment is available to a reader who happens to scroll past it, in the mood to generalise, at the moment they're writing the thing it applies to. That conjunction is rare. Documentation of a hazard sits in one place; the hazard recurs wherever someone writes a similar line. **A comment is not a control** — it's a record that someone once knew something.

So the fix wasn't another comment. The lookup now captures stderr and branches on what the API said: not-found warns and continues, anything else — `AccessDenied` included — fails the deploy loudly and names the secret. And a test asserts that the set of secrets the workflow reads and the set of ARNs the IAM policy permits are the same set. Two lists, two files, two languages, and nothing but a test connecting them.

The consequence was named up front because it's real: **the deploy now fails where it used to warn.** A misconfigured environment stops deploying instead of deploying wrong. I'd take that trade every time; the alternative is what we had, which was two phases of work everyone believed was live.

The same swallowing shape appears on two other lookups in that workflow. I left them, and recorded them. Changing four things at once in a deploy script is how a fix becomes an incident.

## Four endpoints that passed every test and answered 404

The third one is the most uncomfortable, because the work was genuinely good.

A phase delivered four purchase endpoints: request shapes, role gates, a seven-step validation order, entitlement application, telemetry, an error-masking table. A parity harness drove the monolith's real controller against ours over 300 cases — 300 agreed, zero mismatched. Reviewed, merged, deployed.

In staging, all four answered **404**. Not 500, not 401 — the response of a service that has never heard of the route. The catalog endpoint beside them answered 401, which was the control that made it certain: the router was fine, auth was fine, those four paths simply weren't mounted.

Two causes, and the second is the interesting one. **The wiring line was missing** — the entry point supplied one router option, the catalog, and the router mounts a group only when its option provided a handler. Absent option, no routes, no error. Reasonable for genuinely optional subsystems, and exactly what made this silent. And **there was nothing to supply**: five interfaces the endpoints depend on had no non-test implementation anywhere in the repository. Their only references were the declaration and the call site. Adding the missing wiring line wouldn't have compiled. The gap wasn't a forgotten line, it was a missing layer.

Why nothing caught it, twice over:

**No task owned the entry point.** Every task in the phase listed the files it would touch, and all seven lists stopped at the API package, the purchase package, the tools directory or the test directory. `cmd/server/main.go` appears in none of them and the design never assigned it. The work was decomposed by *layer*, and wiring isn't a layer — it's the seam between the layers and the process, and it belonged to nobody.

**Every test built its own router.** Each constructs a router with stubs and drives real HTTP through it — the right design for what they prove: that a route sits behind the auth middleware, that the role gate rejects, that the error mapping masks. A direct handler call couldn't show any of it. And every one passes identically whether or not the entry point supplies anything, because they prove *this* router assembled *this* way. Production runs a different router, assembled elsewhere, by code no test looks at.

**A test that constructs its own subject cannot prove anything about how the subject is constructed in production.** It isn't a weak test. It's a test of a different thing than you assumed.

The structural fix is one assembly function that both the entry point and the tests call, with deployment intent made explicit: a surface that's supplied means *serve this*, nil means *this deployment doesn't*. Within a surface it's all-or-nothing, because half a surface is the failure being fixed. Asking for a surface while supplying half its dependencies is now fatal at startup and names the missing half. Note the change of question — the router asked *is this handler nil?*, which has a silent answer; startup now asks *did this deployment ask for this?*, which has a loud one.

Then the trap inside the fix: **every test builds its own surface set too**, so they all still pass whether or not the entry point populates one. The bug had moved up a level, not away. So there's a test that reads the *source* of the entry point and asserts it calls the shared assembly and doesn't append router options directly.

It's crude. Asserting on source text tests spelling, not behaviour, and I'd normally argue against it. It's also the only check in the suite that fails when someone reverts to inline assembly, and I mutation-tested it to be sure. Crude and load-bearing. I'd rather have an ugly test that catches what actually happened than an elegant one that can't.

And the entry point now logs which surfaces mounted. The *absence* of that line is what let four dead endpoints survive a whole phase. Verification was a route probe against the real binary started from real configuration — not a test-built router:

    /subscriptions/app-store/subscribe          404  ->  401
    /subscriptions/google-play/subscribe        404  ->  401
    /api/v1/subscriptions/{store}/subscribe     404  ->  401
    /subscriptions/app-store       (control)    401  ->  401

401 is the success condition: mounted, behind auth, rejecting an unauthenticated caller. That's everything you can assert without a token, and everything that was missing.

## What I'd take from this

**Count the boundaries a configuration value crosses.** If it's nine, four simultaneous faults isn't bad luck — it's the base rate.

**Read the running system, not the repository.** A repository tells you what should happen. This entire class of bug lives in the difference.

**A symptom several causes produce is not a clue.** Enumerate before fixing, because fixing one gives you no feedback.

**Log the missing precondition per component, by name.** Not "not enabled" — `credentials:true write_pool:false`. One line; its absence hid two of these three.

**Prefer refusing to start over failing on first use.** A nil dependency that refuses to mount is a startup message. The same dependency used once is an incident in front of a user.

**`2>/dev/null || true` claims you don't care why it failed.** On any command whose failure means two different things, that claim is false — and cloud APIs return "not found" for "not permitted" on purpose.

**A green deploy that skipped something is worse than a red one.** Both leave the environment wrong; only one tells you.

**Turn a hazard note into an executable check.** The comment in that file was correct, paid-for, and didn't prevent its own recurrence forty lines later.

**A test that builds its own subject proves the subject, not its construction.** Both are worth testing and they're different tests — so prove the last mile against the real binary.

**Decomposing by layer leaves the seams unowned.** If no task names the entry point, nothing changes it and nothing checks it.
