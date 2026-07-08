---
title: "Performance in Data-Heavy Clojure: Transducers, Transients, and core.async"
date: 2021-11-08
description: "Making Clojure fast on large datasets: why lazy-seq pipelines allocate, how transducers and transients cut that to near zero, killing reflection in hot loops, and using core.async pipelines for I/O-bound fan-out with backpressure."
tags:
  [
    "clojure",
    "performance",
    "core-async",
    "transducers",
    "jvm",
    "backend",
    "optimization",
  ]
---

Clojure code that's instant in the REPL on a hundred maps can crawl on a few million. I've been writing Clojure since 2017, and most of the performance work I've done on data-heavy backends comes down to the same handful of moves. None of them are clever. They're about understanding what the convenient version allocates, and knowing when to trade a little elegance for a lot of throughput.

## Rule 0: Measure, Don't Guess

Every performance claim below should be verified on your workload, not taken on faith—including mine. On the JVM my two tools are [criterium](https://github.com/hugoduncan/criterium) for microbenchmarks (it handles JIT warmup and statistical noise, which naive `(time ...)` does not) and [clj-async-profiler](https://github.com/clojure-goog/clj-async-profiler) for flamegraphs of where the time actually goes.

```clojure
(require '[criterium.core :refer [quick-bench]])
(quick-bench (my-pipeline records))
```

The number of times I've profiled and found the bottleneck was somewhere I'd never have guessed is exactly the number of times I've profiled.

## Lazy Sequences Are Convenient and a Trap

The idiomatic thread-last pipeline is beautiful and, on large data, wasteful:

```clojure
;; Allocates an intermediate lazy seq at every step
(->> records
     (map parse)
     (filter valid?)
     (map enrich)
     (into []))
```

Each stage produces its own lazy sequence, with per-element cons cells and closures. Lazy seqs are also *chunked* in blocks of 32, so you can realize more than you asked for. And if you accidentally hold onto the head of a large lazy seq, you keep the whole thing in memory.

Transducers fix this by separating the *transformation* from the *sequence*. `comp` a stack of transducers and they fuse into a single pass with no intermediate collections:

```clojure
;; One pass, no intermediate seqs, no laziness to reason about
(into []
      (comp (map parse)
            (filter valid?)
            (map enrich))
      records)
```

Same logic, same readability once your eye adjusts, but the intermediate allocations are gone. On a large dataset this is routinely the difference between comfortable and painful, and the transducer is reusable—the same `(comp ...)` works with `into`, `transduce`, `sequence`, or a `core.async` channel.

## Transients for the Build-Up

When you're constructing a big collection by reducing over input, persistent immutability charges you an allocation per step. Transients give you locally-mutable, thread-contained collections that you convert back to persistent at the end—same result, a fraction of the garbage:

```clojure
(defn index-by-id [records]
  (persistent!
    (reduce (fn [acc r] (assoc! acc (:id r) r))
            (transient {})
            records)))
```

The rule is narrow and important: transients are for a single-threaded build-up where you control the whole lifecycle, and you never share one across threads. Inside that box they're safe and fast; outside it they're a foot-gun. This is the one place I'll trade Clojure's default immutability, and only because the mutation never escapes the function.

## Kill Reflection in Hot Loops

Reflective calls on the JVM are slow, and Clojure will silently emit them when it can't infer a type. Turn on the warning and you'll find them:

```clojure
(set! *warn-on-reflection* true)
```

For numeric hot paths, boxing is the other tax—every `Long`/`Double` object allocated in a tight loop is pressure on the allocator. Working over primitive arrays with `areduce`/`amap` keeps the math primitive end to end:

```clojure
;; No boxing: primitive doubles throughout the loop
(defn sum ^double [^doubles xs]
  (areduce xs i acc 0.0 (+ acc (aget xs i))))
```

You don't want type hints smeared across the whole codebase—they're noise on cold paths. You want them exactly where the profiler pointed.

## Parallelism: core.async for I/O-Bound Fan-Out

When the bottleneck is CPU-bound pure computation over a foldable collection, `clojure.core.reducers/fold` will parallelize a fold across cores almost for free. But most data-heavy backend work is *I/O*-bound—enriching records from a database or an HTTP service—and there the tool is a `core.async` pipeline.

```clojure
(require '[clojure.core.async :as a])

(defn process-all [records concurrency]
  (let [in  (a/chan 1024)
        out (a/chan 1024)]
    ;; pipeline-blocking: N parallel workers for blocking I/O.
    ;; Backpressure is built in — if `out` fills up, workers stop pulling `in`.
    (a/pipeline-blocking concurrency out (map enrich-via-io) in)
    (a/onto-chan!! in records)         ; feed the input and close it
    (a/<!! (a/into [] out))))          ; drain the output
```

`pipeline-blocking` runs `concurrency` workers over blocking work; use plain `pipeline` for CPU-bound transforms and `pipeline-async` when the work is already asynchronous. The buffered channels give you backpressure for free: a slow consumer downstream naturally throttles the workers, so you process a huge input stream at a bounded, steady memory footprint instead of pulling it all into a seq and hoping it fits. That backpressure property is the real reason I reach for core.async here rather than `pmap`—`pmap` has no flow control and its chunking behavior surprises people on uneven workloads.

## Trade-Offs, Named

- **Transducers are less familiar.** A team that hasn't seen them reads `(comp (map ...) (filter ...))` more slowly than a thread-last. That's a real cost on shared code; I reserve them for the paths where allocation actually matters and leave the thread-last where it's clear and cold.
- **Transients and primitive arrays give up Clojure's safety net.** Mutable, boxing-free, and unforgiving. Keep them local and hidden behind a pure function boundary.
- **core.async adds concurrency you have to reason about.** Channels, buffer sizes, and closing semantics are their own source of bugs. For a simple parallel map over a few items, `pmap` or a handful of `future`s is less machinery.
- **Above all: don't do any of this before you've measured.** The idiomatic lazy pipeline is the right default. You reach for these tools when a profiler tells you to, on the specific path it points at—not everywhere, and not preemptively.

The theme is that Clojure gives you a fast, immutable default and a set of clearly-marked escape hatches for when the default isn't fast enough. Good performance work is knowing which hatch, and—more importantly—knowing you've earned the right to open it.

---
