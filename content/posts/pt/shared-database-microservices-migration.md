---
title: "Banco de Dados Compartilhado Entre Microsserviços: A Migração Que Você Não Está Pronto Para Fazer"
date: 2026-03-25
draft: false
description: "Quando múltiplos microsserviços compartilham um único banco PostgreSQL, migrations de schema viram um problema de coordenação. Veja como lidamos com isso no R10 com Django, Go e zero downtime."
tags: ["go", "python", "microservices", "database", "migrations", "architecture", "postgresql"]
---

Todo mundo diz que microsserviços devem ser donos dos seus dados. Bancos separados, fronteiras claras, sem estado compartilhado. Estão certos — eventualmente. Mas "eventualmente" pode demorar bastante quando você está migrando um monólito para microsserviços e o negócio não vai pausar pelos seus ideais arquiteturais.

No R10 Score, temos um monólito Python/Django gerenciando 570+ migrations em um único banco PostgreSQL. Dois microsserviços Go — notificações e odds — leem e escrevem nesse mesmo banco. Essa não é a arquitetura alvo. É a que nos permite entregar features enquanto a migração está em andamento.

Este post é sobre a parte que ninguém escreve: o que acontece quando três serviços precisam evoluir o mesmo schema de banco, e só um deles tem o framework de migrations do Django.

## A Situação

```
┌──────────────┐  ┌──────────────────┐  ┌───────────┐
│  r10-hub     │  │ r10-notifications │  │ r10-odds  │
│  (Python)    │  │ (Go)             │  │ (Go)      │
│  Django ORM  │  │ pgx/v5           │  │ pgx/v5    │
│  570+ migr.  │  │ raw SQL migr.    │  │ sem migr. │
└──────┬───────┘  └────────┬─────────┘  └─────┬─────┘
       │                   │                   │
       └───────────────────┼───────────────────┘
                           │
                    ┌──────▼──────┐
                    │  PostgreSQL  │
                    │  (shared)    │
                    └─────────────┘
```

Três serviços. Um banco de dados. Três abordagens diferentes para gerenciamento de schema:

- **r10-hub** (Python): Django migrations. 570 arquivos em `dao/migrations/`. Cada mudança de model gera uma migration numerada que o Django rastreia em `django_migrations`.
- **r10-notifications** (Go): Arquivos SQL puros em `migrations/`. Sem framework. Aplicados manualmente ou via scripts de deploy.
- **r10-odds** (Go): Nenhuma migration. Lê de tabelas existentes, escreve em `r10_odd_company`. Se a tabela existe, funciona.

Esse é o problema do banco compartilhado. Não "devemos compartilhar um banco?" — esse navio já zarpou. A pergunta é: como manter três codebases sem pisar no schema uma da outra?

## O Problema Real: Quem É Dono do Schema?

O Django acha que é dono do banco de dados. Ele rastreia cada migration em `django_migrations` e vai reclamar se a realidade não bater com o estado dele. Quando precisei adicionar uma tabela `r10_live_activity_token` para o serviço de notificações, tive uma escolha:

**Opção A**: Criar uma Django migration no r10-hub para uma tabela que o r10-hub não usa.

**Opção B**: Criar uma migration SQL pura no serviço Go, fora do conhecimento do Django.

Ambas as opções estão erradas. Opção A polui o monólito com schema de features que ele não é dono. Opção B cria tabelas que o Django desconhece, o que é aceitável — até alguém rodar `manage.py migrate` e a introspecção do Django se confundir.

Fui com a Opção B. Aqui está a migration:

```sql
-- migrations/001_create_live_activity_token.sql

CREATE TABLE r10_live_activity_token (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID        NOT NULL,
    match_id            UUID,
    device_token        VARCHAR(500) NOT NULL,
    push_to_start_token VARCHAR(500),
    activity_token      VARCHAR(500),
    state               VARCHAR(20)  NOT NULL DEFAULT 'registered',
    start_retry_count   INT          NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, device_token)
);
```

Perceba o que está faltando: **nenhuma foreign key**. `user_id` referencia `r10_user.id` conceitualmente, mas não tem constraint `REFERENCES`. `match_id` referencia `r10_match.id`, mas novamente — sem FK.

Isso é intencional.

## Regra 1: Sem Foreign Keys Entre Serviços

Foreign keys garantem integridade referencial no nível do banco. Parece bom até você perceber o que isso significa em um banco compartilhado com múltiplos donos:

- **Acoplamento de deploy.** Se o serviço de notificações adiciona uma FK para `r10_user`, fazer deploy de uma mudança de schema que altera `r10_user` no monólito agora requer coordenação com o serviço Go. O deploy de um serviço pode bloquear o de outro.
- **Cascatas surpresa.** Um `DELETE FROM r10_user` com `ON DELETE CASCADE` apagaria tokens de live activity. O time de notificações não assinou para isso.
- **Ordenação de migrations.** O planejador de migrations do Django assume que ele controla os alvos de FK. Uma FK apontando para uma tabela gerenciada por outro serviço cria uma dependência invisível que nenhuma ferramenta rastreia.

A regra: **se dois serviços acessam o mesmo banco, eles se comunicam por UUIDs, não por foreign keys.** O serviço Go armazena `user_id` como um UUID opaco. Se esse usuário não existe em `r10_user`, a query retorna sem resultados. A aplicação trata. O banco não impõe.

É menos seguro? Sim. Linhas órfãs são possíveis. Aceitamos esse tradeoff porque a alternativa — acoplar cada deploy entre três serviços — é pior durante uma migração.

## Regra 2: Tabelas Novas Pertencem ao Serviço Que Precisa Delas

Quando o serviço de notificações precisou armazenar tokens de live activity, a migration ficou no repositório de notificações. Não no r10-hub. O raciocínio:

- O serviço Go é o único consumidor de `r10_live_activity_token`
- O schema da tabela espelha structs Go, não models Django
- O time do serviço controla quando e como a migration é executada
- O Django não precisa saber que essa tabela existe

Isso cria uma divisão: algumas tabelas `r10_*` são gerenciadas por Django migrations, outras por SQL puro. É bagunçado, mas mapeia a realidade. O monólito é dono das tabelas que criou. Novos serviços são donos das tabelas que criam. Tabelas compartilhadas (como `r10_device`, `r10_notification`, `r10_topic`) continuam sob controle do Django porque o monólito as criou e ainda escreve nelas.

## Regra 3: Leia de Tabelas Compartilhadas, Não as Altere

O serviço Go de notificações lê de diversas tabelas gerenciadas pelo Django:

```go
// Lendo da tabela r10_user do Django
query := `SELECT id, role, language FROM r10_user WHERE id = $1`

// Lendo da tabela r10_team do Django
query := `SELECT id FROM r10_team WHERE id = $1`

// Lendo da tabela r10_notification do Django
query := `
    SELECT kind, is_enabled
    FROM r10_notification
    WHERE user_id = $1
`
```

O serviço Go também **escreve** em `r10_notification` (ativando/desativando preferências de notificação) e `r10_device` (registro de dispositivos). Essa costumava ser a parte bagunçada — dois serviços escrevendo nas mesmas tabelas.

Resolvemos isso. A [migração strangler fig](/posts/strangler-fig-python-to-go-rest-api) moveu todos os endpoints REST de notificação de Python para Go. O app mobile agora acessa o serviço Go diretamente. Python não escreve mais em `r10_notification`, `r10_device`, ou qualquer tabela relacionada a notificações. O serviço Go é o único escritor.

Mas antes dessa migração, convivemos com escritas duplas por meses. Dois serviços escrevendo na mesma tabela significa:

- Mudanças de schema em tabelas compartilhadas **devem** passar por Django migrations no r10-hub, porque o Django rastreia o estado das migrations e vai reclamar se a realidade não bater
- O serviço Go precisa se adaptar a mudanças de schema que ele não iniciou
- Renomeações de colunas, mudanças de tipo ou adições de constraints podem quebrar o serviço Go silenciosamente — nenhum compilador pega uma coluna renomeada em uma query SQL pura

O processo é o mesmo tendo escritas duplas ou não: se você precisa mudar uma tabela compartilhada, verifique quem mais lê dela. `grep` entre repositórios pelo nome da tabela. Não existe tooling para isso — é disciplina.

## Regra 4: Prefixe Tudo, Não Colida em Nada

Todas as tabelas R10 usam o prefixo `r10_`. Isso é uma convenção do Django (`db_table = 'r10_notification'`), e carregamos para as migrations Go. Isso significa:

- Sem colisão de nomes entre nossa aplicação e extensões PostgreSQL ou outros schemas
- Fácil identificar quais tabelas pertencem ao R10 vs. ferramentas de terceiros
- Um simples `\dt r10_*` no psql mostra todo o schema da aplicação

A migration Go segue a mesma convenção: `r10_live_activity_token`. Se algum dia dividirmos o banco, o prefixo torna trivial identificar quais tabelas vão para onde.

## O Que Realmente Dá Errado

### A Renomeação de Coluna

A Django migration 0400 renomeou `UserDevice` para `Device`:

```python
migrations.RenameModel(
    old_name='UserDevice',
    new_name='Device',
)
```

O Django trata isso de forma transparente — a tabela continua `r10_device`, mas a referência do model muda. Nenhuma mudança de schema chega ao PostgreSQL. Mas se o Django tivesse renomeado a **tabela** (o que ele pode fazer), o serviço Go teria quebrado na próxima query. Tivemos sorte. A lição: fique de olho nas Django migrations para operações `RenameModel` e `AlterModelTable`.

### A Migration Que Não Existe

O serviço de odds tem zero migrations. Ele lê de `r10_match` e escreve em `r10_odd_company`. Ambas as tabelas foram criadas pelo Django há tempos. O serviço de odds confia que elas existem. Se alguém dropar `r10_odd_company`, o serviço falha em runtime com um erro `relation does not exist`. Nenhum framework de migration pega isso porque o serviço de odds não tem um.

Isso funciona porque `r10_odd_company` é estável — seu schema não muda há meses. Para uma tabela que muda frequentemente, você ia querer pelo menos uma validação de schema no startup. Ainda não temos isso.

### O Problema "Quem Aplicou Isso?"

O Django rastreia migrations em `django_migrations`. O serviço Go não rastreia nada — o arquivo SQL é aplicado manualmente ou em um script de deploy. Se você precisa saber se `001_create_live_activity_token.sql` foi aplicada em produção, você verifica se a tabela existe:

```sql
SELECT EXISTS (
    SELECT FROM information_schema.tables
    WHERE table_name = 'r10_live_activity_token'
);
```

Não existe histórico de migrations para o serviço Go. Para uma migration, tudo bem. Com dez migrations, você vai querer golang-migrate ou goose. Estamos em uma. Vamos resolver quando chegar lá.

## A Estratégia de Saída

O banco compartilhado é um estado de transição. A arquitetura alvo tem cada serviço com seu próprio banco. O caminho daqui até lá:

1. **Identificar ownership de tabelas.** Quais tabelas cada serviço realmente precisa? `r10_live_activity_token` é só do notificações. `r10_match` é compartilhada por todos. `r10_odd_company` é só do odds.
2. **Eliminar escritas compartilhadas.** O passo mais difícil. Dois serviços escrevendo na mesma tabela é onde bugs se escondem. Já resolvemos isso para notificações — a migração strangler fig moveu todos os endpoints REST de Python para Go, tornando o serviço Go o único escritor das tabelas de notificação. Python ainda lê algumas delas, mas isso é seguro.
3. **Replicar dados read-only.** Os serviços Go precisam de `r10_user` e `r10_match` para validação. Esses dados poderiam vir de uma réplica de leitura, stream CDC, ou uma chamada de API em vez de acesso direto à tabela.
4. **Dividir o banco.** Quando um serviço só acessa suas próprias tabelas, extraia essas tabelas para um banco dedicado. O prefixo `r10_` torna o corte óbvio.

Estamos entre os passos 2 e 3. Notificações tem um único escritor. Odds lê de tabelas compartilhadas mas não altera seus schemas. O próximo desafio é desacoplar as dependências de leitura — os serviços Go ainda consultam `r10_user` e `r10_match` diretamente.

## Diretrizes Práticas

Se você está nessa situação — múltiplos serviços, um banco, migração em andamento — aqui está o que funcionou para nós:

**Faça:**
- Mantenha tabelas novas no serviço que é dono delas
- Use UUIDs como referências entre serviços sem foreign keys
- Prefixe nomes de tabela de forma consistente
- Grep entre repositórios antes de mudar schemas de tabelas compartilhadas
- Faça migrations Go idempotentes (`CREATE TABLE IF NOT EXISTS`, `ON CONFLICT`)

**Não faça:**
- Adicionar foreign keys entre tabelas de serviços diferentes
- Criar Django migrations para tabelas que o monólito não usa
- Assumir que o estado de migrations do Django reflete o schema completo do banco
- Mudar colunas de tabelas compartilhadas sem verificar consumidores downstream
- Construir tooling elaborado de migrations para um estado de transição

O banco compartilhado é um compromisso pragmático. Ele permite extrair microsserviços incrementalmente sem resolver o problema de dados distribuídos no dia um. Não é limpo, não é o que os diagramas de arquitetura mostram, e funciona.

O importante é saber que é temporário — e construir suas migrations de forma que sejam fáceis de desemaranhar quando você estiver pronto para a divisão real.
