---
title: "How DDIA's Stream Processing Concepts Apply to Real-Time Notification Systems"
date: 2026-02-21
description: "Connecting Designing Data-Intensive Applications' stream processing concepts—event streams, fan-out, backpressure, materialized views, and exactly-once semantics—to real architectural decisions in a live sports notification system built with Go, gRPC, and AWS SNS."
tags:
  [
    "golang",
    "stream-processing",
    "grpc",
    "distributed-systems",
    "ddia",
    "event-driven",
    "real-time",
    "system-design",
    "notifications",
    "backend",
  ]
---

When I first picked up Martin Kleppmann's _Designing Data-Intensive Applications_, the stream processing chapters hit differently. Not because the concepts were new—I'd been working with event-driven systems and real-time data pipelines for a while—but because Kleppmann frames them in a way that makes you rethink systems you've already built.

I work on a real-time sports platform that delivers live score updates and push notifications to hundreds of thousands of concurrent users. The challenges are exactly what DDIA describes: ordering guarantees, fault tolerance, backpressure, and the eternal question of "what happens when a consumer falls behind?" This post connects DDIA's stream processing concepts to the real architectural decisions I've faced building notification systems at scale.

## The Event Stream: Where Everything Starts

DDIA's most powerful idea about stream processing is deceptively simple: **an event stream is an unbounded, ordered sequence of records**. Kleppmann dedicates significant attention to this concept because it's the foundation of everything else—event sourcing, change data capture, stream processing, and yes, notification systems.

In our architecture, every score update, every goal, every card is an event. A dataloader service watches live match feeds and pushes structured events to our notification backend via gRPC. Each event carries everything the notification system needs to decide what to send, to whom, and how:

```go
type EventRequest struct {
    MatchID      uuid.UUID
    TopicKind    TopicKind // GOAL, PENALTY, RED_CARD, START, END, ...
    GameTime     string
    TeamGround   TeamGround
    HomeName     TranslatedName
    AwayName     TranslatedName
    PlayerName   *TranslatedName
    HomeScore    int32
    AwayScore    int32
    UniqueKey    string // deduplication identifier
    SilentUpdate bool   // correction to a previous notification
}
```

The `UniqueKey` field matters more than you'd think. DDIA explains that in a distributed system, events can arrive out of order or be delivered more than once. Our dataloader can send the same event multiple times—network retries, upstream replays—and the `UniqueKey` is what lets us distinguish a genuine new event from a duplicate or a correction (like a scorer name being updated after VAR review).

We group events by `MatchID` for ordered processing within each match, while processing different matches concurrently. This mirrors DDIA's discussion of partitioned processing: ordering is guaranteed where it matters (within a match), but we don't pay the cost of global ordering across independent streams.

## Fan-Out: From One Event to Thousands of Devices

DDIA describes two messaging patterns: **load balancing** (each message goes to one consumer) and **fan-out** (each message goes to all consumers). Most real systems need both, and notification systems are a perfect example.

Our pipeline looks like this:

1. **Dataloader** pushes score events via gRPC
2. **gRPC handler** converts protobuf to internal models, dispatches asynchronously
3. **Event processor** (worker pool) looks up the SNS topic, deduplicates, builds the FCM payload, and publishes to SNS
4. **AWS SNS** fans out to all subscribed device endpoints via FCM/APNs

The fan-out happens at the SNS layer. Each match and event type combination has its own SNS topic (e.g., `prod_{match_id}_GOAL_PORTUGUESE`). When a user subscribes to a match, their device endpoint is subscribed to the relevant SNS topics. When a goal event arrives, we publish once to the SNS topic and SNS handles fanning the notification out to every subscribed device:

```go
func (p *EventProcessor) processSingleEvent(ctx context.Context, event *EventRequest) error {
    // 1. Look up or auto-create the SNS topic for this match + event type
    topic, err := p.getOrCreateTopic(ctx, event)
    if err != nil {
        return err
    }

    // 2. Deduplicate — is this a new event, a correction, or a duplicate?
    result, _ := p.deduplicator.Check(ctx, event, topic.ARN)
    if result == dedup.ResultDuplicate {
        return nil // Already sent, skip
    }

    // 3. Delay corrections so the original notification is seen first
    if result == dedup.ResultCorrection && event.SilentUpdate {
        time.Sleep(5 * time.Second)
    }

    // 4. Build multi-language FCM v1 payload and publish to SNS
    payload := p.buildNotificationRequest(event)
    return p.deliverer.Deliver(ctx, event, topic, payload)
}
```

This is a fundamentally different fan-out model than what DDIA describes with Kafka consumer groups. Instead of each consumer maintaining its own position in a log, SNS acts as a push-based fan-out layer—we publish once, and the platform handles delivery to potentially hundreds of thousands of devices. The tradeoff is that we lose replayability at the fan-out layer (SNS is fire-and-forget), but we gain massive simplification in delivery infrastructure. DDIA's framework helps you see this tradeoff clearly.

## Backpressure: Three Layers of Defense

This is where DDIA's wisdom saved us from nasty production issues. Kleppmann discusses the fundamental tension in stream processing: **what happens when a producer is faster than a consumer?**

The three options he outlines are: drop messages, buffer them (risking memory overflow), or apply backpressure (slow down the producer). In a notification system pushing to an external service like AWS SNS, none of these are ideal in isolation. During a busy match day—think multiple simultaneous high-profile matches—our event rate spikes dramatically, and AWS SNS has rate limits.

Our solution layers three DDIA-inspired patterns:

**Layer 1: Bounded async processing.** The gRPC handler returns immediately and dispatches work via a bounded semaphore. This prevents the dataloader from being blocked by notification processing, while capping concurrent goroutines:

```go
type EventHandler struct {
    processor EventProcessor
    asyncSem  chan struct{} // capacity: 1000
}

func (h *EventHandler) NotifyEvents(ctx context.Context, req *pb.EventsRequest) (*emptypb.Empty, error) {
    events := convertEvents(req.Events)

    h.asyncSem <- struct{}{} // acquire
    go func() {
        defer func() { <-h.asyncSem }() // release
        h.processor.ProcessEvents(context.Background(), events)
    }()

    return &emptypb.Empty{}, nil
}
```

**Layer 2: Adaptive rate limiting.** When SNS starts throttling, we back off exponentially. When it recovers, we speed up gradually:

```go
type AdaptiveRateLimiter struct {
    currentDelay   time.Duration
    backoffFactor  float64 // 2.0 — double on error
    recoveryFactor float64 // 0.8 — 20% faster on success
    minDelay       time.Duration // 10ms
    maxDelay       time.Duration // 5s
}

func (r *AdaptiveRateLimiter) RecordError(isThrottling bool) {
    factor := r.backoffFactor
    if isThrottling {
        factor *= 1.5 // extra penalty for throttling
    }
    r.currentDelay = min(r.currentDelay * factor, r.maxDelay)
}

func (r *AdaptiveRateLimiter) RecordSuccess() {
    if r.consecutiveSuccess >= r.successThreshold {
        r.currentDelay = max(r.currentDelay * r.recoveryFactor, r.minDelay)
    }
}
```

**Layer 3: Circuit breakers.** If SNS is persistently failing, we stop hammering it entirely and let it recover:

```go
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
    if cb.state == StateOpen {
        if time.Now().Before(cb.expiry) {
            return ErrCircuitOpen // Fast-fail, don't even try
        }
        cb.setState(StateHalfOpen) // Timeout elapsed, test recovery
    }

    err := fn()
    if err == nil {
        cb.onSuccess()
    } else {
        cb.onFailure(err)
    }
    return err
}
```

We run separate circuit breaker instances for delivery and subscriptions. This is a critical design choice: if the subscription API is being throttled, that must not block notification delivery. DDIA's framework for thinking about failure isolation across pipeline stages directly informed this separation.

## Materialized Views: Topic Caching as Stream-Table Duality

One of DDIA's most elegant concepts is the **stream-table duality**: a stream can be seen as the changelog of a table, and a table can be seen as the materialized state of a stream. This directly applies to how we handle topic resolution.

Every score event needs to be published to an SNS topic. The topic metadata (ARN, kind, match association) lives in PostgreSQL, but with hundreds of events per minute, hitting the database for every single event would be brutal.

Instead, we maintain a Redis-backed materialized view of the topics table:

```go
func (p *EventProcessor) getOrCreateTopic(ctx context.Context, event *EventRequest) (*TopicCache, error) {
    // Try cache first (Redis)
    topic, _ := p.topicCache.Get(ctx, event.MatchID, event.TopicKind)
    if topic != nil {
        return topic, nil
    }

    // Cache miss — query PostgreSQL
    dbTopic, err := p.topicRepo.GetByMatchAndKind(ctx, event.MatchID, event.TopicKind)
    if err != nil {
        return nil, err
    }

    // Topic doesn't exist yet — auto-create in SNS + DB
    if dbTopic == nil {
        dbTopic, err = p.createMatchTopic(ctx, event.MatchID, event.TopicKind)
        if err != nil {
            return nil, err
        }
    }

    // Cache the result
    cached := &TopicCache{ARN: dbTopic.ARN, Kind: dbTopic.Kind, MatchID: event.MatchID.String()}
    p.topicCache.Set(ctx, event.MatchID, event.TopicKind, cached)
    return cached, nil
}
```

This is stream-table duality in practice, just not with Kafka. The "stream" is the flow of topic creation events (SNS topic created, stored in PostgreSQL). The "table" is our Redis cache—a materialized view optimized for the event processor's access pattern (lookup by matchID + topicKind). When the underlying data changes (topic deleted externally, new topic created), we invalidate the cache and let it rebuild from the source.

The auto-creation pattern also handles race conditions: when two workers try to create the same topic simultaneously, the second one catches the PostgreSQL unique constraint violation and fetches the topic that the first worker created. No distributed locking, no coordination—just idempotent writes and a well-chosen unique constraint.

## Exactly-Once Semantics: The Pragmatic Reality

DDIA is refreshingly honest about exactly-once processing: it's hard, and in many distributed systems, what you actually achieve is **effectively-once** through idempotency. This resonated with our notification system because sending a duplicate goal notification is a terrible user experience.

But our deduplication requirements are more nuanced than a simple "seen this before? skip it." In sports, events get corrected—a goal scorer's name might be updated, a VAR decision might reverse a penalty. We need to distinguish three cases: **new** events, **corrections** to previous events, and true **duplicates**.

We handle this with an atomic Redis Lua script:

```go
// Lua script for atomic check-and-set deduplication
// Returns: 0 = duplicate, 1 = new, 2 = correction
const dedupScript = `
    local existing = redis.call('GET', KEYS[1])
    if existing then
        local data = cjson.decode(existing)
        if data.h == ARGV[1] then
            return 0  -- Duplicate: same payload hash
        else
            redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
            return 2  -- Correction: different payload, update and send
        end
    else
        redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
        return 1  -- New event
    end
`
```

The payload hash is computed from the fields that make a notification different—score, player name, goal type, game time:

```go
func (d *Deduplicator) computePayloadHash(event *EventRequest) string {
    payload := fmt.Sprintf("%s|%d|%d|%s|%s|%s",
        event.TopicKind, event.HomeScore, event.AwayScore,
        event.PlayerName, event.GoalType, event.GameTime,
    )
    hash := sha256.Sum256([]byte(payload))
    return hex.EncodeToString(hash[:8])
}
```

The key is composed of `dedup:{unique_key}:{topic_arn}` with a 24-hour TTL. We don't need to remember every event forever—just long enough to cover retries and upstream replays. On Redis failure, we fail open: better to send a duplicate than to miss a goal notification entirely.

DDIA's point about the tradeoff between exactly-once guarantees and system complexity is one I think about often. Perfect deduplication across a distributed system requires coordination, and coordination is the enemy of throughput. The three-way classification (new/correction/duplicate) gives us precision where it matters without the overhead of distributed transactions.

## Circuit Breakers: Fault Isolation in the Pipeline

DDIA discusses fault tolerance extensively, and one key insight is that **failures in one part of a pipeline should not cascade to others**. In our system, we depend on AWS SNS for both notification delivery and subscription management. These are fundamentally different workloads with different failure modes.

Subscription operations (subscribing a user's device to a match topic) are bulk operations that can trigger SNS throttling during peak hours. Notification delivery (publishing a goal event to a topic) is latency-sensitive and must not be blocked by subscription throttling.

We solve this by running separate circuit breaker instances:

```go
// In main.go — two SNS clients, two circuit breakers
snsDeliveryBreaker := circuitbreaker.New(circuitbreaker.Config{
    Name:         "sns-delivery",
    FailureRatio: 0.6,
    MinRequests:  10,
    Timeout:      30 * time.Second,
})

snsSubscriptionBreaker := circuitbreaker.New(circuitbreaker.Config{
    Name:         "sns-subscription",
    FailureRatio: 0.6,
    MinRequests:  10,
    Timeout:      30 * time.Second,
})
```

When the subscription breaker trips (because we're creating thousands of subscriptions during a peak registration period), notification delivery continues unaffected. The subscription service uses its own adaptive rate limiter to find the maximum throughput SNS will accept without throttling, while delivery blasts through at full speed on its own circuit.

This maps directly to DDIA's discussion of isolation between stream processing stages. Each stage should handle its own backpressure and failure independently. The dataloader doesn't slow down when the notification service is overwhelmed—fire-and-forget with bounded concurrency handles that. The notification publisher doesn't stop when subscriptions are being throttled—separate circuit breakers handle that. Each boundary is explicit and independently configurable.

## Fault Tolerance: Self-Healing Infrastructure

DDIA emphasizes that in a well-designed stream processing system, **failures should be recoverable without manual intervention**. Our notification system faces a specific reliability challenge: SNS topics can be deleted externally (infrastructure changes, accidental deletions, AWS maintenance), and when that happens, notifications silently fail.

We handle this with automatic topic recreation:

```go
func (p *EventProcessor) processSingleEvent(ctx context.Context, event *EventRequest) error {
    topic, err := p.getOrCreateTopic(ctx, event)
    if err != nil {
        return err
    }

    payload := p.buildNotificationRequest(event)
    err = p.deliverer.Deliver(ctx, event, topic, payload)

    if errors.Is(err, delivery.ErrTopicNotFound) {
        // SNS topic was deleted externally — recreate and retry
        p.topicCache.Delete(ctx, event.MatchID, event.TopicKind)
        newTopic, err := p.recreateTopic(ctx, event)
        if err != nil {
            return err
        }
        return p.deliverer.Deliver(ctx, event, newTopic, payload)
    }

    return err
}
```

The `recreateTopic` function invalidates the Redis cache, deletes the stale database record, creates a fresh SNS topic, stores the new ARN, and caches the result. All of this happens transparently—no alerts, no manual intervention, no missed notifications beyond the one that triggered the recreation.

Combined with our deduplication's fail-open behavior (Redis down? send anyway) and the bounded async processing (gRPC handler never blocks), the system has multiple layers of resilience. This is the "defense in depth" approach that DDIA advocates: don't rely on any single component being available, and design each piece to degrade gracefully.

## What DDIA Taught Me That Production Confirmed

Reading DDIA's stream processing chapters after building these systems was like having someone articulate patterns I'd discovered through trial and error. A few key takeaways:

**The event stream is the heart of the system.** Every architectural decision flows from treating events as the source of truth. When the dataloader sends a goal event, it flows through deduplication, topic resolution, payload building, and delivery—each step a transformation of that original event. Kleppmann's emphasis on this foundational concept isn't academic—it's the most practical advice in the book.

**Derived data simplifies everything.** Our Redis topic cache is derived from PostgreSQL. Our SNS topic subscriptions are derived from user preferences. Each downstream system owns its own representation of the data, optimized for its specific access patterns. This is exactly the derived data philosophy DDIA advocates, and it works beautifully in practice.

**Exactly-once is a spectrum, not a binary.** We spent weeks early on trying to achieve perfect deduplication across the entire pipeline. DDIA helped me understand that atomic deduplication with bounded TTLs and a fail-open policy is not a compromise—it's the pragmatic engineering choice. The three-way new/correction/duplicate classification emerged from understanding that "duplicate" isn't always binary.

**Backpressure must be designed, not discovered.** Every production incident we've had with the notification system traces back to some component being overwhelmed. Bounded semaphores, adaptive rate limiters, and circuit breakers form three layers of protection. DDIA's framework for thinking about what happens when producers outpace consumers should be the first thing you design, not the last.

## Conclusion

_Designing Data-Intensive Applications_ remains the best single resource for understanding the principles behind modern data systems. The stream processing chapters in particular provide a mental framework that directly maps to real-world notification systems, event-driven architectures, and any system where data flows continuously.

What surprised me most is how well the concepts apply even when you're not using Kafka or a traditional log-based architecture. Our system uses gRPC for ingestion and AWS SNS for delivery—no log, no consumer offsets, no replay. But the mental models from DDIA—event streams as the source of truth, derived data, backpressure as a first-class concern, fault isolation between stages—shaped every architectural decision.

If you're building systems like these, I'd encourage you to read the stream processing chapters with your own architecture in mind. Draw the parallels. Question where your system diverges from the patterns DDIA describes, and ask yourself whether that divergence is intentional or accidental.

The best systems aren't built by following patterns blindly—they're built by engineers who understand the tradeoffs deeply enough to make informed decisions. DDIA gives you that depth.

---
