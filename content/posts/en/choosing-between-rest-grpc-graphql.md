---
title: "Choosing Between REST, gRPC, and GraphQL"
date: "2022-10-18"
description: "Three ways to expose an API, three different bets. A practical comparison of REST, gRPC, and GraphQL across contracts, transport, client fit, evolution, and caching — with the concrete conditions under which each is the right call."
tags:
  [
    "architecture",
    "api",
    "grpc",
    "graphql",
    "rest",
    "system-design",
  ]
---

I've shipped all three of these in production, sometimes in the same system, and the question "which API style should this be?" still comes up in every design review. The honest answer is that they're optimized for different problems, and the mistake is picking one as a default religion rather than matching it to the shape of the traffic. Here's how I actually decide.

## What Each One Is, in One Breath

**REST** models resources as URLs and uses HTTP verbs against them. The contract is conventional (often documented with OpenAPI), the payload is usually JSON, and it rides on plain HTTP:

```
GET  /orders/42
POST /orders
```

**gRPC** is contract-first RPC: you define services and messages in a `.proto` file, generate typed client and server code, and it speaks Protobuf over HTTP/2.

```protobuf
service OrderService {
  rpc GetOrder(GetOrderRequest) returns (Order);
}
message GetOrderRequest { string id = 1; }
```

**GraphQL** exposes a single endpoint and a typed schema; the *client* declares exactly which fields it wants in a query, and the server resolves them.

```graphql
query { order(id: "42") { total status customer { name } } }
```

## The Axes That Decide It

**Who's calling, and from where?** This is the biggest factor. gRPC is superb for service-to-service traffic inside your own network — typed contracts, HTTP/2 multiplexing, tiny binary payloads — but it's awkward from a browser (it needs a proxy layer to work over the web). REST and GraphQL are browser-native. If the caller is your own mobile/web front end, REST or GraphQL; if it's another backend service you control, gRPC earns its keep.

**How varied are the read shapes?** REST endpoints return a fixed shape, which leads to two classic pains: *over-fetching* (the mobile client gets 40 fields to render 3) and *under-fetching* (rendering one screen takes five round trips). GraphQL exists precisely to kill both — the client asks for exactly the fields it needs, in one request. If you have many client types each wanting a different slice of a rich graph, that's GraphQL's home turf. If every client wants basically the same fixed shape, GraphQL's machinery is overhead you don't need.

**How much do you value the contract as code?** gRPC's `.proto` is the single source of truth, and codegen means a field rename breaks the build, not production. GraphQL's schema is similarly strong and typed. REST's contract is a convention you maintain out-of-band (OpenAPI helps but isn't enforced by default), so it's the easiest to let drift.

**Caching.** REST wins here and it's underrated: `GET` responses cache cleanly at every layer — browser, CDN, reverse proxy — keyed by URL, for free. GraphQL queries are typically `POST`s with the shape in the body, so HTTP caching doesn't apply and you cache at the resolver/field level instead, which is more work. If cheap edge caching of reads matters, plain REST `GET`s are hard to beat.

**Streaming.** gRPC has first-class bidirectional streaming over HTTP/2. REST does server-sent events or long-polling awkwardly; GraphQL has subscriptions but they're a bolt-on. Real streaming needs point to gRPC.

## Trade-offs

```
Axis              REST                gRPC                    GraphQL
----------------  ------------------  ----------------------  ----------------------
Contract          convention/OpenAPI  .proto (enforced)       schema (enforced)
Payload           JSON (verbose)      Protobuf (compact)      JSON, client-shaped
Browser-native    yes                 no (needs proxy)        yes
Over/under-fetch  common              fixed per-RPC           client picks fields
HTTP caching      excellent (GET)     no                      poor (POST body)
Streaming         awkward             first-class             subscriptions (bolt-on)
Best caller       public/web clients  internal service↔service many client shapes
Tooling maturity  ubiquitous          strong (typed codegen)  good, more setup
```

## How I Actually Choose

- **Public API, many third parties, cacheable reads →** REST. It's the lingua franca; everyone can call it, and CDN caching is free.
- **Internal service-to-service, latency-sensitive, typed contracts →** gRPC. Binary payloads and HTTP/2 multiplexing pay off, and the generated clients remove a class of integration bugs.
- **A rich data graph feeding several different front ends →** GraphQL. Letting each client shape its own payload collapses round trips and stops the over-fetch/under-fetch tax.

And — the reframe that's often right — **you don't have to pick one for the whole system.** A common, healthy shape is gRPC between internal services, with a thin REST or GraphQL edge for the browser. Each boundary gets the style that fits its callers. What you want to avoid is choosing by fashion: GraphQL because it's trendy on a system with one client and fixed shapes, or gRPC on a public API that everyone then struggles to call from a browser. Match the style to who's calling and what they need to read, and all three are good tools.
