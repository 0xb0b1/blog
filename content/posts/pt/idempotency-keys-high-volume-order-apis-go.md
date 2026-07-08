---
title: "Idempotency Keys para APIs de Pedidos de Alto Volume em Go"
date: "2020-03-10"
description: "Como tornar endpoints de pedidos e pagamentos seguros para retry: idempotency keys fornecidas pelo cliente, um store baseado em Postgres que sobrevive a duplicatas concorrentes, e os trade-offs de escopo, TTL e requisições em andamento — em Go."
tags:
  [
    "golang",
    "api",
    "idempotencia",
    "backend",
    "postgresql",
  ]
---

O bug que me ensinou a me importar com idempotência foi uma cobrança em dobro. Um cliente tocou em "Pagar" numa conexão instável, a requisição deu timeout no lado do cliente, o app tentou de novo, e nossa API alegremente criou dois pedidos. A primeira requisição tinha de fato dado certo — a resposta só nunca chegou de volta. Do ponto de vista do servidor, havia duas requisições legítimas. Do ponto de vista do cliente, havia uma intenção e duas cobranças.

Você não conserta isso sendo mais cuidadoso. Em qualquer rede, um cliente que não recebe resposta tem exatamente duas escolhas: desistir, ou tentar de novo. Se tentar de novo pode aplicar um efeito em dobro, tentar de novo é perigoso, então clientes desistem — e agora você trocou cobranças duplicadas por pedidos perdidos. O conserto de verdade é tornar a operação segura para retry. É isso que uma idempotency key te dá.

## O Padrão

O cliente gera uma chave única (um UUID) para cada operação *pretendida* e a envia com a requisição, convencionalmente num header `Idempotency-Key`. Ele reutiliza a mesma chave nos retries daquela mesma intenção. O servidor lembra o que fez para cada chave: na primeira vez que vê uma chave, faz o trabalho e armazena o resultado; em toda vez seguinte, retorna o resultado armazenado sem refazer nada.

A chave representa intenção, não uma requisição. Dez retries de "criar este pedido" carregam uma chave e causam um pedido.

## Um Store Que Sobrevive à Concorrência

A implementação ingênua — "busca a chave; se ausente, faz o trabalho; armazena o resultado" — tem uma corrida. Dois retries podem ambos rodar a busca, ambos dar miss, e ambos fazer o trabalho. O store precisa tornar o "primeiro-a-escrever-vence" atômico, e uma unique constraint no Postgres faz exatamente isso:

```sql
CREATE TABLE idempotency_keys (
    key          TEXT        PRIMARY KEY,
    endpoint     TEXT        NOT NULL,
    state        TEXT        NOT NULL,   -- 'in_progress' | 'completed'
    status_code  INT,
    response     JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

O fluxo: tente *reivindicar* a chave inserindo uma linha `in_progress`. Se o insert vence, esta goroutine é dona da operação. Se bate numa unique violation, alguém chegou primeiro — olhamos a linha dela e ou reproduzimos a resposta completada ou dizemos ao cliente que a original ainda está rodando.

```go
var errInProgress = errors.New("idempotent request already in progress")

// claim attempts to reserve the key. It returns (replay, nil) if the work
// was already completed, (nil, errInProgress) if a duplicate is mid-flight,
// or (nil, nil) if this caller now owns the operation.
func (s *Store) claim(ctx context.Context, key, endpoint string) (*stored, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO idempotency_keys (key, endpoint, state)
		 VALUES ($1, $2, 'in_progress')`, key, endpoint)
	if err == nil {
		return nil, nil // we own it
	}
	if !isUniqueViolation(err) {
		return nil, err
	}

	// Someone else claimed it first — inspect their row.
	var st stored
	err = s.db.QueryRowContext(ctx,
		`SELECT endpoint, state, status_code, response
		 FROM idempotency_keys WHERE key = $1`, key).
		Scan(&st.endpoint, &st.state, &st.statusCode, &st.response)
	if err != nil {
		return nil, err
	}
	if st.endpoint != endpoint {
		return nil, errKeyReused // same key, different operation — client bug
	}
	if st.state == "completed" {
		return &st, nil
	}
	return nil, errInProgress
}
```

Embrulhar num middleware HTTP mantém os handlers alheios:

```go
func Idempotent(store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" { // opt-in: no key, no protection
				next.ServeHTTP(w, r)
				return
			}
			replay, err := store.claim(r.Context(), key, r.URL.Path)
			switch {
			case errors.Is(err, errInProgress):
				http.Error(w, "request in progress", http.StatusConflict)
				return
			case err != nil:
				http.Error(w, "idempotency error", http.StatusInternalServerError)
				return
			case replay != nil:
				w.Header().Set("Idempotent-Replay", "true")
				w.WriteHeader(replay.statusCode)
				w.Write(replay.response)
				return
			}
			// We own the operation: capture the response, then persist it.
			rec := &recorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(rec, r)
			store.complete(r.Context(), key, rec.status, rec.body.Bytes())
		})
	}
}
```

O `recorder` é um wrapper fino de `http.ResponseWriter` que captura o status e o body para que possamos armazená-los para replays futuros.

## As Decisões Que Realmente Importam

O código é a parte fácil. As perguntas de design é onde sistemas de alto volume tomam mordidas:

**O que acontece com um `in_progress` que quebrou?** Se o processo dono morre depois de reivindicar mas antes de completar, a chave fica presa. Todo retry vê `in_progress` e recebe um 409 para sempre. Você precisa de um reaper: tratar linhas `in_progress` mais velhas que algum timeout como abandonadas e deixar um novo caller assumir. Esse timeout tem que exceder sua operação legítima mais longa, ou você vai re-rodar trabalho que ainda está rodando.

**Por quanto tempo você guarda as chaves?** Para sempre é o mais simples e errado — a tabela cresce sem limite. Um TTL (digamos 24–72h) cobre janelas de retry realistas. Passado isso, uma chave que retorna é tratada como nova. A janela é uma aposta: longa o suficiente para pegar retries, curta o suficiente para limitar o armazenamento.

**A operação é de fato determinística?** Reproduzir um `201 Created` armazenado só é correto se o cliente quer o resultado *original*. Se seu endpoint embute um timestamp do servidor ou um ID recém-gerado na resposta, o replay retorna o antigo — geralmente o que você quer, ocasionalmente surpreendente.

## Trade-offs

```
Approach                     Safe under concurrency   Storage cost   Complexity
---------------------------  ----------------------   ------------   ----------
Do nothing (hope)            no                       none           none
Dedup on natural key         partial (needs one)      none           low
Idempotency key + unique     yes                      1 row/op+TTL   medium
Idempotency key + lock/wait  yes, but blocks          1 row/op+TTL   high
```

A abordagem da unique constraint é o ponto ideal para APIs de pedidos e pagamentos: correção sob retries concorrentes, armazenamento limitado com um TTL, e o banco — não locks de aplicação — impondo o primeiro-a-escrever-vence. **Quando é exagero:** operações naturalmente idempotentes (um `PUT` que define o estado para um valor absoluto já é idempotente; não precisa de chave). **Quando não é suficiente:** se o *efeito colateral* vive em outro sistema (um processador de pagamentos real), você também precisa que aquela chamada downstream seja idempotente, porque sua chave só protege seu próprio banco. Idempotência tem que percorrer todo o comprimento da cadeia causal, ou o elo mais fraco aplica em dobro.

A lição que ficou: retries não são um caso extremo a ser minimizado, são o comportamento normal de todo cliente honesto numa rede imperfeita. Projete o caminho de escrita para que um retry seja um no-op, e uma categoria inteira de incidentes das 3 da manhã desaparece.
