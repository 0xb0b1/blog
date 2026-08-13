---
title: "Quantifique a Falha Antes de Redesenhá-la"
date: "2026-07-15"
description: "\"Assinar é instável\" era a premissa para extrair um serviço do nosso monolito. Antes de projetar qualquer coisa eu medi no log group de produção — e a falha não era instabilidade alguma. Era uma máquina de estados rejeitando compras que deveria ter aceitado, 3.995 vezes em sete dias, atrás de um único código de erro opaco."
tags:
  [
    "arquitetura",
    "observabilidade",
    "aws",
    "debugging",
    "backend",
    "extracao-de-assinaturas",
  ]
---

Estávamos prestes a extrair assinaturas de um monolito. A premissa, repetida em reuniões suficientes para ninguém mais questionar, era que assinar é instável — usuários relatam falhas, o suporte vê, o código é antigo.

Essa é a descrição de uma sensação, não de uma falha. Então antes de escrever qualquer design, transformei "medir" num critério de aceite próprio:

> **AC-014** — O quadro de falhas é medido, e reproduzível.

Sete dias do log group de produção depois, o quadro não tinha nada a ver com a premissa.

## O que os logs de fato diziam

Toda compra rejeitada voltava ao cliente como um único código de erro. Na janela medida:

```
WRONG_SUBSCRIPTION_STATE      app_store    subscribe    3995
SUBSCRIPTION_USERS_DO_NOT_MATCH  app_store subscribe     239
PURCHASE_ALREADY_EXISTS       google_play  subscribe       1
```

Quase quatro mil falhas, 94% delas um único código. Não uma dispersão de timeouts e resets de conexão — um branch, tomado repetidamente.

Detalhando o que a loja de fato havia nos dito nesses casos:

```
got=EXPIRED        expected=[ACTIVE]    3645
got=BILLING_RETRY  expected=[ACTIVE]     316
got=REVOKED        expected=[ACTIVE]      32
```

O código aceitava exatamente um estado de loja e rejeitava todos os outros. Esse é o bug inteiro.

## Por que isso muda o design

Leia essas três linhas como comportamento de produto, não como dados.

**`REVOKED` — 32 casos.** Rejeitado corretamente. A loja está nos dizendo que a compra foi retomada.

**`EXPIRED` — 3.645 casos.** Uma assinatura que expirou. Rejeitar a chamada de *subscribe* nesse caso está errado de um jeito interessante: esse é um cliente que está voltando, e o endpoint que ele naturalmente usaria diz a ele que algo opaco deu errado.

**`BILLING_RETRY` — 316 casos.** Esse é o que mudou minha opinião sobre o projeto inteiro. `BILLING_RETRY` significa que a loja falhou em cobrar o cartão e está tentando de novo — e durante esse período de carência **a loja ainda concede acesso ao usuário**. Então tínhamos 316 pessoas numa semana que a Apple considerava com direito de acesso, sendo recusadas por nós, com uma mensagem de erro que não dizia nada.

Nada disso é instabilidade. Não há flakiness para resolver com engenharia, nem orçamento de retry para ajustar, nem pool de conexões para consertar. É uma máquina de estados escrita contra um único caminho felizsim, e a correção é uma tabela de estados com um comportamento pretendido para cada.

O que é um trabalho completamente diferente daquele que estávamos prestes a começar.

## A medição é o artefato

O número que mais me importa desse exercício não é 3.995. É que a evidência vive no repositório:

```json
{
  "_comment": "Measured from CloudWatch Logs Insights on log group ecs/r10-rest-core-prod-backend",
  "window": { "days": 7, "endDate": "2026-08-01" },
  "logGroup": "ecs/r10-rest-core-prod-backend",
  "rejectedStoreStates": [ { "got": "EXPIRED", "expected": ["ACTIVE"], "count": 3645 }, … ]
}
```

Um arquivo versionado com o log group, a janela e a consulta registrados, em vez de um print colado num ticket. Isso importa por três razões.

É **reproduzível** — o critério diz isso, e alguém pode reexecutar no mês que vem e ver se a correção mexeu no número.

É **revisável** — um colega pode discordar da interpretação aceitando os dados, o que é uma discussão muito mais produtiva do que discordar sobre se assinar "parece instável".

E tem **data**. Daqui a seis meses, "medimos isso numa janela de 7 dias terminando em 1º de agosto de 2026" é uma frase muito mais útil do que "medimos isso".

## Classifique, depois escolha um alvo

O critério companheiro é onde o trabalho de design de fato começa:

> **AC-015** — Cada modo de falha é classificado e recebe um comportamento-alvo.

Não "corrija os erros". Para cada modo observado: isso é uma rejeição legítima, um bug, ou uma lacuna no produto? O que deveria acontecer em vez disso? Isso transforma quatro mil linhas de log numa tabela curta de decisões, e força as decisões incômodas a aparecer — `EXPIRED` numa chamada de subscribe não é tanto um bug quanto um caso de produto não tratado, e fingir que é um bug esconde o fato de que alguém tem que decidir o que reassinar significa.

Existe um correspondente mais adiante na spec que eu agora colocaria em todo design desse tipo:

> **AC-019** — Todo modo de falha conhecido tem um sinal consultável.

A razão pela qual precisamos de uma semana escavando logs é que o código original colapsava quatro resultados distintos numa única string de erro. Se cada modo tem seu próprio sinal consultável, a *próxima* pessoa a perguntar "com que frequência isso acontece?" recebe uma resposta em um minuto em vez de numa tarde.

## O que eu levaria disso

**Faça da medição um portão, não um preliminar.** Se os critérios de aceite de um design incluem "o quadro de falhas é medido e reproduzível", você não pode pular e ir direto para a parte interessante. Estávamos a uma reunião de distância de projetar um sistema distribuído para corrigir um `if`.

**Um único código de erro para múltiplas causas é uma indisponibilidade futura.** Não porque falha, mas porque torna a falha não mensurável. `WRONG_SUBSCRIPTION_STATE` é honesto e inútil: diz que o estado estava errado sem dizer qual estado, então ninguém consegue distinguir um bug de uma política de uma peculiaridade da loja.

**Versione a medição.** O número, a janela, a consulta, o log group. Não custa nada e converte uma alegação em evidência — que é a diferença entre uma revisão de design que discute interpretação e uma que discute impressões.

A extração seguiu adiante, por razões que sobreviveram à medição. Mas a primeira coisa que ela entregou foi uma tabela de estados, e isso não seria verdade se tivéssemos confiado na premissa.

---

*Parte de [A Extração de Assinaturas](/pt/posts/the-subscriptions-extraction-a-reading-order), dezessete posts sobre extrair a metade de assinaturas de um monolito Django para um serviço em Go.*
