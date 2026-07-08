---
title: "Idempotency Keys for High-Volume Order APIs in Go"
date: "2020-03-10"
description: "How to make order and payment endpoints safe to retry: client-supplied idempotency keys, a Postgres-backed store that survives concurrent duplicates, and the trade-offs around scope, TTL, and in-flight requests — in Go."
tags:
  [
    "golang",
    "api",
    "idempotency",
    "backend",
    "postgresql",
  ]
---

The bug that taught me to care about idempotency was a double charge. A customer tapped "Pay" on a flaky connection, the request timed out client-side, the app retried, and our API happily created two orders. The first request had actually succeeded — the response just never made it back. From the server's point of view there were two legitimate requests. From the customer's, there was one intention and two charges.

You cannot fix this by being more careful. On any network, a client that doesn't get a response has exactly two choices: give up, or retry. If retrying can double-apply an effect, retrying is dangerous, so clients give up — and now you've traded double charges for lost orders. The real fix is to make the operation safe to retry. That's what an idempotency key buys you.

## The Pattern

The client generates a unique key (a UUID) for each *intended* operation and sends it with the request, conventionally in an `Idempotency-Key` header. It reuses the same key on retries of that same intention. The server remembers what it did for each key: the first time it sees a key, it does the work and stores the result; every subsequent time, it returns the stored result without redoing anything.

The key represents intent, not a request. Ten retries of "place this order" carry one key and cause one order.

## A Store That Survives Concurrency

The naive implementation — "look up the key; if absent, do the work; store the result" — has a race. Two retries can both run the lookup, both miss, and both do the work. The store has to make first-writer-wins atomic, and a unique constraint in Postgres does exactly that:

```sql
CREATE TABLE idempotency_keys (
    key          TEXT        PRIMARY KEY,
    endpoint     TEXT        NOT NULL,
    state        TEXT        NOT NULL,   -- 'in_progress' | 'completed'
    status_code  INT,
    response     JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

The flow: try to *claim* the key by inserting an `in_progress` row. If the insert wins, this goroutine owns the operation. If it hits a unique violation, someone else got there first — we look at their row and either replay their completed response or tell the client the original is still running.

```go
var errInProgress = errors.New("idempotent request already in progress")

// claim attempts to reserve the key. It returns (replay, nil) if the work
// was already completed, (nil, errInProgress) if a duplicate is mid-flight,
// or (nil, nil) if this caller now owns the operation.
func (s *Store) claim(ctx context.Context, key, endpoint string) (*stored, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO idempotency_keys (key, endpoint, state)
		 VALUES ($1, $2, 'in_progress')`, key, endpoint)
	if err == nil {
		return nil, nil // we own it
	}
	if !isUniqueViolation(err) {
		return nil, err
	}

	// Someone else claimed it first — inspect their row.
	var st stored
	err = s.db.QueryRowContext(ctx,
		`SELECT endpoint, state, status_code, response
		 FROM idempotency_keys WHERE key = $1`, key).
		Scan(&st.endpoint, &st.state, &st.statusCode, &st.response)
	if err != nil {
		return nil, err
	}
	if st.endpoint != endpoint {
		return nil, errKeyReused // same key, different operation — client bug
	}
	if st.state == "completed" {
		return &st, nil
	}
	return nil, errInProgress
}
```

Wrapping it in an HTTP middleware keeps handlers oblivious:

```go
func Idempotent(store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" { // opt-in: no key, no protection
				next.ServeHTTP(w, r)
				return
			}
			replay, err := store.claim(r.Context(), key, r.URL.Path)
			switch {
			case errors.Is(err, errInProgress):
				http.Error(w, "request in progress", http.StatusConflict)
				return
			case err != nil:
				http.Error(w, "idempotency error", http.StatusInternalServerError)
				return
			case replay != nil:
				w.Header().Set("Idempotent-Replay", "true")
				w.WriteHeader(replay.statusCode)
				w.Write(replay.response)
				return
			}
			// We own the operation: capture the response, then persist it.
			rec := &recorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(rec, r)
			store.complete(r.Context(), key, rec.status, rec.body.Bytes())
		})
	}
}
```

The `recorder` is a thin `http.ResponseWriter` wrapper that captures the status and body so we can store them for future replays.

## The Decisions That Actually Matter

The code is the easy part. The design questions are where high-volume systems get bitten:

**What happens to a crashed `in_progress`?** If the owning process dies after claiming but before completing, the key is stuck. Every retry sees `in_progress` and gets a 409 forever. You need a reaper: treat `in_progress` rows older than some timeout as abandoned and let a new caller take over. That timeout has to exceed your longest legitimate operation, or you'll re-run work that's still running.

**How long do you keep keys?** Forever is simplest and wrong — the table grows without bound. A TTL (say 24–72h) covers realistic retry windows. Past that, a returning key is treated as new. The window is a bet: long enough to catch retries, short enough to bound storage.

**Is the operation actually deterministic?** Replaying a stored `201 Created` is only correct if the client wants the *original* result. If your endpoint embeds a server timestamp or a freshly generated ID in the response, replay returns the old one — usually what you want, occasionally surprising.

## Trade-offs

```
Approach                     Safe under concurrency   Storage cost   Complexity
---------------------------  ----------------------   ------------   ----------
Do nothing (hope)            no                       none           none
Dedup on natural key         partial (needs one)      none           low
Idempotency key + unique     yes                      1 row/op+TTL   medium
Idempotency key + lock/wait  yes, but blocks          1 row/op+TTL   high
```

The unique-constraint approach is the sweet spot for order and payment APIs: correctness under concurrent retries, bounded storage with a TTL, and the database — not application locks — enforcing first-writer-wins. **When it's overkill:** naturally idempotent operations (a `PUT` that sets state to an absolute value is already idempotent; it doesn't need a key). **When it's not enough:** if the *side effect* lives in another system (a real payment processor), you also need that downstream call to be idempotent, because your key only protects your own database. Idempotency has to run the full length of the causal chain, or the weakest link double-applies.

The lesson that stuck: retries are not an edge case to be minimized, they're the normal behavior of every honest client on an imperfect network. Design the write path so a retry is a no-op, and a whole category of 3 a.m. incidents disappears.
