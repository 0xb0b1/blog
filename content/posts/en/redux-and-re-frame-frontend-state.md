---
title: "Redux and re-frame: Two Takes on Front-End State"
date: "2023-09-21"
description: "Redux (React/JS) and re-frame (ClojureScript) start from the same idea — one immutable app state, pure updates, derived views — and diverge in instructive ways. What each gets right, and what re-frame's effects and interceptors add."
tags:
  [
    "frontend",
    "redux",
    "re-frame",
    "clojurescript",
    "state-management",
    "react",
  ]
---

I've built production front ends in both React with Redux and ClojureScript with re-frame, and the interesting thing is how much they agree. They arrive from different language cultures at nearly the same architecture: a single immutable state value, pure functions that compute the next state, and derived views that recompute only when their inputs change. Seeing the same shape twice taught me which parts of that shape are essential and which are incidental. Here's the comparison I wish I'd had before learning both.

## The Shared Core

Both frameworks are built on the same four commitments:

1. **One source of truth** — the entire app state is a single immutable value (Redux's store, re-frame's `app-db`).
2. **You don't mutate it** — you dispatch a described intent (an *action* / an *event*) and a pure function returns the next state.
3. **Views are derived** — components read from the state through selectors/subscriptions that memoize, so a component re-renders only when the specific slice it reads changes.
4. **Data flows one way** — event → state update → derived views → UI → event. No component reaches sideways into another's state.

If you internalize that in one framework, 80% transfers to the other. It's the genuinely important idea, and it's language-independent.

## Redux, in Its Modern Form

Modern Redux (with Redux Toolkit) is a `slice`: a piece of state plus the pure reducers that update it. Under the hood it uses Immer so you *write* mutating-looking code that actually produces a new immutable state:

```javascript
const ordersSlice = createSlice({
  name: "orders",
  initialState: { items: [], filter: "all" },
  reducers: {
    filterChanged(state, action) {
      state.filter = action.payload; // Immer makes this immutable under the hood
    },
  },
});

// Derived view (reselect): recomputes only when items/filter change
const selectVisible = createSelector(
  [(s) => s.orders.items, (s) => s.orders.filter],
  (items, filter) =>
    filter === "all" ? items : items.filter((o) => o.status === filter)
);
```

## re-frame, the Same Shape in ClojureScript

re-frame registers an *event handler* (a pure function from `db` to new `db`) and a *subscription* (a derived, memoized view). Because ClojureScript data is immutable by default, there's no Immer equivalent — you just return a new value with `assoc`:

```clojure
(rf/reg-event-db
  :filter-changed
  (fn [db [_ filter]]
    (assoc db :filter filter)))

;; Derived view — recomputes only when :items or :filter change
(rf/reg-sub
  :visible-orders
  :<- [:items]
  :<- [:filter]
  (fn [[items filter] _]
    (if (= :all filter)
      items
      (filterv #(= filter (:status %)) items))))
```

Line for line, that's the Redux slice + reselect selector, in a language where immutability didn't need a library bolted on. I wrote about pushing this subscription model hard for large datasets in [the re-frame rendering post](/en/posts/clojure-reframe-data-heavy-rendering-performance).

## Where re-frame Goes Further: Effects and Coeffects

Here's the divergence worth understanding. In Redux, a reducer must be pure, so anything impure — an API call, reading `localStorage`, generating a timestamp — happens in *middleware* (thunks, sagas), and the boundary between "pure update" and "side effect" is a convention you maintain.

re-frame makes that boundary explicit and data-driven. An event handler doesn't *perform* effects; it *returns a description* of the effects it wants, and re-frame's registered effect handlers carry them out:

```clojure
(rf/reg-event-fx                 ; -fx: returns a map of effects, performs nothing
  :save-order
  (fn [{:keys [db]} [_ order]]
    {:db   (assoc db :saving true)         ; the state change
     :http {:method :post :url "/orders"   ; a described side effect
            :body order
            :on-success [:save-succeeded]}}))
```

The handler stays pure — it returns data — and the actual HTTP call lives in a reusable effect handler. Similarly, *coeffects* inject impure inputs (current time, a random id, localStorage) as data, so even reading the outside world doesn't make the handler impure. The upshot: re-frame event handlers are pure and therefore trivially testable — feed them a `db` and a coeffects map, assert on the effects map they return, no mocking. Redux Toolkit gets much of the way there, but re-frame's "effects are just data you return" is the cleaner expression of the idea.

## Trade-offs

```
Aspect              Redux (Toolkit)              re-frame (ClojureScript)
------------------  ---------------------------  ----------------------------
Immutability        via Immer (write "mutating") native (assoc returns new)
Derived views       reselect selectors           layered subscriptions
Side effects        middleware (thunks/sagas)    effects/coeffects as data
Handler purity      reducers pure, effects out   handlers pure, effects returned
Ecosystem           huge (React/JS)              small (CLJS), very coherent
Onboarding          familiar to JS devs          need to learn ClojureScript
```

## The Takeaway

Redux is the pragmatic choice: it's where your React team already is, the ecosystem is vast, and Toolkit has sanded off most of the historical boilerplate. re-frame is the more *coherent* expression of the same architecture — effects-as-data and native immutability make it feel like the idea taken to its logical end — but it asks you to adopt ClojureScript, which is a real cost for a JS-native team. What convinced me is that both landed on the same core, which means the core is the thing to learn: single immutable state, pure updates, derived memoized views, one-way flow. Pick the framework by your team and ecosystem; the architecture is the transferable asset.
