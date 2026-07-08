---
title: "ClojureScript for React Developers"
date: "2024-03-07"
description: "If you already know React, most of ClojureScript is closer than you think. Reagent is React with immutable data and hiccup instead of JSX — here's the mental map, the interop story, and why reference-equality re-rendering is the quiet win."
tags:
  [
    "clojurescript",
    "react",
    "frontend",
    "reagent",
    "functional-programming",
  ]
---

If you write React, you already understand most of ClojureScript's front-end story — you just don't know it yet. Reagent, the dominant ClojureScript UI library, *is* React underneath. Components are functions that return markup, state triggers re-renders, the virtual DOM diffs and patches. What changes is the language around it: immutable data by default, s-expressions instead of JSX, and a build tool you drive from a REPL. This is the map I'd give my past React self.

## Components Are Still Just Functions Returning Markup

A React function component returns JSX. A Reagent component returns *hiccup* — plain ClojureScript vectors describing the DOM. Keyword for the tag, a map for props, then children:

```clojure
(defn greeting [name]
  [:div.greeting                      ; <div class="greeting">
   [:h1 "Hello, " name]
   [:button {:on-click #(js/alert "hi")} "Wave"]])
```

That's `<div className="greeting"><h1>Hello, {name}</h1><button onClick={...}>Wave</button></div>`. No JSX transform, no template DSL — the markup is data structures your language already has, which means you manipulate views with the same `map`/`filter`/`assoc` you use on any other data:

```clojure
(defn order-list [orders]
  [:ul
   (for [o orders]
     ^{:key (:id o)} [:li (:name o) " — $" (:total o)])])   ; map over data → children
```

The `^{:key ...}` is React's reconciliation key, same concept as `key={}` in JSX.

## State: ratoms Instead of useState

React's `useState` gives you a value and a setter that triggers a re-render. Reagent's equivalent is a *reactive atom* (`r/atom`): a mutable container of an immutable value. Deref it (`@`) to read; any component that derefs it re-renders when it changes.

```clojure
(defn counter []
  (let [n (r/atom 0)]                 ; created once (form-2 component)
    (fn []                            ; this render fn re-runs on change
      [:div
       [:span "Count: " @n]           ; deref → this component tracks n
       [:button {:on-click #(swap! n inc)} "+"]])))
```

The one gotcha for React devs: this is a "form-2" component — the outer `let` runs once (like the body of a `useState` setup), and the returned inner function is what re-renders. Mix that up and your atom resets every render. Past that, the model is exactly React's: local state in a ratom, shared app state in something like re-frame, which I compared to Redux [here](/en/posts/redux-and-re-frame-frontend-state).

## The Quiet Win: Immutable Data Makes Re-renders Cheap

Here's the payoff that isn't obvious until you've felt it. In React, avoiding needless re-renders means `memo`, `useMemo`, `useCallback`, and careful dependency arrays — because with mutable JS objects, "did this prop actually change?" can't be answered by a cheap reference check (two different objects can be structurally equal, and one mutated object is reference-equal to its old self).

ClojureScript data is immutable and persistent, so "did this change?" *is* a reference-equality check, and it's always correct: if the reference is the same, the value is unchanged, guaranteed. Reagent leans on this to skip re-rendering subtrees whose inputs are identical, with none of the manual memoization bookkeeping. The thing you fight in React — unnecessary renders from reference churn — mostly evaporates because equality is cheap and honest. (This is the same property that makes the big-dataset rendering techniques in [the re-frame performance post](/en/posts/clojure-reframe-data-heavy-rendering-performance) work.)

## Interop Is a First-Class Citizen

You do not lose the React ecosystem. You can drop any React component into hiccup with the `:>` adapter, passing props as a map:

```clojure
(ns app.core
  (:require ["react-select" :default Select]   ; import an npm package
            [reagent.core :as r]))

(defn country-picker [options]
  [:> Select {:options options                 ; render a JS React component
              :on-change #(js/console.log %)}])
```

`js/console.log`, `(.-property obj)` for field access, `(.method obj args)` for calls — interop with JavaScript is a normal, everyday part of the language, not an escape hatch. You reach for npm libraries when they're the best tool and write the rest in ClojureScript.

## The Build: shadow-cljs

The tool that makes this pleasant for React developers is `shadow-cljs`, because it treats npm as a first-class dependency source — you `npm install` a package and `require` it directly. It gives you hot reloading (edit a component, see it update with state preserved) and a REPL connected to the running browser app, so you evaluate expressions live against your actual UI. Coming from a Webpack/Vite setup, the config is smaller and the npm story is smoother than you'd expect from a "different language" toolchain.

## Trade-offs

```
Concern            React (JS/TS)             ClojureScript (Reagent)
-----------------  ------------------------  ----------------------------
Markup             JSX (transform)           hiccup (plain data)
Local state        useState                  r/atom (ratom)
Avoiding renders   memo/useMemo/useCallback  ~free via reference equality
npm ecosystem      native                    native via shadow-cljs + interop
Types              TypeScript                dynamic (spec/malli optional)
Hiring/onboarding  huge pool                 small pool, ramp-up needed
```

## The Takeaway

For a React developer, ClojureScript is less of a leap than the parentheses suggest: it's the React mental model you already own, expressed in a language where immutable data is the default rather than a discipline. You trade TypeScript's static safety and a huge hiring pool for genuinely cheaper re-render correctness and a REPL-driven workflow. That's a real trade — a JS-native team shouldn't switch lightly — but if you've ever fought a wall of `useMemo` to tame re-renders, the ClojureScript side of that trade will feel like relief.
