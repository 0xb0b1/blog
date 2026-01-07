---
title: "Graceful Shutdown in Go Is Harder Than You Think"
date: 2025-07-11
description: "Handling in-flight requests, Kafka consumers, database connections, and Kubernetes termination signals correctly. The details that tutorials skip."
tags:
  [
    "golang",
    "backend",
    "kubernetes",
    "production",
    "reliability",
    "distributed-systems",
  ]
---

"Just call `server.Shutdown()`" is advice that works great until you have a Kafka consumer mid-batch, a database transaction in progress, and Kubernetes sending SIGTERM while your readiness probe still returns healthy.

Graceful shutdown seems simple. It isn't. Here's everything that can go wrong and how to handle it.

## The Naive Approach

Most tutorials show something like this:

```go
func main() {
    server := &http.Server{Addr: ":8080", Handler: handler}

    go func() {
        if err := server.ListenAndServe(); err != http.ErrServerClosed {
            log.Fatal(err)
        }
    }()

    // Wait for interrupt
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    // Shutdown with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    server.Shutdown(ctx)
}
```

This handles the HTTP server. But real services have more:

- Background workers
- Kafka/RabbitMQ consumers
- Database connection pools
- Distributed locks
- Cache connections
- Metrics reporters

## The Kubernetes Timing Problem

When Kubernetes sends SIGTERM, several things happen simultaneously:

1. Your pod receives SIGTERM
2. Kubernetes removes the pod from Service endpoints
3. Ingress controllers update their backends
4. Other pods' DNS caches might still point to you

The problem: steps 2-4 take time. If you shut down immediately on SIGTERM, requests still routing to you will fail.

```go
func main() {
    // ... setup ...

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    // WRONG: Immediate shutdown
    // Requests still in flight from other pods will 502

    // RIGHT: Wait for traffic to drain
    log.Println("Received shutdown signal, waiting for traffic to drain...")

    // Give Kubernetes time to update endpoints
    time.Sleep(5 * time.Second)

    // Now start graceful shutdown
    ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
    defer cancel()
    server.Shutdown(ctx)
}
```

### The Readiness Probe Dance

Your readiness probe should fail BEFORE you stop accepting requests:

```go
type Server struct {
    httpServer *http.Server
    isReady    atomic.Bool
}

func (s *Server) readinessHandler(w http.ResponseWriter, r *http.Request) {
    if !s.isReady.Load() {
        w.WriteHeader(http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusOK)
}

func (s *Server) Shutdown(ctx context.Context) error {
    // Step 1: Mark as not ready (fails readiness probe)
    s.isReady.Store(false)
    log.Println("Marked as not ready")

    // Step 2: Wait for Kubernetes to notice and stop sending traffic
    time.Sleep(5 * time.Second)
    log.Println("Drain period complete")

    // Step 3: Now shutdown the HTTP server
    return s.httpServer.Shutdown(ctx)
}
```

## Coordinating Multiple Components

Real services have multiple things to shut down, and order matters.

### The Shutdown Orchestrator Pattern

```go
type ShutdownManager struct {
    components []ShutdownComponent
    timeout    time.Duration
}

type ShutdownComponent interface {
    Name() string
    Shutdown(ctx context.Context) error
    Priority() int // Lower = shutdown first
}

func (m *ShutdownManager) Shutdown(ctx context.Context) error {
    // Sort by priority
    sort.Slice(m.components, func(i, j int) bool {
        return m.components[i].Priority() < m.components[j].Priority()
    })

    ctx, cancel := context.WithTimeout(ctx, m.timeout)
    defer cancel()

    var errs []error
    for _, c := range m.components {
        log.Printf("Shutting down %s...", c.Name())
        start := time.Now()

        if err := c.Shutdown(ctx); err != nil {
            log.Printf("Error shutting down %s: %v", c.Name(), err)
            errs = append(errs, fmt.Errorf("%s: %w", c.Name(), err))
        } else {
            log.Printf("Shutdown %s complete (%v)", c.Name(), time.Since(start))
        }
    }

    return errors.Join(errs...)
}
```

### Shutdown Order Matters

```go
func main() {
    manager := &ShutdownManager{
        timeout: 30 * time.Second,
        components: []ShutdownComponent{
            // Priority 1: Stop accepting new work
            &ReadinessComponent{ready: &isReady},

            // Priority 2: Drain period
            &DrainComponent{duration: 5 * time.Second},

            // Priority 3: Stop HTTP server (waits for in-flight)
            &HTTPServerComponent{server: httpServer},

            // Priority 4: Stop background workers
            &WorkerPoolComponent{pool: workers},

            // Priority 5: Stop message consumers
            &KafkaConsumerComponent{consumer: kafkaConsumer},

            // Priority 6: Flush async operations
            &MetricsFlushComponent{reporter: metrics},

            // Priority 7: Close connections (last!)
            &DatabaseComponent{pool: dbPool},
            &RedisComponent{client: redis},
        },
    }

    // ... signal handling ...
    manager.Shutdown(context.Background())
}
```

## Handling Kafka Consumers

Kafka consumers are tricky because you might be mid-batch when shutdown starts.

```go
type KafkaConsumerComponent struct {
    consumer  *kafka.Consumer
    handler   MessageHandler
    wg        sync.WaitGroup
    shutdown  chan struct{}
    batchSize int
}

func (c *KafkaConsumerComponent) Run(ctx context.Context) {
    c.shutdown = make(chan struct{})

    for {
        select {
        case <-c.shutdown:
            return
        case <-ctx.Done():
            return
        default:
            // Fetch batch
            messages, err := c.consumer.FetchBatch(ctx, c.batchSize)
            if err != nil {
                continue
            }

            // Process batch with tracking
            c.wg.Add(1)
            go func(msgs []kafka.Message) {
                defer c.wg.Done()

                for _, msg := range msgs {
                    select {
                    case <-c.shutdown:
                        // Shutdown requested mid-batch
                        // Don't commit, let rebalance handle it
                        return
                    default:
                        if err := c.handler.Handle(msg); err != nil {
                            // Handle error...
                        }
                    }
                }

                // Only commit if we processed the whole batch
                c.consumer.Commit(msgs)
            }(messages)
        }
    }
}

func (c *KafkaConsumerComponent) Shutdown(ctx context.Context) error {
    // Signal consumer to stop
    close(c.shutdown)

    // Wait for in-flight batches with timeout
    done := make(chan struct{})
    go func() {
        c.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        log.Println("All Kafka batches completed")
    case <-ctx.Done():
        log.Println("Timeout waiting for Kafka batches")
    }

    return c.consumer.Close()
}
```

## Database Transactions in Progress

Long-running transactions need special handling:

```go
type TransactionManager struct {
    db         *sql.DB
    activeTxns sync.Map // map[string]*ManagedTx
    shutdown   atomic.Bool
}

type ManagedTx struct {
    tx       *sql.Tx
    id       string
    started  time.Time
    doneChan chan struct{}
}

func (m *TransactionManager) Begin(ctx context.Context) (*ManagedTx, error) {
    if m.shutdown.Load() {
        return nil, errors.New("shutdown in progress, rejecting new transactions")
    }

    tx, err := m.db.BeginTx(ctx, nil)
    if err != nil {
        return nil, err
    }

    mtx := &ManagedTx{
        tx:       tx,
        id:       uuid.New().String(),
        started:  time.Now(),
        doneChan: make(chan struct{}),
    }

    m.activeTxns.Store(mtx.id, mtx)
    return mtx, nil
}

func (m *TransactionManager) Shutdown(ctx context.Context) error {
    m.shutdown.Store(true)
    log.Println("Rejecting new transactions")

    // Wait for active transactions
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            // Timeout - log remaining transactions
            m.activeTxns.Range(func(key, value any) bool {
                mtx := value.(*ManagedTx)
                log.Printf("Transaction %s still active after %v", mtx.id, time.Since(mtx.started))
                return true
            })
            return ctx.Err()

        case <-ticker.C:
            count := 0
            m.activeTxns.Range(func(_, _ any) bool {
                count++
                return true
            })

            if count == 0 {
                log.Println("All transactions completed")
                return m.db.Close()
            }

            log.Printf("Waiting for %d active transactions", count)
        }
    }
}
```

## The Complete Picture

```go
func main() {
    // Setup components...

    // Shutdown orchestration
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

    // Start services
    go httpServer.ListenAndServe()
    go kafkaConsumer.Run(ctx)
    go workers.Start()

    // Wait for signal
    sig := <-quit
    log.Printf("Received %v, starting graceful shutdown", sig)

    // Create shutdown context with total budget
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Execute shutdown sequence
    if err := shutdownManager.Shutdown(ctx); err != nil {
        log.Printf("Shutdown completed with errors: %v", err)
        os.Exit(1)
    }

    log.Println("Shutdown complete")
}
```

## Key Takeaways

1. **HTTP shutdown alone isn't enough**. You need to coordinate all components.

2. **Kubernetes needs time**. Add a drain period before stopping the HTTP server.

3. **Order matters**. Stop accepting work → drain in-flight → close connections.

4. **Track in-flight work**. Use WaitGroups or similar to know when it's safe to close resources.

5. **Set deadlines**. Use context timeouts to avoid hanging forever.

6. **Log the shutdown**. When things go wrong, you'll want to know what was happening.

7. **Test it**. Send SIGTERM to your service and verify the behavior.

Graceful shutdown is one of those things that seems simple until you actually need it to work reliably. Get it right, and your deployments become invisible to users.
