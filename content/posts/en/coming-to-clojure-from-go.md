---
title: "Coming to Clojure from Go: What Finally Clicked"
date: "2020-11-16"
description: "A Go developer's honest account of picking up Clojure — what took real adjustment (parens, dynamic typing, the JVM) and what turned out to be genuinely better (the REPL, immutability by default, and treating every program as data transformation)."
tags:
  [
    "clojure",
    "functional-programming",
    "golang",
    "backend",
  ]
---

I write Go for a living and I like it: it's explicit, it's boring in the good way, and I can read a stranger's Go and know what it does. So when I started spending evenings in Clojure, my first reaction was resistance. The parentheses. The dynamic typing. The stack traces that scroll past like credits. This post is the honest version of that adjustment — what stayed annoying, and what quietly rewired how I think about writing programs.

## What Took Real Adjustment

**The parentheses are not the problem — my editor was.** For a week I counted brackets by hand and hated it. Then I turned on structural editing (paredit) and the parens became invisible, because I stopped editing text and started editing *structure*: slurp a form into an expression, wrap a call, splice one out. Coming from Go, where I edit characters, editing whole expressions as units felt strange for about three days and then felt like a superpower I'd been missing.

**Dynamic typing gave me anxiety.** In Go the compiler is a safety net I lean on constantly; rename a field and every call site lights up red. Clojure hands you a map and trusts you to know what's in it. I missed the net. What partly replaced it wasn't a type checker but the REPL — I'd send a function a real value and *see* the result immediately, so the feedback loop that a compiler gives you at build time, I got interactively, one form at a time. It's a different kind of confidence, and I'm still calibrating how much I trust it.

**The JVM is heavy.** `go build` produces a static binary that starts instantly; a Clojure process takes seconds to boot. For a long-running server that's a non-issue, but it colors the whole experience — you don't restart a Clojure process to test a change, which turns out to be the point (more on that below).

## What Clicked

**Immutability by default flipped a mental cost I didn't know I was paying.** In Go I'm constantly, quietly asking "who else has a pointer to this, and can they mutate it while I'm using it?" In Clojure the data doesn't change, so that question disappears. You transform values into new values:

```clojure
;; The original map is untouched; assoc returns a new one
(def order {:id 42 :status :pending :total 100})
(assoc order :status :paid)
;; => {:id 42, :status :paid, :total 100}
order
;; => {:id 42, :status :pending, :total 100}  (unchanged)
```

The Go equivalent works, but I have to *decide* to copy, and I have to remember to. Here it's the default, and sharing data across goroutines — er, across threads — stops being a thing I have to reason about.

**Everything is data, and the same handful of functions work on all of it.** In Go, iterating a slice, a map, and a channel are three different shapes of code. In Clojure, `map`, `filter`, and `reduce` work over anything sequential, and I compose them with threading:

```clojure
(->> orders
     (filter #(= :paid (:status %)))
     (map :total)
     (reduce + 0))
```

Reading top to bottom: take orders, keep the paid ones, pull out totals, sum them. In Go that's a loop with an accumulator — perfectly clear, but I write the *mechanism* every time. Here I compose *intent*. After a while, the Go version started to feel like I was hand-rolling something the language should give me.

**Data over types, for shaping.** A lot of my Go code defines a struct so I can move a slightly different shape of data around. In Clojure I just... use a map, and reshape it with `select-keys`, `update`, `merge`. For code whose whole job is turning one shape of data into another — which is most backend code — this is less ceremony and more directness.

## What I'm Still Wary Of

I'm not a convert selling you a religion. The lack of static types is a real trade-off, not a free lunch: on a large team, on a big codebase, the compiler's "you changed this shape and forgot these 12 places" is worth a lot, and I don't yet know how Clojure teams get that confidence at scale (spec and good tests, I'm told — I'll find out). Error messages, when a `nil` sneaks into a sequence function, are genuinely worse than Go's. And "it's all just maps" is liberating until you open a function and have no idea what keys the map is supposed to have.

I'd been meaning to write about the front-end side of this too — I've been building UIs with re-frame, which leans on exactly this immutable-data-transformation model, and it's the most coherent front-end architecture I've used ([notes here](/en/posts/clojure-reframe-data-heavy-rendering-performance)).

## The Takeaway

What Clojure changed isn't which language I reach for at work — it's that I now notice the mutable state and the boilerplate transformations in my Go, and I write both more deliberately. The best reason to learn a language whose defaults are the opposite of your daily one isn't to switch. It's that the contrast makes the trade-offs you'd stopped seeing visible again. Go chooses explicitness and a compiler; Clojure chooses expressiveness and a REPL. Knowing both, I choose more consciously.
