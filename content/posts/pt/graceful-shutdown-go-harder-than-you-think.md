---
title: "Graceful Shutdown em Go É Mais Difícil Do Que Você Pensa"
date: 2025-07-11
description: "Lidando com requisições em andamento, consumidores Kafka, conexões de banco de dados e sinais de terminação do Kubernetes corretamente. Os detalhes que os tutoriais pulam."
tags:
  [
    "golang",
    "backend",
    "kubernetes",
    "producao",
    "confiabilidade",
    "sistemas-distribuidos",
  ]
---

"Só chame `server.Shutdown()`" é um conselho que funciona bem até você ter um consumidor Kafka no meio de um batch, uma transação de banco em progresso, e Kubernetes enviando SIGTERM enquanto seu readiness probe ainda retorna healthy.

Graceful shutdown parece simples. Não é. Aqui está tudo que pode dar errado e como lidar.

## A Abordagem Ingênua

A maioria dos tutoriais mostra algo assim:

```go
func main() {
    server := &http.Server{Addr: ":8080", Handler: handler}

    go func() {
        if err := server.ListenAndServe(); err != http.ErrServerClosed {
            log.Fatal(err)
        }
    }()

    // Espera interrupção
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    // Shutdown com timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    server.Shutdown(ctx)
}
```

Isso lida com o servidor HTTP. Mas serviços reais têm mais:
- Workers em background
- Consumidores Kafka/RabbitMQ
- Pools de conexão de banco
- Locks distribuídos
- Conexões de cache
- Reporters de métricas

## O Problema de Timing do Kubernetes

Quando Kubernetes envia SIGTERM, várias coisas acontecem simultaneamente:

1. Seu pod recebe SIGTERM
2. Kubernetes remove o pod dos endpoints do Service
3. Ingress controllers atualizam seus backends
4. Caches de DNS de outros pods podem ainda apontar para você

O problema: passos 2-4 levam tempo. Se você desliga imediatamente no SIGTERM, requisições ainda sendo roteadas para você vão falhar.

```go
func main() {
    // ... setup ...

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    // ERRADO: Shutdown imediato
    // Requisições em voo de outros pods vão dar 502

    // CERTO: Espere o tráfego drenar
    log.Println("Sinal de shutdown recebido, esperando tráfego drenar...")

    // Dê tempo ao Kubernetes para atualizar endpoints
    time.Sleep(5 * time.Second)

    // Agora inicie o graceful shutdown
    ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
    defer cancel()
    server.Shutdown(ctx)
}
```

### A Dança do Readiness Probe

Seu readiness probe deve falhar ANTES de você parar de aceitar requisições:

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
    // Passo 1: Marca como não pronto (falha readiness probe)
    s.isReady.Store(false)
    log.Println("Marcado como não pronto")

    // Passo 2: Espera Kubernetes notar e parar de enviar tráfego
    time.Sleep(5 * time.Second)
    log.Println("Período de drenagem completo")

    // Passo 3: Agora desliga o servidor HTTP
    return s.httpServer.Shutdown(ctx)
}
```

## Coordenando Múltiplos Componentes

Serviços reais têm várias coisas para desligar, e a ordem importa.

### O Padrão Orquestrador de Shutdown

```go
type ShutdownManager struct {
    components []ShutdownComponent
    timeout    time.Duration
}

type ShutdownComponent interface {
    Name() string
    Shutdown(ctx context.Context) error
    Priority() int // Menor = desliga primeiro
}

func (m *ShutdownManager) Shutdown(ctx context.Context) error {
    // Ordena por prioridade (usando pacote slices do Go 1.21+)
    slices.SortFunc(m.components, func(a, b ShutdownComponent) int {
        return cmp.Compare(a.Priority(), b.Priority())
    })

    ctx, cancel := context.WithTimeout(ctx, m.timeout)
    defer cancel()

    var errs []error
    for _, c := range m.components {
        log.Printf("Desligando %s...", c.Name())
        start := time.Now()

        if err := c.Shutdown(ctx); err != nil {
            log.Printf("Erro desligando %s: %v", c.Name(), err)
            errs = append(errs, fmt.Errorf("%s: %w", c.Name(), err))
        } else {
            log.Printf("Shutdown de %s completo (%v)", c.Name(), time.Since(start))
        }
    }

    return errors.Join(errs...)
}
```

### Ordem de Shutdown Importa

```go
func main() {
    manager := &ShutdownManager{
        timeout: 30 * time.Second,
        components: []ShutdownComponent{
            // Prioridade 1: Para de aceitar novo trabalho
            &ReadinessComponent{ready: &isReady},

            // Prioridade 2: Período de drenagem
            &DrainComponent{duration: 5 * time.Second},

            // Prioridade 3: Para servidor HTTP (espera em voo)
            &HTTPServerComponent{server: httpServer},

            // Prioridade 4: Para workers em background
            &WorkerPoolComponent{pool: workers},

            // Prioridade 5: Para consumidores de mensagens
            &KafkaConsumerComponent{consumer: kafkaConsumer},

            // Prioridade 6: Flush de operações assíncronas
            &MetricsFlushComponent{reporter: metrics},

            // Prioridade 7: Fecha conexões (por último!)
            &DatabaseComponent{pool: dbPool},
            &RedisComponent{client: redis},
        },
    }

    // ... signal handling ...
    manager.Shutdown(context.Background())
}
```

## Lidando com Consumidores Kafka

Consumidores Kafka são complicados porque você pode estar no meio de um batch quando o shutdown começa.

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
            // Busca batch
            messages, err := c.consumer.FetchBatch(ctx, c.batchSize)
            if err != nil {
                continue
            }

            // Processa batch com rastreamento
            c.wg.Add(1)
            go func() {
                defer c.wg.Done()

                for _, msg := range messages {
                    select {
                    case <-c.shutdown:
                        // Shutdown requisitado no meio do batch
                        // Não commita, deixe rebalance lidar
                        return
                    default:
                        if err := c.handler.Handle(msg); err != nil {
                            // Trata erro...
                        }
                    }
                }

                // Só commita se processou o batch inteiro
                c.consumer.Commit(messages)
            }()
        }
    }
}

func (c *KafkaConsumerComponent) Shutdown(ctx context.Context) error {
    // Sinaliza consumidor para parar
    close(c.shutdown)

    // Espera batches em voo com timeout
    done := make(chan struct{})
    go func() {
        c.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        log.Println("Todos os batches Kafka completados")
    case <-ctx.Done():
        log.Println("Timeout esperando batches Kafka")
    }

    return c.consumer.Close()
}
```

## Transações de Banco em Progresso

Transações longas precisam de tratamento especial:

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
        return nil, errors.New("shutdown em progresso, rejeitando novas transações")
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
    log.Println("Rejeitando novas transações")

    // Espera transações ativas
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            // Timeout - loga transações restantes
            m.activeTxns.Range(func(key, value any) bool {
                mtx := value.(*ManagedTx)
                log.Printf("Transação %s ainda ativa após %v", mtx.id, time.Since(mtx.started))
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
                log.Println("Todas as transações completadas")
                return m.db.Close()
            }

            log.Printf("Esperando %d transações ativas", count)
        }
    }
}
```

## O Quadro Completo

```go
func main() {
    // Setup dos componentes...

    // Orquestração de shutdown
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

    // Inicia serviços
    go httpServer.ListenAndServe()
    go kafkaConsumer.Run(ctx)
    go workers.Start()

    // Espera sinal
    sig := <-quit
    log.Printf("Recebido %v, iniciando graceful shutdown", sig)

    // Cria contexto de shutdown com budget total
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Executa sequência de shutdown
    if err := shutdownManager.Shutdown(ctx); err != nil {
        log.Printf("Shutdown completado com erros: %v", err)
        os.Exit(1)
    }

    log.Println("Shutdown completo")
}
```

## Pontos-Chave

1. **Shutdown de HTTP sozinho não é suficiente**. Você precisa coordenar todos os componentes.

2. **Kubernetes precisa de tempo**. Adicione um período de drenagem antes de parar o servidor HTTP.

3. **Ordem importa**. Pare de aceitar trabalho → drene em voo → feche conexões.

4. **Rastreie trabalho em voo**. Use WaitGroups ou similar para saber quando é seguro fechar recursos.

5. **Defina deadlines**. Use timeouts de context para não travar infinitamente.

6. **Logue o shutdown**. Quando coisas derem errado, você vai querer saber o que estava acontecendo.

7. **Teste**. Envie SIGTERM para seu serviço e verifique o comportamento.

Graceful shutdown é uma daquelas coisas que parecem simples até você realmente precisar que funcione de forma confiável. Acerte, e seus deploys se tornam invisíveis para os usuários.
