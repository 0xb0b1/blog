---
title: "REPL-Driven Development: How I Actually Write Clojure"
date: "2021-07-05"
description: "REPL-driven development is not 'a better console' — it's editing a live program. How I build functions incrementally against real data from inside my editor, using rich comment blocks and tap>, and where the workflow bites."
tags:
  [
    "clojure",
    "repl",
    "workflow",
    "functional-programming",
    "backend",
  ]
---

When I came to Clojure from Go, "the REPL" sounded like Python's interactive prompt — a scratchpad for trying one-liners. That framing sold it completely short, and it took me months to get the actual workflow. REPL-driven development isn't typing into a console. It's keeping a live program running and reshaping it from your editor, one expression at a time, against real data. Once it clicked, going back to edit-save-restart felt like coding blindfolded.

## The Loop Is the Point

In my Go workflow the loop is: write code, save, `go run`/`go test`, read output, repeat. The program is dead between runs; every change costs a full restart. In Clojure the program stays alive. My editor is connected to a running process, and I evaluate the *form under the cursor* — a single function, a single expression — sending it into that live process and seeing the result inline, without restarting anything.

Concretely: I write a function, evaluate it (it's now defined in the running program), then evaluate a call to it with a real argument and read the result right next to the code. If it's wrong, I edit the function, re-evaluate it, and call it again — the process never restarted, and any expensive state I set up earlier (a loaded dataset, a database connection) is still there.

```clojure
(defn line-total [item]
  (* (:qty item) (:unit-price item)))

;; Evaluate the def above, then evaluate this call right here:
(line-total {:qty 3 :unit-price 250})
;; => 750
```

I didn't run a test file or start a program. I asked the live process a question and it answered. That feedback is instant and it's against a *real value*, which is why it partly stands in for the compiler I miss coming from Go.

## Building Up, Not Writing Down

The deeper shift is that I no longer write a whole function and then find out if it works. I grow it. Say I'm parsing and summarizing orders. I evaluate intermediate steps and keep what works:

```clojure
(def sample (slurp "orders.json"))          ; evaluate — now `sample` holds real data
(def parsed (json/read-str sample :key-fn keyword))  ; evaluate — inspect the shape
(->> parsed (filter #(= "paid" (:status %))))        ; evaluate — does the filter work?
(->> parsed (filter #(= "paid" (:status %))) (map :total))  ; add the next step, evaluate
```

Each line is evaluated as I add it, against the actual data, so I *see* the shape at every step instead of guessing and running the whole thing at the end. By the time I fold these into a function, I've already watched every stage work. This is the opposite of the Go rhythm, where I write the whole function against my mental model of the data and discover mismatches only when I run it.

## Rich Comments Keep the Scratch Work

The danger of all this exploration is litter — throwaway calls scattered through the file. The idiom that solves it is the `(comment ...)` block, universally called a "rich comment." Code inside `comment` never runs when the file loads, but my editor will happily evaluate individual forms inside it. So I keep my exploration next to the function it exercises, permanently:

```clojure
(defn paid-total [orders]
  (->> orders (filter #(= "paid" (:status %))) (map :total) (reduce + 0)))

(comment
  ;; scratch space — evaluate these by hand, never runs on load
  (paid-total parsed)
  (paid-total [])                ; edge case: empty
  (def parsed (json/read-str (slurp "orders.json") :key-fn keyword)))
```

Six months later this block is documentation: it shows the next person (usually me) exactly how to drive the function with real input.

## Seeing Into Running Code with tap>

For values buried inside a pipeline, `tap>` sends anything to a registered handler without disturbing the flow — like a `print`, but structured and toggleable:

```clojure
(add-tap (bound-fn* clojure.pprint/pprint))  ; once, at the REPL

(defn process [orders]
  (->> orders
       (filter #(= "paid" (:status %)))
       (doto tap>)          ; peek at the intermediate seq, pass it through
       (map :total)
       (reduce + 0)))
```

Unlike littering `println`, taps go to a handler I control and can turn off, and they hand me the real data structure to inspect, not a stringified version.

## Where It Bites

It isn't free. **State drift** is the classic trap: you redefine a function, forget to re-evaluate something that depends on it, and your running process no longer matches your file. The discipline is to periodically reload the namespace from disk so the live image can't silently diverge from the source of truth. **It rewards setup**: you need your editor's REPL integration configured, and if you skip that and use a bare terminal REPL, you get Python-prompt ergonomics and none of the magic. And it can breed **REPL-only code** — things that work in your warm process but were never captured in a test, so they break for the next person.

## The Takeaway

The mindset shift from Go is this: I used to treat the running program as the *output* of my editing. In Clojure the running program is the *medium* I edit in. Correctness stops being something I verify at the end of a cycle and becomes something I observe continuously, form by form, against real data. It's the workflow half of what makes Clojure feel different — the [language half I wrote about here](/en/posts/coming-to-clojure-from-go). Pair it with a habit of promoting your rich-comment experiments into real tests, and you get the fast feedback without the drift.
