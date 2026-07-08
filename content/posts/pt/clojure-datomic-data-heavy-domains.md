---
title: "Datomic para Domínios com Muitos Dados: O Banco de Dados como um Valor"
date: 2022-06-27
description: "O que o Datomic dá a um domínio com muitos dados que um banco tradicional não dá: o datom atômico, schema como dados, valores imutáveis do banco que você consulta como funções puras, viagem no tempo e auditoria de graça — e um olhar honesto sobre onde ele machuca."
tags:
  [
    "clojure",
    "datomic",
    "datalog",
    "database",
    "modelagem-de-dados",
    "backend",
    "design-de-sistemas",
  ]
---

O Datomic é a peça do ecossistema Clojure que mais mudou como eu penso sobre dados, e é a que eu tenho que trabalhar mais duro para explicar a quem nunca usou. Escrevo Clojure desde 2017, e em domínios onde o *histórico* dos dados importa tanto quanto o estado atual, o modelo do Datomic para de parecer exótico e começa a parecer obviamente correto. Este post é a explicação que eu gostaria de ter recebido, incluindo as partes em que ele não é a ferramenta certa.

## A Unidade Atômica É um Fato

A unidade de um banco tradicional é a linha, e uma linha é mutável — você faz `UPDATE` nela e o valor antigo se foi. A unidade do Datomic é o **datom**: um único fato atômico, uma tupla de cinco elementos de entidade, atributo, valor, transação, e se foi adicionado ou retraído.

`[account-42, :account/balance, 100.00, tx-1001, true]`

Você nunca sobrescreve um datom. Mudar um saldo *afirma um novo datom* e retrai o antigo, ambos carimbados com a transação que fez isso. O banco acumula fatos em vez de mutar células. Essa única escolha de design é de onde tudo o mais decorre.

Schema também é dado — atributos, não tabelas. Uma entidade é só o conjunto de atributos que foram afirmados sobre ela:

```clojure
[{:db/ident       :account/id
  :db/valueType   :db.type/uuid
  :db/cardinality :db.cardinality/one
  :db/unique      :db.unique/identity}
 {:db/ident       :account/balance
  :db/valueType   :db.type/bigdec
  :db/cardinality :db.cardinality/one}
 {:db/ident       :transfer/from
  :db/valueType   :db.type/ref          ; a reference to another entity
  :db/cardinality :db.cardinality/one}]

;; A transaction is data: assert facts, get back a new database
(d/transact conn {:tx-data [{:account/id      (java.util.UUID/randomUUID)
                             :account/balance 100.00M}]})
```

## O Banco de Dados É um Valor

Aqui está o clique. `(d/db conn)` retorna um snapshot imutável do banco inteiro *como um valor*. Não uma conexão contra a qual você consulta em um servidor vivo e mutante — uma coisa estável e imutável que você segura na mão. Toda consulta é uma função pura desse valor.

```clojure
(let [db (d/db conn)]
  (d/q '[:find ?id ?bal
         :where [?a :account/id ?id]
                [?a :account/balance ?bal]
                [(> ?bal 0M)]]
       db))
```

Porque `db` é um valor imutável, você pode passá-lo para uma função, rodar vinte consultas contra exatamente o mesmo snapshot com risco zero de ele mudar por baixo, comparar dois valores de banco, ou entregá-lo a uma função pura que computa algo sem nenhuma noção de que um "banco de dados" está envolvido. Testar vira trivial: um teste é um valor de db e uma asserção, sem mock, sem fixture mutável compartilhado. Para um domínio com muitos dados cheio de cálculos derivados, "o banco é um valor que você passa para funções puras" remove uma categoria inteira de raciocínio sobre concorrência e consistência.

Consultas são Datalog — casamento de padrões sobre datoms, com joins expressos como variáveis lógicas compartilhadas. Para leituras hierárquicas há o Pull, que caminha por referências de forma declarativa:

```clojure
;; A lookup ref [:account/id uuid] identifies the entity; Pull shapes the result
(d/pull db
        '[:account/id :account/balance {:transfer/_from [:transfer/amount]}]
        [:account/id some-uuid])
```

## Viagem no Tempo e Auditoria, de Graça

Porque os fatos se acumulam e cada datom carrega sua transação, o histórico está *dentro do banco*, não é algo que você parafusa com uma tabela de auditoria e triggers. A mesma consulta roda contra um valor de banco do passado:

```clojure
;; What did the world look like on May 1st? Same query, older db value.
(d/q balances-query (d/as-of (d/db conn) #inst "2022-05-01"))

;; Full change history of an attribute — the ?added flag tells assert from retract
(d/q '[:find ?bal ?tx ?added
       :where [?a :account/balance ?bal ?tx ?added]]
     (d/history (d/db conn)))
```

Em um domínio com muitos dados e necessidades de compliance ou debugging, isso é enorme. "Qual era o saldo desta conta quando a transferência disputada aconteceu, e qual transação a mudou?" é uma consulta, não um projeto de arqueologia. Eu já fechei investigações de discrepância de dados em minutos que teriam sido dias de escavação de logs em um store mutável.

## Leituras Escalam; Escritas Passam por Uma Porta

A arquitetura do Datomic separa leituras de escritas de um jeito que é central para o trade-off. Leituras são servidas pelo motor de consulta com o índice disponível localmente e fortemente cacheado — então workloads read-heavy escalam adicionando capacidade de leitura, e consultas não contendem entre si. Escritas, porém, são serializadas por um único transactor para te dar transações ACID e uma ordenação globalmente consistente dos fatos.

Esse é o acordo em uma frase: **você ganha escritas consistentes, ordenadas e totalmente auditadas e leituras baratas e escaláveis, em troca de um teto no throughput de escrita.** Para os domínios em que eu usei — muitas leituras, escritas moderadas, e um requisito rígido de que os dados sejam corretos e rastreáveis — é uma troca excelente. Para uma mangueira de escritas (ingestão de eventos de alto volume, telemetria), é o formato errado e eu recorreria a outra coisa.

## Onde Ele Machuca — Honestamente

O Datomic não é um banco universal, e fingir o contrário é como as pessoas acabam infelizes com ele:

- **Não é um motor OLAP.** Agregar sobre dezenas de milhões de datoms para analytics não é para o que ele foi construído. Datalog consegue computar agregados, mas para varreduras analíticas pesadas você quer um store colunar alimentado por [ETL](/pt/posts/ddia-trade-offs-data-systems-architecture), não o Datomic fazendo dupla função.
- **O teto de escrita é real.** Um único transactor significa que o throughput de escrita tem um limite que você deveria validar contra o seu pico *antes* de se comprometer, não depois.
- **O armazenamento só cresce.** Acumulação é a feature e o custo — você mantém o histórico, então o volume de dados sobe, e isso muda como você planeja capacidade.
- **Deleção é uma operação de verdade.** Porque o modelo é só-acumular, honrar algo como o "direito ao esquecimento" da GDPR não é um `DELETE` — é excision, que é deliberada e pesada. Se seu domínio tem requisitos rígidos de deleção, projete para isso desde o começo.
- **Pull com cardinalidade-many pode explodir.** Um pull ingênuo através de uma referência de alto fan-out vai puxar muito mais do que você pretendia. Você aprende a limitar suas consultas.

## Quando Certo, Quando Errado

**Certo:** um domínio onde correção e histórico importam, leituras dominam escritas, e você valoriza poder perguntar "o que era verdade, e quando?". Livros-razão financeiros, qualquer coisa com obrigação de auditoria, sistemas onde "o banco como um valor passado para funções puras" simplifica um emaranhado de estado derivado.

**Errado:** ingestão de escritas de alto volume, workloads analytics-first que são na verdade OLAP, domínios pesados em hard-delete, ou um app CRUD pequeno onde PostgreSQL é mais simples e todo mundo no time já conhece.

O que o Datomic me ensinou dura mais do que o próprio Datomic: tratar dados como um log acumulativo de fatos imutáveis, e o estado atual como um *valor derivado desse log*, é uma lente que aparece por todo lugar em sistemas bem projetados — event sourcing, change data capture, a distinção entre sistemas de registro e dados derivados. O Datomic só faz disso o padrão em vez de algo que você monta na mão.

---
