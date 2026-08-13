---
title: "Measuring Production and Believing the Wrong Thing"
date: "2026-08-10"
description: "Porting a service means every scoping decision rests on a fact about production. A log query matched 0 of 292,932,577 records and read as a definitive answer — the format it searched for was 100% of live traffic. A 500-row sample reported 96% and the full corpus reported 80%. On the difference between 'none exist' and 'my query can't see them', and the positive control that separates them."
tags:
  [
    "observability",
    "measurement",
    "debugging",
    "aws",
    "testing",
    "subscriptions-extraction",
  ]
---

We're [porting the subscriptions half of a Django monolith to a Go service](/en/posts/quantify-the-failure-before-you-redesign-it). Which means, constantly, that scoping a task depends on a fact about production: is this receipt format still in use, does this log field exist, how many of these stored rows can we actually replay, is anyone still calling this endpoint.

There's no way to answer those from the repository. The code contains every branch that was ever written, including the ones no traffic has taken in three years. So the project made measurement a first-class step — an early phase's acceptance criterion was literally *the failure picture is measured, and reproducible*, and that habit paid for itself repeatedly.

It also produced the most confidently wrong conclusions in the whole project. Every one of them came from a measurement that was well-formed, ran successfully, returned a clean result, and answered a different question than the one I'd asked.

## A clean zero over 292 million records

I needed to know whether an old receipt format was still in use before deciding to port the code that handles it. StoreKit 1 receipts versus the newer signed JWS transactions: if SK1 was dead, a whole task disappeared, along with a deprecated Apple endpoint and a secret to manage.

The specification said the evidence already existed — the monolith logs a `purchase_token_format` field on every token resolution, so a log query would settle it.

The query matched **0 of 292,932,577 records.**

That's a clean zero over a very large denominator, and it reads like an answer. Nearly 300 million records, no SK1, done. The truth was that SK1 accounted for **100% of observed production traffic**: 14 resolutions in 30 days, every one of them SK1, zero JWS.

The field is passed to the logger as an extra keyword argument, and the monolith's own source says, in a comment, that its log formatter drops those. So the field was never in a log line — not rare, absent by construction, for the entire history of the service. My query was well-formed, ran against the right log group, scanned 292 million records, and could not have matched anything no matter what production did.

The answer came from the message *text*, which is logged unconditionally. Every "purchase token resolved" line is followed by a line about the receipt having no latest-receipts info — which exists only in the SK1 branch, because the JWS branch returns immediately after decoding. The presence of a string in a branch is a worse signal than a structured field in every way except one: it was actually there.

## The same shape, twice more

**A stale log group.** Before that query I ran one against the obvious log group name, present in the production account. It returned `scanned: 0`, which reads as "no traffic."

Its last event was six months old, and its stream names carried the *staging* naming convention. It was an abandoned group sitting in the wrong account's shape. The live group came from reading the running service's log configuration rather than guessing a name — and once queried, it scanned 722,828,950 records.

`scanned: 0` is the most honest output in this story and I still nearly misread it. It doesn't say "no matching events." It says the query examined nothing at all, which is a statement about the query.

**A 404 from a service that isn't the service.** In an earlier phase I needed to compare our catalog response against the monolith's, and probed the staging API hostname. It answered — 404 on the route. Production answered 401 on the same path. I concluded the route existed only in production and parked an acceptance criterion as unprovable in staging, writing up three options for how to proceed without it.

That was wrong. The host answers, and looks like the API, but its OpenAPI document has exactly two paths under a title suggesting a combined API. It's a stub. The real backend sits behind a different load balancer under a different path prefix. A 404 was read as "the route does not exist" when it meant "you are asking the wrong service."

The criterion went from parked to proven the same day, against a live peer, once the request went to the right place. Nothing about the code had changed. And the comparison wasn't trivial when it ran: both stores returned matching subscription sets, the same highlighted plan, the same free-trial summary.

## What the three have in common

Each time, an empty result was produced by a **defect in the observation, not a fact about the system.** And each time the empty result was more persuasive than a wrong non-empty one would have been, because zero feels like the absence of noise rather than the presence of a mistake.

That asymmetry is the trap. A query returning 40,000 records when you expected 12 makes you check your query. A query returning nothing makes you believe your hypothesis. The second is a far better disguise for a broken measurement, and it arrives exactly when you're trying to close a yes/no question and are keen to be done.

It's worth noticing how *reasonable* each wrong reading was. A field the specification said was logged. A log group with the service's name in it. A hostname that serves the API and answers HTTP. None of these was carelessness; each was a plausible artefact that had drifted from the thing it appeared to name.

## The sample that answered a different question

The other failure mode isn't emptiness, it's sampling — and it produced the most instructive mistake of the project.

The monolith had been storing every raw store notification it received for years, which turns a fixtures problem into a replay problem: the corpus is real traffic, and comparing two decoders over it is a far stronger claim than comparing either against hand-written examples. The question that decides whether the strategy works at all is what fraction of those rows still carries a recoverable payload, and which notification types appear.

I measured it three times.

- **5 samples** — "the payload is recoverable." True, and uninformative.
- **500 samples** — 96.3% recoverable, 4 notification types uncovered. **Both numbers wrong.**
- **The full corpus, 404,474 rows** — 80.4% recoverable, 2 types uncovered. Measured.

The 96.3% is the one I'd most like to keep hold of, because it wasn't a sampling error. It was a definition error wearing a sampling error's clothes.

389,541 rows have an associated log record. 325,384 have a *recoverable payload*. The first over the total is 96.3%; the second is 80.4%. I'd written a query for "has a log record" and reported it as "has a payload", because in a five-row spot check those had been the same thing — every row with a log had a payload in it. A single number was presented as the answer to a question it didn't answer, and the gap was sixteen points and 64,000 rows.

No increase in sample size would have corrected that. The 500-row measurement was *more precise about the wrong quantity.*

The habit I took from it: **write the number as the question it answers, in the same breath.** Not `recoverable: 96.3%` but `rows with a log record: 96.3%`. If the label is the query, a mismatch between label and intent is visible. If the label is the conclusion, it isn't.

The second wrong answer was a genuine sampling artefact, and it matters more than the percentage. The 500-row sample reported four notification types absent from the corpus; the full corpus shows two. The difference is two types appearing 1,578 and 3,269 times — roughly 0.4% and 0.8% of the corpus, so their expected counts in 500 rows are about two and four. Seeing zero of either is unremarkable.

But **the rare types are exactly what the verification is for.** A common notification type is exercised by any test anyone writes; it's the pause-related transitions and odd lifecycle events that carry the branches nobody has read carefully. A sample that loses precisely the categories you most need to cover, while reporting a coverage percentage that looks fine, is worse than no sample — it puts confidence in the wrong place. For coverage questions, sample size is a cliff, not a slope: you either see a category or you don't.

## What the full measurement found that nobody asked for

Measuring everything turned up two things no sample would have shown, and both changed decisions.

**Coverage has two holes, and one is recent.** Nothing before a certain month in 2023 carries a payload — expected, that's when the logging was added. The second is the interesting one: a stretch from late 2025 into early 2026 where one month recovers 14 rows of 5,480 and the next recovers **0 of 5,255.** Something changed in the logging and then changed back. Nobody knew, and a percentage — even a correct one — hides it completely. A recovery rate is a distribution over time, and the average is the least useful thing about it.

**The corpus ends.** The most recent notification was dated three months before I ran the query, and the other store's notifications stop on the same date. Staging had stopped receiving store traffic entirely, months earlier, and nobody had noticed because nothing in staging depended on it.

That last one wasn't the question I asked, and it's the most important thing the measurement produced. Several remaining plans involved observing behaviour in staging; all of them were resting on an environment that had been dark since May.

With the real numbers in hand the replay compared **319,507 notifications with zero disagreements**, and the report names the two types the corpus doesn't contain rather than leaving them implied. Two things made that worth publishing: the Python side imports the monolith's own model instead of re-implementing it, and the uncovered types are in the report as data, with a test asserting that field exists — so a run that silently lost a branch can't leave the criterion green.

## The control

The fix for all of this is boring and mechanical. **Before believing a zero, prove the query can produce a non-zero.**

- Drop every filter and confirm the query returns *something*. If it doesn't, you're measuring your access to the data, not the data.
- Search for a string you're certain exists — a startup line, a health check, anything unconditional. If that misses, the target is wrong.
- Read the denominator before the numerator. `scanned: 0` and `scanned: 292,932,577` with the same `matched: 0` are entirely different findings, and only one is about production.
- For an HTTP probe, get a control response from a route you know is served. A 401 from a mounted route beside a 404 from the one you're asking about would have said "wrong service" immediately.

And upstream of all of it: **don't take a claim about what is logged from a specification.** Confirm the field appears in a real line before designing a measurement on it. The claim in mine was written in good faith and had probably been true once; the formatter comment was sitting in the source the whole time.

## What I'd take from this

**An empty result measures your query as much as it measures the world.** Both explanations are always available and nothing about the result distinguishes them.

**A large denominator makes a zero more convincing without making it more true.**

**Run a positive control before reporting a negative finding.**

**Label a number with its query, not its conclusion.** A bigger sample doesn't fix a wrong denominator; it makes a precise measurement of the wrong thing.

**Sample to decide whether to measure, never to publish a number.** Five rows correctly established the payload was recoverable at all, which was all they could support.

**Look at the distribution over time, not the average.** The month that recovered 0 of 5,255 was invisible inside a corpus-wide percentage.

**Measure the whole thing once, early.** It's a query. It costs minutes, settles the question, and tells you things you didn't ask — like that the environment you planned to verify in had been silent for three months.

---

*Part of [The Subscriptions Extraction](/en/posts/the-subscriptions-extraction-a-reading-order), seventeen posts on pulling the subscriptions half of a Django monolith into a Go service.*
