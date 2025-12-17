---
title: "Building a High-Performance Push Notifications Service in Go"
date: 2025-12-17
draft: false
description: "
Building a push notifications service that handles millions of events reliably requires careful architectural decisions. In this post, I'll walk through how I built a notifications backend in Go that processes events via gRPC and delivers push notifications through AWS SNS.
"
tags: ["go", "grpc", "aws-sns", "redis", "notifications", "distributed-systems"]
---

Building a push notifications service that handles millions of events reliably requires careful architectural decisions. In this post, I'll walk through how I built a notifications backend in Go that processes events via gRPC and delivers push notifications through AWS SNS.

## Architecture Overview

The service follows a simple but effective architecture:

```
gRPC Events → Event Processor → Deduplication → SNS Publisher → FCM/APNs
                                     ↓
                                   Redis
```

**Key components:**

- **gRPC server** for receiving events from upstream services
- **Worker pool** for concurrent event processing
- **Redis-based deduplication** to prevent duplicate notifications
- **AWS SNS** for cross-platform delivery (FCM for Android, APNs for iOS)

## Project Structure

I organized the codebase following Go best practices:

```
├── cmd/
│   ├── server/          # Main application entry point
│   └── tasks/           # CLI for scheduled tasks
├── internal/
│   ├── config/          # Configuration management
│   ├── dedup/           # Deduplication logic
│   ├── grpc/            # gRPC handlers
│   ├── processor/       # Event processing
│   ├── repository/      # Database access
│   ├── sns/             # AWS SNS client
│   └── tasks/           # Scheduled task implementations
├── pkg/
│   └── proto/           # Protocol buffer definitions
└── scripts/
    └── benchmark/       # Performance testing tools
```

## Distributed Deduplication with Redis

One of the critical requirements was preventing duplicate notifications. Users receiving the same notification multiple times creates a poor experience.

The challenge: with multiple service instances behind a load balancer, traditional in-memory deduplication doesn't work. The same event could arrive at different instances.

**Solution:** Atomic Redis operations using Lua scripts.

```go
type Deduplicator struct {
    redis       redis.UniversalClient
    ttl         time.Duration
    dedupScript *redis.Script
}

func New(redisClient redis.UniversalClient, ttl time.Duration, logger *zap.Logger) *Deduplicator {
    // Lua script for atomic check-and-set
    // Returns: 0 = duplicate, 1 = new, 2 = correction
    script := redis.NewScript(`
        local existing = redis.call('GET', KEYS[1])
        if existing then
            local data = cjson.decode(existing)
            if data.h == ARGV[1] then
                return 0  -- Duplicate: same hash
            else
                -- Correction: different hash, update and allow
                redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
                return 2
            end
        else
            -- New: set and allow
            redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
            return 1
        end
    `)

    return &Deduplicator{
        redis:       redisClient,
        ttl:         ttl,
        dedupScript: script,
    }
}
```

The Lua script runs atomically on Redis, ensuring that even with concurrent requests from multiple instances, only one notification is sent per unique event.

**Three deduplication states:**

1. **New (1):** First time seeing this event, allow notification
2. **Duplicate (0):** Same event with same content, block
3. **Correction (2):** Same event but content changed (score correction), allow

## Worker Pool Pattern

For processing events concurrently while maintaining control over resource usage, I implemented a worker pool:

```go
type EventProcessor struct {
    eventChan   chan EventJob
    workerCount int
    wg          sync.WaitGroup
    // ... other fields
}

func (p *EventProcessor) Start(ctx context.Context) {
    p.eventChan = make(chan EventJob, p.workerCount*10)

    for i := 0; i < p.workerCount; i++ {
        p.wg.Add(1)
        go p.worker(ctx, i)
    }
}

func (p *EventProcessor) worker(ctx context.Context, id int) {
    defer p.wg.Done()

    for {
        select {
        case <-ctx.Done():
            return
        case job, ok := <-p.eventChan:
            if !ok {
                return
            }
            p.processSingleEvent(job.ctx, &job.event)
        }
    }
}
```

This pattern provides:

- **Bounded concurrency:** Control exactly how many goroutines process events
- **Backpressure:** Buffered channel prevents overwhelming the system
- **Graceful shutdown:** WaitGroup ensures all in-flight work completes

## gRPC Handler with Fire-and-Forget

The gRPC handler receives events and returns immediately, processing asynchronously:

```go
func (h *EventsHandler) NotifyEvents(ctx context.Context, req *pb.EventsRequest) (*emptypb.Empty, error) {
    events := make([]models.EventRequest, 0, len(req.Events))
    for _, e := range req.Events {
        events = append(events, convertProtoToModel(e))
    }

    // Fire and forget - process in background
    go func() {
        processCtx := context.Background()
        if err := h.processor.ProcessEvents(processCtx, events); err != nil {
            h.logger.Error("failed to process events", zap.Error(err))
        }
    }()

    return &emptypb.Empty{}, nil
}
```

This design choice prioritizes:

- **Low latency:** Clients don't wait for processing
- **Reliability:** Processing continues even if client disconnects
- **Throughput:** Server can accept more requests while processing previous ones

## SNS Integration

AWS SNS handles the complexity of delivering to different platforms. The service publishes to SNS topics, which fan out to platform-specific endpoints:

```go
type Publisher struct {
    client *sns.Client
    logger *zap.Logger
}

func (p *Publisher) Publish(ctx context.Context, topicARN string, message SNSMessage) error {
    payload, err := json.Marshal(message)
    if err != nil {
        return fmt.Errorf("marshal message: %w", err)
    }

    input := &sns.PublishInput{
        TopicArn: aws.String(topicARN),
        Message:  aws.String(string(payload)),
    }

    _, err = p.client.Publish(ctx, input)
    if err != nil {
        return fmt.Errorf("publish to SNS: %w", err)
    }

    return nil
}
```

## Configuration Management

I used a struct-based configuration with environment variable support:

```go
type Config struct {
    GRPCPort           int    `env:"GRPC_PORT" envDefault:"50052"`
    HTTPPort           int    `env:"HTTP_PORT" envDefault:"8080"`
    DatabaseURL        string `env:"DATABASE_URL,required"`
    RedisURL           string `env:"REDIS_URL,required"`
    AWSRegion          string `env:"AWS_REGION" envDefault:"us-east-1"`
    EventWorkers       int    `env:"EVENT_WORKERS" envDefault:"10"`
    DedupTTL           time.Duration `env:"DEDUP_TTL" envDefault:"6h"`
}

func Load() (*Config, error) {
    cfg := &Config{}
    if err := env.Parse(cfg); err != nil {
        return nil, fmt.Errorf("parse config: %w", err)
    }
    return cfg, nil
}
```

## Health Checks

A simple health endpoint for Kubernetes/load balancer probes:

```go
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
    health := map[string]string{"status": "healthy"}

    // Check Redis
    if err := s.redis.Ping(r.Context()).Err(); err != nil {
        health["status"] = "unhealthy"
        health["redis"] = err.Error()
    }

    // Check Database
    if err := s.db.Ping(r.Context()); err != nil {
        health["status"] = "unhealthy"
        health["database"] = err.Error()
    }

    w.Header().Set("Content-Type", "application/json")
    if health["status"] != "healthy" {
        w.WriteHeader(http.StatusServiceUnavailable)
    }
    json.NewEncoder(w).Encode(health)
}
```

## Scheduled Tasks with Cobra

The service includes scheduled tasks for maintenance operations, built with Cobra CLI:

```go
var rootCmd = &cobra.Command{
    Use:   "tasks",
    Short: "Notifications backend scheduled tasks",
}

var pruneCmd = &cobra.Command{
    Use:   "prune",
    Short: "Prune expired data",
}

var pruneMatchTopicsCmd = &cobra.Command{
    Use:   "match-topics",
    Short: "Remove topics for matches that ended over 24 hours ago",
    RunE: func(cmd *cobra.Command, args []string) error {
        deps, err := initDeps()
        if err != nil {
            return err
        }
        defer deps.Close()

        runner := tasks.NewRunner(deps.DB, deps.Redis, deps.SNS, deps.Logger)
        return runner.PruneMatchTopics(deps.Ctx, dryRun)
    },
}
```

Tasks include:

- `prune match-topics` - Clean up old match topics
- `prune disabled-devices` - Remove inactive devices
- `generate match-topics` - Create topics for upcoming matches
- `subscriptions fix` - Repair subscription inconsistencies

## Performance Results

Benchmarking on a standard development machine:

```
╔══════════════════════════════════════════════════════════════╗
║                    BENCHMARK RESULTS                         ║
╠══════════════════════════════════════════════════════════════╣
║  Total events:              5000                             ║
║  Concurrency:                 10 workers                     ║
║  Total time:               265ms                             ║
╠══════════════════════════════════════════════════════════════╣
║                         THROUGHPUT                           ║
╠══════════════════════════════════════════════════════════════╣
║  Events/second:            18,879                            ║
╠══════════════════════════════════════════════════════════════╣
║                          LATENCY                             ║
╠══════════════════════════════════════════════════════════════╣
║  Average:                  525µs                             ║
║  P50 (median):             414µs                             ║
║  P95:                      1.16ms                            ║
║  P99:                      2.72ms                            ║
╠══════════════════════════════════════════════════════════════╣
║                        RELIABILITY                           ║
╠══════════════════════════════════════════════════════════════╣
║  Success rate:             100%                              ║
╚══════════════════════════════════════════════════════════════╝
```

## Key Takeaways

1. **Lua scripts in Redis** provide atomic operations essential for distributed deduplication
2. **Worker pools** give fine-grained control over concurrency
3. **Fire-and-forget gRPC handlers** maximize throughput for async workloads
4. **SNS abstracts platform complexity** - one publish, delivery to iOS and Android
5. **Structured configuration** makes deployment across environments straightforward

## What's Next

Future improvements I'm considering:

- **Metrics with Prometheus** for better observability
- **Rate limiting** per device to prevent notification spam
- **Message batching** for SNS to reduce API calls
- **Dead letter queue** for failed notifications

---

The full architecture handles the real-world requirements of a sports notifications app: high throughput during live matches, reliable delivery, and no duplicate notifications. Go's concurrency primitives and the ecosystem of libraries made building this service straightforward.
