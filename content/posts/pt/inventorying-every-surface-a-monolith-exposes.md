---
title: "Inventariando Toda Superfície Que um Monolito Expõe"
date: "2026-07-16"
description: "Você não pode reconstruir um serviço até saber exatamente o que ele promete ao mundo externo. Sobre derivar essa lista do código em vez da memória — rotas REST, aplicações implantadas, canais de tempo real, disparos de notificação — e tornar o inventário um artefato que testes garantem, em vez de um documento que apodrece."
tags:
  [
    "arquitetura",
    "microsservicos",
    "api",
    "contratos",
    "backend",
    "extracao-de-assinaturas",
  ]
---

A primeira pergunta honesta ao extrair um serviço de um monolito não é *como o novo serviço deveria ser*. É *o que o antigo promete hoje?*

Todo mundo pensa que sabe. Na prática, o que as pessoas sabem são as superfícies em que elas pessoalmente trabalham, e a união disso não é o conjunto completo. As partes de que ninguém lembra são exatamente as partes que quebram um consumidor no cutover — um job agendado, um evento que ninguém mais consome exceto uma versão mobile de dezoito meses atrás, uma rota REST criada para uma integração de parceiro que ainda recebe tráfego.

Então escrevemos o inventário como uma feature, com critérios de aceite, antes de projetar qualquer coisa.

## Quatro tipos de superfície, não um

O erro que eu teria cometido sem ajuda é tratar "a API" como a interface. Não é. Os critérios enumeram quatro categorias separadas, e cada uma foi encontrada por um método diferente:

> **AC-022** — Toda rota REST do app está no inventário.
> **AC-023** — Toda aplicação de API implantada está no inventário.
> **AC-024** — Todo canal de tempo real está no inventário.
> **AC-025** — Todo disparo de notificação está no inventário.

Rotas REST são a parte fácil — estão num router, enumeráveis percorrendo o código. **Aplicações implantadas** é o critério que nos salvou: o monolito não é um único deployable, e uma reconstrução que reproduz as rotas da API principal mas esquece um segundo app implantado é uma reconstrução que quebra só em produção.

**Canais de tempo real** e **disparos de notificação** são os esquecidos em toda migração que já vi, porque não são request/response. Ninguém os chama, então ninguém nota a ausência até um usuário não receber uma push, que é um bug report que chega dias depois e é quase impossível de rastrear até um cutover.

## Dois critérios que mantêm um inventário honesto

Todo inventário divergesse. Estes dois são a razão pela qual vale manter este:

> **AC-026** — O inventário não afirma nada que o código não tenha.

A direção dessa frase importa. É fácil verificar que tudo no código aparece no inventário — isso é completude, e é o que você escreveria primeiro. Este é o *outro* sentido: nada no inventário pode ser aspiracional. Um documento listando uma rota que foi apagada no ano passado é pior que um incompleto, porque faz a reconstrução reproduzir algo que não existe mais.

As duas direções são garantidas por testes, o que é o que torna isso um artefato em vez de um documento. Uma rota nova adicionada amanhã sem entrada no inventário falha a verificação.

> **AC-029** — As contagens do relatório batem com o inventário.

Isso parece burocracia e não é. O relatório é o que as pessoas leem; o inventário são os dados. No momento em que um humano escreve "expomos cerca de quarenta endpoints" num resumo, esse número começa a divergir da lista. Derivar a contagem e afirmá-la significa que o resumo não pode ficar errado em silêncio.

## As duas perguntas que transformam uma lista em um plano

Um inventário por si só é apenas uma lista. Dois critérios adicionais o tornam útil para decisão:

> **AC-027** — Toda superfície nomeia os consumidores que dependem dela.
> **AC-028** — Toda superfície declara se a reconstrução precisa reproduzi-la.

O primeiro é o que permite sequenciar um cutover. Se você sabe quais superfícies o app mobile depende versus quais só um job interno toca, você pode mover as internas primeiro e aprender nas superfícies onde um erro é barato.

O segundo é o que encolhe o trabalho. Não toda superfície precisa ser reproduzida. Algumas existem para um cliente que não é mais publicado. Algumas foram feitas para um experimento. Escrever "precisa reproduzir: não" numa superfície, com uma razão nomeada, converte uma discussão que aconteceria durante a migração numa decisão tomada enquanto ninguém está sob pressão.

E é uma distinção genuinamente estrutural. O tamanho de uma reconstrução não é o tamanho do sistema antigo — é o tamanho da parte que ainda tem consumidores.

## O relatório é agrupado por consumidor, não por superfície

Uma pequena escolha estrutural com valor desproporcional:

> **AC-030** — O relatório agrupa as superfícies obrigatórias por consumidor.

A forma natural de apresentar um inventário é por tipo de superfície: aqui estão as rotas, aqui os eventos, aqui os jobs. Esse é o formato certo para os dados e errado para o leitor, porque a pergunta de ninguém é "quais rotas existem". A pergunta é "se eu sou o time mobile, o que muda para mim?"

Agrupado por consumidor, o relatório responde isso diretamente, e um plano de cutover praticamente cai dele — cada consumidor é uma conversa, e cada conversa tem uma lista.

## O que eu levaria disso

**Enumere superfícies por tipo, a partir do código.** Rotas, deployables, canais de tempo real, trabalho agendado, eventos publicados. Quatro buscas diferentes, porque vivem em quatro lugares diferentes e nenhum grep único encontra todos.

**Afirme as duas direções.** Tudo no código está no inventário; nada no inventário está ausente do código. A segunda metade é a que o mantém verdadeiro um ano depois.

**Decida "precisa reproduzir" por superfície, antecipadamente.** É aqui que o escopo de uma reconstrução é de fato definido, e fazer isso antes da migração significa fazer com calma.

**Agrupe o resumo por consumidor.** Os dados querem ser organizados por tipo de superfície. O leitor quer saber o que quebra para ele.

A coisa toda levou uma fração do tempo da extração, e foi o artefato ao qual eu mais recorri — incluindo duas vezes para responder "alguma coisa ainda usa isso?" com uma citação em vez de um chute.

---

*Parte de [A Extração de Assinaturas](/pt/posts/the-subscriptions-extraction-a-reading-order), dezessete posts sobre extrair a metade de assinaturas de um monolito Django para um serviço em Go.*
