---
title: "Worker Pools e Concorrência Limitada em Go"
date: 2025-08-20
description: "Padrões práticos para controlar concorrência: worker pools, errgroup, semáforos, fan-out/fan-in e backpressure. Como processar trabalho concorrentemente sem sobrecarregar seu sistema."
tags:
  ["golang", "concorrencia", "padroes", "goroutines", "channels", "performance"]
---

Criar uma goroutine para cada tarefa é fácil. Criar 10.000 goroutines que martelam seu banco de dados até ele cair também é fácil. A parte difícil é controlar a concorrência—processar trabalho em paralelo respeitando os limites do sistema.

Este post cobre os padrões que permitem fazer exatamente isso.

## O Problema: Concorrência Ilimitada

A abordagem ingênua:

```go
func processAll(items []Item) error {
    var wg sync.WaitGroup
    for _, item := range items {
        wg.Add(1)
        go func() {
            defer wg.Done()
            process(item) // E se isso acessa um banco de dados?
        }()
    }
    wg.Wait()
    return nil
}
```

Com 10.000 itens, você cria 10.000 goroutines que simultaneamente acessam seu banco de dados. Pools de conexão se esgotam, timeouts cascateiam, e seu serviço cai.

Você precisa de concorrência limitada.

## Padrão 1: Worker Pool com Channels

O padrão clássico de "Concurrency in Go": um número fixo de workers consumindo de um channel compartilhado.

```go
func processWithWorkerPool(items []Item, numWorkers int) error {
    jobs := make(chan Item, len(items))
    results := make(chan error, len(items))

    // Inicia workers
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

    // Envia jobs
    for _, item := range items {
        jobs <- item
    }
    close(jobs)

    // Espera workers terminarem
    go func() {
        wg.Wait()
        close(results)
    }()

    // Coleta erros
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

Isso funciona, mas é verboso. Para a maioria dos casos, há uma opção melhor.

## Padrão 2: errgroup para Concorrência Coordenada

O pacote `golang.org/x/sync/errgroup` é a ferramenta padrão para trabalho concorrente limitado. Ele cuida de:

- Esperar goroutines completarem
- Coletar o primeiro erro
- Cancelamento de context em caso de falha

```go
import "golang.org/x/sync/errgroup"

func processWithErrgroup(ctx context.Context, items []Item) error {
    g, ctx := errgroup.WithContext(ctx)

    for _, item := range items {
        g.Go(func() error {
            return process(ctx, item)
        })
    }

    return g.Wait() // Retorna primeiro erro, espera todas as goroutines
}
```

Mas isso ainda cria goroutines ilimitadas. Adicione `SetLimit`:

```go
func processWithBoundedErrgroup(ctx context.Context, items []Item) error {
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(10) // No máximo 10 goroutines concorrentes

    for _, item := range items {
        g.Go(func() error {
            return process(ctx, item)
        })
    }

    return g.Wait()
}
```

`SetLimit` faz o errgroup bloquear quando o limite é atingido, prevenindo criação ilimitada de goroutines.

### Quando errgroup Cancela

Com `errgroup.WithContext`, o context é cancelado quando qualquer goroutine retorna um erro. Outras goroutines devem verificar o context:

```go
func process(ctx context.Context, item Item) error {
    // Verifica se devemos parar
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    // Faz o trabalho...
    result, err := fetchData(ctx, item.URL)
    if err != nil {
        return err // Isso cancela o context para outras goroutines
    }

    return saveResult(ctx, result)
}
```

## Padrão 3: Semáforo com Buffered Channel

Para rate limiting simples, um buffered channel age como um semáforo:

```go
func processWithSemaphore(ctx context.Context, items []Item) error {
    sem := make(chan struct{}, 10) // Limita a 10 concorrentes
    var wg sync.WaitGroup
    errCh := make(chan error, 1)

    for _, item := range items {
        // Adquire semáforo
        select {
        case sem <- struct{}{}:
        case <-ctx.Done():
            return ctx.Err()
        }

        wg.Add(1)
        go func() {
            defer wg.Done()
            defer func() { <-sem }() // Libera semáforo

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

### golang.org/x/sync/semaphore para Limites com Peso

Quando diferentes tarefas consomem diferentes quantidades de recursos:

```go
import "golang.org/x/sync/semaphore"

func processWithWeightedSemaphore(ctx context.Context, items []Item) error {
    // Permite 100 "unidades" de trabalho concorrente
    sem := semaphore.NewWeighted(100)

    g, ctx := errgroup.WithContext(ctx)

    for _, item := range items {
        weight := int64(item.Size / 1024) // Itens maiores consomem mais capacidade
        if weight < 1 {
            weight = 1
        }

        // Adquire permissão com peso
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

## Padrão 4: Fan-Out/Fan-In

Fan-out: distribui trabalho entre múltiplas goroutines.
Fan-in: coleta resultados em um único channel.

```go
func fanOutFanIn(ctx context.Context, urls []string) ([]Result, error) {
    // Fan-out: cria um channel de trabalho
    urlCh := make(chan string, len(urls))
    for _, url := range urls {
        urlCh <- url
    }
    close(urlCh)

    // Inicia workers (fan-out)
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

    // Espera e fecha channel de resultados
    go func() {
        g.Wait()
        close(resultCh)
    }()

    // Fan-in: coleta resultados
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

### Padrão Pipeline

Encadeia estágios para processamento complexo:

```go
func pipeline(ctx context.Context, input []int) ([]int, error) {
    // Estágio 1: Gera
    stage1 := generate(ctx, input)

    // Estágio 2: Eleva ao quadrado (fan-out para 3 workers)
    stage2 := fanOut(ctx, stage1, 3, square)

    // Estágio 3: Filtra
    stage3 := filter(ctx, stage2, func(n int) bool { return n > 10 })

    // Coleta resultados
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

## Padrão 5: Backpressure com Channels

Quando produtores são mais rápidos que consumidores, você precisa de backpressure—desacelerar produtores quando consumidores não conseguem acompanhar.

### Blocking Send (Backpressure Natural)

```go
func producerWithBackpressure(ctx context.Context, items []Item) <-chan Item {
    // Buffer pequeno = backpressure entra em ação rapidamente
    out := make(chan Item, 10)

    go func() {
        defer close(out)
        for _, item := range items {
            select {
            case out <- item: // Bloqueia quando buffer está cheio
            case <-ctx.Done():
                return
            }
        }
    }()

    return out
}
```

### Descartando Itens Sob Pressão

Às vezes é melhor descartar trabalho do que bloquear:

```go
func producerWithDrop(ctx context.Context, items <-chan Item) <-chan Item {
    out := make(chan Item, 100)

    go func() {
        defer close(out)
        for item := range items {
            select {
            case out <- item:
            default:
                // Buffer cheio, descarta item
                log.Printf("descartando item %s devido a backpressure", item.ID)
            }
        }
    }()

    return out
}
```

### Rate Limiting com time.Ticker

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

Worker pools precisam drenar graciosamente:

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
                return // Channel fechado, sai
            }
            result := process(job)
            p.results <- result
        case <-p.quit:
            // Drena jobs restantes antes de sair
            for job := range p.jobs {
                result := process(job)
                p.results <- result
            }
            return
        }
    }
}

func (p *WorkerPool) Shutdown(ctx context.Context) error {
    close(p.jobs) // Para de aceitar novos jobs

    done := make(chan struct{})
    go func() {
        p.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        return nil
    case <-ctx.Done():
        close(p.quit) // Sinaliza workers para drenar e sair
        return ctx.Err()
    }
}
```

## Erros Comuns

### 1. Esquecer de Fechar Channels

```go
// ERRADO: channel de results nunca é fechado, range bloqueia para sempre
go func() {
    for _, item := range items {
        results <- process(item)
    }
    // Faltando: close(results)
}()

for result := range results { // Bloqueia para sempre
    // ...
}
```

### 2. Goroutine Leak por Send Bloqueado

```go
// ERRADO: se retornarmos cedo, goroutine vaza
func fetch(ctx context.Context) (string, error) {
    ch := make(chan string)

    go func() {
        result := slowOperation()
        ch <- result // Bloqueia para sempre se ctx for cancelado
    }()

    select {
    case result := <-ch:
        return result, nil
    case <-ctx.Done():
        return "", ctx.Err() // Goroutine vaza!
    }
}

// CERTO: use buffered channel
func fetch(ctx context.Context) (string, error) {
    ch := make(chan string, 1) // Com buffer!

    go func() {
        result := slowOperation()
        ch <- result // Não bloqueia mesmo se ninguém receber
    }()

    select {
    case result := <-ch:
        return result, nil
    case <-ctx.Done():
        return "", ctx.Err() // Goroutine ainda pode completar
    }
}
```

### 3. Não Respeitar Cancelamento de Context

```go
// ERRADO: ignora cancelamento
for item := range items {
    process(item) // Continua rodando mesmo se ctx for cancelado
}

// CERTO: verifica context
for item := range items {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
    process(ctx, item)
}
```

## Quando Usar O Quê

- **errgroup + SetLimit**: Escolha padrão para trabalho concorrente limitado
- **Worker pool**: Workers de longa duração processando stream contínuo
- **Semáforo**: Precisa de limites com peso ou controle refinado
- **Fan-out/fan-in**: Processamento em pipeline com múltiplos estágios
- **Buffered channel**: Backpressure simples entre produtor/consumidor

## Pontos-Chave

1. **errgroup.SetLimit** é sua ferramenta padrão para concorrência limitada. É simples e trata erros bem.

2. **Channels são semáforos**. Um buffered channel de tamanho N limita concorrência a N.

3. **Feche channels para sinalizar conclusão**. Receivers iteram sobre channels; fechar é como você diz que terminou.

4. **Tamanho do buffer = latência vs memória**. Buffers maiores absorvem picos mas usam mais memória.

5. **Sempre verifique o context**. Trabalho de longa duração deve respeitar cancelamento.

6. **Buffered channel de 1 previne goroutine leaks**. Quando uma goroutine pode sobreviver ao seu chamador, dê a ela um lugar para enviar.

7. **Drene no shutdown**. Não apenas abandone trabalho—dê aos workers a chance de terminar o que estão fazendo.

Concorrência em Go é poderosa, mas concorrência ilimitada é uma armadilha. Esses padrões dão o controle para usá-la com segurança.
