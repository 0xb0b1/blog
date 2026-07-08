---
title: "Performance em Clojure com Muitos Dados: Transducers, Transients e core.async"
date: 2021-11-08
description: "Deixando Clojure rápido em datasets grandes: por que pipelines de lazy-seq alocam, como transducers e transients cortam isso para perto de zero, matando reflection em loops quentes, e usando pipelines de core.async para fan-out I/O-bound com backpressure."
tags:
  [
    "clojure",
    "performance",
    "core-async",
    "transducers",
    "jvm",
    "backend",
    "otimizacao",
  ]
---

Código Clojure que é instantâneo no REPL sobre cem maps pode se arrastar sobre alguns milhões. Escrevo Clojure desde 2017, e a maior parte do trabalho de performance que fiz em backends com muitos dados se resume ao mesmo punhado de movimentos. Nenhum deles é esperto. São sobre entender o que a versão conveniente aloca, e saber quando trocar um pouco de elegância por muito throughput.

## Regra 0: Meça, Não Chute

Toda afirmação de performance abaixo deveria ser verificada no seu workload, não tomada como fé — incluindo as minhas. Na JVM, minhas duas ferramentas são a [criterium](https://github.com/hugoduncan/criterium) para microbenchmarks (ela lida com o warmup do JIT e o ruído estatístico, o que um `(time ...)` ingênuo não faz) e o [clj-async-profiler](https://github.com/clojure-goog/clj-async-profiler) para flamegraphs de onde o tempo realmente vai.

```clojure
(require '[criterium.core :refer [quick-bench]])
(quick-bench (my-pipeline records))
```

O número de vezes que eu perfilei e descobri que o gargalo estava em um lugar que eu jamais teria chutado é exatamente o número de vezes que eu perfilei.

## Lazy Sequences São Convenientes e Uma Armadilha

O pipeline thread-last idiomático é lindo e, em dados grandes, desperdiçador:

```clojure
;; Allocates an intermediate lazy seq at every step
(->> records
     (map parse)
     (filter valid?)
     (map enrich)
     (into []))
```

Cada estágio produz sua própria lazy sequence, com cons cells e closures por elemento. Lazy seqs também são *chunked* em blocos de 32, então você pode realizar mais do que pediu. E se você acidentalmente segurar a cabeça de uma lazy seq grande, você mantém tudo em memória.

Transducers resolvem isso separando a *transformação* da *sequência*. Faça `comp` de uma pilha de transducers e eles se fundem em uma única passada sem coleções intermediárias:

```clojure
;; One pass, no intermediate seqs, no laziness to reason about
(into []
      (comp (map parse)
            (filter valid?)
            (map enrich))
      records)
```

Mesma lógica, mesma legibilidade assim que seu olho se ajusta, mas as alocações intermediárias sumiram. Em um dataset grande, isso é rotineiramente a diferença entre confortável e doloroso, e o transducer é reutilizável — o mesmo `(comp ...)` funciona com `into`, `transduce`, `sequence` ou um canal `core.async`.

## Transients para a Construção

Quando você está construindo uma coleção grande reduzindo sobre a entrada, a imutabilidade persistente te cobra uma alocação por passo. Transients te dão coleções localmente mutáveis, contidas em uma thread, que você converte de volta para persistente no fim — mesmo resultado, uma fração do lixo:

```clojure
(defn index-by-id [records]
  (persistent!
    (reduce (fn [acc r] (assoc! acc (:id r) r))
            (transient {})
            records)))
```

A regra é estreita e importante: transients são para uma construção single-threaded onde você controla o ciclo de vida inteiro, e você nunca compartilha um entre threads. Dentro dessa caixa eles são seguros e rápidos; fora dela são um tiro no pé. Este é o único lugar em que eu abro mão da imutabilidade padrão do Clojure, e só porque a mutação nunca escapa da função.

## Mate Reflection em Loops Quentes

Chamadas reflexivas na JVM são lentas, e Clojure vai emiti-las silenciosamente quando não consegue inferir um tipo. Ligue o aviso e você vai achá-las:

```clojure
(set! *warn-on-reflection* true)
```

Para caminhos numéricos quentes, boxing é o outro imposto — cada objeto `Long`/`Double` alocado em um loop apertado é pressão no alocador. Trabalhar sobre arrays primitivos com `areduce`/`amap` mantém a matemática primitiva de ponta a ponta:

```clojure
;; No boxing: primitive doubles throughout the loop
(defn sum ^double [^doubles xs]
  (areduce xs i acc 0.0 (+ acc (aget xs i))))
```

Você não quer type hints espalhados pela base de código toda — são ruído em caminhos frios. Você quer eles exatamente onde o profiler apontou.

## Paralelismo: core.async para Fan-Out I/O-Bound

Quando o gargalo é computação pura CPU-bound sobre uma coleção "foldable", o `clojure.core.reducers/fold` vai paralelizar um fold entre os cores quase de graça. Mas a maior parte do trabalho de backend com muitos dados é *I/O*-bound — enriquecer registros a partir de um banco ou de um serviço HTTP — e aí a ferramenta é um pipeline de `core.async`.

```clojure
(require '[clojure.core.async :as a])

(defn process-all [records concurrency]
  (let [in  (a/chan 1024)
        out (a/chan 1024)]
    ;; pipeline-blocking: N parallel workers for blocking I/O.
    ;; Backpressure is built in — if `out` fills up, workers stop pulling `in`.
    (a/pipeline-blocking concurrency out (map enrich-via-io) in)
    (a/onto-chan!! in records)         ; feed the input and close it
    (a/<!! (a/into [] out))))          ; drain the output
```

`pipeline-blocking` roda `concurrency` workers sobre trabalho bloqueante; use `pipeline` puro para transformações CPU-bound e `pipeline-async` quando o trabalho já é assíncrono. Os canais buffered te dão backpressure de graça: um consumidor lento lá na frente naturalmente segura os workers, então você processa um stream de entrada enorme com uma pegada de memória limitada e estável em vez de puxar tudo para uma seq e torcer para caber. Essa propriedade de backpressure é a verdadeira razão de eu recorrer ao core.async aqui em vez do `pmap` — `pmap` não tem controle de fluxo e seu comportamento de chunking surpreende as pessoas em workloads desiguais.

## Trade-Offs, Nomeados

- **Transducers são menos familiares.** Um time que não os viu lê `(comp (map ...) (filter ...))` mais devagar do que um thread-last. Isso é um custo real em código compartilhado; eu os reservo para os caminhos onde a alocação realmente importa e deixo o thread-last onde é claro e frio.
- **Transients e arrays primitivos abrem mão da rede de segurança do Clojure.** Mutáveis, sem boxing, e implacáveis. Mantenha-os locais e escondidos atrás de uma fronteira de função pura.
- **core.async adiciona concorrência sobre a qual você tem que raciocinar.** Canais, tamanhos de buffer e semântica de fechamento são sua própria fonte de bugs. Para um map paralelo simples sobre alguns itens, `pmap` ou um punhado de `future`s é menos maquinaria.
- **Acima de tudo: não faça nada disso antes de medir.** O pipeline lazy idiomático é o padrão certo. Você recorre a essas ferramentas quando um profiler te manda, no caminho específico que ele aponta — não em todo lugar, e não preventivamente.

O tema é que Clojure te dá um padrão rápido e imutável e um conjunto de saídas de emergência claramente marcadas para quando o padrão não é rápido o suficiente. Bom trabalho de performance é saber qual saída, e — mais importante — saber que você ganhou o direito de abri-la.

---
