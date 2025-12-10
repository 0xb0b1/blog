---
title: "Rewriting a Python Notifications Service in Go: 5x Throughput, 10x Smaller"
date: 2025-12-10
description: "A deep dive into rewriting a Python asyncio notifications backend in Go, achieving 5x throughput improvement, proper distributed deduplication, and a 10x smaller Docker image."
tags:
  [
    "golang",
    "python",
    "performance",
    "architecture",
    "aws-sns",
    "redis",
    "grpc",
  ]
---

# Rewriting a Python Notifications Service in Go: 5x Throughput, 10x Smaller

I recently rewrote a sports notifications backend from Python to Go. The service handles match events (goals, cards, penalties) and delivers push notifications via AWS SNS to millions of mobile devices through FCM and APNs.

The results surprised even me:

```
| Metric          | Python            | Go                | Improvement |
|-----------------|-------------------|-------------------|-------------|
| Throughput      | ~1,000 events/sec | ~5,000 events/sec | 5x          |
| Cold start      | 3 seconds         | 100ms             | 30x         |
| Docker image    | 500MB             | 50MB              | 10x         |
| Memory baseline | 100MB             | 30MB              | 3x          |

```

But the real win wasn't performance — it was **correctness**. The Python version had a subtle deduplication bug that caused duplicate notifications. Let me explain.

## The Problem: Distributed Deduplication

Sports notifications are time-critical. When Messi scores, millions of users need to know _immediately_. But they should only be notified **once**.

The Python implementation used an in-memory dictionary for deduplication:

```python
# Per-instance deduplication (broken!)
class EventProcessor:
    def __init__(self):
        self._seen_events = {}  # In-memory dict

    async def process_event(self, event):
        if event.key in self._seen_events:
            return  # Skip duplicate
        self._seen_events[event.key] = True
        await self.send_notification(event)
```

**The bug**: Each server instance has its own `_seen_events` dict. With 3 instances behind a load balancer, the same event could hit different instances and bypass deduplication entirely.

```
Event arrives → Load Balancer → Instance A (sends notification)
Event arrives → Load Balancer → Instance B (sends notification) ← DUPLICATE!
Event arrives → Load Balancer → Instance C (sends notification) ← DUPLICATE!
```

Users received 3 notifications for the same goal. Not great.

## The Solution: Redis + Lua Scripts

The Go version uses Redis with atomic Lua scripts. The key insight is that the entire check-and-set operation must happen atomically:

```lua
-- Atomic deduplication in Redis
local existing = redis.call('GET', key)
if existing then
    return 0  -- DUPLICATE
end
redis.call('SET', key, value, 'EX', ttl)
return 1  -- NEW
```

**Why Lua scripts?** The entire operation happens atomically inside Redis. No race conditions possible:

```
Instance A: EVAL script → Returns 1 (NEW) → Sends notification
Instance B: EVAL script → Returns 0 (DUPLICATE) → Skips
Instance C: EVAL script → Returns 0 (DUPLICATE) → Skips
```

One notification. As intended.

### Three Deduplication States

The Lua script returns three possible states:

```
| State          | Meaning                    | Action            |
|----------------|----------------------------|-------------------|
| NEW (1)        | Never seen before          | Send notification |
| DUPLICATE (0)  | Exact same payload         | Skip silently     |
| CORRECTION (2) | Same event, different data | Send update       |

```

The `CORRECTION` state handles real-world scenarios like: "Goal attributed to Player A" followed by "VAR correction: Goal attributed to Player B". Users should receive both notifications.

## Architecture: From Async to Worker Pool

### Python: Single-Threaded Event Loop

```python
async def process_events(events):
    # Looks parallel, but it's not!
    await asyncio.gather(*[process_event(e) for e in events])
    # All tasks share ONE thread
```

Python's `asyncio` is _concurrent_ but not _parallel_. All coroutines run on a single thread, sharing one CPU core. When you `await` an I/O operation, other tasks can run — but CPU-bound work blocks everything.

### Go: True Parallelism

```go
// Worker pool pattern
for i := 0; i < workerCount; i++ {
    go func() {
        for event := range eventChan {
            processEvent(ctx, event)
        }
    }()
}
```

Go's goroutines are multiplexed across OS threads by the runtime. With 10 workers on an 8-core machine, you get true parallelism — CPU-bound and I/O-bound work both scale.

### Worker Pool Benefits

The worker pool pattern provides:

1. **Controlled concurrency**: Fixed number of workers prevents resource exhaustion
2. **Backpressure**: When the channel fills up, producers block
3. **Graceful shutdown**: Close channel, wait for workers to finish

## Error Handling: Explicit vs Implicit

Python makes it easy to miss errors:

```python
async def process_event(event):
    try:
        await sns.publish(...)
    except Exception as e:
        logger.error(e)
        # Event is lost. No retry. No alert.
```

Go forces you to handle every error:

```go
if err := sns.Publish(ctx, topic, payload); err != nil {
    // Must handle - code won't compile without this
    return fmt.Errorf("publish failed: %w", err)
}
```

The Go compiler is your safety net. You can't forget to handle an error — you have to explicitly ignore it with `_`.

### Fail-Open Strategy

For deduplication, we chose a fail-open strategy. If Redis is unavailable, allow the notification through.

**Why fail-open?** In a sports notification system, a missed goal notification is worse than a duplicate. Users can ignore duplicates but can't recover missed events.

## Dependency Injection: Testing Made Easy

Python's global imports make testing painful:

```python
from app.services import sns_service  # Global import

async def send_notification(event):
    await sns_service.publish(...)  # How do you mock this?
```

Go's explicit dependencies make mocking trivial:

```go
type Processor struct {
    deliverer Deliverer  // Interface
}

// In tests - inject mock
processor := NewProcessor(&MockDeliverer{})
```

## Deployment: From 500MB to 50MB

Python requires the interpreter, pip packages, and system dependencies:

```dockerfile
# Python: ~500MB image
FROM python:3.11-slim
RUN pip install -r requirements.txt
COPY . .
CMD ["python", "-m", "app"]
```

Go compiles to a static binary:

```dockerfile
# Go: ~50MB image
FROM golang:1.23-alpine AS builder
RUN CGO_ENABLED=0 go build -o /app ./cmd/server

FROM alpine:3.19
COPY --from=builder /app /app
CMD ["/app"]
```

**Operational benefits:**

- 10x smaller images = faster deploys, less storage
- 30x faster startup = better auto-scaling
- No dependency conflicts = simpler debugging
- Static binary = copy and run anywhere

## The Final Architecture

```
┌─────────────────┐     gRPC      ┌────────────────────────────┐
│  Event Source   │──────────────▶│   Notifications Backend    │
└─────────────────┘               │                            │
                                  │  ┌────────┐  ┌──────────┐  │
                                  │  │ Worker │  │  Topic   │  │
                                  │  │  Pool  │  │  Cache   │  │
                                  │  └───┬────┘  └────┬─────┘  │
                                  │      │            │        │
                                  │  ┌───┴────────────┴───┐    │
                                  │  │   Deduplicator     │    │
                                  │  │   (Redis + Lua)    │    │
                                  │  └─────────┬──────────┘    │
                                  │            │               │
                                  │  ┌─────────┴──────────┐    │
                                  │  │   SNS Delivery     │    │
                                  │  └─────────┬──────────┘    │
                                  └────────────┼───────────────┘
                                               │
                                               ▼
                                      ┌────────────────┐
                                      │    AWS SNS     │
                                      └───────┬────────┘
                                              │
                                    ┌─────────┴─────────┐
                                    ▼                   ▼
                              ┌──────────┐        ┌──────────┐
                              │   FCM    │        │  APNs    │
                              │(Android) │        │  (iOS)   │
                              └──────────┘        └──────────┘
```

### Component Responsibilities

```
| Component      | Responsibility                               |
|----------------|----------------------------------------------|
| gRPC Server    | Receives event batches from upstream         |
| Worker Pool    | Processes events with controlled concurrency |
| Topic Cache    | Redis cache for SNS topic ARN lookups        |
| Deduplicator   | Atomic dedup with Redis Lua scripts          |
| SNS Delivery   | Formats and publishes to AWS SNS             |

```

## Key Takeaways

1. **Distributed systems need distributed state**. In-memory deduplication doesn't work with multiple instances. Use Redis with atomic operations.

2. **asyncio ≠ parallelism**. Python's event loop is concurrent but single-threaded. For true parallelism, you need multiple processes or a different language.

3. **Explicit is better than implicit**. Go's error handling and dependency injection make code easier to test and debug.

4. **Fail-open for critical notifications**. Better to occasionally duplicate than miss a goal notification.

5. **Measure, don't assume**. The 5x throughput improvement came from profiling and understanding where time was actually spent.

## Should You Rewrite?

Not necessarily. Rewrites are expensive and risky. But consider Go when:

- You need true parallelism (not just concurrency)
- You're deploying to containers at scale
- You have distributed state that needs atomic operations
- Your team is comfortable with statically-typed languages

The Python version worked fine at lower scale. But for handling thousands of events per second with guaranteed exactly-once delivery, Go was the right choice.
