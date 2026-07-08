---
title: "PostgreSQL Indexing for High-Volume Services"
date: "2021-02-10"
description: "A practical guide to indexing Postgres under load: how B-tree indexes actually get used, why composite column order matters, covering and partial indexes, reading EXPLAIN, and the write-side cost that makes 'just add an index' the wrong default."
tags:
  [
    "postgresql",
    "database",
    "performance",
    "backend",
  ]
---

Most "the database is slow" incidents I've chased ended at the same place: a query doing a sequential scan over a table that had grown past the point where that was fine. Adding the right index fixed it in seconds. But "add an index" is advice that's easy to get wrong in three different directions — the wrong index, an index that can't be used, or an index that quietly taxes every write. This is what I've learned keeping Postgres fast under real volume.

## Read EXPLAIN Before You Touch Anything

Never add an index on a hunch. Ask Postgres what it's doing:

```sql
EXPLAIN ANALYZE
SELECT * FROM orders
WHERE customer_id = 42 AND status = 'paid'
ORDER BY created_at DESC
LIMIT 20;
```

The words that matter in the output: `Seq Scan` means it read the whole table; `Index Scan` means it used an index to find rows; `Index Only Scan` means it answered entirely from the index without touching the table (the fastest case). `ANALYZE` runs the query and shows real timings and row counts — a big gap between estimated and actual rows usually means stale statistics (`ANALYZE orders;`) and is worth fixing before you blame the index.

## Composite Index Column Order Is the Whole Game

For the query above, the tempting move is three separate indexes on `customer_id`, `status`, and `created_at`. Postgres can combine them with a bitmap, but a single well-ordered composite index is far better:

```sql
CREATE INDEX idx_orders_cust_status_created
    ON orders (customer_id, status, created_at DESC);
```

The rule that took me too long to internalize: **equality columns first, then the range/sort column last.** The query filters `customer_id` and `status` by equality and orders by `created_at`. With this column order, Postgres jumps straight to the `customer_id = 42, status = 'paid'` slice and reads it in `created_at DESC` order — no separate sort step. Flip the order to `(created_at, customer_id, status)` and the index is nearly useless for this query, because the leading column isn't what you're filtering on. A composite index can serve any query that uses a *prefix* of its columns, so column order isn't a detail — it decides which queries the index can help at all.

## Covering Indexes: Answer Without the Table

An index scan normally finds matching rows in the index, then visits the table (the "heap") to fetch the columns you selected. If the index already contains every column the query needs, Postgres skips the heap entirely — an index-only scan. Since Postgres 11 you can add non-key columns purely to enable this:

```sql
CREATE INDEX idx_orders_cust_covering
    ON orders (customer_id, status) INCLUDE (total, created_at);
```

Now `SELECT total, created_at FROM orders WHERE customer_id = 42 AND status = 'paid'` can be answered from the index alone. On a hot query over a wide table, cutting the heap fetch is a real win. The cost is a fatter index — you're duplicating those columns — so reserve covering indexes for genuinely hot paths, not everything.

## Partial Indexes: Index Only What You Query

If your queries always filter on a value, don't index the rows you never look for:

```sql
CREATE INDEX idx_orders_pending
    ON orders (created_at)
    WHERE status = 'pending';
```

In an orders table that's 95% `completed`, a partial index on the pending rows is a fraction of the size, stays in memory, and is cheaper to maintain. This is one of Postgres's best features and the most underused — any time you have a "hot subset" (unprocessed jobs, active sessions, soft-deleted = false), a partial index targets exactly it.

## The Cost Nobody Mentions

Every index makes writes slower. An `INSERT` or an `UPDATE` that touches an indexed column has to update every relevant index, synchronously, inside the transaction. On a write-heavy table, five indexes means five index maintenance operations per write. I've seen a well-meaning "let's index everything the query planner might want" turn a fast ingest path into a bottleneck.

Indexes also **bloat**. Under heavy update/delete churn, B-tree indexes accumulate dead entries and grow; `REINDEX CONCURRENTLY` (Postgres 12+) rebuilds them without locking out writes. And building an index on a live high-volume table locks it against writes unless you use `CREATE INDEX CONCURRENTLY` — slower to build, but it doesn't take your table offline.

## Trade-offs

```
Index type      Read benefit               Write/space cost      Use when
--------------  -------------------------  -------------------   ----------------------------
B-tree single   equality/range on 1 col    low                   simple lookups
Composite       multi-col filter + sort    medium                fixed query shape, order matters
Covering        index-only scan            higher space          hot query over a wide table
Partial         tiny, hot-subset lookups   low                   queries always filter a subset
```

The mental model that keeps me honest: an index is a bet that the read savings outweigh the write cost, paid on every single write forever. For a table read a thousand times per write, index generously. For a write-firehose read occasionally, index reluctantly and measure. "Add an index" is never free — it's a trade, and `EXPLAIN ANALYZE` plus a look at your write rate is how you price it before you commit.
