---
title: "GraphQL in Go: Killing N+1 and Shrinking Payloads"
date: "2023-04-12"
description: "The two problems every GraphQL server hits once real clients arrive: the N+1 query explosion from nested resolvers, and unbounded query cost. How to fix both in Go with dataloaders and complexity limits."
tags:
  [
    "golang",
    "graphql",
    "api",
    "performance",
    "backend",
  ]
---

GraphQL is lovely in the demo and treacherous under load, and both facts have the same cause: the client, not the server, decides the shape of the query. That's the feature — [it's why you'd choose GraphQL](/en/posts/choosing-between-rest-grpc-graphql) — and it's also the source of the two problems that bite every GraphQL server in Go once real clients start sending real queries. Here's how I deal with them.

## The N+1 Explosion

Say a query asks for a list of orders and each order's customer:

```graphql
query {
  orders(first: 50) { id total customer { name } }
}
```

The naive resolver setup runs one query to fetch 50 orders, then — because the `customer` resolver fires once per order — 50 more queries to fetch each customer individually. That's 51 database round trips for one API call, and it scales linearly with the list size. This is the single most common way a GraphQL server falls over: not one slow query, but hundreds of fast ones.

The fix is a **dataloader**: instead of resolving each `customer` immediately, you collect all the customer IDs requested during one tick of the event loop, then satisfy them in a single batched query. The per-order resolver asks the loader for a customer; the loader batches.

```go
// A batch function receives all keys requested in this tick and must
// return results in the SAME order as the keys.
func batchCustomers(ctx context.Context, ids []string) []*dataloader.Result[*Customer] {
	rows, _ := repo.CustomersByIDs(ctx, ids) // ONE query: WHERE id = ANY($1)

	byID := make(map[string]*Customer, len(rows))
	for _, c := range rows {
		byID[c.ID] = c
	}
	out := make([]*dataloader.Result[*Customer], len(ids))
	for i, id := range ids {
		out[i] = &dataloader.Result[*Customer]{Data: byID[id]}
	}
	return out
}

// The customer resolver just asks the loader — no direct DB call.
func (r *orderResolver) Customer(ctx context.Context, o *Order) (*Customer, error) {
	return loadersFrom(ctx).Customer.Load(ctx, o.CustomerID)()
}
```

Now those 50 nested resolutions collapse into one `WHERE id = ANY($1)` query. The loader lives per-request (stashed in the context by middleware) so batching and caching are scoped to a single API call and don't leak across requests. This one pattern is the difference between a GraphQL server that survives production and one that doesn't.

## Unbounded Query Cost

The second danger is subtler. Because the client composes the query, a client can ask for something enormous — deeply nested, or a huge page — and a naive server will dutifully try to serve it:

```graphql
query {
  orders(first: 1000) {
    customer { orders(first: 1000) { customer { orders(first: 1000) { id } } } }
  }
}
```

That's a query designed (maliciously or accidentally) to melt your database. REST doesn't have this problem because the server fixes the shape; GraphQL hands that control to the client, so you have to put bounds back. Two guards:

**Depth limiting** rejects queries nested beyond some level. **Complexity limiting** assigns a cost to each field (higher for list fields, multiplied by the requested page size) and rejects queries whose total exceeds a budget:

```go
srv := handler.NewDefaultServer(schema)

// Reject anything costing more than 200 "points".
srv.Use(extension.FixedComplexityLimit(200))

// A list field's cost scales with how many items it can return.
cfg.Complexity.Query.Orders = func(childComplexity, first int) int {
	return first * childComplexity
}
```

Now the pathological query above is rejected before it touches the database, with a clear error, instead of timing out.

## Shrinking Payloads Is Mostly Free

The flip side of client-controlled queries is the good side: clients fetch exactly the fields they render. A mobile screen that needs `id` and `total` sends a query for `id` and `total` and gets back a tiny payload — no over-fetching the 30 fields a REST endpoint would have returned. You mostly get this for free, but two habits protect it:

- **Resolve lazily.** Don't compute an expensive field (say, an aggregate) in the parent resolver "just in case" — put it behind its own field resolver so it only runs when a client actually selects it. The whole point is that unrequested fields cost nothing.
- **Paginate everything that's a list.** Expose `first`/`after` cursors, not unbounded arrays, so payload size (and complexity cost) stays bounded regardless of how much data sits behind the field.

## Trade-offs

The thing to keep in view: GraphQL moves query-shaping power from the server to the client, and every problem here is the cost of that trade.

```
Concern            Cause                        Mitigation           Residual cost
-----------------  ---------------------------  -------------------  --------------------
N+1 queries        per-item nested resolvers    per-request loaders  batching complexity
Runaway queries    client controls shape/depth  depth+complexity cap tuning the budget
Payload bloat      (avoided by design)          lazy fields + paging discipline required
```

None of this is a reason to avoid GraphQL — it's the standing maintenance cost of the flexibility. If your clients all want the same fixed shape, you're paying that cost for a flexibility you're not using, and REST would be simpler. But when you genuinely have many client shapes over a rich graph, dataloaders and a complexity budget turn GraphQL from a demo into something you can safely put in front of real traffic.
