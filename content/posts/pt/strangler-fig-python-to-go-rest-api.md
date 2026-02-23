---
title: "Strangler Fig na Prática: Migrando uma REST API de Python para Go Sem Downtime"
date: 2026-02-23
draft: false
description: "
Como eliminamos o intermediário Python adicionando endpoints REST diretamente a um microsserviço Go existente — reutilizando sua camada de serviços, repositórios e padrões assíncronos — enquanto o monólito continuava servindo mais de 100 endpoints.
"
tags: ["go", "python", "strangler-fig", "microservices", "rest-api", "architecture", "jwt", "chi"]
---

Migrar de um monólito para microsserviços raramente é uma quebra limpa. Você não pode reescrever tudo de uma vez, e não pode se dar ao luxo de ter downtime. O padrão strangler fig permite fazer isso de forma incremental — extrair um bounded context por vez, rotear tráfego para o novo serviço, e deixar o código antigo atrofiar naturalmente.

Este post apresenta um exemplo real: extraindo o domínio de notificações de um monólito Python/FastAPI para um serviço Go, adicionando handlers REST que reutilizam a mesma camada de serviços que os handlers gRPC já usam. Sem novos serviços, sem novos containers — apenas um novo adaptador de protocolo em uma base de código existente.

## O Problema: Python Como Intermediário

Nosso aplicativo mobile (R10 Score) tem um monólito Python/FastAPI que faz tudo: autenticação, dados de partidas, competições, estatísticas de jogadores, compras in-app e notificações push. Os endpoints relacionados a notificações são estes:

```
PUT    /api/v1/users/device/              → registrar token do dispositivo
POST   /api/v1/users/devices/delete/      → desregistrar dispositivo
GET    /api/v1/notifications/             → listar preferências de notificação
PUT    /api/v1/notifications/{kind}/      → ativar um tipo de notificação
DELETE /api/v1/notifications/{kind}/      → desativar um tipo de notificação
GET    /api/v1/users/heart-team/          → buscar time do coração
POST   /api/v1/users/heart-team/{id}/     → definir time do coração
DELETE /api/v1/users/heart-team/          → remover time do coração
POST   /api/v1/matches/{id}/notification/ → assinar notificações de partida
DELETE /api/v1/matches/{id}/notification/ → cancelar assinatura de partida
```

O problema: o serviço Go já lidava com toda a lógica de negócio de notificações via gRPC. Python era apenas um proxy. Cada requisição mobile passava por um hop desnecessário:

```
Mobile → Python REST → gRPC → Go → DB/SNS
```

Dois serviços, duas linguagens, dois ORMs acessando as mesmas tabelas PostgreSQL. Python validava a requisição, convertia para uma chamada gRPC e encaminhava. O trabalho real acontecia no Go. Estávamos pagando por serialização, latência de rede e complexidade operacional em troca de... nada.

## A Mudança Arquitetural

O backend Go de notificações já tinha um chi router na porta 8081, uma camada de serviços (`DeviceService`, `UserSubscriptionService`, `SubscriptionService`) usada pelos handlers gRPC, uma camada de repositórios com acesso PostgreSQL, e integração com SNS para entrega push.

O plano: adicionar handlers REST que chamam os mesmos serviços que os handlers gRPC chamam. Sem nova camada de serviços, sem nova lógica de negócio — apenas uma nova porta de entrada.

```
ANTES:   Mobile → Python REST → gRPC → Go services → DB/SNS
DEPOIS:  Mobile → Go REST → Go services → DB/SNS
         Dataloader → gRPC → Go services → DB/SNS  (sem alteração)
```

## Autenticação JWT: JWKS em Vez de Chamadas de API

O serviço Python autenticava chamando a API de validação do FusionAuth a cada requisição (com cache de 15 minutos). O serviço Go faz diferente: busca as chaves públicas do FusionAuth uma vez de `/.well-known/jwks.json`, cacheia em memória atrás de um `sync.RWMutex`, e atualiza a cada hora em uma goroutine de background. Sem chamada HTTP por requisição.

Um middleware chi extrai o header `Authorization`, valida a assinatura JWT com a chave RSA cacheada, e armazena o usuário autenticado (ID, role, idioma) no contexto da requisição. Qualquer handler downstream simplesmente chama `auth.UserFromContext(r.Context())`. Parse uma vez, use em todo lugar.

A diferença de performance é significativa: verificação de assinatura RSA leva microssegundos. Um round-trip HTTP para a API de validação do FusionAuth leva milissegundos na melhor das hipóteses — e introduz uma dependência de runtime na disponibilidade do FusionAuth.

## Novos Repositórios: Lendo Tabelas do Django a Partir do Go

O serviço Go já tinha repositórios para dispositivos, assinaturas, tópicos e partidas. Mas os endpoints REST precisavam de queries de validação que os handlers gRPC nunca precisaram — porque Python fazia essa validação antes de encaminhar a chamada gRPC.

Adicionamos três novos repositórios (`UserRepo`, `FavoriteRepo`, `TeamRepo`) que leem tabelas gerenciadas pelo Django: `r10_user` para roles e dados do time do coração, `r10_favorite_match` e `r10_subscribed_match` para assinaturas, `r10_team` para validação de existência. Sem migração de schema, sem sincronização de dados — Go lê e escreve nas mesmas tabelas que Python.

O `NotificationRepo` existente também precisou de operações de escrita. Antes, ele apenas lia tipos de notificação habilitados (para lógica de assinatura gRPC). Agora habilita, desabilita e atualiza preferências em lote. Todas as escritas usam `ON CONFLICT` para idempotência — REST é stateless, clientes podem retentar, e o banco lida com isso graciosamente:

```go
func (r *NotificationRepository) EnableKind(ctx context.Context, userID uuid.UUID, kind models.TopicKind) error {
    query := `
        INSERT INTO r10_notification (id, user_id, kind, is_enabled, created_at, updated_at)
        VALUES (gen_random_uuid(), $1, $2, true, NOW(), NOW())
        ON CONFLICT (user_id, kind) DO UPDATE SET is_enabled = true, updated_at = NOW()
    `
    _, err := r.pool.Exec(ctx, query, userID, string(kind))
    return err
}
```

## Handlers REST: Adaptadores Finos de Protocolo

A struct do handler mantém referências a repositórios e serviços — sem lógica de negócio, apenas tradução de protocolo. O registro de rotas coloca todos os 11 endpoints sob `/api/v1/` com autenticação JWT:

```go
func RegisterRoutes(r chi.Router, h *Handler, authMW *auth.Middleware) {
    r.Route("/api/v1", func(r chi.Router) {
        r.Use(authMW.Authenticate)

        r.Put("/devices", h.RegisterDevice)
        r.Delete("/devices", h.UnregisterDevice)

        r.Get("/notifications", h.ListNotifications)
        r.Put("/notifications", h.BulkUpdateNotifications)
        r.Put("/notifications/{kind}", h.EnableNotification)
        r.Delete("/notifications/{kind}", h.DisableNotification)

        r.Post("/matches/{matchId}/subscribe", h.SubscribeMatch)
        r.Delete("/matches/{matchId}/subscribe", h.UnsubscribeMatch)

        r.Post("/heart-team/{teamId}", h.SetHeartTeam)
        r.Delete("/heart-team", h.RemoveHeartTeam)
        r.Get("/heart-team", h.GetHeartTeam)
    })
}
```

Os paths são intencionalmente diferentes do estilo `/api/v1.8/users/device/` do Python. Paths novos significam que podemos rodar ambas as APIs simultaneamente sem conflitos, embora exija um release mobile coordenado.

### Sync vs. Async: Uma Divisão Estratégica

Os handlers seguem um padrão consistente: **escritas síncronas no banco, operações SNS assíncronas**. Usuários esperam feedback imediato ao mudar preferências. Assinaturas de tópicos SNS podem ser eventualmente consistentes.

```go
func (h *Handler) EnableNotification(w http.ResponseWriter, r *http.Request) {
    user := auth.UserFromContext(r.Context())
    kind := models.TopicKind(strings.ToUpper(chi.URLParam(r, "kind")))

    // Sync: escreve no banco
    if err := h.notifRepo.EnableKind(r.Context(), user.ID, kind); err != nil {
        writeError(w, http.StatusInternalServerError, "failed to enable notification")
        return
    }

    // Async: atualiza assinaturas SNS
    userID := user.ID
    h.runAsync(func(ctx context.Context) {
        h.userSubSvc.SubscribeToNotificationKind(ctx, userID, kind)
    })

    writeOK(w, map[string]string{"status": "ok"})
}
```

O método `runAsync` usa um semáforo baseado em channel (capacidade 1000) — mesmo padrão que os handlers gRPC usam. Se o semáforo estiver cheio, o handler bloqueia brevemente em vez de criar goroutines ilimitadas. Cada tarefa assíncrona recebe um contexto novo com timeout de 5 minutos, desvinculado do ciclo de vida da requisição HTTP.

### Atualizações Bulk Declarativas

A API Python exigia que o app mobile habilitasse/desabilitasse tipos de notificação um por vez. A API Go adiciona um endpoint bulk declarativo: `PUT /api/v1/notifications` aceita `{"enabled_kinds": ["GOAL", "MATCH_START"]}`. O handler busca o estado atual, calcula o diff (o que habilitar, o que desabilitar), escreve o delta em uma única transação, e depois dispara atualizações assíncronas de assinaturas SNS. Uma requisição em vez de cinco chamadas sequenciais.

### Lógica de Validação Portada

Várias regras de negócio que viviam no Python precisaram ser portadas para os handlers REST: limites de notificação para guests (máximo configurável de tipos), cooldown do time do coração (período de 10 dias entre mudanças), exclusão mútua de lembretes (apenas um intervalo de lembrete ativo por vez), e verificação de existência de time antes de definir o time do coração.

Essa validação vive nos handlers REST, não na camada de serviços. Os serviços permanecem agnósticos ao transporte — eles não sabem sobre roles de usuário, cooldowns ou códigos de status HTTP. Os handlers gRPC têm sua própria validação (delegada do Python anteriormente). Isso mantém ambos os adaptadores de protocolo independentes.

## Onde gRPC e REST se Encontram

A decisão de design crítica que faz tudo funcionar: **ambos os adaptadores de protocolo chamam os mesmos serviços**.

```go
// Handler REST
err := h.deviceService.RegisterDevice(r.Context(), service.RegisterDeviceRequest{
    UserID:      user.ID,
    DeviceToken: req.DeviceToken,
    HeartTeamID: parseUUID(req.HeartTeamID),
})

// Handler gRPC
err := h.deps.DeviceService.RegisterDevice(ctx, service.RegisterDeviceRequest{
    UserID:      userID,
    DeviceToken: req.DeviceToken,
    HeartTeamID: heartTeamID,
})
```

Mesmo serviço, mesma struct de request, mesmo comportamento. `DeviceService.RegisterDevice` lida com o ciclo de vida complexo: deduplicando tokens de dispositivo entre usuários, criando endpoints SNS, assinando tipos de notificação habilitados — tudo em uma chamada. Ambos os handlers são wrappers finos que parseiam seus respectivos protocolos, chamam o serviço e formatam a resposta.

O servidor gRPC continua na porta 50052 para serviços internos como o dataloader. O servidor HTTP na porta 8081 agora serve tanto health checks quanto a REST API. Mesmo processo, mesmos serviços, dois protocolos.

## O Que Quebrou no Caminho

**Split de versão de módulo Go.** O Swagger UI carregava normalmente, mas `/swagger/doc.json` retornava 500. Causa raiz: `docs.go` importava `github.com/swaggo/swag/v2` (RC), mas o handler HTTP importava `github.com/swaggo/swag` (v1). Em Go modules, esses são pacotes completamente separados com registros globais independentes. Os docs registravam a spec no v2; o handler lia do v1; não encontrava nada; 500. Correção: regenerar com o CLI v1, fixar em v1.16.4.

**Endpoint JWKS errado por ambiente.** A configuração padrão apontava para o FusionAuth de produção, mas tokens de dev são emitidos por uma instância diferente com pares de chaves RSA diferentes. Mesmo formato de `kid` — a verificação de assinatura falhava silenciosamente. Correção: tornar `fusionauth_url` uma variável de configuração por ambiente.

## O Padrão Strangler Fig

Veja como a arquitetura fica durante a migração:

```
                    ┌───────────────────┐
                    │  Clientes Mobile   │
                    └───┬───────────┬───┘
                        │           │
        Notificações    │           │ Todo o resto
        (11 endpoints)  │           │ (100+ endpoints)
                        │           │
                   ┌────▼───┐  ┌───▼──────────┐
                   │   Go   │  │ Python/FastAPI│
                   │  REST  │  │   Monólito    │
                   │  API   │  └──────┬────────┘
                   └────┬───┘         │
                        │             │ (gRPC, sendo descontinuado)
                   ┌────▼─────────────▼───┐
                   │  Camada de Serviços   │
                   │  Go                   │
                   │   DeviceService       │
                   │   SubscriptionService │
                   │   UserSubService      │
                   └────┬─────────┬───────┘
                        │         │
                   ┌────▼──┐ ┌───▼──┐
                   │  RDS  │ │  SNS │
                   └───────┘ └──────┘
```

Ambos os caminhos de protocolo convergem na camada de serviços Go. Durante a migração, ambos estão ativos. Uma vez que os clientes mobile migrem para os endpoints REST Go, o caminho Python→gRPC se torna código morto.

### O que o serviço Go possui agora

```
| Método   | Path                                  | Propósito                           |
|----------|---------------------------------------|-------------------------------------|
| PUT      | /api/v1/devices                       | Registrar dispositivo para push     |
| DELETE   | /api/v1/devices                       | Desregistrar dispositivo            |
| GET      | /api/v1/notifications                 | Listar preferências de notificação  |
| PUT      | /api/v1/notifications                 | Atualização bulk de preferências    |
| PUT      | /api/v1/notifications/{kind}          | Habilitar um tipo de notificação    |
| DELETE   | /api/v1/notifications/{kind}          | Desabilitar um tipo de notificação  |
| GET      | /api/v1/heart-team                    | Buscar time do coração              |
| POST     | /api/v1/heart-team/{teamId}           | Definir time do coração             |
| DELETE   | /api/v1/heart-team                    | Remover time do coração             |
| POST     | /api/v1/matches/{matchId}/subscribe   | Assinar partida                     |
| DELETE   | /api/v1/matches/{matchId}/subscribe   | Cancelar assinatura de partida      |
```

### O que o monólito Python ainda possui

Autenticação, gerenciamento de usuários, dados de partidas (15+ endpoints), competições, times, jogadores, compras in-app, módulos de treinamento e dezenas de outros recursos. O serviço Go é um corte vertical focado — um bounded context, completamente sob sua responsabilidade.

## Principais Aprendizados

**1. A camada de serviços é a verdadeira API.** gRPC e REST são apenas adaptadores de protocolo. Se seus serviços são agnósticos ao transporte, adicionar um novo protocolo é questão de escrever handlers finos. Adicionamos 11 endpoints REST sem tocar em uma única linha de lógica de negócio.

**2. Validação pertence ao handler, não ao serviço.** Limites de guest, cooldown do time do coração, exclusão mútua de lembretes — são preocupações da camada de transporte que precisam do contexto do usuário autenticado. Mantê-las fora da camada de serviços significa que handlers gRPC podem ter suas próprias regras de validação sem conflito.

**3. Escritas síncronas, efeitos assíncronos.** Usuários querem feedback imediato quando mudam uma preferência. Assinaturas SNS podem ser eventualmente consistentes. Essa divisão mantém tempos de resposta rápidos enquanto lida com o fan-out custoso em goroutines limitadas em background.

**4. JWKS supera validação por API.** Cachear chaves públicas RSA localmente e fazer verificação de assinatura in-process é mais rápido (microssegundos vs. milissegundos) e remove uma dependência de runtime. O serviço Python chamava o endpoint HTTP de validação do FusionAuth a cada requisição. O serviço Go não depende do FusionAuth estar disponível após a inicialização.

**5. Mesmo banco, sem migração.** Os repositórios Go leem e escrevem em tabelas gerenciadas pelo Django (`r10_user`, `r10_notification`, `r10_team`, `r10_subscribed_match`). Sem migração de schema, sem sincronização de dados, sem problema de dual-write. Ambos os serviços compartilham o banco — o que é aceitável para uma janela de migração limitada.

**6. O monólito não é o inimigo.** Após esta migração, o monólito Python ainda serve 100+ endpoints. E tudo bem. O padrão strangler fig não exige matar a árvore hospedeira — apenas permite que o novo crescimento tome conta das partes que se beneficiam disso. Notificações precisavam do modelo de concorrência do Go e da integração gRPC. Detalhes de partidas e gerenciamento de usuários funcionam perfeitamente bem em Python.

## Estratégia de Migração

1. Deploy do serviço Go com endpoints REST ao lado do serviço Python existente
2. Time mobile atualiza o app para apontar endpoints de notificação para o serviço Go
3. Monitorar ambos os serviços para paridade (diff de logs, taxas de erro)
4. Quando a adoção mobile atingir 100%, desabilitar endpoints de notificação no Python
5. Remover código de cliente gRPC do Python; remover controllers relacionados a notificação
6. Eventualmente aposentar o Python por completo — quando todos os endpoints forem migrados

Sem migração big bang. Sem flag day. Ambos os serviços coexistem, e o caminho antigo atrofia conforme o tráfego migra para o novo.

A figueira continua crescendo.
