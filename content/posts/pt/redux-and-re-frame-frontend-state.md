---
title: "Redux e re-frame: Duas Abordagens para Estado no Front-End"
date: "2023-09-21"
description: "Redux (React/JS) e re-frame (ClojureScript) partem da mesma ideia — um estado imutável da app, updates puros, views derivadas — e divergem de formas instrutivas. O que cada um acerta, e o que os effects e interceptors do re-frame adicionam."
tags:
  [
    "frontend",
    "redux",
    "re-frame",
    "clojurescript",
    "gerenciamento-de-estado",
    "react",
  ]
---

Já construí front-ends em produção tanto em React com Redux quanto em ClojureScript com re-frame, e o interessante é o quanto eles concordam. Eles chegam de culturas de linguagem diferentes a quase a mesma arquitetura: um único valor de estado imutável, funções puras que computam o próximo estado, e views derivadas que recomputam só quando suas entradas mudam. Ver o mesmo formato duas vezes me ensinou quais partes desse formato são essenciais e quais são incidentais. Eis a comparação que eu queria ter tido antes de aprender ambos.

## O Núcleo Compartilhado

Ambos os frameworks são construídos sobre os mesmos quatro compromissos:

1. **Uma fonte da verdade** — o estado inteiro da app é um único valor imutável (o store do Redux, o `app-db` do re-frame).
2. **Você não o muta** — você despacha uma intenção descrita (uma *action* / um *event*) e uma função pura retorna o próximo estado.
3. **Views são derivadas** — componentes leem do estado através de selectors/subscriptions que memoizam, então um componente re-renderiza só quando a fatia específica que lê muda.
4. **Dados fluem numa direção** — event → update de estado → views derivadas → UI → event. Nenhum componente alcança de lado para o estado de outro.

Se você internaliza isso num framework, 80% transfere para o outro. É a ideia genuinamente importante, e é independente de linguagem.

## Redux, na Sua Forma Moderna

O Redux moderno (com Redux Toolkit) é um `slice`: um pedaço de estado mais os reducers puros que o atualizam. Por baixo ele usa Immer para que você *escreva* código com aparência de mutação que de fato produz um novo estado imutável:

```javascript
const ordersSlice = createSlice({
  name: "orders",
  initialState: { items: [], filter: "all" },
  reducers: {
    filterChanged(state, action) {
      state.filter = action.payload; // Immer makes this immutable under the hood
    },
  },
});

// Derived view (reselect): recomputes only when items/filter change
const selectVisible = createSelector(
  [(s) => s.orders.items, (s) => s.orders.filter],
  (items, filter) =>
    filter === "all" ? items : items.filter((o) => o.status === filter)
);
```

## re-frame, o Mesmo Formato em ClojureScript

O re-frame registra um *event handler* (uma função pura de `db` para novo `db`) e uma *subscription* (uma view derivada e memoizada). Como dados em ClojureScript são imutáveis por padrão, não há equivalente ao Immer — você só retorna um novo valor com `assoc`:

```clojure
(rf/reg-event-db
  :filter-changed
  (fn [db [_ filter]]
    (assoc db :filter filter)))

;; Derived view — recomputes only when :items or :filter change
(rf/reg-sub
  :visible-orders
  :<- [:items]
  :<- [:filter]
  (fn [[items filter] _]
    (if (= :all filter)
      items
      (filterv #(= filter (:status %)) items))))
```

Linha por linha, isso é o slice do Redux + selector do reselect, numa linguagem onde imutabilidade não precisou de uma biblioteca parafusada. Escrevi sobre empurrar este modelo de subscriptions ao limite para datasets grandes [no post sobre renderização com re-frame](/pt/posts/clojure-reframe-data-heavy-rendering-performance).

## Onde o re-frame Vai Além: Effects e Coeffects

Aqui está a divergência que vale entender. No Redux, um reducer tem que ser puro, então qualquer coisa impura — uma chamada de API, ler `localStorage`, gerar um timestamp — acontece em *middleware* (thunks, sagas), e a fronteira entre "update puro" e "efeito colateral" é uma convenção que você mantém.

O re-frame torna essa fronteira explícita e orientada a dados. Um event handler não *executa* efeitos; ele *retorna uma descrição* dos efeitos que quer, e os effect handlers registrados do re-frame os realizam:

```clojure
(rf/reg-event-fx                 ; -fx: returns a map of effects, performs nothing
  :save-order
  (fn [{:keys [db]} [_ order]]
    {:db   (assoc db :saving true)         ; the state change
     :http {:method :post :url "/orders"   ; a described side effect
            :body order
            :on-success [:save-succeeded]}}))
```

O handler permanece puro — retorna dados — e a chamada HTTP de fato vive num effect handler reutilizável. Similarmente, *coeffects* injetam entradas impuras (hora atual, um id aleatório, localStorage) como dados, então mesmo ler o mundo externo não torna o handler impuro. O resultado: event handlers do re-frame são puros e portanto trivialmente testáveis — alimente-os com um `db` e um map de coeffects, faça asserções sobre o map de effects que retornam, sem mocking. O Redux Toolkit chega perto, mas o "efeitos são só dados que você retorna" do re-frame é a expressão mais limpa da ideia.

## Trade-offs

```
Aspect              Redux (Toolkit)              re-frame (ClojureScript)
------------------  ---------------------------  ----------------------------
Immutability        via Immer (write "mutating") native (assoc returns new)
Derived views       reselect selectors           layered subscriptions
Side effects        middleware (thunks/sagas)    effects/coeffects as data
Handler purity      reducers pure, effects out   handlers pure, effects returned
Ecosystem           huge (React/JS)              small (CLJS), very coherent
Onboarding          familiar to JS devs          need to learn ClojureScript
```

## A Lição

Redux é a escolha pragmática: é onde seu time de React já está, o ecossistema é vasto, e o Toolkit lixou a maior parte do boilerplate histórico. re-frame é a expressão mais *coerente* da mesma arquitetura — efeitos-como-dados e imutabilidade nativa fazem parecer a ideia levada à sua conclusão lógica — mas te pede para adotar ClojureScript, o que é um custo real para um time nativo de JS. O que me convenceu é que ambos aterrissaram no mesmo núcleo, o que significa que o núcleo é a coisa a aprender: estado único imutável, updates puros, views derivadas memoizadas, fluxo numa direção. Escolha o framework pelo seu time e ecossistema; a arquitetura é o ativo transferível.
