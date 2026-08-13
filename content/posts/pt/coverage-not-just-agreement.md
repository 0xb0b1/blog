---
title: "Cobertura, Não Apenas Concordância"
date: "2026-07-23"
description: "Um harness de paridade reportando 100% de concordância não diz nada até você saber o que ele perguntou. Sobre transformar cobertura num portão em vez de uma estatística, declarar as lacunas em voz alta, e delimitar um replay ao núcleo determinístico para que a alegação publicada seja uma que você consiga defender."
tags:
  [
    "testes",
    "migracao",
    "go",
    "python",
    "qualidade",
    "extracao-de-assinaturas",
  ]
---

Escrevi sobre [provar uma reescrita comparando-a com o sistema que ela substituiu](/pt/posts/proving-a-rewrite-against-real-purchases) sobre um corpus completo. O número que saiu foi zero discordâncias, e eu disse na ocasião que o número era a parte menos interessante.

Este é o porquê. Um resultado de comparação tem duas dimensões, e apenas uma delas é reportada por padrão.

**Concordância** é o que todo mundo mede: dos casos que comparamos, quantos bateram? **Cobertura** é o que torna a concordância significativa: dos casos que existem, quais nós comparamos? Um harness que reporta apenas o primeiro está descrevendo sua própria diligência nos termos mais lisonjeiros possíveis.

## A falha que isso previne

Concretamente: nosso harness de replay passa notificações reais de loja por um novo decodificador e compara sua saída contra a do antigo. Suponha que ele reporte 100% de concordância sobre 700.000 notificações.

Isso é compatível com o harness nunca ter exercitado mais que dois tipos de notificação — porque esses dois representam 95% do volume e os outros onze tipos são raros. Os raros são também os que têm a lógica interessante: revogação, reembolso, upgrade, uma assinatura mudando de produto no meio do período. Concordância perfeita em renovações não diz nada sobre reembolsos, e métricas ponderadas por volume nunca vão notar.

Então o critério é explícito:

> **AC-048** — A cobertura por tipo de notificação é reportada, e lacunas são declaradas.

Duas obrigações. Reportar a cobertura *por tipo*, não em agregado. E onde um tipo não foi exercitado, **dizer isso, na saída.**

## Declarar lacunas é a metade mais difícil

Reportar cobertura por tipo é um agrupamento. Declarar lacunas é um compromisso cultural, e é a parte que se degrada primeiro.

A tentação com um tipo não exercitado é o silêncio. O corpus não tem exemplos, nada falhou, o relatório está verde. Todo incentivo aponta para deixar quem lê presumir que a cobertura foi completa — e leitores presumem isso, porque um relatório que não menciona lacunas se lê como um relatório sem lacunas.

Tornar a declaração obrigatória muda o que o artefato é. Ele deixa de ser evidência de que o port funciona e se torna uma descrição honesta do que foi estabelecido: *estes nove tipos estão provados contra dados reais, estes dois não têm exemplos no corpus, este está não implementado.* Quem revisa pode agir sobre isso. Pode decidir que os dois tipos não exercitados são risco aceitável, ou ir procurar exemplos, ou barrar a release. O que não pode fazer é agir sobre um selo verde que silenciosamente significa "não verificamos".

O critério companheiro torna a lacuna consequente em vez de meramente visível: a cobertura por tipo **barra** o port. Um tipo não exercitado não é uma nota num relatório, é uma release bloqueada. É isso que impede a declaração de lacunas de se tornar uma formalidade que todos rolam para baixo.

## Delimite a alegação ao que você de fato provou

A decisão que quero destacar é a que soa como fraqueza:

> **D-6** — O replay cobre o núcleo determinístico, não o handler inteiro.

Um handler de notificação faz várias coisas: autentica o payload, decodifica, decide o que mudou, escreve, e emite efeitos colaterais. Só parte disso é determinística dada a entrada. Escrever depende do estado atual do banco. Efeitos colaterais tocam terceiros. Timestamps e ids gerados diferem por execução.

Poderíamos ter construído um harness que reexecuta o handler inteiro e normaliza tudo que é não determinístico. Isso é muita maquinaria, e cada normalização é um lugar onde o harness silenciosamente para de testar o que alega testar — você acaba mascarando uma diferença real porque ela pareceu um timestamp.

Em vez disso o replay cobre o núcleo de decodificar-e-decidir, onde a complexidade herdada vive e onde uma reescrita tem mais chance de diferir. O resto é coberto por testes comuns. E crucialmente, o escopo está escrito como uma decisão, então a alegação publicada é "o núcleo determinístico concorda sobre o corpus" em vez da alegação mais forte que ninguém conseguiria defender.

Esse é o mesmo instinto de declarar lacunas, aplicado à própria fronteira do harness. **Uma alegação mais estreita que você consegue defender vence uma mais ampla que você não consegue.** Quem lê `D-6` sabe exatamente o que foi e o que não foi estabelecido, e se achar que a fronteira está errada pode discutir — o que não pode fazer com uma premissa não declarada.

## Por que cobertura pertence aos critérios, não à documentação

Nada disso é novo como ideia. Todo engenheiro concorda com "cobertura importa". Ela se perde de qualquer forma, e acho que a razão é estrutural: cobertura não é entregável de ninguém.

Concordância tem um dono natural — é a coisa que o harness existe para produzir, e uma discordância é uma tarefa. Cobertura não tem dono. É uma propriedade do corpus, que ninguém escolheu, e só pode falhar em silêncio. Então a forma de mantê-la é torná-la um critério com um portão atrás, e nesse momento ela adquire um dono à força.

O padrão geral, que aparece em três lugares diferentes neste projeto: **mantenha os dois números separados e barre em ambos.** Critérios que têm testes versus critérios cujos testes passaram. Tarefas commitadas versus tarefas registradas. Casos comparados versus casos concordados. Cada vez, colapsar o par numa única figura reconfortante é o que deixa trabalho não verificado parecer terminado.

## O que eu levaria disso

**Reporte cobertura por categoria, nunca em agregado.** Cobertura ponderada por volume é dominada pelo caso comum, que é o caso que você menos precisa verificar.

**Torne a declaração de lacunas obrigatória e coloque um portão atrás.** Um relatório que não menciona lacunas é lido como um relatório sem lacunas, e um branch não exercitado sem consequência anexada vai permanecer não exercitado.

**Escreva o que seu harness não cobre.** Uma fronteira declarada pode ser discutida. Uma presumida é descoberta durante um incidente.

**Prefira a alegação mais estreita e defensável.** "O núcleo determinístico concorda sobre o corpus" vale mais que "o handler está verificado", porque a primeira é verdadeira.

---

*Parte de [A Extração de Assinaturas](/pt/posts/the-subscriptions-extraction-a-reading-order), dezessete posts sobre extrair a metade de assinaturas de um monolito Django para um serviço em Go.*
