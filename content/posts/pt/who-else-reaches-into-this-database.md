---
title: "Quem Mais Mexe Neste Banco de Dados?"
date: "2026-07-17"
description: "A pergunta bloqueante de qualquer extração. Sobre enumerar todo serviço que toca um banco compartilhado com direção e evidência, descobrir quais entidades são chaveadas por um provedor externo — e por que uma chave de provedor único silenciosamente torna seu histórico dependente de um fornecedor que você talvez substitua."
tags:
  [
    "arquitetura",
    "bancos-de-dados",
    "posse-de-dados",
    "microsservicos",
    "backend",
    "extracao-de-assinaturas",
  ]
---

Já escrevi antes que [uma fronteira de serviço é sobre posse de dados](/pt/posts/designing-service-boundaries-api-contracts): um serviço possui uma fatia de dados e é a única coisa que a escreve. Esse é o princípio. Este post é sobre o levantamento que você tem que fazer antes de conseguir aplicá-lo a um banco que é mais antigo que o princípio.

A pergunta é simples e a resposta nunca está na cabeça de uma única pessoa: **quem mais mexe neste banco, e para quê?**

## Acoplamentos precisam de direção e evidência

O critério que escrevemos:

> **AC-034** — Todo acoplamento nomeia seu serviço, tabelas, direção e evidência.

Quatro campos, e cada um está ali por causa de uma forma pela qual esse levantamento dá errado.

**Serviço** é óbvio. **Tabelas** importa porque "o serviço X usa o banco de assinaturas" não é acionável — três tabelas é um problema diferente de trinta.

**Direção** é a que as pessoas pulam, e é a que decide o plano. Um serviço que só lê uma tabela pode ser tratado com uma view, uma réplica ou uma API, e pode continuar funcionando durante um cutover. Um serviço que *escreve* uma tabela que você está prestes a possuir é um bloqueador: dois escritores numa tabela significa que você não tem uma fronteira, você tem uma variável mutável compartilhada entre dois deployables.

**Evidência** é o campo que mantém o documento honesto. Não "eu acho que o serviço de notificações lê isso", mas a consulta, o arquivo, a linha. Sem isso, um levantamento de acoplamentos se torna uma coleção de lembranças, e os acoplamentos que as pessoas lembram errado são precisamente os que causam o incidente.

## Entidades chaveadas por provedor, e a armadilha nelas

A parte que eu não havia antecipado:

> **AC-031** — Toda entidade chaveada por provedor está no inventário com seus provedores.
> **AC-033** — Toda entidade chaveada por um único provedor é marcada como histórico-exposto.

Nós ingerimos dados esportivos de provedores externos. Muitas entidades são chaveadas pelo identificador do provedor em vez de por um id que nós geramos — uma partida, uma competição, um time é o id *deles* na nossa coluna.

Isso é normal, e em grande parte inofensivo, até o momento em que você considera trocar de provedor. Então uma entidade chaveada a exatamente um provedor significa que todo o seu histórico daquela entidade está expresso num vocabulário para o qual você não tem mais assinatura. Você pode manter as linhas. Você só não pode juntá-las a nada novo, e não pode rebuscar o que falta.

O critério chama isso de **histórico-exposto**, e marcá-lo é o valor inteiro. Em paralelo estávamos [avaliando se um provedor de dados esportivos poderia substituir outro](/pt/posts/what-the-api-returns-vs-what-the-docs-claim); as entidades marcadas aqui são exatamente aquelas em que essa migração deixa de ser um exercício de integração e se torna um exercício de migração de dados. Duas features em repositórios diferentes, e o risco mais difícil da segunda estava escrito no inventário da primeira.

Se seu schema chaveia qualquer coisa pelo identificador de um fornecedor, isso é um acoplamento ao fornecedor tão real quanto uma chamada de API — e muito menos visível, porque não aparece em nenhuma lista de dependências.

## Opções avaliadas contra os acoplamentos

Este é o critério que eu roubaria para qualquer documento de design:

> **AC-035** — Toda opção declara seu efeito sobre todo acoplamento registrado.

Não "aqui estão três arquiteturas e seus tradeoffs gerais". Cada opção, cruzada contra cada acoplamento que você de fato encontrou. Opção A deixa o serviço X lendo diretamente; opção B força X a usar uma API e custa a X uma release; opção C move a tabela e quebra X até que ele seja atualizado.

Isso transforma a seleção de arquitetura em algo mais próximo de aritmética. A discussão abstrata — banco compartilhado versus API versus stream de eventos — não tem fim, porque todo mundo está certo em geral. A discussão concreta tem fim, porque o número de acoplamentos é finito e o efeito sobre cada um é verificável.

Também traz à superfície a opção que as pessoas não propõem em voz alta: *deixe essa parte onde está.* Uma vez que toda opção é pontuada contra acoplamentos reais, "extrair tudo" frequentemente deixa de ser a de melhor pontuação, e você descobre que a fronteira sensata é mais estreita que a da proposta original.

## Uma recomendação, e seu risco principal

> **AC-037** — O relatório nomeia uma recomendação e seu risco principal.

As duas metades são deliberadas.

**Uma** recomendação, porque um relatório que apresenta três opções de forma neutra e para terceirizou a decisão de volta para quem lê — normalmente para uma reunião, onde quem fala mais decide. Se você fez a análise, você tem uma opinião; diga.

**Seu risco principal**, no singular, porque uma recomendação sem risco declarado se lê como defesa de causa e é tratada como tal. E uma lista de nove riscos é uma forma de não se comprometer. Nomear a única coisa mais provável de fazer disso a decisão errada é o que permite a quem revisa engajar com ela — pode atacar aquele risco especificamente, e ou ele sobrevive ou você aprendeu algo de forma barata.

## O que eu levaria disso

**Levante antes de projetar.** O conjunto de serviços que tocam um banco compartilhado é descobrível, finito, e não é conhecível de memória. São algumas horas de trabalho e muda o que você propõe.

**Direção e evidência, por acoplamento.** Leitores podem ser tratados; escritores são fronteiras. E "evidência" é o que impede o levantamento de ser uma enquete.

**Procure identificadores de fornecedor nas suas chaves.** Uma coluna guardando o id de outra pessoa é um acoplamento àquele fornecedor sem nenhuma da visibilidade de uma dependência de API. Se exatamente um provedor pode fornecer aquela chave, seu histórico está exposto ao contrato daquele provedor.

**Pontue opções contra os acoplamentos que você encontrou, não no abstrato.** Encerra a discussão, e torna "não extraia isso" uma opção visível em vez de uma indizível.

---

*Parte de [A Extração de Assinaturas](/pt/posts/the-subscriptions-extraction-a-reading-order), dezessete posts sobre extrair a metade de assinaturas de um monolito Django para um serviço em Go.*
