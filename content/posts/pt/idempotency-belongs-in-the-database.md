---
title: "Idempotência Pertence ao Banco de Dados"
date: "2026-07-26"
description: "Uma notificação de loja reentregue não pode duplicar uma compra. Verificações em nível de aplicação são necessárias e insuficientes — sob entrega concorrente a única coisa que se sustenta com confiança é uma constraint. Sobre escolher a chave, a correção que tivemos que fazer nela no meio da execução, e por que o critério nomeia o mecanismo de imposição em vez do resultado."
tags:
  [
    "bancos-de-dados",
    "postgres",
    "idempotencia",
    "confiabilidade",
    "backend",
  ]
---

Lojas fazem retry. App Store e Google Play reenviam uma notificação se não recebem um acknowledgement rápido, e as duas ocasionalmente entregam o mesmo evento duas vezes por razões inteiramente próprias. Qualquer coisa que ingere webhooks vai ver duplicatas; a única pergunta é o que acontece quando vê.

Dois critérios, e o interessante é o segundo:

> **AC-044** — A mesma notificação processada duas vezes gera uma linha e um resultado.
> **AC-045** — Idempotência é imposta pelo banco, não apenas por lógica de aplicação.

O primeiro declara o resultado. O segundo declara o mecanismo, e o declara como um *piso* — "não apenas por lógica de aplicação". Essa formulação é deliberada, porque a implementação natural satisfaz o primeiro critério em teste e falha nele em produção.

## Por que a verificação na aplicação não basta

O código óbvio é:

```go
existing, err := repo.FindPurchase(ctx, storePurchaseID)
if existing != nil {
    return nil          // já processado
}
return repo.InsertPurchase(ctx, p)
```

Isso passa em qualquer teste que você escreva para ele. Envie a notificação duas vezes sequencialmente e você recebe uma linha.

Então dois workers pegam duas cópias da mesma notificação da fila ao mesmo tempo. Os dois chamam `FindPurchase`, os dois não recebem nada, os dois chamam `InsertPurchase`, e você tem duas linhas. A janela é pequena — microssegundos — e entrega duplicada é precisamente a condição que torna processamento concorrente provável, já que um retry chega enquanto o original ainda está em andamento.

A verificação não está errada. É uma otimização: evita uma tentativa de insert no caso comum. O que ela não pode fazer é fornecer uma garantia, porque entre a leitura e a escrita ela não segura nada.

Uma constraint de unicidade segura. É avaliada pelo banco no momento da escrita, sob qualquer isolamento em que você esteja, através de toda conexão e todo worker. Isso não é uma versão melhor da mesma técnica — é a única versão que é uma garantia em vez de uma probabilidade.

Então o handler mantém a busca para o caminho comum, e trata um erro de violação de unicidade como sucesso em vez de falha. Que é a forma que eu escreveria para qualquer insert idempotente: **tente, e interprete a colisão como "outra pessoa já fez isso".**

## Escolhendo a chave, e errando primeiro

A chave em que paramos é `(store, store_purchase_id)`, e a corrigimos durante a execução — o commit diz isso claramente: *correct the idempotency key to (store, store_purchase_id)*.

O instinto é chavear apenas pelo identificador de compra da loja. É o que a loja considera a identidade da coisa, é único dentro daquela loja, e parece canônico.

É único *dentro daquela loja*. Nós ingerimos de App Store, Google Play, e em breve Stripe, e não existe regra dizendo que três fornecedores independentes não vão gerar identificadores colidentes. Provavelmente não vão. "Provavelmente" é a força errada para uma constraint de unicidade, porque o modo de falha é descartar silenciosamente uma compra real de uma loja porque outra loja emitiu o mesmo id — um bug que você encontraria meses depois, por um ticket de suporte, sem forma de reconstruir o que aconteceu.

Incluir a loja faz a chave dizer o que ela significa: *esta compra, nesta loja.* A correção foi uma linha de migração. Encontrar isso depois não teria sido uma linha de nada.

A forma geral: **um identificador de um sistema externo só é único dentro daquele sistema.** Se você ingere de mais de um, a origem pertence à chave. Essa é a mesma lição das entidades chaveadas por provedor num schema compartilhado, chegando por outra direção.

## Nomeie o mecanismo no critério

O que eu mais gostaria de guardar disso é como o AC-045 está escrito.

Um critério que diz *a mesma notificação duas vezes gera uma linha* é satisfazível pelo código com bug. Alguém escreve a busca, escreve um teste que envia a notificação duas vezes em sequência, vê passar, e segue adiante — honestamente, tendo cumprido o requisito declarado.

Um critério que diz *imposta pelo banco, não apenas por lógica de aplicação* não pode ser satisfeito desse jeito. Ele nomeia o mecanismo, então a revisão tem algo concreto para verificar: existe uma constraint? Quem revisa não precisa raciocinar sobre entrelaçamento para avaliar conformidade.

Isso vai contra o conselho usual de especificar resultados em vez de implementações, e acho que a exceção é fundamentada. Onde o resultado só é observável sob concorrência, um teste não consegue demonstrá-lo com confiança e quem revisa não consegue raciocinar sobre ele facilmente. Nessa situação nomear o mecanismo é a forma honesta de tornar o requisito verificável — você não está superespecificando, está especificando a parte que é verificável.

O mesmo raciocínio aparece em outros pontos deste projeto: um pool "limitado por configuração" em vez de "cuidadoso", uma fila que "é o registro durável" em vez de "durabilidade antes do acknowledgement". Cada vez, nomear o mecanismo foi o que tornou a propriedade revisável.

## O que eu levaria disso

**Uma verificação ler-depois-escrever é uma otimização, não uma garantia.** Ela não segura nada entre as duas instruções, e entrega duplicada é exatamente a condição que faz a lacuna importar.

**Deixe a constraint ser a garantia e trate a violação como sucesso.** Tente o insert; uma violação de unicidade significa que outra pessoa já fez o trabalho.

**Coloque a origem na chave quando você ingere de múltiplos sistemas.** Os identificadores deles são únicos no espaço deles, não no seu.

**Onde a correção só é visível sob concorrência, nomeie o mecanismo no requisito.** "Imposta pelo banco" é verificável em revisão. "Gera uma linha" é verificável por um teste que passa pelo motivo errado.
