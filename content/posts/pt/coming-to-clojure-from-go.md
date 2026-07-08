---
title: "Chegando ao Clojure Vindo de Go: O Que Finalmente Fez Sentido"
date: "2020-11-16"
description: "O relato honesto de um desenvolvedor Go aprendendo Clojure — o que exigiu ajuste de verdade (parênteses, tipagem dinâmica, a JVM) e o que se mostrou genuinamente melhor (o REPL, imutabilidade por padrão, e tratar todo programa como transformação de dados)."
tags:
  [
    "clojure",
    "programacao-funcional",
    "golang",
    "backend",
  ]
---

Eu escrevo Go para viver e gosto: é explícito, é entediante no bom sentido, e consigo ler o Go de um estranho e saber o que ele faz. Então, quando comecei a passar as noites em Clojure, minha primeira reação foi resistência. Os parênteses. A tipagem dinâmica. Os stack traces que rolam feito créditos de filme. Este post é a versão honesta desse ajuste — o que continuou irritante, e o que silenciosamente recablou como eu penso sobre escrever programas.

## O Que Exigiu Ajuste de Verdade

**Os parênteses não são o problema — meu editor era.** Por uma semana eu contei colchetes na mão e odiei. Aí liguei edição estrutural (paredit) e os parênteses ficaram invisíveis, porque parei de editar texto e comecei a editar *estrutura*: puxar uma forma para dentro de uma expressão, envolver uma chamada, extrair uma. Vindo de Go, onde edito caracteres, editar expressões inteiras como unidades pareceu estranho por uns três dias e depois pareceu um superpoder que eu não sabia que me faltava.

**Tipagem dinâmica me deu ansiedade.** Em Go o compilador é uma rede de segurança na qual me apoio constantemente; renomeie um campo e todo call site acende em vermelho. Clojure te entrega um map e confia que você sabe o que tem nele. Senti falta da rede. O que parcialmente a substituiu não foi um type checker, mas o REPL — eu mandava um valor real para uma função e *via* o resultado imediatamente, então o feedback loop que um compilador te dá em build time, eu tinha interativamente, uma forma por vez. É um tipo diferente de confiança, e ainda estou calibrando quanto confio nela.

**A JVM é pesada.** `go build` produz um binário estático que inicia instantaneamente; um processo Clojure leva segundos para subir. Para um servidor de longa duração isso é irrelevante, mas colore a experiência inteira — você não reinicia um processo Clojure para testar uma mudança, o que acaba sendo o ponto (mais sobre isso abaixo).

## O Que Fez Sentido

**Imutabilidade por padrão inverteu um custo mental que eu não sabia que pagava.** Em Go estou constantemente, silenciosamente perguntando "quem mais tem um ponteiro para isso, e pode mutá-lo enquanto eu uso?" Em Clojure os dados não mudam, então essa pergunta desaparece. Você transforma valores em novos valores:

```clojure
;; The original map is untouched; assoc returns a new one
(def order {:id 42 :status :pending :total 100})
(assoc order :status :paid)
;; => {:id 42, :status :paid, :total 100}
order
;; => {:id 42, :status :pending, :total 100}  (unchanged)
```

O equivalente em Go funciona, mas eu tenho que *decidir* copiar, e tenho que lembrar de fazer isso. Aqui é o padrão, e compartilhar dados entre goroutines — quer dizer, entre threads — deixa de ser algo sobre o qual preciso raciocinar.

**Tudo é dado, e o mesmo punhado de funções funciona em tudo.** Em Go, iterar um slice, um map e um channel são três formatos diferentes de código. Em Clojure, `map`, `filter` e `reduce` funcionam sobre qualquer coisa sequencial, e eu componho com threading:

```clojure
(->> orders
     (filter #(= :paid (:status %)))
     (map :total)
     (reduce + 0))
```

Lendo de cima para baixo: pegue os pedidos, mantenha os pagos, extraia os totais, some. Em Go isso é um loop com um acumulador — perfeitamente claro, mas eu escrevo o *mecanismo* toda vez. Aqui eu componho *intenção*. Depois de um tempo, a versão Go começou a parecer que eu estava fazendo à mão algo que a linguagem deveria me dar.

**Dados sobre tipos, para moldar.** Boa parte do meu código Go define uma struct para eu mover um formato ligeiramente diferente de dados por aí. Em Clojure eu só... uso um map, e o remodelo com `select-keys`, `update`, `merge`. Para código cujo trabalho inteiro é transformar um formato de dados em outro — que é a maior parte do código backend — isso é menos cerimônia e mais franqueza.

## Do Que Ainda Desconfio

Não sou um convertido te vendendo uma religião. A falta de tipos estáticos é um trade-off real, não um almoço grátis: num time grande, numa base de código grande, o "você mudou este formato e esqueceu destes 12 lugares" do compilador vale muito, e ainda não sei como times Clojure conseguem essa confiança em escala (spec e bons testes, me dizem — vou descobrir). Mensagens de erro, quando um `nil` se infiltra numa função de sequência, são genuinamente piores que as de Go. E "é tudo só map" é libertador até você abrir uma função e não ter ideia de quais chaves o map deveria ter.

Eu vinha querendo escrever sobre o lado front-end disso também — venho construindo UIs com re-frame, que se apoia exatamente neste modelo de transformação de dados imutáveis, e é a arquitetura de front-end mais coerente que já usei ([notas aqui](/pt/posts/clojure-reframe-data-heavy-rendering-performance)).

## A Lição

O que o Clojure mudou não é qual linguagem eu pego no trabalho — é que agora eu noto o estado mutável e as transformações repetitivas no meu Go, e escrevo ambos com mais deliberação. A melhor razão para aprender uma linguagem cujos padrões são o oposto da sua do dia a dia não é para trocar. É que o contraste torna visíveis de novo os trade-offs que você tinha parado de ver. Go escolhe explicitude e um compilador; Clojure escolhe expressividade e um REPL. Conhecendo ambos, eu escolho com mais consciência.
