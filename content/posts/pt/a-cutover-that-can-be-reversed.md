---
title: "Um Cutover Que Pode Ser Revertido"
date: "2026-07-19"
description: "Reversibilidade como critério de aceite em vez de um plano de rollback escrito na noite anterior. Sobre fasear uma extração para que cada passo possa ser desfeito, por que \"a gente reverte o deploy\" deixa de ser verdade no momento em que dados se movem, e o critério de que o que fica para trás precisa ser tão explícito quanto o que se move."
tags:
  [
    "arquitetura",
    "deploy",
    "migracao",
    "risco",
    "backend",
  ]
---

Todo plano de migração tem uma seção de rollback. A maioria delas diz alguma versão de "reverta o deploy e roteie o tráfego de volta".

Isso é verdade para um serviço sem estado e falso para quase todo o resto. No momento em que o novo sistema aceitou uma escrita que o antigo não conhece, reverter o deploy não restaura o estado anterior — restaura o *código* anterior, apontado para dados que seguiram adiante sem ele. Você está agora num estado para o qual nenhum dos sistemas foi projetado, no pior momento possível.

Então transformamos isso num critério:

> **AC-020** — O cutover é faseado, reversível, e cobre os dados.

Três requisitos numa frase, e o terceiro é o que faz o trabalho.

## Faseado, para que cada passo seja pequeno o suficiente para desfazer

Reversibilidade não é uma propriedade que você adiciona no fim. É uma propriedade de como você corta o trabalho, e precisa ser decidida antes de a primeira fase ir ao ar.

A nossa foi: subir o serviço apenas lendo; provar que suas respostas batem com o sistema antigo offline; então mover um caminho de escrita. Cada fase é independentemente reversível porque cada uma deixa o sistema anterior totalmente autoritativo. Desligar um serviço que só leu é grátis. Desligar um serviço que foi o único escritor por uma semana é um exercício de recuperação de dados.

A regra geral que eu extrairia: **ordene as fases pelo custo da reversão, mais barata primeiro.** Essa ordenação não é automática — a ordem tentadora é por quão interessante o trabalho é, ou por conveniência de dependências, e as duas colocam o passo caro-de-reverter mais cedo do que precisa ser.

Existe um bônus não óbvio de antemão. Fasear assim faz as fases iniciais produzirem evidência em vez de risco. Shadow reads nos disseram que a nova lógica de entitlement concordava com a antiga em um quarto de milhão de compras reais antes de servir um único usuário. Essa evidência só estava disponível porque a fase foi projetada para ser descartável.

## "Cobre os dados" é a parte difícil

Essa é a cláusula que separa um plano de rollback real de um parágrafo.

Para cada fase, três perguntas:

**Que dados esta fase cria ou altera?** Se a resposta é "nenhum", a reversão é um deploy. Se for qualquer outra coisa, continue.

**Se revertermos, o que acontece com esses dados?** Descartar? Migrar de volta? Deixar e reconciliar depois? As três são respostas legítimas; não ter uma não é.

**O sistema antigo tolera dados que o novo escreveu?** Essa é a pergunta que as pessoas perdem. Reverter para o código antigo significa reverter para as premissas do código antigo — sobre schema, sobre quais estados são válidos, sobre quem escreve qual coluna. Se o novo serviço introduziu um formato de linha que o antigo rejeita, então reverter o deploy entrega ao seu código antigo um banco contra o qual ele vai falhar.

Escrever isso por fase é sem glamour e é de onde o plano de verdade vem. Duas vezes mudou a fronteira da fase: acabou sendo mais barato mover um pedaço de schema num passo próprio do que ter uma fase cuja reversão exigia uma migração de dados.

## O que fica para trás, declarado tão explicitamente quanto o que se move

> **AC-013** — O que fica para trás é tão explícito quanto o que se move.

Numa primeira leitura isso pertence ao trabalho de inventário, não ao cutover. Está aqui por causa do que uma extração parcial te deixa.

Uma extração produz dois sistemas, e os bugs interessantes vivem naquilo que nenhum dos dois reivindica. Se o plano enumera o que se move e trata o restante como "todo o resto", o restante é indefinido — e indefinido significa que cada engenheiro desenha a linha onde a própria mudança dele por acaso precisa. É assim que você acaba com dois serviços escrevendo a mesma tabela, cada um acreditando que o outro parou.

Escrever o restante explicitamente também torna a reversão mais barata, porque você sabe exatamente do que o sistema antigo continua responsável. "Reverta para o sistema antigo" só é uma instrução significativa se você consegue dizer o que o sistema antigo ainda possui.

## Infraestrutura escrita, aplicada deliberadamente

Uma escolha de implementação caiu disso, e eu gosto dela mais quanto mais penso: a primeira fase do novo serviço incluiu seu Terraform, **escrito e validado mas não aplicado**.

Isso soa como meia medida. É o oposto. A definição de infraestrutura é revisada junto do código que precisa dela, quando o contexto está fresco — mas nada existe ainda, então não há nada a reverter. Aplicar é um ato separado e deliberado, tomado quando a fase que precisa dela está de fato começando.

A alternativa que eu já fiz antes é aplicar infraestrutura cedo "para estar pronta", o que silenciosamente significa que a superfície de reversão da fase um agora inclui recursos de nuvem, e a fase que você achava grátis não é.

## A parte que não é reversível

Honestidade exige um limite. Algumas coisas genuinamente não podem ser desfeitas, e o plano deveria dizer quais.

Uma vez que você deu acknowledgement a uma notificação de loja, você não pode retirá-lo — a loja se considera encerrada. Uma vez que você disse a um provedor de pagamento que aceitou uma compra, isso é um compromisso com um terceiro. Nenhum design de fases torna essas coisas reversíveis; o máximo que você pode fazer é torná-las *tardias*, para que os passos irreversíveis fiquem atrás de tanto comportamento comprovado quanto possível.

O que é outra forma de enunciar a regra de ordenação: passos irreversíveis vão por último, depois de a evidência ter acumulado. E é uma razão para desconfiar de qualquer plano cuja primeira fase toca um terceiro.

## O que eu levaria disso

**Faça da reversibilidade um critério, não uma seção.** Um critério é verificado por fase. Uma seção é escrita uma vez, no fim, por quem estiver mais otimista.

**Ordene fases pelo custo da reversão, mais barata primeiro.** As ordens tentadoras — por interesse, por dependência — ambas antecipam as caras.

**Responda "o que acontece com os dados" por fase, escrito.** Essa é a diferença inteira entre um plano de rollback e um parágrafo de rollback, e vai mover suas fronteiras de fase.

**Declare o que fica para trás.** O restante indefinido é onde dois sistemas acabam escrevendo a mesma linha.

**Nomeie os passos irreversíveis e coloque-os no fim.** Você não pode consertá-los; pode garantir que aconteçam depois de você ter aprendido tudo o que é mais barato.
