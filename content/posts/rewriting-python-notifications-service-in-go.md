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

I recently rewrote our sports notifications backend from Python to Go. The original service handled match events (goals, cards, penalties) and delivered push notifications via AWS SNS to millions of mobile devices through FCM and APNs.

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
# Python: Per-instance deduplication (broken!)
class EventProcessor:
    def __init__(self):
        self._seen_events = {}  # In-memory dict

    async def process_event(self, event):
        key = f"{event.unique_key}:{event.topic_arn}"

        if key in self._seen_events:
            return  # Skip duplicate

        self._seen_events[key] = True
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

Go's version uses Redis with atomic Lua scripts:

```go
// Go: Distributed deduplication (correct!)
const luaScript = `
local key = KEYS[1]
local hash = ARGV[1]
local ttl = tonumber(ARGV[2])

local existing = redis.call('GET', key)
if existing then
    local data = cjson.decode(existing)
    if data.hash == hash then
        return 0  -- DUPLICATE: exact same payload
    else
        redis.call('SET', key, ARGV[3], 'EX', ttl)
        return 2  -- CORRECTION: same event, updated data
    end
else
    redis.call('SET', key, ARGV[3], 'EX', ttl)
    return 1  -- NEW: first time seeing this
end
`

func (d *Deduplicator) Check(ctx context.Context, event *Event, topicARN string) (Result, error) {
    key := fmt.Sprintf("dedup:%s:%s", event.UniqueKey, topicARN)
    hash := d.computeHash(event)

    result, err := d.script.Run(ctx, d.redis, []string{key}, hash, ttlSeconds, entry).Int()
    if err != nil {
        // Fail-open: better duplicate than missed goal
        return ResultNew, nil
    }

    return Result(result), nil
}
```

**Why Lua scripts?** The entire check-and-set operation happens atomically inside Redis. No race conditions possible:

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
    await asyncio.gather(*[
        process_event(e) for e in events
    ])
    # All tasks share ONE thread
    # I/O-bound only, can't use multiple CPU cores
```

Python's `asyncio` is _concurrent_ but not _parallel_. All coroutines run on a single thread, sharing one CPU core. When you `await` an I/O operation, other tasks can run — but CPU-bound work blocks everything.

### Go: True Parallelism

```go
type EventProcessor struct {
    eventChan chan *Event
    workers   int
}

func (p *EventProcessor) Start(ctx context.Context) {
    // Spawn N workers across multiple OS threads
    for i := 0; i < p.workers; i++ {
        go func(workerID int) {
            for event := range p.eventChan {
                p.processSingleEvent(ctx, event)
            }
        }(i)
    }
}
```

Go's goroutines are multiplexed across OS threads by the runtime. With 10 workers on an 8-core machine, you get true parallelism — CPU-bound and I/O-bound work both scale.

### Worker Pool Benefits

The worker pool pattern provides:

1. **Controlled concurrency**: Fixed number of workers prevents resource exhaustion
2. **Backpressure**: When the channel fills up, producers block
3. **Graceful shutdown**: Close channel, wait for workers to finish

```go
func (p *EventProcessor) Stop() {
    close(p.eventChan)  // Signal workers to stop
    p.wg.Wait()         // Wait for in-flight events
}
```

## SNS Delivery: Building FCM-Compatible Messages

AWS SNS acts as a fan-out layer, delivering to FCM (Android) and APNs (iOS) simultaneously:

```go
func (s *SNSDeliverer) PublishToTopic(ctx context.Context, topicARN string, payload *Payload) error {
    // Build FCM-compatible message
    fcmMessage := map[string]interface{}{
        "notification": map[string]string{
            "title": payload.Title.Portuguese,
            "body":  payload.Body.Portuguese,
        },
        "data": map[string]string{
            "match_id":   payload.Data.MatchID,
            "topic_kind": payload.Data.TopicKind,
            // All languages for client-side rendering
            "title_pt": payload.Title.Portuguese,
            "title_en": payload.Title.English,
            "title_es": payload.Title.Spanish,
            "body_pt":  payload.Body.Portuguese,
            "body_en":  payload.Body.English,
            "body_es":  payload.Body.Spanish,
        },
        "android": map[string]interface{}{
            "priority": "high",
            "notification": map[string]interface{}{
                "channel_id": "match_events",
            },
        },
    }

    // SNS message structure for multi-platform delivery
    snsMessage := map[string]string{
        "default": payload.Body.Portuguese,
        "GCM":     toJSON(fcmMessage),
    }

    _, err := s.sns.Publish(ctx, &sns.PublishInput{
        TopicArn:         aws.String(topicARN),
        Message:          aws.String(toJSON(snsMessage)),
        MessageStructure: aws.String("json"),
    })

    return err
}
```

**Why include all languages in data?** SNS topics are language-specific (users subscribe to their preferred language), but the data payload contains all translations. This allows the mobile app to switch display language without re-subscribing.

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
func (p *Processor) processEvent(ctx context.Context, event *Event) error {
    if err := p.sns.Publish(ctx, topicARN, payload); err != nil {
        // Can't ignore this - code won't compile
        return fmt.Errorf("sns publish failed for %s: %w", event.MatchID, err)
    }
    return nil
}
```

The Go compiler is your safety net. You can't forget to handle an error — you have to explicitly ignore it with `_`.

### Fail-Open Strategy

For deduplication, we chose a fail-open strategy:

```go
result, err := d.script.Run(ctx, d.redis, keys, args...).Int()
if err != nil {
    d.logger.Warn("dedup check failed, allowing through", zap.Error(err))
    return ResultNew, nil  // Allow notification
}
```

**Why fail-open?** In a sports notification system, a missed goal notification is worse than a duplicate. Users can ignore duplicates but can't recover missed events.

## Dependency Injection: Testing Made Easy

Python's global imports make testing painful:

```python
# service.py
from app.services import aws_sns_service  # Global import

async def send_notification(event):
    await aws_sns_service.publish(...)  # How do you mock this?
```

Go's explicit dependencies make mocking trivial:

```go
// Constructor injection
type EventProcessor struct {
    deliverer Deliverer  // Interface
    dedup     Deduplicator
    cache     TopicCache
}

func NewEventProcessor(d Deliverer, dd Deduplicator, c TopicCache) *EventProcessor {
    return &EventProcessor{deliverer: d, dedup: dd, cache: c}
}

// In tests:
func TestEventProcessor(t *testing.T) {
    mockDeliverer := &MockDeliverer{}
    mockDedup := &MockDeduplicator{result: dedup.ResultNew}

    processor := NewEventProcessor(mockDeliverer, mockDedup, mockCache)

    err := processor.Process(ctx, event)

    assert.NoError(t, err)
    assert.True(t, mockDeliverer.Called)
}
```

### Testing with miniredis

For deduplication tests, we use `miniredis` — an in-memory Redis implementation:

```go
func TestDeduplicator_Duplicate(t *testing.T) {
    mr := miniredis.RunT(t)
    client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
    dedup := NewDeduplicator(client, 6*time.Hour, zap.NewNop())

    // First check: NEW
    result1, _ := dedup.Check(ctx, event, "arn:...")
    assert.Equal(t, ResultNew, result1)

    // Second check: DUPLICATE
    result2, _ := dedup.Check(ctx, event, "arn:...")
    assert.Equal(t, ResultDuplicate, result2)
}

func TestDeduplicator_TTLExpiry(t *testing.T) {
    mr := miniredis.RunT(t)
    // ... setup ...

    // Time travel!
    mr.FastForward(7 * time.Hour)

    // Same event is "new" again after TTL
    result, _ := dedup.Check(ctx, event, "arn:...")
    assert.Equal(t, ResultNew, result)
}
```

No external Redis required. Deterministic. Fast.

## Deployment: From 500MB to 50MB

Python requires the interpreter, pip packages, and system dependencies:

```dockerfile
# Python: 500MB image
FROM python:3.11-slim

RUN apt-get update && apt-get install -y \
    gcc libpq-dev && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install -r requirements.txt

COPY . .
CMD ["python", "-m", "notifications_backend"]
```

Go compiles to a static binary:

```dockerfile
# Go: 50MB image
FROM golang:1.23-alpine AS builder
COPY . .
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
│  Data Loader    │──────────────▶│   Notifications Backend    │
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

| Component        | Responsibility                               |
| ---------------- | -------------------------------------------- |
| **gRPC Server**  | Receives event batches from upstream         |
| **Worker Pool**  | Processes events with controlled concurrency |
| **Topic Cache**  | Redis cache for SNS topic ARN lookups        |
| **Deduplicator** | Atomic dedup with Redis Lua scripts          |
| **SNS Delivery** | Formats and publishes to AWS SNS             |

## Configuration: Environment-First

Go's Viper library handles configuration elegantly:

```go
type Config struct {
    Server   ServerConfig   `mapstructure:"server"`
    Redis    RedisConfig    `mapstructure:"redis"`
    AWS      AWSConfig      `mapstructure:"aws"`
    Dedup    DedupConfig    `mapstructure:"dedup"`
}

func Load() (*Config, error) {
    v := viper.New()

    v.SetEnvPrefix("NOTIFICATIONS")
    v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
    v.AutomaticEnv()

    // Set defaults
    v.SetDefault("server.grpc_port", 50052)
    v.SetDefault("server.event_workers", 10)
    v.SetDefault("dedup.ttl", "6h")

    var cfg Config
    return &cfg, v.Unmarshal(&cfg)
}
```

Environment variables map automatically:

- `NOTIFICATIONS_SERVER_GRPC_PORT` → `cfg.Server.GRPCPort`
- `NOTIFICATIONS_DEDUP_TTL` → `cfg.Dedup.TTL`

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

---

_If you're facing similar challenges with notification systems at scale, I'd love to hear about your approach._
