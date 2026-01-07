---
title: "Race Conditions em Sistemas Event-Driven com Go"
date: 2025-11-02
description: "Entendendo e prevenindo race conditions em aplicações Go event-driven, com exemplos práticos e soluções."
tags: ["golang", "concorrência", "race-conditions", "event-driven"]
---

Race conditions são um dos bugs mais difíceis de detectar e corrigir em sistemas concorrentes. Em aplicações event-driven, onde múltiplos eventos podem ser processados simultaneamente, o risco é ainda maior. Vamos explorar como identificar e prevenir esses problemas em Go.

## O Que É Uma Race Condition?

Uma race condition ocorre quando duas ou mais goroutines acessam dados compartilhados simultaneamente, e pelo menos uma delas está escrevendo. O resultado final depende da ordem de execução, que é não-determinística.

```go
// PROBLEMA: Race condition
var contador int

func incrementar() {
    contador++ // Lê, incrementa, escreve - não é atômico!
}

func main() {
    for i := 0; i < 1000; i++ {
        go incrementar()
    }
    time.Sleep(time.Second)
    fmt.Println(contador) // Resultado imprevisível!
}
```

## Detectando Race Conditions

Go tem um detector de race conditions embutido:

```bash
go run -race main.go
go test -race ./...
```

Saída típica:

```
WARNING: DATA RACE
Read at 0x00c0000a4010 by goroutine 8:
  main.incrementar()
      /path/main.go:10 +0x4e

Previous write at 0x00c0000a4010 by goroutine 7:
  main.incrementar()
      /path/main.go:10 +0x64
```

## Soluções para Race Conditions

### 1. Mutex (Exclusão Mútua)

```go
var (
    contador int
    mu       sync.Mutex
)

func incrementar() {
    mu.Lock()
    contador++
    mu.Unlock()
}
```

### 2. RWMutex (Leitura/Escrita)

Quando há mais leituras que escritas:

```go
var (
    dados map[string]string
    mu    sync.RWMutex
)

func ler(chave string) string {
    mu.RLock()
    defer mu.RUnlock()
    return dados[chave]
}

func escrever(chave, valor string) {
    mu.Lock()
    defer mu.Unlock()
    dados[chave] = valor
}
```

### 3. Operações Atômicas

Para operações simples:

```go
var contador int64

func incrementar() {
    atomic.AddInt64(&contador, 1)
}

func obter() int64 {
    return atomic.LoadInt64(&contador)
}
```

### 4. Channels (Comunicação)

Prefira comunicação sobre compartilhamento:

```go
type Contador struct {
    incrementos chan struct{}
    valor       chan int
}

func NovoContador() *Contador {
    c := &Contador{
        incrementos: make(chan struct{}),
        valor:       make(chan int),
    }
    go c.executar()
    return c
}

func (c *Contador) executar() {
    var contador int
    for {
        select {
        case <-c.incrementos:
            contador++
        case c.valor <- contador:
        }
    }
}

func (c *Contador) Incrementar() {
    c.incrementos <- struct{}{}
}

func (c *Contador) Obter() int {
    return <-c.valor
}
```

## Race Conditions em Event-Driven

Em sistemas event-driven, race conditions frequentemente ocorrem em:

### 1. Estado do Agregado

```go
// PROBLEMA: Múltiplos eventos modificando mesmo agregado
type Conta struct {
    saldo float64
}

func (c *Conta) ProcessarEvento(evento Evento) {
    switch e := evento.(type) {
    case DepositoRealizado:
        c.saldo += e.Valor // Race se eventos chegam em paralelo
    case SaqueRealizado:
        c.saldo -= e.Valor
    }
}

// SOLUÇÃO: Processar eventos sequencialmente por agregado
type ProcessadorEventos struct {
    mu       sync.Mutex
    agregados map[string]*Conta
}

func (p *ProcessadorEventos) Processar(aggregateID string, evento Evento) {
    p.mu.Lock()
    defer p.mu.Unlock()

    conta := p.agregados[aggregateID]
    conta.ProcessarEvento(evento)
}
```

### 2. Cache de Projeções

```go
// PROBLEMA: Atualizações concorrentes no cache
type Cache struct {
    dados map[string]interface{}
}

func (c *Cache) Atualizar(chave string, valor interface{}) {
    c.dados[chave] = valor // Race condition!
}

// SOLUÇÃO: sync.Map para cache concorrente
type CacheSeguro struct {
    dados sync.Map
}

func (c *CacheSeguro) Atualizar(chave string, valor interface{}) {
    c.dados.Store(chave, valor)
}

func (c *CacheSeguro) Obter(chave string) (interface{}, bool) {
    return c.dados.Load(chave)
}
```

### 3. Deduplicação de Eventos

```go
// PROBLEMA: Verificação e armazenamento não são atômicos
func (d *Deduplicador) JaProcessado(eventoID string) bool {
    d.mu.RLock()
    _, existe := d.processados[eventoID]
    d.mu.RUnlock()

    if !existe {
        d.mu.Lock()
        d.processados[eventoID] = true // Outro goroutine pode ter inserido!
        d.mu.Unlock()
    }
    return existe
}

// SOLUÇÃO: Operação atômica de verificar-e-inserir
func (d *Deduplicador) TentarProcessar(eventoID string) bool {
    d.mu.Lock()
    defer d.mu.Unlock()

    if _, existe := d.processados[eventoID]; existe {
        return false // Já foi processado
    }

    d.processados[eventoID] = true
    return true // Pode processar
}
```

## Padrões para Evitar Race Conditions

### 1. Processamento Serial por Chave

```go
type ProcessadorParticionado struct {
    filas     map[string]chan Evento
    mu        sync.RWMutex
    numWorkers int
}

func (p *ProcessadorParticionado) Processar(evento Evento) {
    chave := evento.AggregateID()

    p.mu.RLock()
    fila, existe := p.filas[chave]
    p.mu.RUnlock()

    if !existe {
        p.mu.Lock()
        fila = make(chan Evento, 100)
        p.filas[chave] = fila
        go p.worker(fila)
        p.mu.Unlock()
    }

    fila <- evento
}

func (p *ProcessadorParticionado) worker(fila chan Evento) {
    for evento := range fila {
        p.processarEvento(evento)
    }
}
```

### 2. Optimistic Locking

```go
type Agregado struct {
    ID      string
    Versao  int
    Dados   interface{}
}

func (r *Repositorio) Salvar(a *Agregado) error {
    result, err := r.db.Exec(`
        UPDATE agregados
        SET dados = $1, versao = versao + 1
        WHERE id = $2 AND versao = $3
    `, a.Dados, a.ID, a.Versao)

    if err != nil {
        return err
    }

    rowsAffected, _ := result.RowsAffected()
    if rowsAffected == 0 {
        return ErrConcorrenciaConflito
    }

    return nil
}
```

### 3. Actor Model

```go
type Ator struct {
    mailbox chan Mensagem
    estado  interface{}
}

func NovoAtor(estadoInicial interface{}) *Ator {
    a := &Ator{
        mailbox: make(chan Mensagem, 100),
        estado:  estadoInicial,
    }
    go a.executar()
    return a
}

func (a *Ator) executar() {
    for msg := range a.mailbox {
        a.processar(msg)
    }
}

func (a *Ator) Enviar(msg Mensagem) {
    a.mailbox <- msg
}
```

## Testes para Race Conditions

```go
func TestConcorrencia(t *testing.T) {
    contador := NovoContadorSeguro()
    var wg sync.WaitGroup

    // 100 goroutines incrementando simultaneamente
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                contador.Incrementar()
            }
        }()
    }

    wg.Wait()

    if contador.Obter() != 100000 {
        t.Errorf("Esperado 100000, obtido %d", contador.Obter())
    }
}
```

## Conclusão

1. **Use sempre o detector de races**: `go test -race`
2. **Prefira channels sobre mutexes**: "Don't communicate by sharing memory; share memory by communicating"
3. **Minimize estado compartilhado**: Quanto menos compartilhamento, menos races
4. **Operações atômicas de verificar-e-modificar**: Evite check-then-act
5. **Teste sob concorrência**: Muitas goroutines ajudam a expor races

Race conditions são inevitáveis em sistemas concorrentes, mas com as ferramentas e práticas corretas do Go, podemos detectá-las e eliminá-las sistematicamente.
