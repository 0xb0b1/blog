---
title: "Escolhendo Entre REST, gRPC e GraphQL"
date: "2022-10-18"
description: "Três formas de expor uma API, três apostas diferentes. Uma comparação prática de REST, gRPC e GraphQL em contratos, transporte, adequação ao cliente, evolução e cache — com as condições concretas sob as quais cada um é a escolha certa."
tags:
  [
    "arquitetura",
    "api",
    "grpc",
    "graphql",
    "rest",
    "design-de-sistemas",
  ]
---

Já coloquei os três em produção, às vezes no mesmo sistema, e a pergunta "que estilo de API isto deveria ser?" ainda aparece em toda revisão de design. A resposta honesta é que eles são otimizados para problemas diferentes, e o erro é escolher um como religião padrão em vez de casá-lo com o formato do tráfego. Eis como eu de fato decido.

## O Que Cada Um É, em Uma Frase

**REST** modela recursos como URLs e usa verbos HTTP contra eles. O contrato é convencional (frequentemente documentado com OpenAPI), o payload geralmente é JSON, e roda sobre HTTP puro:

```
GET  /orders/42
POST /orders
```

**gRPC** é RPC contract-first: você define serviços e mensagens num arquivo `.proto`, gera código tipado de cliente e servidor, e ele fala Protobuf sobre HTTP/2.

```protobuf
service OrderService {
  rpc GetOrder(GetOrderRequest) returns (Order);
}
message GetOrderRequest { string id = 1; }
```

**GraphQL** expõe um único endpoint e um schema tipado; o *cliente* declara exatamente quais campos quer numa query, e o servidor os resolve.

```graphql
query { order(id: "42") { total status customer { name } } }
```

## Os Eixos Que Decidem

**Quem está chamando, e de onde?** Este é o maior fator. gRPC é excelente para tráfego serviço-a-serviço dentro da sua própria rede — contratos tipados, multiplexação HTTP/2, payloads binários minúsculos — mas é desajeitado a partir de um browser (precisa de uma camada de proxy para funcionar na web). REST e GraphQL são nativos de browser. Se quem chama é seu próprio front-end mobile/web, REST ou GraphQL; se é outro backend que você controla, gRPC ganha seu lugar.

**Quão variados são os formatos de leitura?** Endpoints REST retornam um formato fixo, o que leva a duas dores clássicas: *over-fetching* (o cliente mobile recebe 40 campos para renderizar 3) e *under-fetching* (renderizar uma tela leva cinco round trips). GraphQL existe precisamente para matar ambos — o cliente pede exatamente os campos que precisa, numa requisição. Se você tem muitos tipos de cliente cada um querendo uma fatia diferente de um grafo rico, isso é o território de casa do GraphQL. Se todo cliente quer basicamente o mesmo formato fixo, a maquinaria do GraphQL é overhead que você não precisa.

**Quanto você valoriza o contrato como código?** O `.proto` do gRPC é a única fonte da verdade, e codegen significa que uma renomeação de campo quebra o build, não a produção. O schema do GraphQL é similarmente forte e tipado. O contrato do REST é uma convenção que você mantém por fora (OpenAPI ajuda mas não é imposto por padrão), então é o mais fácil de deixar divergir.

**Cache.** REST vence aqui e é subestimado: respostas `GET` cacheiam limpo em toda camada — browser, CDN, reverse proxy — chaveadas por URL, de graça. Queries GraphQL tipicamente são `POST`s com o formato no body, então cache HTTP não se aplica e você cacheia no nível de resolver/campo, o que é mais trabalho. Se cache barato de leituras na borda importa, `GET`s REST puros são difíceis de superar.

**Streaming.** gRPC tem streaming bidirecional de primeira classe sobre HTTP/2. REST faz server-sent events ou long-polling de forma desajeitada; GraphQL tem subscriptions mas são um enxerto. Streaming de verdade aponta para gRPC.

## Trade-offs

```
Axis              REST                gRPC                    GraphQL
----------------  ------------------  ----------------------  ----------------------
Contract          convention/OpenAPI  .proto (enforced)       schema (enforced)
Payload           JSON (verbose)      Protobuf (compact)      JSON, client-shaped
Browser-native    yes                 no (needs proxy)        yes
Over/under-fetch  common              fixed per-RPC           client picks fields
HTTP caching      excellent (GET)     no                      poor (POST body)
Streaming         awkward             first-class             subscriptions (bolt-on)
Best caller       public/web clients  internal service↔service many client shapes
Tooling maturity  ubiquitous          strong (typed codegen)  good, more setup
```

## Como Eu de Fato Escolho

- **API pública, muitos terceiros, leituras cacheáveis →** REST. É a língua franca; todo mundo consegue chamar, e cache de CDN é de graça.
- **Serviço-a-serviço interno, sensível a latência, contratos tipados →** gRPC. Payloads binários e multiplexação HTTP/2 compensam, e os clientes gerados removem uma classe de bugs de integração.
- **Um grafo de dados rico alimentando vários front-ends diferentes →** GraphQL. Deixar cada cliente moldar seu próprio payload colapsa round trips e para a taxa de over-fetch/under-fetch.

E — o reframe que frequentemente está certo — **você não precisa escolher um para o sistema inteiro.** Um formato comum e saudável é gRPC entre serviços internos, com uma borda fina REST ou GraphQL para o browser. Cada fronteira recebe o estilo que serve seus callers. O que você quer evitar é escolher por moda: GraphQL porque está na moda num sistema com um cliente e formatos fixos, ou gRPC numa API pública que todo mundo depois tem dificuldade de chamar de um browser. Case o estilo com quem chama e o que precisa ler, e os três são boas ferramentas.
