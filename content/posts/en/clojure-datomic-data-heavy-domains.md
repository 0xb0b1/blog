---
title: "Datomic for Data-Heavy Domains: The Database as a Value"
date: 2022-06-27
description: "What Datomic gives a data-heavy domain that a traditional database doesn't: the atomic datom, schema as data, immutable database values you query as pure functions, time-travel and audit for free—and an honest look at where it bites."
tags:
  [
    "clojure",
    "datomic",
    "datalog",
    "database",
    "data-modeling",
    "backend",
    "system-design",
  ]
---

Datomic is the piece of the Clojure ecosystem that changed how I think about data the most, and it's the one I have to work hardest to explain to people who haven't used it. I've been writing Clojure since 2017, and on domains where the *history* of the data matters as much as its current state, Datomic's model stops feeling exotic and starts feeling obviously correct. This post is the explanation I wish I'd been given, including the parts where it isn't the right tool.

## The Atomic Unit Is a Fact

A traditional database's unit is the row, and a row is mutable—you `UPDATE` it and the old value is gone. Datomic's unit is the **datom**: a single atomic fact, a five-tuple of entity, attribute, value, transaction, and whether it was added or retracted.

`[account-42, :account/balance, 100.00, tx-1001, true]`

You never overwrite a datom. Changing a balance *asserts a new datom* and retracts the old one, both stamped with the transaction that did it. The database accretes facts rather than mutating cells. That one design choice is where everything else follows from.

Schema is data too—attributes, not tables. An entity is just whatever attributes have been asserted about it:

```clojure
[{:db/ident       :account/id
  :db/valueType   :db.type/uuid
  :db/cardinality :db.cardinality/one
  :db/unique      :db.unique/identity}
 {:db/ident       :account/balance
  :db/valueType   :db.type/bigdec
  :db/cardinality :db.cardinality/one}
 {:db/ident       :transfer/from
  :db/valueType   :db.type/ref          ; a reference to another entity
  :db/cardinality :db.cardinality/one}]

;; A transaction is data: assert facts, get back a new database
(d/transact conn {:tx-data [{:account/id      (java.util.UUID/randomUUID)
                             :account/balance 100.00M}]})
```

## The Database Is a Value

Here's the unlock. `(d/db conn)` returns an immutable snapshot of the entire database *as a value*. Not a connection you query against a live, shifting server—a stable, immutable thing you hold in your hand. Every query is a pure function of that value.

```clojure
(let [db (d/db conn)]
  (d/q '[:find ?id ?bal
         :where [?a :account/id ?id]
                [?a :account/balance ?bal]
                [(> ?bal 0M)]]
       db))
```

Because `db` is an immutable value, you can pass it to a function, run twenty queries against the exact same snapshot with zero risk of it changing underneath you, compare two database values, or hand it to a pure function that computes something without any notion that a "database" is involved. Testing becomes trivial: a test is a db value and an assertion, no mocking, no shared mutable fixture. For a data-heavy domain full of derived calculations, "the database is a value you pass to pure functions" removes an entire category of concurrency and consistency reasoning.

Queries are Datalog—pattern matching over datoms, with joins expressed as shared logic variables. For hierarchical reads there's Pull, which walks references declaratively:

```clojure
;; A lookup ref [:account/id uuid] identifies the entity; Pull shapes the result
(d/pull db
        '[:account/id :account/balance {:transfer/_from [:transfer/amount]}]
        [:account/id some-uuid])
```

## Time Travel and Audit, for Free

Because facts accrete and every datom carries its transaction, the history is *in the database*, not something you bolt on with an audit table and triggers. The same query runs against a past database value:

```clojure
;; What did the world look like on May 1st? Same query, older db value.
(d/q balances-query (d/as-of (d/db conn) #inst "2022-05-01"))

;; Full change history of an attribute — the ?added flag tells assert from retract
(d/q '[:find ?bal ?tx ?added
       :where [?a :account/balance ?bal ?tx ?added]]
     (d/history (d/db conn)))
```

On a data-heavy domain with compliance or debugging needs, this is enormous. "What was this account's balance when the disputed transfer happened, and which transaction changed it?" is a query, not an archaeology project. I've closed data-discrepancy investigations in minutes that would have been days of log-spelunking on a mutable store.

## Reads Scale; Writes Go Through One Door

Datomic's architecture separates reads from writes in a way that's central to the trade-off. Reads are served from the query engine with the index available locally and heavily cached—so read-heavy workloads scale by adding read capacity, and queries don't contend with each other. Writes, however, are serialized through a single transactor to give you ACID transactions and a globally consistent ordering of facts.

That's the deal in one sentence: **you get consistent, ordered, fully-audited writes and cheap scalable reads, in exchange for a ceiling on write throughput.** For the domains I've used it on—lots of reads, moderate writes, and a hard requirement that the data be correct and traceable—that's an excellent trade. For a write-firehose (high-volume event ingestion, telemetry), it's the wrong shape and I'd reach for something else.

## Where It Bites — Honestly

Datomic is not a universal database, and pretending otherwise is how people end up unhappy with it:

- **It is not an OLAP engine.** Aggregating over tens of millions of datoms for analytics is not what it's built for. Datalog can compute aggregates, but for heavy analytical scans you want a columnar store fed by [ETL](/en/posts/ddia-trade-offs-data-systems-architecture), not Datomic doing double duty.
- **The write ceiling is real.** One transactor means write throughput has a limit you should validate against your peak *before* committing, not after.
- **Storage only grows.** Accretion is the feature and the cost—you keep history, so data volume climbs, and it changes how you plan capacity.
- **Deletion is a real operation.** Because the model is accumulate-only, honoring something like a GDPR "right to be forgotten" isn't a `DELETE`—it's excision, which is deliberate and heavyweight. If your domain has hard deletion requirements, design for that up front.
- **Cardinality-many Pull can explode.** A naive pull across a high-fan-out reference will pull far more than you meant to. You learn to bound your queries.

## When Right, When Wrong

**Right:** a domain where correctness and history matter, reads dominate writes, and you value being able to ask "what was true, and when?" Financial ledgers, anything with an audit obligation, systems where "the database as a value passed to pure functions" simplifies a tangle of derived state.

**Wrong:** high-volume write ingestion, analytics-first workloads that are really OLAP, hard-delete-heavy domains, or a small CRUD app where PostgreSQL is simpler and everyone on the team already knows it.

What Datomic taught me outlasts Datomic itself: treating data as an accreting log of immutable facts, and the current state as a *value derived from that log*, is a lens that shows up all over well-designed systems—event sourcing, change data capture, the systems-of-record-and-derived-data distinction. Datomic just makes it the default instead of something you assemble by hand.

---
