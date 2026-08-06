---
title: "Uma Loja Nunca Espera Pelo Nosso Banco de Dados"
date: "2026-07-24"
description: "App Store e Google Play fazem retry agressivamente quando você está lento, então um handler que escreve no Postgres antes de dar acknowledgement transforma uma query lenta numa tempestade de retentativas. Sobre separar durabilidade de processamento, o registro de decisão que tivemos que substituir ao descobrir o que \"durável\" precisava significar, e por que nada pode receber ack sem estar registrado."
tags:
  [
    "arquitetura",
    "aws",
    "sqs",
    "confiabilidade",
    "backend",
  ]
---

O monolito processava notificações de loja de forma inline. Um webhook chega, o handler decodifica, busca a compra, escreve o novo estado, emite o que vem depois, e *então* retorna 200.

Isso funciona até o banco ficar lento. Então o cliente da loja dá timeout, e como App Store e Google Play fazem retry em timeout, você recebe a mesma notificação de novo — enquanto a primeira ainda está rodando. Uma query lenta se torna trabalho duplicado, o que deixa o banco mais lento, o que produz mais retries. A falha se autoamplifica, e o gatilho pode ser algo inteiramente não relacionado a assinaturas.

Então o primeiro critério para o novo caminho de ingestão era sobre quem espera por quem:

> **AC-041** — O acknowledgement é rápido e não depende do processamento.

## A decisão que tivemos que substituir

Nosso primeiro registro de decisão dizia, aproximadamente, *seja durável antes de dar ack*. Obviamente correto, e acabou sendo subespecificado de um jeito que importava. Durável *onde?*

A leitura com que começamos foi: escreva a notificação no nosso próprio banco, então dê ack. Isso é durável, e ainda acopla o acknowledgement ao Postgres — exatamente o acoplamento que estávamos tentando remover. É uma escrita menor que o processamento completo, então a janela diminui, mas um banco lento ou indisponível ainda significa acks lentos ou falhos.

O registro de design mostra a correção em vez de escondê-la:

```
~~D-1~~ — Durabilidade antes do acknowledgement — SUBSTITUÍDO por D-1a
D-1a  — O SQS é o registro durável
```

A fila é o registro durável. O handler autentica o payload, coloca no SQS, e dá ack. Nada no caminho do acknowledgement toca nosso banco. O processamento acontece depois, a partir da fila, no ritmo que o banco conseguir sustentar — e se o banco estiver fora, a fila cresce e nada é perdido.

Deixei o `D-1` riscado em vez de apagá-lo, e faria de novo. A versão substituída registra que consideramos uma forma mais fraca de durabilidade e diz por que ela não bastou. Quem chegar depois com "por que não escrever primeiro no banco?" recebe uma resposta em vez de repetir a análise.

## A outra metade da regra

Acknowledgement rápido, tomado isoladamente, é como você perde dados. Então ele vem em par:

> **AC-043** — Nada recebe acknowledgement que não tenha sido durávelmente registrado.

As duas direções são estruturais. O primeiro critério diz *não faça a loja esperar pelo processamento*. O segundo diz *não diga à loja que você tem, a menos que você realmente tenha*. Sem o segundo, "dê ack rápido" degrada para "dê ack imediatamente e reze", e um enqueue falho se torna uma notificação que não existe mais em lugar nenhum — a loja se considera encerrada, e você não tem registro de que ela chegou.

Essa combinação é o que torna a fila não opcional em vez de uma otimização. O acknowledgement é uma promessa, e o enqueue é o que torna a promessa verdadeira.

## Autentique antes de fazer qualquer trabalho

> **AC-042** — Uma notificação não autenticada é recusada antes de qualquer trabalho.

A ordenação é o critério. Autenticação acontece primeiro, antes de decodificar, antes do enqueue, antes de qualquer coisa ser alocada em nome da requisição.

Um endpoint público de webhook é uma capacidade gratuita oferecida à internet. Se um payload não autenticado chega ao ponto de ser decodificado e enfileirado, então qualquer um que encontre a URL pode encher sua fila sem pagar nada. Recusar antes do trabalho transforma isso numa rejeição barata.

Existe uma interação com o critério acima que vale notar: uma requisição não autenticada é recusada, não enfileirada, então "nada recebe ack sem estar registrado" não te compromete acidentalmente a armazenar lixo.

## Falha precisa ser visível

> **AC-050** — Falha repetida cai na dead-letter queue e dispara um alarme.

A dead-letter queue e o alarme são especificados na *mesma tarefa* que a própria fila, deliberadamente. Uma DLQ sem alarme é um lugar onde mensagens vão para ser esquecidas em silêncio — discutivelmente pior que nenhuma DLQ, porque a fila parece saudável enquanto notificações acumulam em algum lugar para o qual ninguém tem dashboard.

O pareamento é o ponto. Se uma mensagem pode ser posta de lado, algo precisa anunciar que isso aconteceu.

> **AC-051** — Uma loja sem processador é estacionada visivelmente, não descartada.

Este vem do rollout faseado. Implementamos App Store e Google Play; Stripe estava planejado mas não construído. Os comportamentos tentadores são ambos ruins: uma notificação de uma loja não implementada é ou silenciosamente descartada (perda de dados, invisível) ou derruba o handler (tempestade de retries, de uma loja que vai continuar tentando).

Estacionada visivelmente significa que é registrada, não processada, e exposta. Quando Stripe chegar, existe uma fila de notificações reais para reexecutar por ela — e nesse meio-tempo ninguém está adivinhando se tráfego de Stripe está chegando.

## O que eu levaria disso

**Latência de acknowledgement é um acoplamento.** O que o ack espera se torna algo para o qual a política de retry do seu provedor de webhook está apontada. Faça a lista do que ele espera tão curta quanto possível — idealmente um único append durável.

**"Seja durável" é subespecificado até você nomear onde.** Nosso primeiro registro de decisão era correto e inútil pelo mesmo motivo.

**Ack rápido e nunca-dar-ack-sem-registrar são uma regra em duas metades.** Qualquer uma sozinha é um bug: a primeira perde dados, a segunda perde a garantia de latência.

**Especifique a DLQ e seu alarme juntos.** Uma dead-letter queue sobre a qual ninguém é avisado é um mecanismo silencioso de perda de dados vestido de confiabilidade.

**Substitua decisões no lugar em vez de apagá-las.** A opção rejeitada é a forma mais barata de impedir que o mesmo debate volte.
