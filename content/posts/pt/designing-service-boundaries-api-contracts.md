---
title: "Projetando Fronteiras de Serviço e Contratos de API"
date: "2024-09-10"
description: "Onde você desenha fronteiras de serviço — e como trata os contratos entre elas — decide se um sistema é genuinamente desacoplado ou um monolito distribuído vestido de microsserviço. Sobre posse de dados, acoplamento, e evoluir contratos sem quebrar consumidores."
tags:
  [
    "arquitetura",
    "api",
    "design-de-sistemas",
    "contratos",
    "backend",
  ]
---

A parte mais difícil de construir sistemas a partir de serviços não são os serviços — são as linhas entre eles. Desenhe as fronteiras errado e você ganha o pior dos dois mundos: o custo operacional de um sistema distribuído com o acoplamento de um monolito, de modo que todo serviço "independente" tem que fazer deploy em lockstep com outros três. Desenhe-as certo e times de fato se movem por conta própria. Depois de alguns sistemas que erraram isso antes de acertar, eis o que decidi.

## Uma Fronteira É Sobre Posse de Dados, Não Tamanho de Código

O instinto é dividir por camada técnica ou por "este arquivo está ficando grande". A divisão durável é por **posse de dados**: um serviço é dono de uma fatia coerente de dados e é a *única* coisa que a escreve. Todo mundo que precisa daquele dado passa pela API do serviço, nunca dentro do banco dele.

O sinal de que uma fronteira está errada é a direção e o volume de conversa. Se dois serviços não conseguem fazer nada útil sem uma dúzia de chamadas síncronas de ida e volta, eles não são dois serviços — são um serviço com um cabo de rede grampeado no meio, e você adicionou latência e modos de falha à toa. Fronteiras deveriam cair onde o acoplamento é naturalmente baixo: gestão de pedidos é dona de pedidos, catálogo é dono de produtos, e eles interagem por meio de algumas chamadas bem definidas, não uma conversa constante.

O antipadrão para nomear em voz alta é o **banco de dados compartilhado**: dois serviços lendo e escrevendo as mesmas tabelas. Parece reuso; é na verdade o acoplamento mais apertado que existe, porque agora o schema do banco *é* a API de ambos os serviços, e nenhum pode mudar uma coluna sem coordenar com o outro. Se serviços compartilham um banco, você não tem uma fronteira — tem um monolito com passos extras de deploy.

## O Contrato É a Fronteira de Verdade

Assim que um serviço é dono de seus dados, o contrato de API se torna a interface de fato — e o ponto todo é que a *implementação* atrás dele pode mudar livremente enquanto o *contrato* permanece estável. Isso só funciona se o contrato for explícito e imposto, não implícito. Seja um documento OpenAPI, um `.proto`, ou um schema GraphQL (comparei esses [aqui](/pt/posts/choosing-between-rest-grpc-graphql)), um bom contrato é:

- **Explícito** — escrito numa forma contra a qual ambos os lados fazem build, não "o que quer que o endpoint retorne hoje".
- **Tipado** — tipos de campo e obrigatoriedade fazem parte do contrato, então uma incompatibilidade é pega em build ou request time, não por um consumidor quebrando num `null`. Eu me apoio em tooling de schema como malli exatamente para isso na camada backend-for-frontend ([post](/pt/posts/clojure-bff-typed-contracts-reitit-malli)).
- **Do produtor, moldado para o consumidor** — o serviço é dono de seu contrato, mas ele é projetado em torno do que os callers de fato precisam, não um dump cru do modelo interno.

## Evoluindo Sem Quebrar Consumidores

Aqui é onde a maior parte da dor de contrato de fato vive: não projetar a primeira versão, mas mudá-la enquanto pessoas dependem dela. Você quase nunca controla todos os consumidores, e não pode atualizá-los atomicamente. Então a regra é **mudança aditiva e retrocompatível por padrão**:

- **Adicionar** um campo opcional é seguro — consumidores antigos ignoram o que não conhecem.
- **Remover** um campo, **renomear** um, ou tornar um campo opcional **obrigatório** é uma mudança quebradora — vai derrubar consumidores que não a esperam.

```
Change to a contract                 Safe?   Why
-----------------------------------  ------  -------------------------------------
Add an optional field                yes     old clients ignore unknown fields
Add a new endpoint / RPC             yes     nobody depends on it yet
Make an optional field required      NO      old clients omit it → they break
Remove or rename a field             NO      clients reading it → they break
Change a field's type                NO      deserialization breaks
Tighten validation on existing input NO      previously-valid requests now rejected
```

Quando você genuinamente precisa fazer uma mudança quebradora, você não muta o contrato existente — você **versiona** (um novo endpoint, uma mensagem `v2`, um novo schema) e roda ambos até os consumidores migrarem, depois aposenta o antigo. É mais trabalho, e é o preço de não conseguir fazer redeploy do mundo de uma vez.

Uma disciplina que vale adotar quando o risco é alto: **testes de contrato orientados ao consumidor**, onde cada consumidor contribui um teste afirmando o formato do qual depende, e o produtor roda todos eles no CI. Aí "acabei de quebrar alguém?" é respondido por um build vermelho antes do deploy, não pelo pager de um consumidor depois.

## Trade-offs

O meta-ponto é que fronteiras e contratos são uma aposta sobre **mudança**: você gasta custo de coordenação antecipadamente (contratos explícitos, disciplina de versionamento, sem tabelas compartilhadas) para comprar a habilidade de mudar cada serviço independentemente depois.

- **Quando fronteiras fortes compensam:** múltiplos times, serviços que evoluem em ritmos diferentes, qualquer coisa onde você não pode coordenar todo deploy. A disciplina antecipada é o que deixa os times pararem de se bloquear.
- **Quando são exagero:** um time pequeno num produto jovem onde o domínio ainda muda toda semana. Fronteiras de serviço prematuras congelam seu modelo de dados antes de você entendê-lo, e mover uma fronteira depois é muito mais doloroso que mover uma função. Comece com um monolito bem estruturado e fronteiras *internas* claras; extraia serviços quando os dados de acoplamento (contenção de deploy, escala diferente, posse de time) de fato justificarem a rede.

A frase à qual sempre volto: uma fronteira de serviço é uma promessa que custa algo para manter e algo para quebrar. Coloque-a onde a promessa é fácil de manter — ao redor de dados possuídos, atrás de um contrato explícito que você pode evoluir de forma aditiva — e serviços te compram independência real. Coloque-a em qualquer outro lugar e você só distribuiu seu monolito.
