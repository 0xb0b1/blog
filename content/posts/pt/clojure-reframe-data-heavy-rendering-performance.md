---
title: "Deixando uma UI re-frame com Muitos Dados Rápida: Subscriptions, Re-renders e Windowing"
date: 2020-09-14
description: "Como eu mantenho um single-page app re-frame/Reagent com muitos dados responsivo: estruturando em camadas o grafo de sinais das subscriptions, empurrando os derefs para baixo para que só os componentes que mudaram re-renderizem, e fazendo windowing de tabelas grandes em vez de renderizar tudo."
tags:
  [
    "clojure",
    "clojurescript",
    "re-frame",
    "reagent",
    "frontend",
    "performance",
    "web-rendering",
  ]
---

Escrevo Clojure desde 2017, e boa parte disso foi ClojureScript na frente de produtos com muitos dados — do tipo em que uma única tela contém uma tabela de milhares de linhas, filtros ao vivo e um punhado de gráficos, todos lendo de um único app-db grande. O re-frame torna isso agradável de construir. Ele também torna fácil construir algo que trava a cada tecla se você não entende como o grafo reativo realmente recomputa.

Isto é o que eu aprendi sobre manter essas UIs rápidas, enquadrado em torno dos três erros que eu cometi e depois parei de cometer.

## O Modelo Mental: Um Grafo de Sinais Que Memoiza

As subscriptions do re-frame formam um grafo direcionado. O `app-db` é a raiz. Cada `reg-sub` é um nó que deriva um valor de outros nós, e — este é o ponto todo — um nó só recomputa quando uma das suas entradas realmente muda. Por baixo, essas são reactions do Reagent, que memoizam seu último valor e só disparam para baixo quando esse valor difere.

Um componente Reagent, por sua vez, re-renderiza só quando um valor reativo que ele *dereferencia* muda. Não quando o app-db muda. Quando a reaction específica que ele lê muda.

Quase todo problema de performance que eu enfrentei é uma violação de um desses dois fatos: ou uma subscription recomputa demais, ou um componente re-renderiza porque está lendo algo mais grosso do que precisa.

## Erro 1: A Subscription Divina

A primeira versão tentadora coloca toda a derivação em um só lugar:

```clojure
;; Everything the table needs, computed in one subscription
(rf/reg-sub
  :table-data
  (fn [db _]
    (let [rows    (:rows db)
          filter- (:filter-text db)
          sort-k  (:sort-key db)]
      (->> rows
           (filter #(str/includes? (:name %) filter-))
           (sort-by sort-k)))))
```

Isso recomputa o filtro-e-ordenação *inteiro* toda vez que **qualquer** uma das suas entradas muda — e porque lê `db` diretamente, é re-executado a cada mudança no app-db, mesmo as não relacionadas à tabela. Digite um caractere na caixa de filtro e você re-ordena milhares de linhas.

A correção é estruturar o grafo em camadas do jeito que o re-frame pretende: subscriptions de **extração** baratas que só puxam uma fatia do app-db, e subscriptions de **materialização** que derivam *dessas* e recomputam só quando suas entradas específicas mudam.

```clojure
;; Layer 2 — extraction: trivial, cheap, re-run only when that key changes
(rf/reg-sub :rows        (fn [db _] (:rows db)))
(rf/reg-sub :filter-text (fn [db _] (:filter-text db)))
(rf/reg-sub :sort-key    (fn [db _] (:sort-key db)))

;; Layer 3 — materialization: subscribes to Layer 2, not to db
(rf/reg-sub
  :filtered-rows
  :<- [:rows]
  :<- [:filter-text]
  (fn [[rows filter-text] _]
    (filterv #(str/includes? (:name %) filter-text) rows)))

(rf/reg-sub
  :visible-rows
  :<- [:filtered-rows]
  :<- [:sort-key]
  (fn [[rows sort-key] _]
    (sort-by sort-key rows)))
```

Agora mudar `:sort-key` re-executa só a ordenação, sobre o conjunto já filtrado. Mudar uma parte não relacionada do app-db não re-executa nada aqui, porque nenhuma dessas subscriptions lê `db` diretamente mais. O grafo faz o mínimo de trabalho que cada mudança exige. Essa estruturação em camadas é a coisa de maior alavancagem que você pode fazer em um app re-frame, e não custa nada além de disciplina.

## Erro 2: Dereferenciar Alto Demais na Árvore

Mesmo com um grafo de sinais limpo, *onde* você faz deref decide o que re-renderiza. Se o componente da tabela dá deref na coleção inteira de linhas, então qualquer mudança em qualquer linha re-renderiza a tabela toda:

```clojure
;; Every row re-renders when any row changes
(defn table []
  (let [rows @(rf/subscribe [:visible-rows])]
    [:table
     [:tbody
      (for [r rows]
        ^{:key (:id r)} [:tr [:td (:name r)] [:td (:value r)]])]]))
```

Empurre o deref para baixo. O pai se inscreve só nos *ids* (um valor barato que muda raramente); cada linha se inscreve na sua própria fatia por id e re-renderiza isoladamente:

```clojure
(rf/reg-sub :visible-row-ids
  :<- [:visible-rows]
  (fn [rows _] (mapv :id rows)))

(rf/reg-sub :row
  :<- [:rows-by-id]
  (fn [rows-by-id [_ id]] (get rows-by-id id)))

(defn row-view [id]
  (let [r @(rf/subscribe [:row id])]        ; each row owns its subscription
    [:tr [:td (:name r)] [:td (:value r)]]))

(defn table []
  (let [ids @(rf/subscribe [:visible-row-ids])]   ; re-renders only when the set of ids changes
    [:table
     [:tbody
      (for [id ids]
        ^{:key id} [row-view id])]]))
```

Atualize o valor de uma linha e exatamente um `<tr>` re-renderiza. A metadata `^{:key id}` não é decoração — é o que permite ao reconciliador do React casar as linhas entre renders em vez de destruir e reconstruir a lista. Erre a key (índice, ou ausente) e você paga por uma rotatividade de DOM que não precisava.

## Erro 3: Renderizar Linhas Que Ninguém Vê

Subscriptions em camadas e componentes por linha ainda não vão te salvar de pedir ao DOM para segurar 10.000 nós `<tr>`. O navegador não liga para quão elegante é seu grafo de sinais; ele tem que fazer layout e pintar cada nó que você der.

A resposta é windowing: renderizar só as linhas atualmente na viewport, mais um pequeno buffer, e traduzir a posição do scroll em uma fatia.

```clojure
(defn windowed-table [row-height buffer]
  (let [scroll-top (r/atom 0)
        view-h     (r/atom 600)]
    (fn []
      (let [ids   @(rf/subscribe [:visible-row-ids])
            total (count ids)
            start (max 0 (- (quot @scroll-top row-height) buffer))
            end   (min total (+ (quot (+ @scroll-top @view-h) row-height) buffer))]
        [:div.viewport
         {:style {:height @view-h :overflow-y "auto"}
          :on-scroll #(reset! scroll-top (.. % -target -scrollTop))}
         ;; spacer preserves the real scrollbar height
         [:div {:style {:height (* total row-height) :position "relative"}}
          (for [id (subvec ids start end)]
            ^{:key id}
            [:div {:style {:position "absolute"
                           :top (* (.indexOf ids id) row-height)
                           :height row-height}}
             [row-view id]])]]))))
```

Em produção eu normalmente recorreria a uma biblioteca já testada em batalha via interop — o `react-window` embrulha bem em um componente Reagent — em vez de escrever à mão os casos extremos (alturas de linha variáveis, scroll com inércia). Mas o princípio é o que importa: o custo de uma lista deveria escalar com o que está *visível*, não com o que *existe*.

## O Imposto de Interop Que Ninguém Te Avisa

Uma armadilha específica de muitos dados: se seus dados chegam como JSON e você faz `js->clj` de um payload grande, essa conversão é cara e aloca uma estrutura paralela inteira. Em alguns kilobytes não é nada; em uma resposta de 5 MB é um travamento visível. Opções, mais ou menos na ordem de quão frequentemente eu uso: manter os dados quentes como JS nativo e mexer neles só onde precisa, converter de forma preguiçosa/incremental, ou empurrar a moldagem para o servidor (um [BFF](/pt/posts/clojure-bff-typed-contracts-reitit-malli) é um bom lugar para fazer exatamente isso) para que o cliente receba algo pequeno e já moldado.

## Trade-Offs, Nomeados

- **Camadas adicionam indireção.** Três subscriptions onde você começou com uma. Vale a pena quando os dados são grandes ou as atualizações são frequentes; exagero para uma página de configurações estática. Combine a maquinaria com a carga.
- **Subscriptions por linha também têm overhead.** Cada subscription é uma reaction com sua própria contabilidade. Para um punhado de linhas, a subscription divina é genuinamente ok e mais simples. Isso compensa em escala, não abaixo dela.
- **Windowing complica o DOM.** Posicionamento absoluto, matemática de scroll e "pular para a linha" viram problema seu. Não faça windowing de uma tabela de 50 linhas.

O meta-ponto é que o re-frame te dá um modelo de custo preciso — o grafo de sinais te diz exatamente o que recomputa e o que re-renderiza — e UIs rápidas com muitos dados vêm de trabalhar *com* esse modelo em vez de lutar contra ele. Quando algo trava, eu não chuto; eu pergunto qual subscription está recomputando e qual componente está re-renderizando, e a resposta é quase sempre um desses três erros.

---
