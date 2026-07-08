---
title: "ClojureScript para Desenvolvedores React"
date: "2024-03-07"
description: "Se você já conhece React, a maior parte do ClojureScript está mais perto do que você imagina. Reagent é React com dados imutáveis e hiccup no lugar de JSX — eis o mapa mental, a história de interop, e por que re-render por igualdade de referência é o ganho silencioso."
tags:
  [
    "clojurescript",
    "react",
    "frontend",
    "reagent",
    "programacao-funcional",
  ]
---

Se você escreve React, já entende a maior parte da história de front-end do ClojureScript — só ainda não sabe disso. Reagent, a biblioteca de UI dominante em ClojureScript, *é* React por baixo. Componentes são funções que retornam markup, estado dispara re-renders, o virtual DOM faz diff e patch. O que muda é a linguagem ao redor: dados imutáveis por padrão, s-expressions no lugar de JSX, e uma ferramenta de build que você dirige de um REPL. Este é o mapa que eu daria ao meu eu React do passado.

## Componentes Ainda São Só Funções Retornando Markup

Um function component do React retorna JSX. Um componente Reagent retorna *hiccup* — vetores ClojureScript puros descrevendo o DOM. Keyword para a tag, um map para props, depois filhos:

```clojure
(defn greeting [name]
  [:div.greeting                      ; <div class="greeting">
   [:h1 "Hello, " name]
   [:button {:on-click #(js/alert "hi")} "Wave"]])
```

Isso é `<div className="greeting"><h1>Hello, {name}</h1><button onClick={...}>Wave</button></div>`. Sem transform de JSX, sem DSL de template — o markup são estruturas de dados que sua linguagem já tem, o que significa que você manipula views com o mesmo `map`/`filter`/`assoc` que usa em qualquer outro dado:

```clojure
(defn order-list [orders]
  [:ul
   (for [o orders]
     ^{:key (:id o)} [:li (:name o) " — $" (:total o)])])   ; map over data → children
```

O `^{:key ...}` é a key de reconciliação do React, mesmo conceito do `key={}` em JSX.

## Estado: ratoms no Lugar de useState

O `useState` do React te dá um valor e um setter que dispara um re-render. O equivalente do Reagent é um *reactive atom* (`r/atom`): um container mutável de um valor imutável. Faça deref (`@`) para ler; qualquer componente que faz deref dele re-renderiza quando ele muda.

```clojure
(defn counter []
  (let [n (r/atom 0)]                 ; created once (form-2 component)
    (fn []                            ; this render fn re-runs on change
      [:div
       [:span "Count: " @n]           ; deref → this component tracks n
       [:button {:on-click #(swap! n inc)} "+"]])))
```

O único detalhe para devs React: este é um componente "form-2" — o `let` externo roda uma vez (como o corpo de um setup de `useState`), e a função interna retornada é o que re-renderiza. Confunda isso e seu atom reseta a cada render. Passado isso, o modelo é exatamente o do React: estado local num ratom, estado global compartilhado em algo como re-frame, que comparei ao Redux [aqui](/pt/posts/redux-and-re-frame-frontend-state).

## O Ganho Silencioso: Dados Imutáveis Tornam Re-renders Baratos

Aqui está o payoff que não é óbvio até você sentir. No React, evitar re-renders desnecessários significa `memo`, `useMemo`, `useCallback` e arrays de dependência cuidadosos — porque com objetos JS mutáveis, "esta prop de fato mudou?" não pode ser respondido por uma checagem barata de referência (dois objetos diferentes podem ser estruturalmente iguais, e um objeto mutado é referência-igual ao seu eu antigo).

Dados em ClojureScript são imutáveis e persistentes, então "isto mudou?" *é* uma checagem de igualdade de referência, e é sempre correta: se a referência é a mesma, o valor não mudou, garantido. O Reagent se apoia nisso para pular re-render de subárvores cujas entradas são idênticas, sem nenhuma da contabilidade manual de memoização. A coisa que você luta no React — renders desnecessários por churn de referência — em grande parte evapora porque igualdade é barata e honesta. (Esta é a mesma propriedade que faz as técnicas de renderização de datasets grandes no [post sobre performance com re-frame](/pt/posts/clojure-reframe-data-heavy-rendering-performance) funcionarem.)

## Interop É Cidadão de Primeira Classe

Você não perde o ecossistema React. Você pode colocar qualquer componente React no hiccup com o adapter `:>`, passando props como um map:

```clojure
(ns app.core
  (:require ["react-select" :default Select]   ; import an npm package
            [reagent.core :as r]))

(defn country-picker [options]
  [:> Select {:options options                 ; render a JS React component
              :on-change #(js/console.log %)}])
```

`js/console.log`, `(.-property obj)` para acesso a campo, `(.method obj args)` para chamadas — interop com JavaScript é uma parte normal e cotidiana da linguagem, não uma saída de emergência. Você recorre a bibliotecas npm quando são a melhor ferramenta e escreve o resto em ClojureScript.

## O Build: shadow-cljs

A ferramenta que torna isso agradável para desenvolvedores React é o `shadow-cljs`, porque ele trata npm como fonte de dependência de primeira classe — você `npm install` um pacote e `require` diretamente. Ele te dá hot reloading (edite um componente, veja atualizar com estado preservado) e um REPL conectado à app rodando no browser, então você avalia expressões ao vivo contra sua UI real. Vindo de um setup Webpack/Vite, a config é menor e a história do npm é mais suave do que você esperaria de um toolchain de "linguagem diferente".

## Trade-offs

```
Concern            React (JS/TS)             ClojureScript (Reagent)
-----------------  ------------------------  ----------------------------
Markup             JSX (transform)           hiccup (plain data)
Local state        useState                  r/atom (ratom)
Avoiding renders   memo/useMemo/useCallback  ~free via reference equality
npm ecosystem      native                    native via shadow-cljs + interop
Types              TypeScript                dynamic (spec/malli optional)
Hiring/onboarding  huge pool                 small pool, ramp-up needed
```

## A Lição

Para um desenvolvedor React, ClojureScript é menos um salto do que os parênteses sugerem: é o modelo mental do React que você já domina, expresso numa linguagem onde dados imutáveis são o padrão e não uma disciplina. Você troca a segurança estática do TypeScript e um enorme pool de contratação por correção de re-render genuinamente mais barata e um fluxo guiado pelo REPL. É uma troca real — um time nativo de JS não deveria trocar de leve — mas se você já lutou com uma parede de `useMemo` para domar re-renders, o lado ClojureScript dessa troca vai parecer alívio.
