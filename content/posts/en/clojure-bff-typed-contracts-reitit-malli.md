---
title: "The BFF Pattern in Clojure: Typed Contracts for a Data-Heavy Frontend"
date: 2021-04-19
description: "Building a backend-for-frontend in Clojure with Ring, Reitit, and malli: data-driven routing, typed HTTP contracts without a static type system, parallel fan-out to downstream services, and graceful degradation when one of them is slow."
tags:
  [
    "clojure",
    "reitit",
    "malli",
    "bff",
    "api-design",
    "backend",
    "system-design",
  ]
---

I've been writing Clojure since 2017, and the pattern I keep reaching for on data-heavy products is the backend-for-frontend (BFF). Not because it's fashionable, but because the alternative—a rich frontend talking directly to a handful of services—turns every screen into a waterfall of requests, each returning more data than the UI needs, none of it shaped the way the view wants it.

This post is about how I build a BFF in Clojure, and why the language's data-orientation makes two of the hardest parts—routing and typed contracts—almost boring, in the good way.

## The Problem a BFF Actually Solves

Picture a dashboard screen. To render it, the frontend needs metadata about the dashboard, the widgets it contains, the current user's permissions, and a first page of data for each widget. Behind the scenes that's four or five different services, each with its own API, its own pagination, its own idea of what a timestamp looks like.

Without a BFF, the browser orchestrates all of that: five requests, five round trips over a mobile connection, and a pile of client-side code stitching the responses together and reshaping them for the view. The frontend becomes an integration layer. Every backend change ripples into the client.

A BFF moves that orchestration server-side. The frontend makes **one** request to an endpoint that exists specifically for that screen. The BFF fans out to the downstream services in parallel, shapes the result into exactly what the view needs, and returns a single typed payload. The client gets dumber and faster; the contract gets explicit.

The two things you have to get right are the **contract** (what the endpoint promises the frontend) and the **aggregation** (how it talks to everything downstream without falling over). Clojure has a clean answer for both.

## Routing as Data with Reitit

Reitit describes routes as plain data—a vector of vectors. That sounds like a small thing. It isn't. Because routes are data, the contract, coercion rules, and documentation all live in the same structure the router walks, and you can inspect or generate any of it.

```clojure
(ns bff.routes
  (:require [reitit.ring :as ring]
            [reitit.coercion.malli]
            [reitit.ring.coercion :as coercion]
            [reitit.ring.middleware.muuntaja :as muuntaja]
            [muuntaja.core :as m]))

(def routes
  [["/api/dashboards/:id"
    {:get {:summary    "Everything one dashboard screen needs, in one call"
           :parameters {:path  [:map [:id :uuid]]
                        :query [:map [:page {:optional true} [:int {:min 0}]]]}
           :responses  {200 {:body DashboardView}}
           :handler    dashboard-handler}}]])

(def app
  (ring/ring-handler
    (ring/router
      routes
      {:data {:coercion   reitit.coercion.malli/coercion
              :muuntaja   m/instance
              :middleware [muuntaja/format-middleware
                           coercion/coerce-exceptions-middleware
                           coercion/coerce-request-middleware
                           coercion/coerce-response-middleware]}})))
```

The `:parameters` and `:responses` keys aren't documentation. They're enforced. The coercion middleware parses the `:id` path segment into a real `java.util.UUID` before your handler runs, rejects a request whose `:page` isn't a non-negative integer with a 400 and a machine-readable explanation, and—this is the part people underestimate—validates your *own response* against `DashboardView` on the way out.

## Typed Contracts Without a Type System

Clojure is dynamically typed, and the reflexive worry from someone coming out of a static language is: how do you keep an API honest without a compiler? The answer I settled on is [malli](https://github.com/metosin/malli): schemas that are themselves data.

```clojure
(def Widget
  [:map
   [:id       :uuid]
   [:kind     [:enum :chart :table :metric]]
   [:title    :string]
   [:data-url :string]])

(def DashboardView
  [:map
   [:id          :uuid]
   [:title       :string]
   [:updated-at  inst?]
   [:can-edit    :boolean]
   [:widgets     [:vector Widget]]])
```

A malli schema is a value. I can validate with it, `m/explain` a failure into something the frontend can display field-by-field, generate example data from it for tests, coerce loosely-typed JSON into the right Clojure types, and—via `reitit`—emit an OpenAPI document straight from the same schemas that guard the endpoint. One source of truth, and it's the same data the router already holds:

```clojure
(require '[reitit.openapi :as openapi])

;; The API docs are derived from the route data, never hand-written,
;; so they cannot drift from what the endpoint actually enforces.
["/openapi.json" {:get {:handler (openapi/create-openapi-handler)}}]
```

This is the pitch I'd make to anyone skeptical of dynamic typing on a service boundary: I get runtime-enforced contracts, generated docs, and generative tests from *one* declaration, and because it's data rather than syntax, I can compose and transform it. A static type is checked once at compile time and then it's gone; a malli schema is a value I can keep using. The trade-off is honest—the check happens at runtime, not compile time—but on an HTTP boundary, where the input is untrusted JSON anyway, runtime is exactly where you want the check.

## Aggregation: Fan Out in Parallel, Shape at the Edge

The handler's job is to gather from several downstream services and assemble the `DashboardView`. Doing that sequentially would make the BFF slower than the chatty client it replaced, so the calls have to run in parallel. For fanning out a handful of independent I/O calls, Clojure `future`s with a bounded `deref` timeout are the simplest correct tool:

```clojure
(defn dashboard-handler
  [{{{:keys [id]} :path {:keys [page]} :query} :parameters}]
  (let [meta-f    (future (svc/dashboard-meta id))
        widgets-f (future (svc/widgets id))
        perms-f   (future (svc/permissions id))
        ;; deref with a timeout so one slow service can't hang the request
        meta      (deref meta-f    2000 ::timeout)
        widgets   (deref widgets-f 2000 ::timeout)
        perms     (deref perms-f   2000 ::timeout)]
    (if (some #{::timeout} [meta widgets perms])
      {:status 504 :body {:error "upstream-timeout"}}
      {:status 200
       :body   (shape-dashboard meta widgets perms page)})))
```

The three `future`s start immediately and run concurrently; the request takes about as long as the slowest downstream call, not the sum of all of them. `(deref f 2000 ::timeout)` is the whole backpressure story for this shape of work—no single upstream can hold the request open past two seconds.

`shape-dashboard` is where the BFF earns its keep. It's pure data transformation: take three service responses in three different shapes and produce the one shape the frontend declared. This is Clojure's home turf—`select-keys`, `update`, `map`, a little `clojure.set/rename-keys`—no mapping framework, no DTO classes, just functions on maps.

```clojure
(defn shape-dashboard [meta widgets perms page]
  {:id         (:dashboard/id meta)
   :title      (:dashboard/title meta)
   :updated-at (:dashboard/updated-at meta)
   :can-edit   (contains? (:permissions perms) :dashboard/edit)
   :widgets    (->> widgets
                    (sort-by :position)
                    (mapv #(select-keys % [:id :kind :title :data-url])))})

;; Because the route declares :responses {200 {:body DashboardView}},
;; a mistake here—a missing key, a stringified UUID—fails the response
;; coercion in development instead of reaching the client.
```

For heavier cases—many more than a handful of downstreams, or streaming results—I reach for [manifold](https://github.com/clj-commons/manifold)'s deferreds (`d/zip`, `let-flow`) for non-blocking composition, or `core.async` when the work is a genuine pipeline rather than a fan-out. But I don't start there. `future` + `deref` covers the common BFF case and stays readable, and readable is what you want in the layer everyone touches.

## Failure Handling Is the Real Design

The version above fails the whole request if any downstream times out. Sometimes that's right—a dashboard with no widgets is useless. But often the better product decision is to **degrade**: render what you have, mark what you don't.

```clojure
(defn ->widgets [widgets-result]
  (if (= ::timeout widgets-result)
    {:widgets [] :widgets-degraded true}   ; tell the UI to show a retry affordance
    {:widgets (shape-widgets widgets-result)}))
```

This is a decision the BFF is uniquely positioned to make, because it's the only place that knows both what the frontend needs and which downstreams answered. Bury it in the client and every screen reinvents it; bury it in the downstream services and none of them has the context. The BFF owns the "what does this screen do when service X is down?" question, and answering it explicitly—per screen—is most of what makes a data-heavy UI feel solid instead of flaky.

## The Trade-Offs, Named

A BFF is not free, and the Clojure version doesn't change the shape of the costs:

- **It couples to the frontend.** The endpoint exists to serve one client's screen. When the screen changes, the BFF changes. That's the point—but it means the BFF team and the frontend team move together, which is an org decision as much as a technical one.
- **It's another hop and another deploy.** You've added a service. If your frontend talks to exactly one backend that already returns the right shape, a BFF is premature—you'd be adding a network boundary to solve a problem you don't have.
- **It can become a dumping ground.** "Just put it in the BFF" is tempting for any logic that's awkward elsewhere. The discipline is that the BFF *orchestrates and shapes*; it doesn't own business rules. The moment it starts making authoritative decisions about data, it's turning into the distributed monolith you were trying to avoid.

**When it's right:** a data-heavy frontend, several downstream services, and shapes that don't match what any single service returns. **When it's wrong:** one backend, one client, and a payload that's already close to what the view needs.

## Why Clojure, Specifically

Every part of this pattern is data. Routes are data (Reitit). Contracts are data (malli). The transformation from service responses to the view is data-in, data-out (plain functions on maps). There's no routing DSL to learn, no annotation processor, no code generation step, no DTO layer mirroring your schemas in three places. The contract you validate against is the same value you generate docs from and the same value you write tests against.

That's the through-line of why I keep coming back to Clojure for this layer: the BFF is fundamentally a data-shaping problem, and Clojure treats programs as data manipulation all the way down. The language and the problem are the same shape.

---
