---
title: "Profiling de Alocações de Memória em um Serviço Go de Alta Vazão"
date: 2025-09-11
description: "Como reduzimos pausas de GC de 50ms para 2ms encontrando alocações escondidas. Técnicas práticas de pprof, escape analysis e as otimizações que realmente importam."
tags:
  [
    "golang",
    "performance",
    "profiling",
    "memoria",
    "otimizacao",
    "producao",
  ]
---

Nossa API estava processando 50k requisições por segundo, mas a latência p99 continuava subindo para 200ms. O culpado não era código lento—era o garbage collector pausando tudo enquanto limpava milhões de pequenas alocações que não sabíamos que estávamos fazendo.

Aqui está como as encontramos e o que fizemos.

## Os Sintomas

Sintomas clássicos de pressão de GC:
- Picos de latência a cada poucos segundos
- Uso de CPU maior que o esperado
- Uso de memória estável mas GC rodando constantemente

```bash
# Verificar estatísticas de GC
GODEBUG=gctrace=1 ./myservice

# Output mostra GCs frequentes:
# gc 1 @0.012s 2%: 0.018+2.3+0.018 ms clock, 0.14+0.23/4.5/0+0.14 ms cpu, 4->4->2 MB, 5 MB goal, 8 P
# gc 2 @0.025s 3%: 0.019+3.1+0.021 ms clock, 0.15+0.31/6.1/0+0.17 ms cpu, 4->5->3 MB, 5 MB goal, 8 P
# gc 3 @0.041s 4%: ...
```

GC rodando a cada 15ms significa que cada requisição tem chance de pegar uma pausa.

## Encontrando Alocações com pprof

### Profile de Heap

```go
import _ "net/http/pprof"

func main() {
    go func() {
        // Expõe /debug/pprof/*
        log.Println(http.ListenAndServe("localhost:6060", nil))
    }()
    // ... resto do seu serviço
}
```

Capture um profile de heap:

```bash
# Alocações desde o início do programa
go tool pprof http://localhost:6060/debug/pprof/heap

# Ou salve para análise posterior
curl -o heap.prof http://localhost:6060/debug/pprof/heap
go tool pprof heap.prof
```

Dentro do pprof:

```
(pprof) top 20
Showing nodes accounting for 1.5GB, 89% of 1.7GB total
      flat  flat%   sum%        cum   cum%
    512MB 30.12% 30.12%      512MB 30.12%  encoding/json.(*decodeState).literalStore
    256MB 15.06% 45.18%      768MB 45.18%  myservice/handlers.(*Handler).ProcessRequest
    128MB  7.53% 52.71%      128MB  7.53%  fmt.Sprintf
```

### O Insight Chave: alloc_objects vs inuse_objects

```bash
# Total de alocações (mesmo se liberadas) - mostra taxa de alocação
go tool pprof -alloc_objects http://localhost:6060/debug/pprof/heap

# Atualmente em uso - mostra retenção de memória
go tool pprof -inuse_objects http://localhost:6060/debug/pprof/heap
```

**Para pressão de GC, `alloc_objects` importa mais.** Você pode ter baixo uso de memória mas alta taxa de alocação, causando trabalho constante de GC.

## Alocações Escondidas Comuns

### 1. Concatenação de Strings

```go
// RUIM: Cada + aloca uma nova string
func buildKey(prefix, id, suffix string) string {
    return prefix + ":" + id + ":" + suffix
}

// BOM: strings.Builder pré-aloca
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

// MELHOR para casos simples: fmt com buffer pool
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

### 2. Appends em Slice Sem Capacidade

```go
// RUIM: Múltiplas realocações conforme slice cresce
func collectIDs(items []Item) []string {
    var ids []string
    for _, item := range items {
        ids = append(ids, item.ID)
    }
    return ids
}

// BOM: Pré-aloca
func collectIDs(items []Item) []string {
    ids := make([]string, 0, len(items))
    for _, item := range items {
        ids = append(ids, item.ID)
    }
    return ids
}
```

### 3. Boxing de Interface

```go
// RUIM: Cada chamada faz boxing do int
func logValue(key string, value any) {
    log.Printf("%s: %v", key, value)
}

func process(count int) {
    logValue("count", count) // int -> any aloca
}

// BOM: Métodos específicos por tipo
func logInt(key string, value int) {
    log.Printf("%s: %d", key, value)
}
```

### 4. Closures Capturando Variáveis

```go
// Ambos os padrões são corretos no Go 1.22+, mas passar como
// parâmetro pode ajudar a escape analysis em alguns casos

// Captura em closure (correto, mas item pode escapar para heap)
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

// Passagem por parâmetro (pode ficar na stack em alguns casos)
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

### 5. fmt.Sprintf para Conversões Simples

```go
// RUIM: fmt.Sprintf aloca
id := fmt.Sprintf("%d", userID)

// BOM: strconv não aloca (para ints pequenos)
id := strconv.Itoa(userID)

// Para int64:
id := strconv.FormatInt(userID, 10)
```

## Escape Analysis: Por Que Coisas Alocam

Go decide em tempo de compilação se uma variável escapa para o heap. Verifique com:

```bash
go build -gcflags='-m -m' ./... 2>&1 | grep escape
```

Razões comuns para escape:

```go
// Escapa: ponteiro retornado para variável local
func newUser() *User {
    u := User{Name: "test"} // escapa para heap
    return &u
}

// Escapa: atribuído a interface
func process(u User) {
    var i any = u // u escapa
}

// Escapa: capturado por closure em goroutine
func startWorker(data []byte) {
    go func() {
        process(data) // data escapa
    }()
}

// Escapa: muito grande para stack (varia por versão do Go)
func bigArray() {
    data := make([]byte, 10*1024*1024) // escapa, muito grande
}
```

## sync.Pool: Reciclando Alocações

Para objetos frequentemente alocados, sync.Pool elimina alocações:

```go
var bufferPool = sync.Pool{
    New: func() any {
        return make([]byte, 0, 4096)
    },
}

func processRequest(data []byte) []byte {
    buf := bufferPool.Get().([]byte)
    buf = buf[:0] // Reseta length, mantém capacity
    defer bufferPool.Put(buf)

    // Usa buf...
    buf = append(buf, data...)

    // Importante: retorna uma cópia se buf escapa desta função
    result := make([]byte, len(buf))
    copy(result, buf)
    return result
}
```

### Armadilhas do Pool

```go
// ERRADO: Devolvendo tamanhos diferentes
var pool = sync.Pool{New: func() any { return make([]byte, 1024) }}

func process(size int) {
    buf := pool.Get().([]byte)
    if size > len(buf) {
        buf = make([]byte, size) // Criou buffer maior
    }
    defer pool.Put(buf) // Agora pool tem tamanhos misturados
}

// CERTO: Use tamanhos fixos ou limite o pool
func process(size int) {
    buf := pool.Get().([]byte)
    if size > cap(buf) {
        // Não devolve buffers grandes demais
        buf = make([]byte, size)
        defer func() { /* não retorna ao pool */ }()
    } else {
        buf = buf[:size]
        defer pool.Put(buf[:0])
    }
}
```

## Exemplo Real: Encoding JSON

Nossa maior fonte de alocação era encoding JSON em handlers HTTP:

```go
// ANTES: ~5 alocações por requisição
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
    user := h.db.GetUser(r.Context(), userID)

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(user) // Aloca encoder + buffer
}
```

Após profiling:

```go
// DEPOIS: Encoders com pool
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

## Benchmarking de Alocações

Sempre faça benchmark antes de otimizar:

```go
func BenchmarkBuildKey(b *testing.B) {
    b.ReportAllocs() // Mostra alocações por operação

    for i := 0; i < b.N; i++ {
        _ = buildKey("user", "12345", "profile")
    }
}
```

Output:

```
BenchmarkBuildKey-8    5000000    234 ns/op    64 B/op    2 allocs/op
```

Após otimização:

```
BenchmarkBuildKey-8    10000000   112 ns/op    32 B/op    1 allocs/op
```

## Os Resultados

Após aplicar esses padrões:

| Métrica | Antes | Depois |
|---------|-------|--------|
| Alocações/req | ~45 | ~12 |
| GC pause p99 | 50ms | 2ms |
| Latência p99 | 200ms | 35ms |
| Frequência de GC | 15ms | 200ms |

## Pontos-Chave

1. **Profile primeiro**. Não adivinhe onde alocações acontecem. Use `pprof -alloc_objects`.

2. **alloc_objects > inuse_objects** para pressão de GC. Alta taxa de alocação importa mesmo se memória é liberada rápido.

3. **Escape analysis** diz por que coisas alocam. Use `-gcflags='-m'` para entender.

4. **sync.Pool** é seu amigo para hot paths. Mas meça—tem overhead também.

5. **Pré-aloque slices** quando souber o tamanho. `make([]T, 0, n)` é seu amigo.

6. **Evite boxing de interface** em hot paths. Funções específicas por tipo alocam menos.

7. **Operações de string são caras**. Use `strings.Builder` ou operações com `[]byte`.

8. **Benchmark com `b.ReportAllocs()`**. Alocações por operação dizem se você está melhorando.

A maioria dos serviços não precisa desse nível de otimização. Mas quando você está processando dezenas de milhares de requisições por segundo, cada alocação conta. Profile primeiro, otimize o que importa, e sempre meça os resultados.
