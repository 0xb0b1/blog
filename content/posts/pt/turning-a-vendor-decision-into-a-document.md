---
title: "Um Spike de Migração Deveria Produzir uma Lista de Perdas, Não uma Recomendação"
date: "2026-07-28"
description: "O resultado de uma avaliação não deveria ser um veredito em que alguém tem que confiar — deveria ser um documento com o qual a pessoa possa discordar. Sobre ranquear lacunas por impacto em vez de tabelar features, estabelecer o tier mínimo por capacidade contra o catálogo que você de fato atende, e tornar o custo do \"sim\" explícito."
tags:
  [
    "arquitetura",
    "tomada-de-decisao",
    "documentacao",
    "avaliacao-de-fornecedor",
    "migracao-de-provedor",
  ]
---

Três spikes saíram da avaliação de se poderíamos substituir um provedor de dados esportivos. O primeiro inventariou o que consumimos e verificou se a alternativa poderia fornecer. O segundo descobriu quais tiers de preço eram de fato necessários. O terceiro fez a pergunta direta: se fôssemos só com o provedor, o que perderíamos?

Esse terceiro é o formato interessante, e é o que eu repetiria. O entregável dele não é uma recomendação. É uma **lista de perdas**.

## Lacunas ranqueadas por impacto, não tabeladas por feature

Uma matriz de features tem uma propriedade reconfortante: parece completa. Quatorze linhas, duas colunas, vistos e xis. E é quase inútil para decidir, porque trata cada linha como igualmente importante.

O critério que usamos em vez disso:

> **US-003** — Ver as lacunas ranqueadas, para que a decisão seja informada.

Ranqueadas por impacto. Uma capacidade ausente da qual um relatório interno depende e uma capacidade ausente da qual a tela de partida ao vivo depende não são a mesma linha, e uma matriz que as renderiza de forma idêntica descartou a única informação que importa.

Ranquear força um julgamento que a matriz te deixa evitar. Você tem que dizer *esta lacuna seria visível para usuários um minuto depois de a partida começar* e *esta seria notada por duas pessoas no fechamento do mês*. Essas frases são discutíveis de um jeito que um xis numa célula não é — e discutível é o ponto, porque quem discorda pode dizer especificamente.

## Tiers, contra o catálogo que você de fato atende

O segundo spike é mais estreito e é o que teria sido pulado:

> **US-004** — Saber quais tiers fornecem cada capacidade.
> **US-005** — Ver os tiers em que o produto da R10 de fato funciona.

Preço de fornecedor é em tiers, e a disponibilidade de capacidades difere por tier. Então "eles conseguem fornecer isso?" é incompleto — a pergunta real é "em qual tier, e o produto funciona nesse tier para as competições que atendemos?"

Essa segunda cláusula é onde a análise quase deu errado. Um tier pode tecnicamente fornecer uma capacidade cobrindo apenas as poucas ligas do topo. Se seu produto atende uma cauda longa de competições, uma capacidade disponível para 5% do seu catálogo não está disponível. Estabelecer o tier mínimo *por capacidade*, e então verificar contra o **catálogo real de competições** em vez de uma amostra representativa, é o que transformou uma resposta plausível numa defensável.

Existe um detalhe de documentação que eu gosto aqui: essa régua foi depois **formalmente substituída** pelo spike de go/no-go, em vez de silenciosamente trocada. O documento anterior permanece, marcado como substituído pelo mais novo. Quem encontrar a régua sabe que existe uma análise posterior; quem ler a mais nova pode ver o que ela revisou. Mesmo instinto de riscar um registro de decisão em vez de apagá-lo.

## A lista de perdas é o entregável

Aqui está a parte pela qual eu argumentaria com mais força. A coisa mais forte que uma avaliação pode produzir não é "recomendamos X". É uma lista explícita do que dizer sim custa.

Toda decisão de migração tem perdas. Alguma capacidade é pior, alguma latência é maior, algum dado não chega mais, algum fluxo precisa de contorno. Um documento que apresenta só benefícios e uma recomendação não é uma análise, é defesa de causa — e todo mundo que lê sabe disso, que é por que esses documentos geram desconfiança em vez de concordância.

Escrever as perdas faz três coisas.

**Torna a decisão revisável.** Quem lê pode olhar a lista e dizer "conseguimos viver sem essas três, mas não sem aquela". Essa é uma conversa real, e é muito mais rápida que uma em que a pessoa tem que fazer engenharia reversa dos custos a partir do seu entusiasmo.

**Sobrevive à decisão.** Seis meses depois, quando alguém bate numa das perdas, a pergunta é "isso era conhecido?" Uma lista de perdas responde sim, com data. Sem ela, um tradeoff conhecido se torna um aparente descuido, e a confiança na análise se esvai exatamente quando você precisa dela.

**Disciplina quem analisa.** É desconfortável escrever, que é o sinal de que é a parte útil. Se um spike não produz perdas, ou a migração é gratuita — e não é — ou a análise parou cedo.

## Recomende de qualquer forma

Nada disso significa ser neutro. Já escrevi que um relatório deveria nomear uma recomendação e seu risco principal, e isso se aplica aqui também.

Um documento que expõe lacunas, tiers e perdas e então se recusa a concluir devolveu a decisão para uma reunião. Se você fez o trabalho, você tem uma opinião, e retê-la não é rigor — é aversão a risco vestida de objetividade.

A combinação é o que funciona: **uma recomendação, seu risco principal, e a lista completa de perdas.** A recomendação dá a quem lê um lugar para começar; o risco diz onde atacá-la; a lista de perdas diz o que está sendo comprado. Os três, e quem revisa consegue engajar de verdade. Faltando qualquer um, a pessoa está ou confiando em você ou te ignorando.

## O que eu levaria disso

**Ranqueie lacunas por impacto; nunca entregue uma matriz de features nua.** A matriz parece completa e descarta a única distinção que decide qualquer coisa.

**Verifique capacidade contra o catálogo real, não uma amostra.** "Disponível" com 5% de cobertura não está disponível, e uma amostra representativa esconde exatamente isso.

**Substitua documentos formalmente.** Uma análise marcada como substituída é navegável. Uma trocada em silêncio deixa alguém agindo sobre conclusões velhas.

**Faça da lista de perdas o entregável principal.** É a metade desconfortável, é o que torna o documento confiável, e é a parte que ainda vale um ano depois quando alguém pergunta se isso era conhecido.

**Então recomende.** Com um risco nomeado. Análise que se recusa a concluir terceirizou a decisão para quem fala mais na reunião.

---

*Parte de uma linha de dois posts sobre substituir um provedor de dados esportivos — ao lado de [A Extração de Assinaturas](/pt/posts/the-subscriptions-extraction-a-reading-order), o projeto que corria em paralelo.*
