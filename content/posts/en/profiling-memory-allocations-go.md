---
title: "Profiling Memory Allocations in a High-Throughput Go Service"
date: 2025-09-11
description: "How we reduced GC pauses from 50ms to 2ms by finding hidden allocations. Practical pprof techniques, escape analysis, and the optimizations that actually matter."
tags:
  ["golang", "performance", "profiling", "memory", "optimization", "production"]
---

Our API was handling 50k requests per second, but p99 latency kept spiking to 200ms. The culprit wasn't slow code—it was the garbage collector pausing everything while it cleaned up millions of tiny allocations we didn't know we were making.

Here's how we found them and what we did about it.

## The Symptoms

Classic GC pressure symptoms:

- Latency spikes every few seconds
- CPU usage higher than expected
- Memory usage stable but GC running constantly

```bash
# Check GC stats
GODEBUG=gctrace=1 ./myservice

# Output shows frequent GCs:
# gc 1 @0.012s 2%: 0.018+2.3+0.018 ms clock, 0.14+0.23/4.5/0+0.14 ms cpu, 4->4->2 MB, 5 MB goal, 8 P
# gc 2 @0.025s 3%: 0.019+3.1+0.021 ms clock, 0.15+0.31/6.1/0+0.17 ms cpu, 4->5->3 MB, 5 MB goal, 8 P
# gc 3 @0.041s 4%: ...
```

GC running every 15ms means every request has a chance of hitting a pause.

## Finding Allocations with pprof

### Heap Profile

```go
import _ "net/http/pprof"

func main() {
    go func() {
        // Exposes /debug/pprof/*
        log.Println(http.ListenAndServe("localhost:6060", nil))
    }()
    // ... rest of your service
}
```

Grab a heap profile:

```bash
# Allocations since program start
go tool pprof http://localhost:6060/debug/pprof/heap

# Or save for later analysis
curl -o heap.prof http://localhost:6060/debug/pprof/heap
go tool pprof heap.prof
```

Inside pprof:

```
(pprof) top 20
Showing nodes accounting for 1.5GB, 89% of 1.7GB total
      flat  flat%   sum%        cum   cum%
    512MB 30.12% 30.12%      512MB 30.12%  encoding/json.(*decodeState).literalStore
    256MB 15.06% 45.18%      768MB 45.18%  myservice/handlers.(*Handler).ProcessRequest
    128MB  7.53% 52.71%      128MB  7.53%  fmt.Sprintf
```

### The Key Insight: alloc_objects vs inuse_objects

```bash
# Total allocations (even if freed) - shows allocation rate
go tool pprof -alloc_objects http://localhost:6060/debug/pprof/heap

# Currently in use - shows memory retention
go tool pprof -inuse_objects http://localhost:6060/debug/pprof/heap
```

**For GC pressure, `alloc_objects` matters more.** You might have low memory usage but high allocation rate, causing constant GC work.

## Common Hidden Allocations

### 1. String Concatenation

```go
// BAD: Each + allocates a new string
func buildKey(prefix, id, suffix string) string {
    return prefix + ":" + id + ":" + suffix
}

// GOOD: strings.Builder pre-allocates
func buildKey(prefix, id, suffix string) string {
    var b strings.Builder
    b.Grow(len(prefix) + len(id) + len(suffix) + 2)
    b.WriteString(prefix)
    b.WriteByte(':')
    b.WriteString(id)
    b.WriteByte(':')
    b.WriteString(suffix)
    return b.String()
}

// BETTER for simple cases: fmt with buffer pool
var keyBufferPool = sync.Pool{
    New: func() any {
        return new(strings.Builder)
    },
}

func buildKey(prefix, id, suffix string) string {
    b := keyBufferPool.Get().(*strings.Builder)
    b.Reset()
    defer keyBufferPool.Put(b)

    b.Grow(len(prefix) + len(id) + len(suffix) + 2)
    b.WriteString(prefix)
    b.WriteByte(':')
    b.WriteString(id)
    b.WriteByte(':')
    b.WriteString(suffix)
    return b.String()
}
```

### 2. Slice Appends Without Capacity

```go
// BAD: Multiple reallocations as slice grows
func collectIDs(items []Item) []string {
    var ids []string
    for _, item := range items {
        ids = append(ids, item.ID)
    }
    return ids
}

// GOOD: Pre-allocate
func collectIDs(items []Item) []string {
    ids := make([]string, 0, len(items))
    for _, item := range items {
        ids = append(ids, item.ID)
    }
    return ids
}
```

### 3. Interface Boxing

```go
// BAD: Each call boxes the int
func logValue(key string, value any) {
    log.Printf("%s: %v", key, value)
}

func process(count int) {
    logValue("count", count) // int -> any allocation
}

// GOOD: Type-specific methods
func logInt(key string, value int) {
    log.Printf("%s: %d", key, value)
}
```

### 4. Closures Capturing Variables

```go
// Both patterns are correct in Go 1.22+, but parameter passing
// can help escape analysis in some cases

// Closure capture (correct, but item may escape to heap)
func processAll(items []Item) {
    var wg sync.WaitGroup
    for _, item := range items {
        wg.Add(1)
        go func() {
            defer wg.Done()
            process(item)
        }()
    }
    wg.Wait()
}

// Parameter passing (may stay on stack in some cases)
func processAll(items []Item) {
    var wg sync.WaitGroup
    for _, item := range items {
        wg.Add(1)
        go func(it Item) {
            defer wg.Done()
            process(it)
        }(item)
    }
    wg.Wait()
}
```

### 5. fmt.Sprintf for Simple Conversions

```go
// BAD: fmt.Sprintf allocates
id := fmt.Sprintf("%d", userID)

// GOOD: strconv doesn't (for small ints)
id := strconv.Itoa(userID)

// For int64:
id := strconv.FormatInt(userID, 10)
```

## Escape Analysis: Why Things Allocate

Go decides at compile time whether a variable escapes to the heap. Check with:

```bash
go build -gcflags='-m -m' ./... 2>&1 | grep escape
```

Common reasons for escape:

```go
// Escapes: returned pointer to local variable
func newUser() *User {
    u := User{Name: "test"} // escapes to heap
    return &u
}

// Escapes: assigned to interface
func process(u User) {
    var i any = u // u escapes
}

// Escapes: captured by closure in goroutine
func startWorker(data []byte) {
    go func() {
        process(data) // data escapes
    }()
}

// Escapes: too large for stack (varies by Go version)
func bigArray() {
    data := make([]byte, 10*1024*1024) // escapes, too big
}
```

## sync.Pool: Recycling Allocations

For frequently allocated objects, sync.Pool eliminates allocations:

```go
var bufferPool = sync.Pool{
    New: func() any {
        return make([]byte, 0, 4096)
    },
}

func processRequest(data []byte) []byte {
    buf := bufferPool.Get().([]byte)
    buf = buf[:0] // Reset length, keep capacity
    defer bufferPool.Put(buf)

    // Use buf...
    buf = append(buf, data...)

    // Important: return a copy if buf escapes this function
    result := make([]byte, len(buf))
    copy(result, buf)
    return result
}
```

### Pool Gotchas

```go
// WRONG: Putting different sizes back
var pool = sync.Pool{New: func() any { return make([]byte, 1024) }}

func process(size int) {
    buf := pool.Get().([]byte)
    if size > len(buf) {
        buf = make([]byte, size) // Created larger buffer
    }
    defer pool.Put(buf) // Now pool has mixed sizes
}

// RIGHT: Either use fixed sizes or cap the pool
func process(size int) {
    buf := pool.Get().([]byte)
    if size > cap(buf) {
        // Don't put oversized buffers back
        buf = make([]byte, size)
        defer func() { /* don't return to pool */ }()
    } else {
        buf = buf[:size]
        defer pool.Put(buf[:0])
    }
}
```

## Real Example: JSON Encoding

Our biggest allocation source was JSON encoding in HTTP handlers:

```go
// BEFORE: ~5 allocations per request
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
    user := h.db.GetUser(r.Context(), userID)

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(user) // Allocates encoder + buffer
}
```

After profiling:

```go
// AFTER: Pooled encoders
var encoderPool = sync.Pool{
    New: func() any {
        return &pooledEncoder{
            buf: bytes.NewBuffer(make([]byte, 0, 4096)),
        }
    },
}

type pooledEncoder struct {
    buf *bytes.Buffer
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
    user := h.db.GetUser(r.Context(), userID)

    enc := encoderPool.Get().(*pooledEncoder)
    enc.buf.Reset()
    defer encoderPool.Put(enc)

    if err := json.NewEncoder(enc.buf).Encode(user); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Content-Length", strconv.Itoa(enc.buf.Len()))
    w.Write(enc.buf.Bytes())
}
```

## Benchmarking Allocations

Always benchmark before optimizing:

```go
func BenchmarkBuildKey(b *testing.B) {
    b.ReportAllocs() // Shows allocations per op

    for i := 0; i < b.N; i++ {
        _ = buildKey("user", "12345", "profile")
    }
}
```

Output:

```
BenchmarkBuildKey-8    5000000    234 ns/op    64 B/op    2 allocs/op
```

After optimization:

```
BenchmarkBuildKey-8    10000000   112 ns/op    32 B/op    1 allocs/op
```

## The Results

After applying these patterns:

| Metric          | Before | After |
| --------------- | ------ | ----- |
| Allocations/req | ~45    | ~12   |
| GC pause p99    | 50ms   | 2ms   |
| Latency p99     | 200ms  | 35ms  |
| GC frequency    | 15ms   | 200ms |

## Key Takeaways

1. **Profile first**. Don't guess where allocations happen. Use `pprof -alloc_objects`.

2. **alloc_objects > inuse_objects** for GC pressure. High allocation rate matters even if memory is freed quickly.

3. **Escape analysis** tells you why things allocate. Use `-gcflags='-m'` to understand.

4. **sync.Pool** is your friend for hot paths. But measure—it has overhead too.

5. **Pre-allocate slices** when you know the size. `make([]T, 0, n)` is your friend.

6. **Avoid interface boxing** in hot paths. Type-specific functions allocate less.

7. **String operations are expensive**. Use `strings.Builder` or `[]byte` operations.

8. **Benchmark with `b.ReportAllocs()`**. Allocations per operation tells you if you're improving.

Most services don't need this level of optimization. But when you're handling tens of thousands of requests per second, every allocation counts. Profile first, optimize what matters, and always measure the results.
