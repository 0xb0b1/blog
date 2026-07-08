---
title: "Estratégias de Cache com Redis Que Realmente Fizeram Diferença"
date: "2022-03-14"
description: "Cache-aside vs write-through, TTLs com jitter, e o cache stampede que derruba seu banco na pior hora possível — com Go e Redis, e um olhar honesto sobre o problema de invalidação do qual ninguém escapa."
tags:
  [
    "redis",
    "cache",
    "performance",
    "backend",
    "golang",
  ]
---

Cache é onde muitos backends conseguem o maior e mais barato ganho — e também onde muitos adquirem os bugs mais confusos. "Coloque Redis na frente do banco" é fácil de dizer e fácil de fazer mal. Isto é o que de fato funcionou para mim sob carga de leitura real, e os modos de falha que aprendi a projetar contra antes que me encontrassem em produção.

## Cache-Aside É o Padrão por um Motivo

O padrão que pego primeiro é cache-aside (lazy loading): a aplicação é dona do cache. Numa leitura, checa o Redis; num miss, lê o banco, popula o cache, retorna. Escritas vão para o banco e invalidam (ou atualizam) o cache.

```go
func (s *Service) GetProduct(ctx context.Context, id string) (*Product, error) {
	key := "product:" + id

	// 1. Try the cache
	if b, err := s.rdb.Get(ctx, key).Bytes(); err == nil {
		var p Product
		if json.Unmarshal(b, &p) == nil {
			return &p, nil
		}
	}

	// 2. Miss → source of truth
	p, err := s.repo.GetProduct(ctx, id)
	if err != nil {
		return nil, err
	}

	// 3. Populate for next time (fire-and-forget on cache errors)
	if b, err := json.Marshal(p); err == nil {
		s.rdb.Set(ctx, key, b, ttlWithJitter(10*time.Minute))
	}
	return p, nil
}
```

Duas coisas ali são deliberadas. Primeiro, um erro de cache nunca faz a requisição falhar — se o Redis está fora, caímos para o banco e continuamos servindo. O cache é uma otimização, não uma dependência; no momento em que ele vira load-bearing você construiu um sistema que cai quando seu *cache* cai. Segundo, o TTL tem jitter, o que importa mais do que parece.

## TTLs com Jitter Previnem Expiração Sincronizada

Se você cacheia um lote de itens com um TTL idêntico de 10 minutos — digamos, tudo carregado na hora do deploy — todos expiram no mesmo segundo, e no segundo seguinte toda requisição é um miss martelando o banco simultaneamente. Espalhar as expirações por uma janela resolve:

```go
func ttlWithJitter(base time.Duration) time.Duration {
	jitter := time.Duration(rand.Int63n(int64(base) / 5)) // up to +20%
	return base + jitter
}
```

Barato, e transforma um thundering herd sincronizado num fluxo suave de refreshes.

## O Stampede Que Derruba o Banco

Jitter lida com muitas chaves expirando juntas. O caso mais desagradável é *uma* chave quente expirar. Imagine o cache do seu produto mais popular expirando durante o pico de tráfego: centenas de requisições concorrentes todas dão miss de uma vez, e todas rodam a mesma query cara para reconstruir o mesmo valor. O cache não reduziu a carga — ele a agrupou num pico mirado no seu banco.

O conserto é deixar só uma requisição reconstruir enquanto as outras esperam pelo resultado dela. Em Go, `singleflight` faz exatamente isso sem infraestrutura extra:

```go
import "golang.org/x/sync/singleflight"

var group singleflight.Group

func (s *Service) getProductCoalesced(ctx context.Context, id string) (*Product, error) {
	v, err, _ := group.Do("product:"+id, func() (interface{}, error) {
		return s.repo.GetProduct(ctx, id) // only ONE of N concurrent callers runs this
	})
	if err != nil {
		return nil, err
	}
	return v.(*Product), nil
}
```

`singleflight` colapsa N chamadas concorrentes para a mesma chave numa execução e entrega a todos os callers o resultado compartilhado. Sob um miss de chave quente, seu banco vê uma query em vez de centenas. (Isso coalesce por-processo; para um lock verdadeiramente global entre muitas instâncias você usaria um lock de vida curta no Redis, ao custo de mais complexidade — raramente precisei ir tão longe.)

## Invalidação: A Parte Difícil

Há uma piada velha de que os dois problemas difíceis da computação são invalidação de cache e dar nome às coisas. A piada está certa sobre o primeiro. As estratégias confortáveis, da pior à melhor em correção:

- **Só TTL** — nunca invalidar explicitamente; tolerar staleness até o TTL. O mais simples, e ok quando leituras ligeiramente stale são aceitáveis (descrições de produto, config). A janela de staleness é uma decisão de produto, não técnica — consiga alguém para assinar embaixo de "até 10 minutos stale".
- **Invalidação write-through / write-around** — a cada escrita, atualizar ou deletar a chave do cache. Mais fresco, mas agora seu caminho de escrita tem que saber toda chave que um dado alimenta, e se você esquecer uma, ele serve dado stale para sempre sem TTL para salvar.
- **Invalidação orientada a eventos** — escritas emitem eventos de mudança; um consumidor invalida as chaves afetadas. Desacopla o caminho de escrita do conhecimento do cache, ao custo de infraestrutura real e frescor eventual (não imediato).

Eu default para só-TTL e só adiciono invalidação explícita para dados onde staleness genuinamente machuca, porque todo caminho de invalidação é uma nova forma de errar.

## Trade-offs

```
Strategy         Freshness         Write-path cost    Failure mode
---------------  ---------------   ----------------   ---------------------------
TTL only         stale ≤ TTL       none               serves stale within window
Write invalidate fresh             couples write→keys stale forever if a key missed
Event-driven     near-fresh        infra + consumer   lag; more moving parts
```

O enquadramento que me mantém fora de encrenca: um cache é uma **aposta de que leituras dominam escritas e que algum staleness é aceitável**. Se leituras não dominam, o cache mal ajuda. Se nenhum staleness é aceitável, você não está realmente cacheando — está construindo uma segunda fonte da verdade, e isso é um problema muito mais difícil. Nomeie seu orçamento de staleness, mantenha o cache não-load-bearing, coalesça seus stampedes, e Redis na frente do Postgres é uma das coisas de melhor custo-benefício que você pode fazer a um serviço read-heavy.
