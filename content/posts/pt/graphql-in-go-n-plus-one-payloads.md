---
title: "GraphQL em Go: Matando o N+1 e Encolhendo Payloads"
date: "2023-04-12"
description: "Os dois problemas que todo servidor GraphQL enfrenta quando clientes reais chegam: a explosão de queries N+1 vinda de resolvers aninhados, e o custo ilimitado de query. Como consertar ambos em Go com dataloaders e limites de complexidade."
tags:
  [
    "golang",
    "graphql",
    "api",
    "performance",
    "backend",
  ]
---

GraphQL é adorável na demo e traiçoeiro sob carga, e ambos os fatos têm a mesma causa: o cliente, não o servidor, decide o formato da query. Essa é a feature — [é por isso que você escolheria GraphQL](/pt/posts/choosing-between-rest-grpc-graphql) — e é também a fonte dos dois problemas que mordem todo servidor GraphQL em Go assim que clientes reais começam a mandar queries reais. Eis como eu lido com eles.

## A Explosão N+1

Digamos que uma query pede uma lista de pedidos e o cliente de cada pedido:

```graphql
query {
  orders(first: 50) { id total customer { name } }
}
```

O setup ingênuo de resolvers roda uma query para buscar 50 pedidos, depois — porque o resolver `customer` dispara uma vez por pedido — mais 50 queries para buscar cada cliente individualmente. São 51 round trips ao banco para uma chamada de API, e escala linearmente com o tamanho da lista. Esta é a forma mais comum de um servidor GraphQL cair: não uma query lenta, mas centenas de rápidas.

O conserto é um **dataloader**: em vez de resolver cada `customer` imediatamente, você coleta todos os IDs de cliente pedidos durante um tick do event loop, depois os satisfaz numa única query em lote. O resolver por-pedido pede um cliente ao loader; o loader agrupa.

```go
// A batch function receives all keys requested in this tick and must
// return results in the SAME order as the keys.
func batchCustomers(ctx context.Context, ids []string) []*dataloader.Result[*Customer] {
	rows, _ := repo.CustomersByIDs(ctx, ids) // ONE query: WHERE id = ANY($1)

	byID := make(map[string]*Customer, len(rows))
	for _, c := range rows {
		byID[c.ID] = c
	}
	out := make([]*dataloader.Result[*Customer], len(ids))
	for i, id := range ids {
		out[i] = &dataloader.Result[*Customer]{Data: byID[id]}
	}
	return out
}

// The customer resolver just asks the loader — no direct DB call.
func (r *orderResolver) Customer(ctx context.Context, o *Order) (*Customer, error) {
	return loadersFrom(ctx).Customer.Load(ctx, o.CustomerID)()
}
```

Agora aquelas 50 resoluções aninhadas colapsam numa query `WHERE id = ANY($1)`. O loader vive por-requisição (guardado no context por um middleware) para que agrupamento e cache sejam escopados a uma única chamada de API e não vazem entre requisições. Este único padrão é a diferença entre um servidor GraphQL que sobrevive à produção e um que não.

## Custo Ilimitado de Query

O segundo perigo é mais sutil. Porque o cliente compõe a query, um cliente pode pedir algo enorme — profundamente aninhado, ou uma página gigante — e um servidor ingênuo obedientemente tentará servir:

```graphql
query {
  orders(first: 1000) {
    customer { orders(first: 1000) { customer { orders(first: 1000) { id } } } }
  }
}
```

Isso é uma query projetada (maliciosamente ou por acidente) para derreter seu banco. REST não tem esse problema porque o servidor fixa o formato; GraphQL entrega esse controle ao cliente, então você tem que recolocar os limites. Duas defesas:

**Limite de profundidade** rejeita queries aninhadas além de algum nível. **Limite de complexidade** atribui um custo a cada campo (maior para campos de lista, multiplicado pelo tamanho da página pedida) e rejeita queries cujo total excede um orçamento:

```go
srv := handler.NewDefaultServer(schema)

// Reject anything costing more than 200 "points".
srv.Use(extension.FixedComplexityLimit(200))

// A list field's cost scales with how many items it can return.
cfg.Complexity.Query.Orders = func(childComplexity, first int) int {
	return first * childComplexity
}
```

Agora a query patológica acima é rejeitada antes de tocar o banco, com um erro claro, em vez de dar timeout.

## Encolher Payloads É Quase de Graça

O outro lado das queries controladas pelo cliente é o lado bom: clientes buscam exatamente os campos que renderizam. Uma tela mobile que precisa de `id` e `total` manda uma query por `id` e `total` e recebe de volta um payload minúsculo — sem over-fetching dos 30 campos que um endpoint REST teria retornado. Você ganha isso quase de graça, mas dois hábitos protegem:

- **Resolva de forma preguiçosa.** Não compute um campo caro (digamos, um agregado) no resolver pai "só por precaução" — coloque-o atrás do seu próprio resolver de campo para que só rode quando um cliente de fato o selecionar. O ponto todo é que campos não pedidos não custam nada.
- **Pagine tudo que é lista.** Exponha cursores `first`/`after`, não arrays ilimitados, para que o tamanho do payload (e o custo de complexidade) permaneça limitado independente de quanto dado está atrás do campo.

## Trade-offs

A coisa a manter à vista: GraphQL move o poder de moldar queries do servidor para o cliente, e todo problema aqui é o custo dessa troca.

```
Concern            Cause                        Mitigation           Residual cost
-----------------  ---------------------------  -------------------  --------------------
N+1 queries        per-item nested resolvers    per-request loaders  batching complexity
Runaway queries    client controls shape/depth  depth+complexity cap tuning the budget
Payload bloat      (avoided by design)          lazy fields + paging discipline required
```

Nada disso é motivo para evitar GraphQL — é o custo de manutenção permanente da flexibilidade. Se seus clientes todos querem o mesmo formato fixo, você está pagando esse custo por uma flexibilidade que não está usando, e REST seria mais simples. Mas quando você genuinamente tem muitos formatos de cliente sobre um grafo rico, dataloaders e um orçamento de complexidade transformam GraphQL de uma demo em algo que você pode colocar com segurança na frente de tráfego real.
