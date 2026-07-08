---
title: "Making a Data-Heavy re-frame UI Fast: Subscriptions, Re-renders, and Windowing"
date: 2020-09-14
description: "How I keep a data-heavy re-frame/Reagent single-page app responsive: layering the subscription signal graph, pushing derefs down so only changed components re-render, and windowing large tables instead of rendering everything."
tags:
  [
    "clojure",
    "clojurescript",
    "re-frame",
    "reagent",
    "frontend",
    "performance",
    "web-rendering",
  ]
---

I've been writing Clojure since 2017, and a lot of that has been ClojureScript on the front of data-heavy products—the kind where a single screen holds a table of thousands of rows, live filters, and a handful of charts, all reading from one big app-db. re-frame makes that pleasant to build. It also makes it easy to build something that janks on every keystroke if you don't understand how the reactive graph actually recomputes.

This is what I learned about keeping those UIs fast, framed around the three mistakes I've made and then stopped making.

## The Mental Model: A Signal Graph That Memoizes

re-frame subscriptions form a directed graph. `app-db` is the root. Each `reg-sub` is a node that derives a value from other nodes, and—this is the whole point—a node only recomputes when one of its inputs actually changes. Under the hood these are Reagent reactions, which memoize their last value and only fire downstream when that value differs.

A Reagent component, in turn, re-renders only when a reactive value it *dereferences* changes. Not when app-db changes. When the specific reaction it reads changes.

Almost every performance problem I've hit is a violation of one of those two facts: either a subscription recomputes too much, or a component re-renders because it's reading something coarser than what it needs.

## Mistake 1: The God Subscription

The tempting first version puts all the derivation in one place:

```clojure
;; Everything the table needs, computed in one subscription
(rf/reg-sub
  :table-data
  (fn [db _]
    (let [rows    (:rows db)
          filter- (:filter-text db)
          sort-k  (:sort-key db)]
      (->> rows
           (filter #(str/includes? (:name %) filter-))
           (sort-by sort-k)))))
```

This recomputes the *entire* filter-and-sort every time **any** of its inputs changes—and because it reads `db` directly, it's re-run on every single app-db change, even ones unrelated to the table. Type one character into the filter box and you re-sort thousands of rows.

The fix is to layer the graph the way re-frame intends: cheap **extraction** subscriptions that just pull a slice out of app-db, and **materialization** subscriptions that derive from *those* and recompute only when their specific inputs change.

```clojure
;; Layer 2 — extraction: trivial, cheap, re-run only when that key changes
(rf/reg-sub :rows        (fn [db _] (:rows db)))
(rf/reg-sub :filter-text (fn [db _] (:filter-text db)))
(rf/reg-sub :sort-key    (fn [db _] (:sort-key db)))

;; Layer 3 — materialization: subscribes to Layer 2, not to db
(rf/reg-sub
  :filtered-rows
  :<- [:rows]
  :<- [:filter-text]
  (fn [[rows filter-text] _]
    (filterv #(str/includes? (:name %) filter-text) rows)))

(rf/reg-sub
  :visible-rows
  :<- [:filtered-rows]
  :<- [:sort-key]
  (fn [[rows sort-key] _]
    (sort-by sort-key rows)))
```

Now changing `:sort-key` reruns only the sort, over the already-filtered set. Changing an unrelated part of app-db reruns nothing here, because none of these subscriptions read `db` directly anymore. The graph does the minimum work each change requires. This layering is the single highest-leverage thing you can do in a re-frame app, and it costs nothing but discipline.

## Mistake 2: Dereferencing Too High in the Tree

Even with a clean signal graph, *where* you deref decides what re-renders. If the table component derefs the whole row collection, then any change to any row re-renders the whole table:

```clojure
;; Every row re-renders when any row changes
(defn table []
  (let [rows @(rf/subscribe [:visible-rows])]
    [:table
     [:tbody
      (for [r rows]
        ^{:key (:id r)} [:tr [:td (:name r)] [:td (:value r)]])]]))
```

Push the deref down. The parent subscribes only to the *ids* (a cheap value that changes rarely); each row subscribes to its own slice by id and re-renders in isolation:

```clojure
(rf/reg-sub :visible-row-ids
  :<- [:visible-rows]
  (fn [rows _] (mapv :id rows)))

(rf/reg-sub :row
  :<- [:rows-by-id]
  (fn [rows-by-id [_ id]] (get rows-by-id id)))

(defn row-view [id]
  (let [r @(rf/subscribe [:row id])]        ; each row owns its subscription
    [:tr [:td (:name r)] [:td (:value r)]]))

(defn table []
  (let [ids @(rf/subscribe [:visible-row-ids])]   ; re-renders only when the set of ids changes
    [:table
     [:tbody
      (for [id ids]
        ^{:key id} [row-view id])]]))
```

Update one row's value and exactly one `<tr>` re-renders. The `^{:key id}` metadata isn't decoration—it's what lets React's reconciler match rows across renders instead of tearing down and rebuilding the list. Get the key wrong (index, or missing) and you pay for DOM churn you didn't need.

## Mistake 3: Rendering Rows Nobody Can See

Layered subscriptions and per-row components still won't save you from asking the DOM to hold 10,000 `<tr>` nodes. The browser doesn't care how elegant your signal graph is; it has to lay out and paint every node you give it.

The answer is windowing: render only the rows currently in the viewport, plus a small buffer, and translate the scroll position into a slice.

```clojure
(defn windowed-table [row-height buffer]
  (let [scroll-top (r/atom 0)
        view-h     (r/atom 600)]
    (fn []
      (let [ids   @(rf/subscribe [:visible-row-ids])
            total (count ids)
            start (max 0 (- (quot @scroll-top row-height) buffer))
            end   (min total (+ (quot (+ @scroll-top @view-h) row-height) buffer))]
        [:div.viewport
         {:style {:height @view-h :overflow-y "auto"}
          :on-scroll #(reset! scroll-top (.. % -target -scrollTop))}
         ;; spacer preserves the real scrollbar height
         [:div {:style {:height (* total row-height) :position "relative"}}
          (for [id (subvec ids start end)]
            ^{:key id}
            [:div {:style {:position "absolute"
                           :top (* (.indexOf ids id) row-height)
                           :height row-height}}
             [row-view id]])]]))))
```

For production I'd usually reach for a battle-tested library via interop—`react-window` wraps cleanly in a Reagent component—rather than hand-roll the edge cases (variable row heights, momentum scrolling). But the principle is the point: the cost of a list should scale with what's *visible*, not with what *exists*.

## The Interop Tax Nobody Warns You About

One data-heavy-specific trap: if your data arrives as JSON and you `js->clj` a large payload, that conversion is expensive and allocates a whole parallel structure. On a few kilobytes it's nothing; on a 5 MB response it's a visible stall. Options, roughly in order of how often I use them: keep the hot data as native JS and reach into it only where needed, convert lazily/incrementally, or push the shaping to the server (a [BFF](/en/posts/clojure-bff-typed-contracts-reitit-malli) is a good place to do exactly this) so the client receives something small and already-shaped.

## Trade-Offs, Named

- **Layering adds indirection.** Three subscriptions where you started with one. Worth it when data is large or updates are frequent; overkill for a static settings page. Match the machinery to the load.
- **Per-row subscriptions have overhead too.** Each subscription is a reaction with its own bookkeeping. For a handful of rows, the god-subscription is genuinely fine and simpler. This pays off at scale, not below it.
- **Windowing complicates the DOM.** Absolute positioning, scroll math, and "jump to row" become your problem. Don't windowize a 50-row table.

The meta-point is that re-frame gives you a precise cost model—the signal graph tells you exactly what recomputes and what re-renders—and fast data-heavy UIs come from working *with* that model rather than fighting it. When something janks, I don't guess; I ask which subscription is recomputing and which component is re-rendering, and the answer is almost always one of these three mistakes.

---
