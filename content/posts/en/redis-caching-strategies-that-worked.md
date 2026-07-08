---
title: "Redis Caching Strategies That Actually Moved the Needle"
date: "2022-03-14"
description: "Cache-aside vs write-through, TTLs with jitter, and the cache stampede that takes your database down at the worst possible moment — with Go and Redis, and an honest look at the invalidation problem nobody escapes."
tags:
  [
    "redis",
    "caching",
    "performance",
    "backend",
    "golang",
  ]
---

Caching is where a lot of backends get their biggest, cheapest win — and also where a lot of them acquire their most confusing bugs. "Put Redis in front of the database" is easy to say and easy to do badly. This is what has actually worked for me under real read load, and the failure modes I learned to design around before they found me in production.

## Cache-Aside Is the Default for a Reason

The pattern I reach for first is cache-aside (lazy loading): the application owns the cache. On a read, check Redis; on a miss, read the database, populate the cache, return. Writes go to the database and invalidate (or update) the cache.

```go
func (s *Service) GetProduct(ctx context.Context, id string) (*Product, error) {
	key := "product:" + id

	// 1. Try the cache
	if b, err := s.rdb.Get(ctx, key).Bytes(); err == nil {
		var p Product
		if json.Unmarshal(b, &p) == nil {
			return &p, nil
		}
	}

	// 2. Miss → source of truth
	p, err := s.repo.GetProduct(ctx, id)
	if err != nil {
		return nil, err
	}

	// 3. Populate for next time (fire-and-forget on cache errors)
	if b, err := json.Marshal(p); err == nil {
		s.rdb.Set(ctx, key, b, ttlWithJitter(10*time.Minute))
	}
	return p, nil
}
```

Two things in there are deliberate. First, a cache error never fails the request — if Redis is down, we fall through to the database and keep serving. The cache is an optimization, not a dependency; the moment it becomes load-bearing you've built a system that goes down when your *cache* does. Second, the TTL has jitter, which matters more than it looks.

## Jittered TTLs Prevent Synchronized Expiry

If you cache a batch of items with an identical 10-minute TTL — say, everything loaded at deploy time — they all expire in the same second, and the next second every request is a miss hammering the database simultaneously. Spreading expirations over a window fixes it:

```go
func ttlWithJitter(base time.Duration) time.Duration {
	jitter := time.Duration(rand.Int63n(int64(base) / 5)) // up to +20%
	return base + jitter
}
```

Cheap, and it turns a synchronized thundering herd into a smooth trickle of refreshes.

## The Stampede That Takes Down the Database

Jitter handles many keys expiring together. The nastier case is *one* hot key expiring. Picture your most popular product's cache entry lapsing during peak traffic: hundreds of concurrent requests all miss at once, and all of them run the same expensive database query to rebuild the same value. The cache didn't reduce load — it batched it into a spike aimed at your database.

The fix is to let only one request rebuild while the others wait for its result. In Go, `singleflight` does exactly this with no extra infrastructure:

```go
import "golang.org/x/sync/singleflight"

var group singleflight.Group

func (s *Service) getProductCoalesced(ctx context.Context, id string) (*Product, error) {
	v, err, _ := group.Do("product:"+id, func() (interface{}, error) {
		return s.repo.GetProduct(ctx, id) // only ONE of N concurrent callers runs this
	})
	if err != nil {
		return nil, err
	}
	return v.(*Product), nil
}
```

`singleflight` collapses N concurrent calls for the same key into one execution and hands every caller the shared result. Under a hot-key miss, your database sees one query instead of hundreds. (This coalesces per-process; for a truly global lock across many instances you'd use a short-lived Redis lock, at the cost of more complexity — I've rarely needed to go that far.)

## Invalidation: The Hard Part

There's an old joke that the two hard problems in computing are cache invalidation and naming things. The joke is right about the first one. The comfortable strategies, worst to best for correctness:

- **TTL only** — never explicitly invalidate; tolerate staleness up to the TTL. Simplest, and fine when slightly stale reads are acceptable (product descriptions, config). The staleness window is a product decision, not a technical one — get someone to sign off on "up to 10 minutes stale."
- **Write-through / write-around invalidation** — on every write, update or delete the cache key. Fresher, but now your write path has to know every key that a piece of data feeds, and if you forget one, it serves stale data forever with no TTL to save you.
- **Event-driven invalidation** — writes emit change events; a consumer invalidates affected keys. Decouples the write path from cache knowledge, at the cost of real infrastructure and eventual (not immediate) freshness.

I default to TTL-only and only add explicit invalidation for data where staleness genuinely hurts, because every invalidation path is a new way to be wrong.

## Trade-offs

```
Strategy         Freshness         Write-path cost    Failure mode
---------------  ---------------   ----------------   ---------------------------
TTL only         stale ≤ TTL       none               serves stale within window
Write invalidate fresh             couples write→keys stale forever if a key missed
Event-driven     near-fresh        infra + consumer   lag; more moving parts
```

The framing that keeps me out of trouble: a cache is a **bet that reads dominate writes and that some staleness is acceptable**. If reads don't dominate, the cache barely helps. If no staleness is acceptable, you're not really caching — you're building a second source of truth, and that's a much harder problem. Name your staleness budget, keep the cache non-load-bearing, coalesce your stampedes, and Redis in front of Postgres is one of the best-value things you can do to a read-heavy service.
