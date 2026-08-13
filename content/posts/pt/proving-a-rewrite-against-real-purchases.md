---
title: "Provando uma Reescrita Contra 243.325 Compras Reais"
date: "2026-07-22"
description: "Antes de o novo serviço responder a um único usuário, ele computou entitlement para toda compra que tínhamos e suas respostas foram comparadas offline contra as do monolito. Zero discordâncias. A engenharia está inteiramente no que \"zero\" exigiu: fixar o relógio, escrever a lógica duas vezes de propósito, e tornar toda discordância diagnosticável sem reexecutar."
tags:
  [
    "go",
    "testes",
    "migracao",
    "arquitetura",
    "backend",
    "extracao-de-assinaturas",
  ]
---

A parte mais arriscada de reescrever um pedaço de um sistema não é o código novo. É que ninguém consegue te dizer se o código novo concorda com o antigo, porque o comportamento do antigo só é definido por aquilo que ele faz.

Entitlement — se um dado usuário tem acesso agora — é o pior tipo de lógica para herdar. É um predicado sobre estado de compra, estado de loja, datas, períodos de carência e uma década de casos especiais que eram cada um individualmente razoável. Não há especificação. A especificação *é* o Python.

Então antes de o serviço Go servir qualquer coisa, rodamos as duas implementações sobre o corpus inteiro e comparamos:

> **AC-032** — Go e Python concordam sobre entitlement em todo o corpus.

243.325 compras. Zero discordâncias. Esse número é a parte menos interessante deste post.

## O corpus inteiro, não uma amostra

A primeira decisão que importou: comparar tudo, não uma janela de tráfego recente.

Uma semana de tráfego é uma amostra enviesada exatamente do jeito que machuca aqui. Compras recentes estão desproporcionalmente em estados comuns — ativas, renovadas recentemente. Os casos em que duas implementações divergem são os estranhos: uma assinatura que expirou durante uma retentativa de cobrança, uma compra reconciliada de outra loja, algo que passou por uma migração três anos atrás. Esses são raros por dia e abundantes por corpus.

"Corpus inteiro" também elimina uma discussão. Ninguém pode perguntar se a amostra era representativa.

## Fixar o relógio

Entitlement é dependente de tempo, o que significa que a comparação tem um bug embutido a menos que você trate isso explicitamente: rode o Go às 14:02 e o Python às 14:03 e uma assinatura pode expirar entre os dois. Você recebe uma discordância que não é uma discordância, e vai procurar um bug de lógica que não existe.

Pior, é não determinístico. Reexecute e a discordância se move.

O design tem uma seção chamada *"Fixando o relógio"* exatamente para isso: as duas implementações recebem o mesmo instante de avaliação, injetado em vez de lido do sistema. Um efeito colateral é que a comparação se torna reproduzível — o mesmo corpus e o mesmo instante fixado dão a mesma resposta para sempre, que é o que torna um resultado que você pode colocar num documento.

Qualquer harness de comparação sobre lógica dependente de tempo precisa disso, e é o tipo de coisa que é óbvia em retrospecto e invisível de antemão.

## Offline, e incapaz de tocar em nada

> **AC-049** — A comparação não pode mutar as tabelas compartilhadas.

O harness lê um snapshot. Não tem caminho de escrita, e na fase posterior em que o corpus cresce para incluir replay de notificações, ele semeia um banco efêmero em vez de tocar a conta viva.

Isso não é paranoia sobre o código estar errado. É sobre o que o harness *é*: uma coisa que você vai querer rodar vinte vezes enquanto caça discordâncias, na hora que estiver caçando. Uma ferramenta que poderia mutar produção é uma ferramenta que você vai hesitar em rodar, e uma comparação que você hesita em rodar é uma que você vai rodar menos do que deveria.

Torne a coisa arriscada estruturalmente impossível e você ganha o direito de ser descuidado com a ferramenta, que é o ponto da ferramenta.

## Escrevendo a lógica duas vezes, deliberadamente

O harness tem um oráculo em Python ao lado da implementação em Go. Isso parece duplicação e é o mecanismo inteiro.

O sistema antigo é Python. Se o harness comparasse Go contra uma *reimplementação* das regras, estaria comparando minha leitura das regras contra minha outra leitura das regras — um teste da minha consistência, não de equivalência comportamental. Rodar o código original como oráculo significa que a coisa com que se concorda é a coisa que está em produção hoje, casos especiais e tudo.

A consequência a aceitar: o harness é tão bom quanto a fidelidade do oráculo, e o oráculo precisa ser o caminho de código real, não uma cópia que desde então divergiu.

## Cobertura é um critério separado de concordância

> **AC-034** — Período de carência e a regra de loja reconciliada são exercitados, não presumidos.

Este é o critério que eu lutaria para manter se só pudesse manter um. "Zero discordâncias" é compatível com "nunca fizemos a pergunta interessante". Um corpus em que 99% das linhas estão claramente ativas te diz que as duas implementações concordam sobre linhas claramente ativas.

Então regras específicas são nomeadas e o exercício delas é afirmado. Período de carência — a janela em que uma loja está retentando uma cobrança mas ainda concedendo acesso — é o caso que já nos havia queimado em produção, e é exatamente o tipo de branch que um corpus pode sub-representar. Nomeá-lo significa que o harness reporta quantas linhas de fato tomaram aquele caminho, e uma execução em que a resposta é zero é uma execução falha, não uma limpa.

Já escrevi que "tem teste" e "o teste passou" precisam permanecer números separados. Esta é a mesma ideia um nível acima: **concordância e cobertura são números separados**, e um harness que reporta só o primeiro está se elogiando.

## Diagnosticável sem reexecutar

> **AC-033** — Toda discordância é diagnosticável sem reexecutar.

Quando os dois discordam, a saída precisa conter o suficiente para entender por quê — as entradas, as duas respostas, o instante fixado, o branch tomado. Não uma contagem. Não "17 divergências".

A razão é operacional. Uma execução sobre o corpus inteiro não é instantânea, e um harness que te diz *que* algo discordou mas não *o quê* força um ciclo: adicione logging, reexecute, espere, olhe. Faça isso três vezes e o laço custou mais que escrever os diagnósticos custaria.

Existe um benefício mais subtil. Um relatório de discordância rico o suficiente para diagnosticar é também rico o suficiente para anexar a uma decisão — você pode colocá-lo na frente de alguém e perguntar "qual dessas duas respostas está correta?", que é frequentemente uma pergunta de produto e não de engenharia.

## O que eu levaria disso

**Compare contra a implementação em execução, sobre o corpus completo.** Uma amostra é enviesada para os casos comuns, que são os que você não precisa verificar.

**Fixe o relógio.** Qualquer comparação dependente de tempo tem um gerador não determinístico de falsos positivos dentro dela até você injetar o instante.

**Torne o harness estruturalmente incapaz de escrever.** Você quer rodá-lo sem pensar; isso exige que ele seja seguro sem pensar.

**Afirme cobertura separadamente de concordância, e nomeie as regras que precisam ser exercitadas.** Zero discordâncias sobre um corpus que nunca atingiu o branch interessante não é evidência.

**Gaste o esforço no relatório de discordância, não no resumo.** A contagem te diz se deve olhar. O relatório é como você olha.

O resultado foi uma reescrita que subiu tendo já respondido um quarto de milhão de perguntas reais de forma idêntica ao sistema que substituiu. Essa é uma posição muito melhor que uma suíte de testes, e custou menos do que a suíte custaria — porque o oráculo já estava escrito, e estava em produção havia anos.

---

*Parte de [A Extração de Assinaturas](/pt/posts/the-subscriptions-extraction-a-reading-order), dezessete posts sobre extrair a metade de assinaturas de um monolito Django para um serviço em Go.*
