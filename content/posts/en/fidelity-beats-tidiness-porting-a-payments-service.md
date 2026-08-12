---
title: "Fidelity Beats Tidiness: Porting a Payments Service"
date: "2026-08-07"
description: "When you extract a service and both copies write the same database, the success condition stops being 'it works' and becomes 'it agrees'. On a comment that would have made us refuse paying subscribers, two ugly quirks I ported on purpose, and the one place we diverged deliberately — plus the auth change that looked like an optimisation and was actually a security decision."
tags:
  [
    "migration",
    "architecture",
    "go",
    "python",
    "backend",
  ]
---

Some context, because the rest of this only makes sense with it.

The product is a sports app with a paid tier. Users subscribe through Apple's App Store, Google Play, or Stripe on the web, and a successful subscription grants them a role — internally PRO — that unlocks the paid features. All of it lived in one Django monolith: the purchase endpoints the mobile app calls, the webhook endpoints the stores call, the catalog of what's purchasable, and the logic that decides whether a given purchase entitles someone to PRO.

We're pulling the subscriptions half of that into a new Go service. And the most consequential decision in the whole project was made before any code: **the data does not move.** The Go service connects to the monolith's existing Postgres cluster and writes the same tables. No new database, no migration, no dual-write layer.

That was judged the lesser risk. Moving payment tables can't be rolled back in minutes, and it would break every back-office report that joins them. The cost of avoiding it is that for the entire transition, **two independently-written implementations write the same rows.** The monolith can't be switched off either — Stripe is still entirely its problem — so this isn't a brief cutover window. It's the normal state of affairs for months.

That one fact rewrites the definition of done. A rewrite succeeds when it works. A port in this position succeeds only when it *agrees* — when the row our Go service writes for a purchase is the row the Python would have written. Which means every place the old system is inconsistent, ugly, or apparently wrong becomes a decision, and the default has to be to reproduce it.

That sounds obvious written down. In practice the pressure to tidy is constant, and it arrives disguised as competence.

## How we made agreement checkable

Before the arguments, the instrument, because it's what made the arguments decidable rather than rhetorical.

We built harnesses that drive **the monolith's own Python** — not a transcription of its logic into Go, not a description of it in a test. The entitlement harness recomputes, for every purchase in the database, whether it entitles, on both sides, and compares. The subscribe harness runs the monolith's real purchase controller against Django and Postgres, stubbing only the store API and the identity provider, and compares what each side actually wrote to the database rather than what each side reported.

That distinction matters more than it sounds. Comparing two implementations I wrote proves only that I was consistent. Comparing each side's own report takes one implementation's word for it. Reading the resulting database rows is the only comparison that can't be talked into agreeing.

With that in place, "should we tidy this?" becomes a question with a measurable answer.

## The comment that would have refused paying customers

In our own Go codebase, a comment on the entitlement package stated that the monolith's purchase endpoint accepted only `ACTIVE` subscriptions, while its notification path accepted the full set of statuses that grant access. It described that asymmetry as "real and intentional to preserve."

It had been false for months. The monolith's purchase endpoint validates against the full entitling set, and had done since a change earlier in the year.

Consider who that comment was for. It's on the package you'd read while building the resolver — it exists precisely to save you a trip into the Python. Anyone who trusted it would have built an endpoint that refuses every subscriber in grace period or billing retry.

Those are not edge cases. A subscriber in billing retry is someone whose renewal charge failed and whose access is granted anyway, by explicit product decision, while the store retries the card. Our endpoint would have compiled, passed review, agreed with its own tests, and turned away paying customers.

The comment wasn't careless. It was true when written, sat next to code that never changed, and described behaviour in *another repository* that did. There is no mechanism by which it could have been updated — no test, no compiler, no reviewer who sees both sides. **A comment about a different system is a snapshot with no timestamp and no owner.**

Two things changed as a result. A test now pins all five statuses at that boundary, and the verification bridge fails if that claim ever reappears in the file. The stale comment became a test failure instead of a trap.

## The two quirks I ported on purpose

Then the real version of the same pressure, where the code is genuinely bad and copying it feels wrong.

The monolith writes **different purchase rows for the same purchase**, depending on which path created it. On the purchase endpoint, the offer id is written as null — even when the store's transaction response carries one. And an "introductory offer" flag is derived from the *truthiness* of the offer type field, so any offer at all sets it, not only an introductory one.

Neither is defensible on its own terms. The first discards information sitting right there in the response. The second sets a field named for one thing based on the presence of another. Both are almost certainly accidents that nobody has had reason to look at.

And our service now has both paths in a single process — the purchase endpoint and the notification processor, sharing a package. The tidy move is obvious: derive the row once, correctly, use it in both places. Less code, no duplication, no strange nulls. It would have looked better in review and I'd have felt better writing it.

Here's why it's wrong. **Making our two paths agree with each other makes both of them disagree with the monolith.** Both services write these tables today, concurrently. Tidying doesn't remove an inconsistency — it relocates it, out of our codebase where it's annotated and tested, into the boundary between two live systems, where it surfaces months later as a data difference somebody finds in a report.

So both quirks are ported, each with a test naming the behaviour and a comment explaining why it exists. This codebase forbids duplicated policy as a rule; this is the exception, and it's the exception because the duplication is *in the thing being modelled*.

The rule I'd write from it: **while two systems both write, fidelity beats internal consistency.** Tidying during a migration also destroys attribution — if a report shifts next month, was it the migration or the improvement? The window for tidying opens the day the old writer is switched off, and not before.

## Small fidelity, once you accept the rule

A set of smaller decisions then follow from the rule instead of needing individual argument:

**An enum member's name is not its wire code.** One error's Go/Python member name and the string it actually sends to clients differ — the member says service-unavailable, the wire says unavailable. Port the wire code. The app is matching on the string.

**An email mask, asterisk for asterisk.** The monolith masks addresses with a fixed count of eight asterisks that doesn't track the name's length, so a one-character name yields the same visible character twice. Copied exactly. A "better" mask whose width tracked the real length would leak that length — and any client comparing masked values across the two services would watch them diverge.

**Event names and a SHA-1.** The telemetry a purchase leaves behind is a join key between two services. A renamed event or a "better" hash is a join that silently returns nothing during the incident it was meant to explain. The linter flags the SHA-1; it's annotated with the reason rather than changed.

**An error mapping kept as an explicit table.** Five of six internal refusals reach the app as one generic validation failure — including the one that means *the user paid and did not get access*. That's what the monolith does. Keeping the mapping as a table is what makes the asymmetry visible rather than implied, so it reads as a deliberate masking decision instead of a gap.

## Diverging on purpose, and paying for it in writing

None of this argues against changing things. It argues about *when*, and about labelling.

One behavioural divergence in this project is intentional. When the mobile app retries a purchase against our service that the monolith already processed, we adopt the existing row; the monolith, with its adoption flag off, refuses. That was raised as a question, decided by a person, and recorded — and the parity report names that branch as one where a **mismatch is the expected result**, so 300 agreeing cases can never be misread as "identical in all cases."

The second deliberate divergence is more interesting, because it looked like an optimisation.

The monolith does not verify JWTs locally. On every cache miss it calls the identity provider's validation endpoint over the network and caches the answer for five seconds. Ported straight across, that's a network round trip per request per five-second window, for a check that could be a local signature verification against a published key set. Faster, cheaper, no dependency on the provider being up. Exactly the kind of thing you fix on the way past.

It also changes who has access. Local verification tells you the signature is valid and the token hasn't expired. It cannot tell you the session was revoked ten minutes ago, because revocation is a fact held by the issuer and nothing in the token changes when it happens. The monolith surfaces a revocation within five seconds. Local verification surfaces it *never* — a revoked session stays valid for the token's remaining lifetime, hours on a mobile client.

So the two implementations answer differently for a real user: someone whose session was revoked. The monolith says 401. Local verification says 200. **That's not a performance change with a caveat — it's a change in how long a revocation takes to take effect, from five seconds to hours.** Whether that's acceptable belongs to whoever owns the security posture, not to whoever is porting auth middleware that afternoon.

It was recorded as an open question and made the *first* task of the phase, so the answer would arrive before any auth code existed. The decision came back as local verification **plus an explicit revocation check**, reading the same revocation list the monolith consults.

What I care about is what that answer changed before a line was written. The recorded assumption had argued *against* local verification; it was rewritten to state the decision and its cost — that a revoked session is visible to the monolith within five seconds and visible here only via the revocation check, which is therefore **load-bearing rather than belt-and-braces.** That reclassification is the whole value. In the rejected design, the same lookup is a redundant safety net whose failure nobody prioritises. In the chosen design it's the only thing between a revoked session and a 200.

And an acceptance criterion was rewritten rather than reinterpreted. The original said the provider is called once inside the cache window — not a meaningful claim about a service that doesn't call the provider per request. A criterion carried across a design change is worse than none: it passes, and it tests something nobody wants.

There's a coda that makes "load-bearing" concrete. The revocation list lives in a cache shared with the Django monolith, and Django prefixes its cache keys. An early implementation read the unprefixed key. It found nothing, every time, and reported that the session was not revoked. **A revocation check that reads the wrong key fails open and reports success** — worse than no check, because its existence is what justified the design, and nobody re-examines a decision on the strength of a component they believe is working. It was found by reading the monolith's cache configuration, not by a test: a test written against our own key format would have used that format on both sides and passed.

## What I'd take from this

**If both copies write, the bar is agreement, not correctness.** Decide that on day one, because it determines whether every subsequent judgement call goes toward tidying or toward fidelity.

**Drive the old implementation, don't transcribe it.** A harness that runs the real Python and compares database rows can't be argued with. Two things I wrote agreeing proves only that I'm consistent.

**Read the source, not the commentary — especially commentary about another repository.** It has no owner and no expiry, and it was probably true once.

**Turn a corrected claim into a test.** A stale comment that's now a failing assertion is the only kind of documentation that can't rot quietly.

**Making your paths agree with each other can make both disagree with the system you're porting.** That relocates the inconsistency somewhere nobody tests.

**Tidying during a migration destroys attribution.** Improve after the old writer is off, when a change in a report has exactly one possible cause.

**A performance change that alters when a fact becomes false is a security change.** Name the window in units and let its owner decide.

**Record deliberate divergences where the harness can see them.** Then agreement means something, and disagreement is expected rather than ambiguous.
