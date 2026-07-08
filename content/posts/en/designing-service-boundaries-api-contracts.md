---
title: "Designing Service Boundaries and API Contracts"
date: "2024-09-10"
description: "Where you draw service boundaries — and how you treat the contracts between them — decides whether a system is genuinely decoupled or a distributed monolith wearing microservice clothes. On data ownership, coupling, and evolving contracts without breaking consumers."
tags:
  [
    "architecture",
    "api",
    "system-design",
    "contracts",
    "backend",
  ]
---

The hardest part of building systems out of services isn't the services — it's the lines between them. Draw the boundaries wrong and you get the worst of both worlds: the operational cost of a distributed system with the coupling of a monolith, so every "independent" service has to deploy in lockstep with three others. Draw them right and teams genuinely move on their own. After a few systems that got this wrong before they got it right, here's what I've settled on.

## A Boundary Is About Data Ownership, Not Code Size

The instinct is to split by technical layer or by "this file is getting big." The durable split is by **data ownership**: a service owns a coherent slice of data and is the *only* thing that writes it. Everyone else who needs that data goes through the service's API, never into its database.

The tell that a boundary is wrong is the direction and volume of chatter. If two services can't do anything useful without a dozen synchronous calls back and forth, they're not two services — they're one service with a network cable stapled through the middle, and you've added latency and failure modes for nothing. Boundaries should fall where coupling is naturally low: order management owns orders, catalog owns products, and they interact through a few well-defined calls, not a constant conversation.

The antipattern to name out loud is the **shared database**: two services reading and writing the same tables. It looks like reuse; it's actually the tightest coupling there is, because now the database schema *is* both services' API, and neither can change a column without coordinating with the other. If services share a database, you don't have a boundary — you have a monolith with extra deployment steps.

## The Contract Is the Real Boundary

Once a service owns its data, the API contract becomes the actual interface — and the whole point is that the *implementation* behind it can change freely while the *contract* stays stable. That only works if the contract is explicit and enforced, not implied. Whether it's an OpenAPI document, a `.proto`, or a GraphQL schema (I compared those [here](/en/posts/choosing-between-rest-grpc-graphql)), a good contract is:

- **Explicit** — written down in a form both sides build against, not "whatever the endpoint happens to return today."
- **Typed** — field types and required-ness are part of the contract, so a mismatch is caught at build or request time, not by a consumer crashing on a `null`. I lean on schema tooling like malli for exactly this on the backend-for-frontend layer ([post](/en/posts/clojure-bff-typed-contracts-reitit-malli)).
- **Owned by the producer, shaped for the consumer** — the service owns its contract, but it's designed around what callers actually need, not a raw dump of its internal model.

## Evolving Without Breaking Consumers

Here's where most contract pain actually lives: not designing the first version, but changing it while people depend on it. You almost never control all the consumers, and you can't upgrade them atomically. So the rule is **additive, backward-compatible change by default**:

- **Adding** an optional field is safe — old consumers ignore what they don't know about.
- **Removing** a field, **renaming** one, or making an optional field **required** is a breaking change — it will crash consumers that don't expect it.

```
Change to a contract                 Safe?   Why
-----------------------------------  ------  -------------------------------------
Add an optional field                yes     old clients ignore unknown fields
Add a new endpoint / RPC             yes     nobody depends on it yet
Make an optional field required      NO      old clients omit it → they break
Remove or rename a field             NO      clients reading it → they break
Change a field's type                NO      deserialization breaks
Tighten validation on existing input NO      previously-valid requests now rejected
```

When you genuinely must make a breaking change, you don't mutate the existing contract — you **version** it (a new endpoint, a `v2` message, a new schema) and run both until consumers migrate, then retire the old one. It's more work, and it's the price of not being able to redeploy the world at once.

A discipline worth adopting when the stakes are high: **consumer-driven contract tests**, where each consumer contributes a test asserting the shape it depends on, and the producer runs all of them in CI. Then "did I just break someone?" is answered by a red build before deploy, not by a consumer's pager after.

## Trade-offs

The meta-point is that boundaries and contracts are a bet about **change**: you're spending coordination cost up front (explicit contracts, versioning discipline, no shared tables) to buy the ability to change each service independently later.

- **When strong boundaries pay off:** multiple teams, services that evolve at different rates, anything where you can't coordinate every deploy. The up-front discipline is what lets teams stop blocking each other.
- **When they're overkill:** a small team on a young product where the domain is still shifting weekly. Premature service boundaries freeze your data model before you understand it, and moving a boundary later is far more painful than moving a function. Start with a well-structured monolith and clear *internal* boundaries; extract services when the coupling data (deploy contention, differing scale, team ownership) actually justifies the network.

The line I keep coming back to: a service boundary is a promise that costs something to keep and something to break. Put it where the promise is easy to keep — around owned data, behind an explicit contract you can evolve additively — and services buy you real independence. Put it anywhere else and you've just distributed your monolith.
