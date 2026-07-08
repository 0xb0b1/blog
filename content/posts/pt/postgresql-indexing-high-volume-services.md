---
title: "Indexação no PostgreSQL para Serviços de Alto Volume"
date: "2021-02-10"
description: "Um guia prático de indexação no Postgres sob carga: como índices B-tree de fato são usados, por que a ordem das colunas num índice composto importa, índices covering e parciais, lendo o EXPLAIN, e o custo de escrita que torna 'só adiciona um índice' o padrão errado."
tags:
  [
    "postgresql",
    "database",
    "performance",
    "backend",
  ]
---

A maioria dos incidentes de "o banco está lento" que persegui terminaram no mesmo lugar: uma query fazendo um sequential scan sobre uma tabela que tinha crescido além do ponto onde isso era aceitável. Adicionar o índice certo resolveu em segundos. Mas "adicione um índice" é um conselho fácil de errar em três direções diferentes — o índice errado, um índice que não pode ser usado, ou um índice que silenciosamente taxa toda escrita. Isto é o que aprendi mantendo o Postgres rápido sob volume real.

## Leia o EXPLAIN Antes de Tocar em Qualquer Coisa

Nunca adicione um índice por palpite. Pergunte ao Postgres o que ele está fazendo:

```sql
EXPLAIN ANALYZE
SELECT * FROM orders
WHERE customer_id = 42 AND status = 'paid'
ORDER BY created_at DESC
LIMIT 20;
```

As palavras que importam na saída: `Seq Scan` significa que leu a tabela inteira; `Index Scan` significa que usou um índice para achar linhas; `Index Only Scan` significa que respondeu inteiramente pelo índice sem tocar a tabela (o caso mais rápido). `ANALYZE` roda a query e mostra tempos e contagens reais — uma diferença grande entre linhas estimadas e reais geralmente significa estatísticas desatualizadas (`ANALYZE orders;`) e vale corrigir antes de culpar o índice.

## A Ordem das Colunas num Índice Composto É o Jogo Inteiro

Para a query acima, o movimento tentador são três índices separados em `customer_id`, `status` e `created_at`. O Postgres consegue combiná-los com um bitmap, mas um único índice composto bem ordenado é muito melhor:

```sql
CREATE INDEX idx_orders_cust_status_created
    ON orders (customer_id, status, created_at DESC);
```

A regra que demorei demais para internalizar: **colunas de igualdade primeiro, depois a coluna de range/ordenação por último.** A query filtra `customer_id` e `status` por igualdade e ordena por `created_at`. Com esta ordem de colunas, o Postgres pula direto para a fatia `customer_id = 42, status = 'paid'` e a lê já em ordem `created_at DESC` — sem passo de sort separado. Inverta a ordem para `(created_at, customer_id, status)` e o índice fica quase inútil para esta query, porque a coluna líder não é a que você está filtrando. Um índice composto pode servir qualquer query que use um *prefixo* de suas colunas, então a ordem das colunas não é um detalhe — ela decide quais queries o índice pode ajudar.

## Índices Covering: Responder Sem a Tabela

Um index scan normalmente acha as linhas correspondentes no índice, depois visita a tabela (o "heap") para buscar as colunas que você selecionou. Se o índice já contém toda coluna que a query precisa, o Postgres pula o heap inteiramente — um index-only scan. Desde o Postgres 11 você pode adicionar colunas não-chave puramente para habilitar isso:

```sql
CREATE INDEX idx_orders_cust_covering
    ON orders (customer_id, status) INCLUDE (total, created_at);
```

Agora `SELECT total, created_at FROM orders WHERE customer_id = 42 AND status = 'paid'` pode ser respondido só pelo índice. Numa query quente sobre uma tabela larga, cortar a busca no heap é um ganho real. O custo é um índice mais gordo — você está duplicando essas colunas — então reserve índices covering para caminhos genuinamente quentes, não para tudo.

## Índices Parciais: Indexe Só o Que Você Consulta

Se suas queries sempre filtram por um valor, não indexe as linhas que você nunca procura:

```sql
CREATE INDEX idx_orders_pending
    ON orders (created_at)
    WHERE status = 'pending';
```

Numa tabela de pedidos que é 95% `completed`, um índice parcial sobre as linhas pendentes é uma fração do tamanho, fica em memória, e é mais barato de manter. Este é um dos melhores recursos do Postgres e o mais subutilizado — sempre que você tem um "subconjunto quente" (jobs não processados, sessões ativas, soft-deleted = false), um índice parcial mira exatamente nele.

## O Custo Que Ninguém Menciona

Todo índice deixa as escritas mais lentas. Um `INSERT` ou `UPDATE` que toca uma coluna indexada tem que atualizar todo índice relevante, sincronamente, dentro da transação. Numa tabela write-heavy, cinco índices significam cinco operações de manutenção de índice por escrita. Já vi um bem-intencionado "vamos indexar tudo que o planner possa querer" transformar um caminho de ingestão rápido num gargalo.

Índices também sofrem **bloat**. Sob churn pesado de update/delete, índices B-tree acumulam entradas mortas e crescem; `REINDEX CONCURRENTLY` (Postgres 12+) os reconstrói sem travar escritas. E construir um índice numa tabela viva de alto volume a trava contra escritas a menos que você use `CREATE INDEX CONCURRENTLY` — mais lento de construir, mas não tira sua tabela do ar.

## Trade-offs

```
Index type      Read benefit               Write/space cost      Use when
--------------  -------------------------  -------------------   ----------------------------
B-tree single   equality/range on 1 col    low                   simple lookups
Composite       multi-col filter + sort    medium                fixed query shape, order matters
Covering        index-only scan            higher space          hot query over a wide table
Partial         tiny, hot-subset lookups   low                   queries always filter a subset
```

O modelo mental que me mantém honesto: um índice é uma aposta de que a economia de leitura supera o custo de escrita, paga em toda escrita para sempre. Para uma tabela lida mil vezes por escrita, indexe generosamente. Para um write-firehose lido ocasionalmente, indexe com relutância e meça. "Adicione um índice" nunca é de graça — é uma troca, e `EXPLAIN ANALYZE` mais uma olhada na sua taxa de escrita é como você a precifica antes de se comprometer.
