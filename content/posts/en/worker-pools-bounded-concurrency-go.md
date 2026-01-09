---
title: "Worker Pools and Bounded Concurrency in Go"
date: 2025-08-20
description: "Practical patterns for controlling concurrency: worker pools, errgroup, semaphores, fan-out/fan-in, and backpressure. How to process work concurrently without overwhelming your system."
tags:
  ["golang", "concurrency", "patterns", "goroutines", "channels", "performance"]
---

Spinning up a goroutine for every task is easy. Spinning up 10,000 goroutines that hammer your database into the ground is also easy. The hard part is controlling concurrency—processing work in parallel while respecting system limits.

This post covers the patterns that let you do exactly that.

## The Problem: Unbounded Concurrency

The naive approach:

```go
func processAll(items []Item) error {
    var wg sync.WaitGroup
    for _, item := range items {
        wg.Add(1)
        go func() {
            defer wg.Done()
            process(item) // What if this hits a database?
        }()
    }
    wg.Wait()
    return nil
}
```

With 10,000 items, you spawn 10,000 goroutines that simultaneously hit your database. Connection pools get exhausted, timeouts cascade, and your service falls over.

You need bounded concurrency.

## Pattern 1: Worker Pool with Channels

The classic pattern from "Concurrency in Go": a fixed number of workers consuming from a shared channel.

```go
func processWithWorkerPool(items []Item, numWorkers int) error {
    jobs := make(chan Item, len(items))
    results := make(chan error, len(items))

    // Start workers
    var wg sync.WaitGroup
    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for item := range jobs {
                results <- process(item)
            }
        }()
    }

    // Send jobs
    for _, item := range items {
        jobs <- item
    }
    close(jobs)

    // Wait for workers to finish
    go func() {
        wg.Wait()
        close(results)
    }()

    // Collect errors
    var errs []error
    for err := range results {
        if err != nil {
            errs = append(errs, err)
        }
    }

    if len(errs) > 0 {
        return fmt.Errorf("%d items failed", len(errs))
    }
    return nil
}
```

This works, but it's verbose. For most cases, there's a better option.

## Pattern 2: errgroup for Coordinated Concurrency

The `golang.org/x/sync/errgroup` package is the standard tool for bounded concurrent work. It handles:

- Waiting for goroutines to complete
- Collecting the first error
- Context cancellation on failure

```go
import "golang.org/x/sync/errgroup"

func processWithErrgroup(ctx context.Context, items []Item) error {
    g, ctx := errgroup.WithContext(ctx)

    for _, item := range items {
        g.Go(func() error {
            return process(ctx, item)
        })
    }

    return g.Wait() // Returns first error, waits for all goroutines
}
```

But this still spawns unbounded goroutines. Add `SetLimit`:

```go
func processWithBoundedErrgroup(ctx context.Context, items []Item) error {
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(10) // At most 10 concurrent goroutines

    for _, item := range items {
        g.Go(func() error {
            return process(ctx, item)
        })
    }

    return g.Wait()
}
```

`SetLimit` makes errgroup block when the limit is reached, preventing unbounded goroutine creation.

### When errgroup Cancels

With `errgroup.WithContext`, the context is canceled when any goroutine returns an error. Other goroutines should check the context:

```go
func process(ctx context.Context, item Item) error {
    // Check if we should stop
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    // Do work...
    result, err := fetchData(ctx, item.URL)
    if err != nil {
        return err // This cancels the context for other goroutines
    }

    return saveResult(ctx, result)
}
```

## Pattern 3: Semaphore with Buffered Channel

For simple rate limiting, a buffered channel acts as a semaphore:

```go
func processWithSemaphore(ctx context.Context, items []Item) error {
    sem := make(chan struct{}, 10) // Limit to 10 concurrent
    var wg sync.WaitGroup
    errCh := make(chan error, 1)

    for _, item := range items {
        // Acquire semaphore
        select {
        case sem <- struct{}{}:
        case <-ctx.Done():
            return ctx.Err()
        }

        wg.Add(1)
        go func() {
            defer wg.Done()
            defer func() { <-sem }() // Release semaphore

            if err := process(ctx, item); err != nil {
                select {
                case errCh <- err:
                default:
                }
            }
        }()
    }

    wg.Wait()

    select {
    case err := <-errCh:
        return err
    default:
        return nil
    }
}
```

### golang.org/x/sync/semaphore for Weighted Limits

When different tasks consume different amounts of resources:

```go
import "golang.org/x/sync/semaphore"

func processWithWeightedSemaphore(ctx context.Context, items []Item) error {
    // Allow 100 "units" of concurrent work
    sem := semaphore.NewWeighted(100)

    g, ctx := errgroup.WithContext(ctx)

    for _, item := range items {
        weight := int64(item.Size / 1024) // Heavier items take more capacity
        if weight < 1 {
            weight = 1
        }

        // Acquire weighted permit
        if err := sem.Acquire(ctx, weight); err != nil {
            return err
        }

        g.Go(func() error {
            defer sem.Release(weight)
            return process(ctx, item)
        })
    }

    return g.Wait()
}
```

## Pattern 4: Fan-Out/Fan-In

Fan-out: distribute work across multiple goroutines.
Fan-in: collect results into a single channel.

```go
func fanOutFanIn(ctx context.Context, urls []string) ([]Result, error) {
    // Fan-out: create a channel of work
    urlCh := make(chan string, len(urls))
    for _, url := range urls {
        urlCh <- url
    }
    close(urlCh)

    // Start workers (fan-out)
    resultCh := make(chan Result, len(urls))
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(5)

    for i := 0; i < 5; i++ {
        g.Go(func() error {
            for url := range urlCh {
                select {
                case <-ctx.Done():
                    return ctx.Err()
                default:
                }

                result, err := fetch(ctx, url)
                if err != nil {
                    return err
                }
                resultCh <- result
            }
            return nil
        })
    }

    // Wait and close results channel
    go func() {
        g.Wait()
        close(resultCh)
    }()

    // Fan-in: collect results
    var results []Result
    for result := range resultCh {
        results = append(results, result)
    }

    if err := g.Wait(); err != nil {
        return nil, err
    }

    return results, nil
}
```

### Pipeline Pattern

Chain stages together for complex processing:

```go
func pipeline(ctx context.Context, input []int) ([]int, error) {
    // Stage 1: Generate
    stage1 := generate(ctx, input)

    // Stage 2: Square (fan-out to 3 workers)
    stage2 := fanOut(ctx, stage1, 3, square)

    // Stage 3: Filter
    stage3 := filter(ctx, stage2, func(n int) bool { return n > 10 })

    // Collect results
    return collect(ctx, stage3)
}

func generate(ctx context.Context, nums []int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for _, n := range nums {
            select {
            case out <- n:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}

func fanOut[T, R any](ctx context.Context, in <-chan T, workers int, fn func(T) R) <-chan R {
    out := make(chan R)

    var wg sync.WaitGroup
    for i := 0; i < workers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for item := range in {
                select {
                case out <- fn(item):
                case <-ctx.Done():
                    return
                }
            }
        }()
    }

    go func() {
        wg.Wait()
        close(out)
    }()

    return out
}
```

## Pattern 5: Backpressure with Channels

When producers are faster than consumers, you need backpressure—slowing down producers when consumers can't keep up.

### Blocking Send (Natural Backpressure)

```go
func producerWithBackpressure(ctx context.Context, items []Item) <-chan Item {
    // Small buffer = backpressure kicks in quickly
    out := make(chan Item, 10)

    go func() {
        defer close(out)
        for _, item := range items {
            select {
            case out <- item: // Blocks when buffer is full
            case <-ctx.Done():
                return
            }
        }
    }()

    return out
}
```

### Dropping Items Under Pressure

Sometimes it's better to drop work than block:

```go
func producerWithDrop(ctx context.Context, items <-chan Item) <-chan Item {
    out := make(chan Item, 100)

    go func() {
        defer close(out)
        for item := range items {
            select {
            case out <- item:
            default:
                // Buffer full, drop item
                log.Printf("dropping item %s due to backpressure", item.ID)
            }
        }
    }()

    return out
}
```

### Rate Limiting with time.Ticker

```go
func rateLimitedWorker(ctx context.Context, items <-chan Item, ratePerSecond int) error {
    ticker := time.NewTicker(time.Second / time.Duration(ratePerSecond))
    defer ticker.Stop()

    for item := range items {
        select {
        case <-ticker.C:
            if err := process(ctx, item); err != nil {
                return err
            }
        case <-ctx.Done():
            return ctx.Err()
        }
    }

    return nil
}
```

## Graceful Shutdown

Worker pools need to drain gracefully:

```go
type WorkerPool struct {
    jobs    chan Job
    results chan Result
    quit    chan struct{}
    wg      sync.WaitGroup
}

func NewWorkerPool(numWorkers, jobBuffer int) *WorkerPool {
    p := &WorkerPool{
        jobs:    make(chan Job, jobBuffer),
        results: make(chan Result, jobBuffer),
        quit:    make(chan struct{}),
    }

    for i := 0; i < numWorkers; i++ {
        p.wg.Add(1)
        go p.worker()
    }

    return p
}

func (p *WorkerPool) worker() {
    defer p.wg.Done()
    for {
        select {
        case job, ok := <-p.jobs:
            if !ok {
                return // Channel closed, exit
            }
            result := process(job)
            p.results <- result
        case <-p.quit:
            // Drain remaining jobs before exiting
            for job := range p.jobs {
                result := process(job)
                p.results <- result
            }
            return
        }
    }
}

func (p *WorkerPool) Shutdown(ctx context.Context) error {
    close(p.jobs) // Stop accepting new jobs

    done := make(chan struct{})
    go func() {
        p.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        return nil
    case <-ctx.Done():
        close(p.quit) // Signal workers to drain and exit
        return ctx.Err()
    }
}
```

## Common Mistakes

### 1. Forgetting to Close Channels

```go
// WRONG: results channel never closed, range blocks forever
go func() {
    for _, item := range items {
        results <- process(item)
    }
    // Missing: close(results)
}()

for result := range results { // Blocks forever
    // ...
}
```

### 2. Goroutine Leak from Blocked Send

```go
// WRONG: if we return early, goroutine leaks
func fetch(ctx context.Context) (string, error) {
    ch := make(chan string)

    go func() {
        result := slowOperation()
        ch <- result // Blocks forever if ctx is canceled
    }()

    select {
    case result := <-ch:
        return result, nil
    case <-ctx.Done():
        return "", ctx.Err() // Goroutine leaks!
    }
}

// RIGHT: use buffered channel
func fetch(ctx context.Context) (string, error) {
    ch := make(chan string, 1) // Buffered!

    go func() {
        result := slowOperation()
        ch <- result // Won't block even if nobody receives
    }()

    select {
    case result := <-ch:
        return result, nil
    case <-ctx.Done():
        return "", ctx.Err() // Goroutine can still complete
    }
}
```

### 3. Not Respecting Context Cancellation

```go
// WRONG: ignores cancellation
for item := range items {
    process(item) // Keeps running even if ctx is canceled
}

// RIGHT: check context
for item := range items {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
    process(ctx, item)
}
```

## When to Use What

- **errgroup + SetLimit**: Default choice for bounded concurrent work
- **Worker pool**: Long-lived workers processing continuous stream
- **Semaphore**: Need weighted limits or fine-grained control
- **Fan-out/fan-in**: Pipeline processing with multiple stages
- **Buffered channel**: Simple backpressure between producer/consumer

## Key Takeaways

1. **errgroup.SetLimit** is your default tool for bounded concurrency. It's simple and handles errors well.

2. **Channels are semaphores**. A buffered channel of size N limits concurrency to N.

3. **Close channels to signal completion**. Receivers range over channels; closing is how you tell them you're done.

4. **Buffer size = latency vs memory**. Larger buffers absorb bursts but use more memory.

5. **Always check context**. Long-running work should respect cancellation.

6. **Buffered channel of 1 prevents goroutine leaks**. When a goroutine might outlive its caller, give it somewhere to send.

7. **Drain on shutdown**. Don't just abandon work—give workers a chance to finish what they're doing.

Concurrency in Go is powerful, but unbounded concurrency is a footgun. These patterns give you the control to use it safely.
