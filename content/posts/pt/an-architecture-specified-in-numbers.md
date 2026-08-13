---
title: "Uma Arquitetura Especificada em Números, Não em Adjetivos"
date: "2026-07-18"
description: "\"Escalável e confiável\" não é uma especificação — é um desejo com boa assessoria de imprensa. Sobre escrever uma arquitetura-alvo como mecanismos e orçamentos, exigir que todo design nomeie seus tradeoffs junto com suas fronteiras, e dar a todo modo de falha conhecido um sinal consultável antes de construir qualquer coisa."
tags:
  [
    "arquitetura",
    "design-de-sistemas",
    "documentacao",
    "backend",
    "extracao-de-assinaturas",
  ]
---

Documentos de design falham de forma previsível. Eles descrevem uma forma — caixas, setas, uma fila — e então afirmam propriedades: isso vai ser escalável, isso vai ser confiável, isso vai ser observável. A forma é verificável. As propriedades não são, então ninguém as verifica, e seis meses depois você descobre quais delas eram verdade.

A história de usuário que escrevemos para o design da nossa extração tentou fechar essa lacuna:

> **US-008** — Uma arquitetura-alvo especificada em números
>
> Como revisor do design, eu quero que as alegações de confiabilidade e escalabilidade sejam expressas como mecanismos e orçamentos concretos, para que "boas práticas" seja algo que possamos construir e verificar em vez de um desejo.

A expressão à qual eu sempre volto é *"algo que possamos construir e verificar em vez de um desejo."* Quatro critérios de aceite saíram dela, e cada um fecha uma saída de emergência diferente.

## Nomeie os tradeoffs, não apenas as fronteiras

> **AC-016** — A arquitetura nomeia suas fronteiras **e seus tradeoffs**.

A primeira metade é padrão: qual serviço possui o quê. A segunda metade é a que muda como o documento se lê.

Um design que só lista fronteiras está apresentando uma solução. Um design que lista o que cada fronteira *custa* está apresentando uma decisão — e uma decisão é algo com que quem revisa pode engajar. Mover assinaturas para fora significa um salto de rede extra num caminho que era uma chamada de função; significa dois deployables para lançar em ordem; significa um período em que dois sistemas podem estar ambos certos sobre a mesma linha.

Nada disso é um argumento contra a fronteira. Escrever isso é o que torna a fronteira honesta, e é o que impede que a mesma conversa aconteça de novo em três meses por alguém que notou o salto de rede.

O modo de falha que isso previne é subtil: um documento sem tradeoffs declarados se lê como se não houvesse nenhum, e a primeira pessoa a bater em um pensa que encontrou um erro em vez de um custo conhecido.

## Confiabilidade como mecanismos e orçamentos

> **AC-017** — Confiabilidade é declarada como mecanismos e orçamentos.

Duas palavras fazendo trabalhos separados.

Um **mecanismo** é uma coisa no sistema: um pool limitado, uma dead-letter queue, uma restrição de idempotência, um retry com teto. "Confiável" não é um mecanismo. "A fila é o registro durável e o acknowledgement não espera pelo processamento" é.

Um **orçamento** é um número que você pode exceder: um teto de pool, um timeout, um atraso máximo aceitável. O ponto de um orçamento não é ele ser o número certo — primeiros chutes raramente são. É que um número pode ser violado, e uma violação é detectável. "Rápido" não pode ser violado, então nunca vai falhar num teste, e portanto nunca vai ser verdadeiro ou falso.

Juntos eles tornam a alegação testável. Uma vez que o design diz *este pool tem um teto de N conexões* em vez de *o serviço é cuidadoso com carga no banco*, você pode escrever uma asserção. No nosso caso o mesmo par produziu um critério formulado como uma capacidade que o serviço precisa **não ter** — ele não pode ser capaz de esgotar as conexões do monolito — que é uma afirmação muito mais forte do que qualquer quantidade de intenção.

## O trace cobre todo salto

> **AC-018** — O design de tracing cobre todo salto.

Tracing distribuído costuma ser especificado como um componente: adicione OpenTelemetry, pronto. Este critério é sobre cobertura em vez disso — um trace que cobre três de quatro saltos é pior que nenhum trace, porque produz quadros confiantes e incompletos. Você olha uma árvore de spans, não vê o problema, e conclui que o problema está em outro lugar.

Exigir *todo salto* força a enumeração: cliente para gateway, gateway para serviço, serviço para fila, fila para worker, worker para banco. Cada um deles é um lugar onde correlação pode ser perdida, e o que falta é sempre o que você precisa às 3h da manhã.

Isso combina com uma decisão na implementação sobre a qual vou escrever separadamente — o tracing foi instrumentado desde o primeiro dia mas não emitia nada, porque nenhum coletor havia sido escolhido. O design ser explícito sobre saltos é o que tornou seguro postergar a metade operacional sem postergar o código.

## Todo modo de falha ganha um sinal consultável

> **AC-019** — Todo modo de falha conhecido tem um sinal consultável.

Este é o critério que eu adicionaria a todo documento de design que escrever de agora em diante, e ele existe por causa do que a medição de falhas havia acabado de nos ensinar.

Passamos uma semana descobrindo o que estava errado com o antigo caminho de subscribe, e a razão de ter levado uma semana é que quatro resultados distintos compartilhavam uma única string de erro. A informação não estava faltando — estava agregada até a inutilidade. Ninguém consegue distinguir uma rejeição de política de um bug quando os dois chegam como `WRONG_SUBSCRIPTION_STATE`.

Então: para cada modo de falha que o design antecipa, qual consulta responde "com que frequência isso está acontecendo?" Não um dashboard, não uma linha de log — uma consulta, ou seja, um sinal distinto o suficiente para ser contado. Fazer isso em tempo de design custa quase nada. Fazer retroativamente custa uma semana escavando logs, que é exatamente o que acabávamos de pagar.

Existe uma propriedade agradável aqui também: enumerar modos de falha com precisão suficiente para dar a cada um um sinal tende a revelar um ou dois em que você não havia pensado. O exercício é em parte uma revisão de design disfarçada de tarefa de observabilidade.

## O que eu levaria disso

**Faça de "e seus tradeoffs" uma seção obrigatória.** Um design sem custos declarados se lê como se não tivesse nenhum, e todo custo descoberto depois parece um erro.

**Toda alegação de propriedade precisa de um mecanismo e um número.** Se você não consegue nomear a coisa no sistema que produz a propriedade, e um número que contaria como violá-la, você escreveu uma aspiração.

**Enumere saltos para tracing, não componentes.** Cobertura é a propriedade que importa; um trace parcial engana com confiança.

**Dê a cada modo de falha um sinal consultável antes de construí-lo.** A alternativa é medi-lo depois a partir de logs que nunca foram projetados para serem medidos — e esse preço é pago em tardes inteiras.

A forma geral: para todo adjetivo num documento de design, pergunte o que teria que ser verdade para ele ser falso. Se não há resposta, o adjetivo não está fazendo trabalho nenhum, e vai ser silenciosamente descartado por quem implementar a coisa.

---

*Parte de [A Extração de Assinaturas](/pt/posts/the-subscriptions-extraction-a-reading-order), dezessete posts sobre extrair a metade de assinaturas de um monolito Django para um serviço em Go.*
