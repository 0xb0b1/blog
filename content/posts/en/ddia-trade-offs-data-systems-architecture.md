---
title: "There Are No Solutions, Only Trade-Offs: DDIA Chapter 1 and Four Decisions I Actually Made"
date: 2026-07-06
description: "The first chapter of Designing Data-Intensive Applications, 2nd edition, isn't about techniques—it's about trade-offs. Reading it against four real backend decisions at a sports platform: monolith vs distributed, shared database vs database-per-service, systems of record vs derived data, and cloud vs self-hosting."
tags:
  [
    "golang",
    "ddia",
    "architecture",
    "system-design",
    "microservices",
    "distributed-systems",
    "postgresql",
    "trade-offs",
    "backend",
  ]
---

The first chapter of the second edition of _Designing Data-Intensive Applications_ opens with a Thomas Sowell quote that turns out to be the whole point of the book:

> There are no solutions; there are only trade-offs. [...] But you try to get the best trade-off you can get, and that's all you can hope for.

I wrote earlier about [how DDIA's stream processing concepts map to a real notification system](/en/posts/ddia-stream-processing-notification-systems). That chapter taught me techniques—event streams, backpressure, exactly-once. Chapter 1 is different. It doesn't teach you a technique. It teaches you which questions to ask before you reach for one, by walking through four contrasting choices that shape every data system: operational vs analytical, cloud vs self-hosting, distributed vs single-node, and the tension between the business and the rights of the people whose data you hold.

Reading it after the fact was uncomfortable, in a good way. Every decision the chapter frames as a trade-off, I'd made under deadline pressure at R10 Score—sometimes well, sometimes by accident. This post runs four of those decisions back through Kleppmann and Riccomini's framing. Not to show off good architecture, but to show what it looks like when you pick the losing side of a "best practice" on purpose and know exactly what you gave up.

## Decision 1: We Stayed a Monolith Longer Than the Diagrams Allow

DDIA lists eight legitimate reasons to distribute a system: inherent distribution, requests between cloud services, fault tolerance, scalability, latency, elasticity, specialized hardware, and legal compliance. Then it does something most architecture content refuses to do—it argues the other direction:

> More nodes are not always faster; in some cases, a simple single-threaded program on one computer can perform significantly better than a cluster with over 100 CPU cores. [...] performing a task on a single machine is often much simpler and cheaper than setting up a distributed system.

And on microservices specifically:

> Microservices are primarily a technical solution to a people problem: allowing different teams to make progress independently without having to coordinate with each other.

That sentence is the one I wish I'd read three years earlier. R10 started as a Python/Django monolith—`r10-hub`—and stayed one far longer than the microservices blog posts said it should. We only extracted Go services when there was a concrete reason DDIA would recognize: notifications had a genuinely different resource profile (I/O-bound, spiky fan-out during matches) and needed to scale independently of the request-serving monolith. Odds was a separate team with its own deploy cadence. Those are the "people problem" and "independent scaling" reasons, not "microservices are the modern way."

I already wrote the long version of this argument in [When to Go Distributed](/en/posts/when-to-go-distributed). The short version, and the part Chapter 1 sharpened for me, is that the extraction only stayed cheap because the boundary existed inside the monolith first. In Go that boundary is an interface:

```go
// Inside the monolith: order depends on an interface, not a package
type UserService interface {
    GetUser(ctx context.Context, id string) (*User, error)
}

// Later, extracted: same interface, now a network call
type HTTPUserService struct {
    baseURL string
}

func (s *HTTPUserService) GetUser(ctx context.Context, id string) (*User, error) {
    // the caller's code does not change—only the failure modes do
}
```

The interface stays identical. What changes is everything DDIA warns about in the same breath: that in-process call that never failed now traverses a network that "may be interrupted, or the service may be overloaded or crash, and therefore any request may time out without receiving a response. In this case, we don't know whether the service received the request, and simply retrying it might not be safe."

**The trade-off.** Staying monolithic bought us simplicity: one deploy, one database transaction, no distributed tracing to debug a single request, no retry-idempotency puzzle. It cost us independent scaling and team autonomy—until those costs got real enough to pay for the distributed complexity. **What would have changed my mind earlier:** a measured bottleneck the monolith couldn't absorb with caching and indexes, not a theoretical one. We didn't have one for a long time, so we didn't move.

## Decision 2: We Share a Database Across Services, On Purpose

Here is Chapter 1 stating the rule as plainly as it gets:

> It is common for each service to have its own databases and not to share databases between services. Sharing a database would effectively make the entire database structure a part of the service's API, and then that structure would be difficult to change. Shared databases could also cause one service's queries to negatively impact the performance of other services.

We share a database across services. Three of them—the Django monolith, `r10-notifications`, and `r10-odds`—all read and write the same PostgreSQL instance. By the book, this is the antipattern with a name: the distributed monolith, coupled at the schema instead of the API.

I know. I [wrote about it in detail](/en/posts/shared-database-microservices-migration). The point isn't that we didn't know the rule. The point is that during a monolith-to-microservices migration, the "correct" answer—database-per-service on day one—requires solving the distributed data problem before you've shipped a single feature, and the business doesn't pause for that. So we took the trade-off DDIA describes, with eyes open, and then spent our engineering effort containing the blast radius rather than pretending we'd avoided it.

The containment is a set of rules, and every one of them is a direct response to "the schema is now part of the API":

```sql
-- migrations/001_create_live_activity_token.sql
-- Owned by the notifications service. Note what is NOT here.
CREATE TABLE r10_live_activity_token (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID         NOT NULL,   -- references r10_user.id, but no FK
    match_id     UUID,                    -- references r10_match.id, but no FK
    device_token VARCHAR(500) NOT NULL,
    state        VARCHAR(20)  NOT NULL DEFAULT 'registered',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, device_token)
);
```

No cross-service foreign keys. `user_id` points at a user conceptually, but there is no `REFERENCES r10_user(id)`. That's deliberate: a foreign key would make a schema change to `r10_user` in the monolith require a coordinated deploy of a Go service that the monolith's team has never seen. DDIA's "difficult to change" becomes "impossible to change without a cross-team meeting." Dropping the FK trades referential integrity—orphaned rows are now possible—for deploy independence. In a migration, the second is worth more than the first.

**The trade-off.** A shared database let us extract services incrementally without building CDC pipelines or an API gateway on day one. It cost us the clean boundary: schema changes to shared tables require grepping across three repos because no tool tracks the coupling, and two services writing the same table is where the bugs live. **What would change my mind:** we already know the exit. Once a service touches only its own tables—`r10_live_activity_token` is notifications-only—we cut those tables into a dedicated database, and the `r10_` prefix makes the cut obvious. The shared database is a state we're leaving, not a destination we chose.

## Decision 3: The Monolith Is the System of Record; Everyone Else Holds Derived Data

Chapter 1 introduces a distinction I now reach for constantly:

> A system of record, also known as a source of truth, holds the authoritative or canonical version of data. [...] If there is any discrepancy between another system and the system of record, the value in the system of record is (by definition) the correct one.

> Data in a derived system is the result of taking existing data from another system and transforming or processing it in some way. If you lose derived data, you can re-create it from the original source.

The no-foreign-key decision from the last section is really a statement about which system owns which data. `r10_user` is written by the monolith; the monolith is its system of record. When the notifications service stores a `user_id`, it isn't co-owning that user—it's holding a derived reference it could rebuild from the source if it had to. Naming that out loud changes how you reason about failures. A query against a `user_id` that no longer exists isn't data corruption; it's a stale derived value, and the application handles the miss:

```go
// The Go service reads the monolith's system-of-record tables directly
query := `SELECT id, role, language FROM r10_user WHERE id = $1`
// A miss here means "the source of truth moved on," not "the DB is broken."
```

(I deliberately won't re-cover the Redis topic cache here—that's the classic derived-data example and I already worked through it as [stream-table duality](/en/posts/ddia-stream-processing-notification-systems) in the other post.)

What Chapter 1 made me confront is the part I hadn't solved: derived data has to be kept up to date. Kleppmann and Riccomini are blunt that "when the data in one system is derived from the data in another, you need a process for updating the derived data when the original in the system of record changes." Right now the Go services read `r10_user` and `r10_match` by querying the source table directly—the crudest possible "update process," which is to have no derived copy at all and just reach into the source. That works only because we share the database. The moment we split it, we owe a real answer: a read replica, a change data capture stream, or an API call. Each is a different trade-off between staleness, coupling, and operational cost, and Chapter 1 is honest that there's no free version.

**The trade-off.** Treating the monolith as the single source of truth keeps consistency simple—there's exactly one writer for each fact. It costs the Go services autonomy: they can't answer a query the source table can't serve, and they inherit the monolith's availability. **What would change my mind:** when direct coupling to `r10_user` starts causing incidents—the monolith's downtime becoming the notification service's downtime—that's the signal to build a genuine derived copy and pay the propagation cost.

## Decision 4: We Buy Fan-Out Instead of Building It

DDIA frames cloud vs self-hosting as build-vs-buy, and it's refreshingly unromantic about both sides. The case for buying:

> If you need a system that you don't already know how to deploy and operate, adopting a cloud service is often easier and quicker than learning to manage the system.

The case against, which most cloud marketing skips:

> The biggest downside of a cloud service is that you have no control over it. [...] If the service goes down, all you can do is to wait for it to recover. [...] making vendor lock-in a problem.

Notification fan-out is the clearest buy-vs-build call we made. When a goal is scored, one event has to reach hundreds of thousands of devices. We could have built that fan-out layer—a log, consumer offsets, delivery workers, retry state. Instead we publish once to AWS SNS and let it fan out to FCM and APNs. That's the DDIA trade-off in its purest form: we outsourced the operation of a hard distributed problem to a vendor that runs it for thousands of customers, and in exchange we accepted exactly the downside the book names. SNS is fire-and-forget—we lost replayability at the fan-out layer. If SNS throttles or degrades, we wait; we can't crack it open. And the API is proprietary, so switching push providers is a real migration, not a config change.

We decided the operational simplicity was worth more than the control, because delivery fan-out is not R10's competitive advantage—the sports data and the product are. That's DDIA's own heuristic: "things that are a core competency or a competitive advantage of your organization should be done in-house, whereas things that are non-core, routine, or commonplace should be left to a vendor."

The same reasoning runs the other way for things we do control. This blog is a single Go binary on a single Fly.io machine—no Kubernetes, no autoscaling group. DDIA would call that the right read: the load is predictable and small, so "it's often cheaper to buy your own machines and run the software on them yourself" (or in this case, one small instance). Distributing it would be complexity in search of a problem.

**The trade-off.** Managed fan-out gave us a hard problem solved by specialists and zero delivery infrastructure to run. It cost us replayability, deep observability into delivery, and cheap provider portability. **What would change my mind:** if delivery guarantees or per-message audit became a product requirement—say, provable delivery for a paid tier—the loss of control would start to outweigh the operational savings, and a self-hosted or hybrid fan-out would earn its keep.

## Decision 5: The Axis Engineers Forget

Chapter 1's fourth pillar is the one I'd have skipped a few years ago, and the one I care about most now that I've moved toward application security: data systems, law, and society. The book is direct that this is a design input, not a compliance afterthought:

> Legal considerations are influencing the very foundations of data system design. For example, the GDPR grants individuals the right to have their data erased on request [...]. However, [...] many data systems rely on immutable constructs such as append-only logs as part of their design. How can we ensure deletion of some data in the middle of a file that is supposed to be immutable?

That question lands hard on any event-sourced or append-only design. It also reframes a storage decision as a liability decision. We store device tokens, and we could store far more—IP logs, fine-grained location from match check-ins. DDIA's principle of _data minimization_ (the German _Datensparsamkeit_) is the counterweight to the reflexive "store everything, it might be useful":

> Once all the risks are taken into account, it might be reasonable to decide that some data is simply not worth storing, and that it should therefore be deleted. [...] the costs of storage extend beyond the bill you pay [...]. The cost-benefit calculation should also take into account the risks of liability and reputational damage if the data were to be leaked.

I won't claim we've fully solved this—that would be exactly the kind of fabricated tidiness this blog tries to avoid. The honest state is that Chapter 1 turned "what should we log?" from a debugging question into a trade-off with a liability axis: every field you retain is a field you have to protect, delete on request, and justify keeping. The safest data is the data you never stored.

## The Whole Chapter in One Table

None of these decisions has a right answer—only a chosen side and a known cost. That's the entire thesis of the chapter, and it's worth seeing the four laid out on the same axes:

```
Decision              DDIA concept          What we chose        What we traded away        What would change my mind
--------------------  --------------------  -------------------  -------------------------  ----------------------------
Monolith vs           distributed vs        stayed monolithic    independent scaling,       a measured bottleneck the
 distributed          single-node; "don't    until real need     team autonomy (for a       monolith can't absorb with
                      rush to distributed"                        while)                     caching/indexes
Shared DB vs          "sharing a DB makes   one shared Postgres  clean boundaries;          a service touching only its
 DB-per-service        the schema part of                        schema changes need a      own tables (then we split)
                       the API"                                   cross-repo grep
System of record vs   source of truth vs    monolith is SoR,     Go-service autonomy;       direct coupling to r10_user
 derived data         derived data           others hold          they inherit the           causing cross-service
                                             derived refs         monolith's availability    incidents
Cloud vs              build vs buy;          buy: SNS managed     replayability, delivery    provable-delivery becoming a
 self-hosting         vendor lock-in         fan-out              observability, provider    product requirement
                                                                  portability
Data & society        data minimization;    minimize what we     features that "more        a liability/privacy risk
                      right to be forgotten  store                 data" might have           outweighing the data's value
                                                                  enabled
```

The best engineers I've worked with don't have a bag of correct answers. They have a sharp sense of which trade-off they're standing on and what it would take to move. That's what Chapter 1 gives you—not solutions, but the axes to reason on. Every time I've regretted an architecture decision, it wasn't because I picked the wrong side. It was because I didn't realize there was a trade-off at all until it broke in production.

Read the chapter with your own systems open in another window. For each of your load-bearing decisions, ask the one question the chapter keeps asking: what did you give up to get this, and what would make the other side worth it?

---
