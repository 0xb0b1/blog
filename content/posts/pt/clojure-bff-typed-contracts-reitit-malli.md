---
title: "O Padrão BFF em Clojure: Contratos Tipados para um Frontend com Muitos Dados"
date: 2021-04-19
description: "Construindo um backend-for-frontend em Clojure com Ring, Reitit e malli: roteamento orientado a dados, contratos HTTP tipados sem um sistema de tipos estático, fan-out paralelo para serviços downstream e degradação graciosa quando um deles está lento."
tags:
  [
    "clojure",
    "reitit",
    "malli",
    "bff",
    "api-design",
    "backend",
    "design-de-sistemas",
  ]
---

Escrevo Clojure desde 2017, e o padrão que eu mais recorro em produtos com muitos dados é o backend-for-frontend (BFF). Não porque está na moda, mas porque a alternativa — um frontend rico conversando diretamente com um punhado de serviços — transforma cada tela em uma cascata de requisições, cada uma retornando mais dados do que a UI precisa, nenhum deles no formato que a view quer.

Este post é sobre como eu construo um BFF em Clojure, e por que a orientação a dados da linguagem torna duas das partes mais difíceis — roteamento e contratos tipados — quase entediantes, no bom sentido.

## O Problema Que um BFF Realmente Resolve

Imagine uma tela de dashboard. Para renderizá-la, o frontend precisa dos metadados do dashboard, dos widgets que ele contém, das permissões do usuário atual e de uma primeira página de dados para cada widget. Nos bastidores, isso são quatro ou cinco serviços diferentes, cada um com sua própria API, sua própria paginação, sua própria ideia de como um timestamp se parece.

Sem um BFF, o navegador orquestra tudo isso: cinco requisições, cinco round trips por uma conexão móvel, e uma pilha de código no cliente costurando as respostas e remodelando tudo para a view. O frontend vira uma camada de integração. Toda mudança no backend reverbera no cliente.

Um BFF move essa orquestração para o servidor. O frontend faz **uma** requisição para um endpoint que existe especificamente para aquela tela. O BFF faz fan-out para os serviços downstream em paralelo, molda o resultado exatamente no que a view precisa e retorna um único payload tipado. O cliente fica mais burro e mais rápido; o contrato fica explícito.

As duas coisas que você tem que acertar são o **contrato** (o que o endpoint promete ao frontend) e a **agregação** (como ele conversa com tudo downstream sem quebrar). Clojure tem uma resposta limpa para as duas.

## Roteamento como Dados com Reitit

O Reitit descreve rotas como dados puros — um vetor de vetores. Isso parece uma coisa pequena. Não é. Porque as rotas são dados, o contrato, as regras de coerção e a documentação vivem todos na mesma estrutura que o router percorre, e você pode inspecionar ou gerar qualquer um deles.

```clojure
(ns bff.routes
  (:require [reitit.ring :as ring]
            [reitit.coercion.malli]
            [reitit.ring.coercion :as coercion]
            [reitit.ring.middleware.muuntaja :as muuntaja]
            [muuntaja.core :as m]))

(def routes
  [["/api/dashboards/:id"
    {:get {:summary    "Everything one dashboard screen needs, in one call"
           :parameters {:path  [:map [:id :uuid]]
                        :query [:map [:page {:optional true} [:int {:min 0}]]]}
           :responses  {200 {:body DashboardView}}
           :handler    dashboard-handler}}]])

(def app
  (ring/ring-handler
    (ring/router
      routes
      {:data {:coercion   reitit.coercion.malli/coercion
              :muuntaja   m/instance
              :middleware [muuntaja/format-middleware
                           coercion/coerce-exceptions-middleware
                           coercion/coerce-request-middleware
                           coercion/coerce-response-middleware]}})))
```

As chaves `:parameters` e `:responses` não são documentação. Elas são aplicadas. O middleware de coerção converte o segmento de path `:id` em um `java.util.UUID` de verdade antes do seu handler rodar, rejeita uma requisição cujo `:page` não seja um inteiro não-negativo com um 400 e uma explicação legível por máquina, e — esta é a parte que as pessoas subestimam — valida a sua *própria resposta* contra `DashboardView` na saída.

## Contratos Tipados Sem um Sistema de Tipos

Clojure é dinamicamente tipada, e a preocupação reflexa de quem vem de uma linguagem estática é: como você mantém uma API honesta sem um compilador? A resposta que adotei é a [malli](https://github.com/metosin/malli): schemas que são, eles próprios, dados.

```clojure
(def Widget
  [:map
   [:id       :uuid]
   [:kind     [:enum :chart :table :metric]]
   [:title    :string]
   [:data-url :string]])

(def DashboardView
  [:map
   [:id          :uuid]
   [:title       :string]
   [:updated-at  inst?]
   [:can-edit    :boolean]
   [:widgets     [:vector Widget]]])
```

Um schema malli é um valor. Eu posso validar com ele, transformar uma falha com `m/explain` em algo que o frontend consegue exibir campo a campo, gerar dados de exemplo a partir dele para testes, coagir JSON fracamente tipado para os tipos Clojure corretos e — via `reitit` — emitir um documento OpenAPI direto dos mesmos schemas que guardam o endpoint. Uma única fonte de verdade, e são os mesmos dados que o router já contém:

```clojure
(require '[reitit.openapi :as openapi])

;; The API docs are derived from the route data, never hand-written,
;; so they cannot drift from what the endpoint actually enforces.
["/openapi.json" {:get {:handler (openapi/create-openapi-handler)}}]
```

Este é o argumento que eu faria para qualquer um cético quanto a tipagem dinâmica em uma fronteira de serviço: eu ganho contratos aplicados em runtime, docs geradas e testes generativos a partir de *uma* declaração, e porque é dado em vez de sintaxe, posso compor e transformar. Um tipo estático é checado uma vez em tempo de compilação e depois some; um schema malli é um valor que eu continuo usando. O trade-off é honesto — a checagem acontece em runtime, não em tempo de compilação — mas em uma fronteira HTTP, onde a entrada é JSON não-confiável de qualquer forma, runtime é exatamente onde você quer a checagem.

## Agregação: Fan-Out em Paralelo, Moldar na Borda

O trabalho do handler é reunir dados de vários serviços downstream e montar o `DashboardView`. Fazer isso sequencialmente deixaria o BFF mais lento do que o cliente tagarela que ele substituiu, então as chamadas têm que rodar em paralelo. Para fazer fan-out de um punhado de chamadas de I/O independentes, `future`s do Clojure com um `deref` com timeout são a ferramenta correta mais simples:

```clojure
(defn dashboard-handler
  [{{{:keys [id]} :path {:keys [page]} :query} :parameters}]
  (let [meta-f    (future (svc/dashboard-meta id))
        widgets-f (future (svc/widgets id))
        perms-f   (future (svc/permissions id))
        ;; deref with a timeout so one slow service can't hang the request
        meta      (deref meta-f    2000 ::timeout)
        widgets   (deref widgets-f 2000 ::timeout)
        perms     (deref perms-f   2000 ::timeout)]
    (if (some #{::timeout} [meta widgets perms])
      {:status 504 :body {:error "upstream-timeout"}}
      {:status 200
       :body   (shape-dashboard meta widgets perms page)})))
```

Os três `future`s começam imediatamente e rodam concorrentemente; a requisição leva mais ou menos o tempo da chamada downstream mais lenta, não a soma de todas elas. `(deref f 2000 ::timeout)` é toda a história de backpressure para esse formato de trabalho — nenhum upstream sozinho consegue segurar a requisição aberta por mais de dois segundos.

`shape-dashboard` é onde o BFF prova seu valor. É pura transformação de dados: pegar três respostas de serviço em três formatos diferentes e produzir o único formato que o frontend declarou. Este é o território de casa do Clojure — `select-keys`, `update`, `map`, um pouco de `clojure.set/rename-keys` — sem framework de mapeamento, sem classes DTO, só funções sobre maps.

```clojure
(defn shape-dashboard [meta widgets perms page]
  {:id         (:dashboard/id meta)
   :title      (:dashboard/title meta)
   :updated-at (:dashboard/updated-at meta)
   :can-edit   (contains? (:permissions perms) :dashboard/edit)
   :widgets    (->> widgets
                    (sort-by :position)
                    (mapv #(select-keys % [:id :kind :title :data-url])))})

;; Because the route declares :responses {200 {:body DashboardView}},
;; a mistake here—a missing key, a stringified UUID—fails the response
;; coercion in development instead of reaching the client.
```

Para casos mais pesados — muito mais do que um punhado de downstreams, ou resultados em streaming — eu recorro aos deferreds da [manifold](https://github.com/clj-commons/manifold) (`d/zip`, `let-flow`) para composição não-bloqueante, ou ao `core.async` quando o trabalho é um pipeline de verdade e não um fan-out. Mas eu não começo por aí. `future` + `deref` cobre o caso comum de BFF e permanece legível, e legível é o que você quer na camada que todo mundo mexe.

## Tratamento de Falhas É o Verdadeiro Design

A versão acima falha a requisição inteira se qualquer downstream der timeout. Às vezes isso está certo — um dashboard sem widgets é inútil. Mas frequentemente a decisão de produto melhor é **degradar**: renderizar o que você tem, marcar o que você não tem.

```clojure
(defn ->widgets [widgets-result]
  (if (= ::timeout widgets-result)
    {:widgets [] :widgets-degraded true}   ; tell the UI to show a retry affordance
    {:widgets (shape-widgets widgets-result)}))
```

Esta é uma decisão que o BFF está unicamente posicionado para tomar, porque ele é o único lugar que sabe tanto o que o frontend precisa quanto quais downstreams responderam. Enterre isso no cliente e toda tela reinventa; enterre nos serviços downstream e nenhum deles tem o contexto. O BFF é dono da pergunta "o que esta tela faz quando o serviço X está fora?", e respondê-la de forma explícita — por tela — é a maior parte do que faz uma UI com muitos dados parecer sólida em vez de instável.

## Os Trade-Offs, Nomeados

Um BFF não é de graça, e a versão Clojure não muda o formato dos custos:

- **Ele acopla ao frontend.** O endpoint existe para servir a tela de um cliente. Quando a tela muda, o BFF muda. Esse é o objetivo — mas significa que o time do BFF e o time do frontend andam juntos, o que é uma decisão organizacional tanto quanto técnica.
- **É mais um hop e mais um deploy.** Você adicionou um serviço. Se seu frontend conversa com exatamente um backend que já retorna o formato certo, um BFF é prematuro — você estaria adicionando uma fronteira de rede para resolver um problema que você não tem.
- **Pode virar um depósito de lixo.** "Só põe no BFF" é tentador para qualquer lógica que fica desajeitada em outro lugar. A disciplina é que o BFF *orquestra e molda*; ele não é dono de regras de negócio. No momento em que ele começa a tomar decisões autoritativas sobre dados, está virando o monolito distribuído que você tentava evitar.

**Quando está certo:** um frontend com muitos dados, vários serviços downstream, e formatos que não batem com o que nenhum serviço isolado retorna. **Quando está errado:** um backend, um cliente, e um payload que já está perto do que a view precisa.

## Por Que Clojure, Especificamente

Toda parte deste padrão é dado. Rotas são dados (Reitit). Contratos são dados (malli). A transformação das respostas dos serviços para a view é dado-entra, dado-sai (funções puras sobre maps). Não há DSL de roteamento para aprender, sem processador de anotações, sem passo de geração de código, sem camada de DTO espelhando seus schemas em três lugares. O contrato contra o qual você valida é o mesmo valor do qual você gera docs e o mesmo valor contra o qual você escreve testes.

Essa é a linha condutora de por que eu continuo voltando ao Clojure para essa camada: o BFF é fundamentalmente um problema de moldagem de dados, e Clojure trata programas como manipulação de dados até o fim. A linguagem e o problema têm o mesmo formato.

---
