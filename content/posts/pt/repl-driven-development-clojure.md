---
title: "Desenvolvimento Guiado pelo REPL: Como Eu Realmente Escrevo Clojure"
date: "2021-07-05"
description: "Desenvolvimento guiado pelo REPL não é 'um console melhor' — é editar um programa vivo. Como eu construo funções incrementalmente contra dados reais de dentro do meu editor, usando rich comments e tap>, e onde o fluxo morde."
tags:
  [
    "clojure",
    "repl",
    "fluxo-de-trabalho",
    "programacao-funcional",
    "backend",
  ]
---

Quando cheguei ao Clojure vindo de Go, "o REPL" soava como o prompt interativo do Python — um rascunho para testar one-liners. Esse enquadramento o subestimava completamente, e levei meses para pegar o fluxo de trabalho de verdade. Desenvolvimento guiado pelo REPL não é digitar num console. É manter um programa vivo rodando e remodelá-lo a partir do seu editor, uma expressão por vez, contra dados reais. Depois que fez sentido, voltar ao editar-salvar-reiniciar pareceu programar de olhos vendados.

## O Loop É o Ponto

No meu fluxo com Go o loop é: escrever código, salvar, `go run`/`go test`, ler a saída, repetir. O programa está morto entre execuções; toda mudança custa um restart completo. Em Clojure o programa fica vivo. Meu editor está conectado a um processo em execução, e eu avalio a *forma sob o cursor* — uma única função, uma única expressão — enviando-a para aquele processo vivo e vendo o resultado inline, sem reiniciar nada.

Concretamente: eu escrevo uma função, avalio-a (ela agora está definida no programa em execução), depois avalio uma chamada a ela com um argumento real e leio o resultado logo ao lado do código. Se está errado, edito a função, re-avalio, e chamo de novo — o processo nunca reiniciou, e qualquer estado caro que montei antes (um dataset carregado, uma conexão de banco) ainda está lá.

```clojure
(defn line-total [item]
  (* (:qty item) (:unit-price item)))

;; Evaluate the def above, then evaluate this call right here:
(line-total {:qty 3 :unit-price 250})
;; => 750
```

Eu não rodei um arquivo de teste nem iniciei um programa. Perguntei ao processo vivo e ele respondeu. Esse feedback é instantâneo e é contra um *valor real*, que é por que ele parcialmente substitui o compilador do qual sinto falta vindo de Go.

## Construindo de Baixo pra Cima, Não Escrevendo de Uma Vez

A mudança mais profunda é que eu não escrevo mais uma função inteira e depois descubro se funciona. Eu a faço crescer. Digamos que estou fazendo parsing e resumindo pedidos. Avalio passos intermediários e mantenho o que funciona:

```clojure
(def sample (slurp "orders.json"))          ; evaluate — now `sample` holds real data
(def parsed (json/read-str sample :key-fn keyword))  ; evaluate — inspect the shape
(->> parsed (filter #(= "paid" (:status %))))        ; evaluate — does the filter work?
(->> parsed (filter #(= "paid" (:status %))) (map :total))  ; add the next step, evaluate
```

Cada linha é avaliada conforme a adiciono, contra os dados reais, então eu *vejo* o formato a cada passo em vez de adivinhar e rodar tudo no final. Quando eu dobro isso numa função, já assisti cada estágio funcionar. Isto é o oposto do ritmo do Go, onde escrevo a função inteira contra meu modelo mental dos dados e descubro incompatibilidades só quando rodo.

## Rich Comments Guardam o Trabalho de Rascunho

O perigo de toda essa exploração é lixo — chamadas descartáveis espalhadas pelo arquivo. O idioma que resolve isso é o bloco `(comment ...)`, universalmente chamado de "rich comment". Código dentro de `comment` nunca roda quando o arquivo carrega, mas meu editor felizmente avalia formas individuais dentro dele. Então eu guardo minha exploração ao lado da função que ela exercita, permanentemente:

```clojure
(defn paid-total [orders]
  (->> orders (filter #(= "paid" (:status %))) (map :total) (reduce + 0)))

(comment
  ;; scratch space — evaluate these by hand, never runs on load
  (paid-total parsed)
  (paid-total [])                ; edge case: empty
  (def parsed (json/read-str (slurp "orders.json") :key-fn keyword)))
```

Seis meses depois esse bloco é documentação: ele mostra à próxima pessoa (geralmente eu) exatamente como dirigir a função com input real.

## Enxergando Dentro do Código em Execução com tap>

Para valores enterrados dentro de um pipeline, `tap>` envia qualquer coisa para um handler registrado sem perturbar o fluxo — como um `print`, mas estruturado e ligável/desligável:

```clojure
(add-tap (bound-fn* clojure.pprint/pprint))  ; once, at the REPL

(defn process [orders]
  (->> orders
       (filter #(= "paid" (:status %)))
       (doto tap>)          ; peek at the intermediate seq, pass it through
       (map :total)
       (reduce + 0)))
```

Diferente de espalhar `println`, os taps vão para um handler que eu controlo e posso desligar, e me entregam a estrutura de dados real para inspecionar, não uma versão em string.

## Onde Morde

Não é de graça. **Drift de estado** é a armadilha clássica: você redefine uma função, esquece de re-avaliar algo que depende dela, e seu processo em execução não bate mais com seu arquivo. A disciplina é recarregar periodicamente o namespace do disco para que a imagem viva não divirja silenciosamente da fonte da verdade. **Recompensa configuração**: você precisa da integração REPL do seu editor configurada, e se pular isso e usar um REPL de terminal puro, você tem a ergonomia do prompt-do-Python e nada da mágica. E pode gerar **código só-de-REPL** — coisas que funcionam no seu processo aquecido mas nunca foram capturadas num teste, então quebram para a próxima pessoa.

## A Lição

A mudança de mentalidade em relação a Go é esta: eu costumava tratar o programa em execução como a *saída* da minha edição. Em Clojure o programa em execução é o *meio* no qual eu edito. Correção deixa de ser algo que verifico no fim de um ciclo e se torna algo que observo continuamente, forma por forma, contra dados reais. É a metade-fluxo do que faz o Clojure parecer diferente — a [metade-linguagem sobre a qual escrevi aqui](/pt/posts/coming-to-clojure-from-go). Combine-a com o hábito de promover seus experimentos de rich-comment para testes de verdade, e você ganha o feedback rápido sem o drift.
