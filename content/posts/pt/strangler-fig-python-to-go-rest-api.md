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

O backend Go de notificações já tinha:
- Um **chi router** servindo health checks na porta 8081
- Uma **camada de serviços** (`DeviceService`, `UserSubscriptionService`, `SubscriptionService`) usada pelos handlers gRPC
- Uma **camada de repositórios** com acesso PostgreSQL via `pgxpool`
- **Integração com SNS** para entrega de notificações push

O plano: adicionar handlers REST que chamam os mesmos serviços que os handlers gRPC chamam. Sem nova camada de serviços, sem nova lógica de negócio — apenas uma nova porta de entrada.

```
ANTES:   Mobile → Python REST → gRPC → Go services → DB/SNS
DEPOIS:  Mobile → Go REST → Go services → DB/SNS
         Dataloader → gRPC → Go services → DB/SNS  (sem alteração)
```

## Fase 1: Autenticação JWT

O serviço Python autenticava chamando a API de validação do FusionAuth a cada requisição (com cache de 15 minutos). O serviço Go precisava autenticar diretamente.

Escolhemos **validação JWT local via JWKS** — buscar as chaves públicas do FusionAuth uma vez, cachear em memória, atualizar a cada hora. Sem chamada HTTP por requisição.

```go
// internal/auth/jwt.go
type JWKSProvider struct {
    jwksURL         string
    refreshInterval time.Duration
    mu              sync.RWMutex
    keys            map[string]*rsa.PublicKey // kid → chave pública
}

func (p *JWKSProvider) Keyfunc(token *jwt.Token) (any, error) {
    kid, ok := token.Header["kid"].(string)
    if !ok {
        return nil, fmt.Errorf("token missing kid header")
    }

    p.mu.RLock()
    key, exists := p.keys[kid]
    p.mu.RUnlock()

    if !exists {
        return nil, fmt.Errorf("unknown kid: %s", kid)
    }
    return key, nil
}
```

O middleware extrai o JWT, valida com a chave pública cacheada e insere o usuário autenticado no contexto da requisição:

```go
// internal/auth/middleware.go
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        authHeader := r.Header.Get("Authorization")
        tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

        token, err := jwt.Parse(tokenStr, m.jwks.Keyfunc,
            jwt.WithValidMethods([]string{"RS256"}),
        )
        if err != nil {
            writeError(w, http.StatusUnauthorized, "invalid token")
            return
        }

        claims := token.Claims.(jwt.MapClaims)
        userID, _ := uuid.Parse(claims["sub"].(string))

        user := &AuthUser{
            ID:       userID,
            Role:     extractRole(claims),
            Language: extractLanguage(claims),
        }

        ctx := ContextWithUser(r.Context(), user)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

Qualquer handler downstream simplesmente chama `auth.UserFromContext(r.Context())` para obter o usuário autenticado. Parse uma vez, use em todo lugar.

A diferença de performance é significativa: verificação de assinatura RSA leva microssegundos. Um round-trip HTTP para a API de validação do FusionAuth leva milissegundos na melhor das hipóteses — e introduz uma dependência de runtime na disponibilidade do FusionAuth.

## Fase 2: Novos Repositórios para Validação

O serviço Go já tinha repositórios para dispositivos, assinaturas, tópicos e partidas. Mas os endpoints REST precisavam de queries de validação que os handlers gRPC nunca precisaram — porque Python fazia essa validação antes de encaminhar a chamada gRPC.

Três novos repositórios, todos lendo de tabelas gerenciadas pelo Django:

```go
// internal/repository/user.go — queries de time do coração
type UserRepo interface {
    GetByID(ctx context.Context, id uuid.UUID) (*User, error)
    GetHeartTeam(ctx context.Context, userID uuid.UUID) (*HeartTeamInfo, error)
    SetHeartTeam(ctx context.Context, userID, teamID uuid.UUID) error
    ClearHeartTeam(ctx context.Context, userID uuid.UUID) error
    GetLastHeartTeamDeletion(ctx context.Context, userID uuid.UUID) (*time.Time, error)
    SetLastHeartTeamDeletion(ctx context.Context, userID uuid.UUID, t time.Time) error
}

// internal/repository/favorite.go — queries de assinatura de partida
type FavoriteRepo interface {
    AddSubscribedMatch(ctx context.Context, userID, matchID uuid.UUID) error
    RemoveSubscribedMatch(ctx context.Context, userID, matchID uuid.UUID) error
    IsSubscribedMatch(ctx context.Context, userID, matchID uuid.UUID) (bool, error)
}

// internal/repository/team.go — validação de existência de time
type TeamRepo interface {
    Exists(ctx context.Context, teamID uuid.UUID) (bool, error)
    GetByID(ctx context.Context, teamID uuid.UUID) (*Team, error)
}
```

O `NotificationRepo` existente também precisou de operações de escrita. Antes, ele apenas lia os tipos habilitados (para a lógica de assinatura gRPC). Agora precisa habilitar, desabilitar e atualizar preferências em lote:

```go
// internal/repository/notification.go — adicionado para REST
EnableKind(ctx context.Context, userID uuid.UUID, kind TopicKind) error
DisableKind(ctx context.Context, userID uuid.UUID, kind TopicKind) error
BulkSetKinds(ctx context.Context, userID uuid.UUID, toEnable, toDisable []TopicKind) error
DisableReminderKindsExcept(ctx context.Context, userID uuid.UUID, except TopicKind) error
```

Todas as operações de escrita usam cláusulas `ON CONFLICT` para idempotência — REST é stateless, clientes podem retentar, e o banco deve lidar com isso graciosamente:

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

## Fase 3: Handlers REST — Adaptadores Finos de Protocolo

A struct do handler mantém referências a repositórios e serviços. Sem lógica de negócio aqui — apenas tradução de protocolo:

```go
// internal/rest/handlers.go
type Handler struct {
    userRepo      repository.UserRepo
    favoriteRepo  repository.FavoriteRepo
    teamRepo      repository.TeamRepo
    notifRepo     repository.NotificationRepo
    deviceService *service.DeviceService
    userSubSvc    *service.UserSubscriptionService
    subSvc        *service.SubscriptionService
    logger        *zap.Logger
    cfg           config.RESTConfig
    asyncSem      chan struct{}
}
```

O registro de rotas é direto — todos os endpoints sob `/api/v1/` com autenticação JWT:

```go
// internal/rest/routes.go
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

Repare que os paths são novos — não seguem o estilo `/api/v1.8/users/device/` do Python. Isso foi intencional. Paths novos significam que podemos rodar ambas as APIs simultaneamente sem conflitos, embora exija um release mobile coordenado.

### Sync vs. Async: Uma Divisão Estratégica

Os handlers seguem um padrão consistente: **escritas síncronas no banco, operações SNS assíncronas**. Usuários esperam feedback imediato ao mudar preferências. Assinaturas de tópicos SNS podem ser eventualmente consistentes.

O registro de dispositivo é totalmente síncrono — o app mobile lê o registro do dispositivo imediatamente após:

```go
// internal/rest/device.go
func (h *Handler) RegisterDevice(w http.ResponseWriter, r *http.Request) {
    user := auth.UserFromContext(r.Context())

    var req registerDeviceRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    err := h.deviceService.RegisterDevice(r.Context(), service.RegisterDeviceRequest{
        UserID:      user.ID,
        DeviceToken: req.DeviceToken,
        HeartTeamID: parseUUID(req.HeartTeamID),
    })
    if err != nil {
        writeError(w, http.StatusInternalServerError, "device registration failed")
        return
    }

    writeOK(w, map[string]string{"status": "ok"})
}
```

Mudanças de tipo de notificação escrevem no banco sincronamente, depois disparam a atualização de assinatura SNS em fire-and-forget:

```go
// internal/rest/notification.go (simplificado)
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

O padrão `runAsync` usa um semáforo baseado em channel — mesmo padrão que os handlers gRPC usam — para limitar goroutines concorrentes:

```go
func (h *Handler) runAsync(fn func(ctx context.Context)) {
    h.asyncSem <- struct{}{}
    go func() {
        defer func() { <-h.asyncSem }()
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
        defer cancel()
        fn(ctx)
    }()
}
```

Isso previne explosão de goroutines sob carga. Se o semáforo estiver cheio (1000 tarefas assíncronas concorrentes), o handler bloqueia brevemente no envio em vez de criar goroutines ilimitadas.

### Atualizações Bulk Declarativas

A API Python exigia que o app mobile habilitasse/desabilitasse tipos um por vez. A API Go adiciona um endpoint bulk declarativo — envie o conjunto desejado de tipos habilitados, deixe o servidor calcular o diff:

```go
// PUT /api/v1/notifications
// Body: {"enabled_kinds": ["GOAL", "MATCH_START", "REMINDER_15_MINUTES_BEFORE"]}

func (h *Handler) BulkUpdateNotifications(w http.ResponseWriter, r *http.Request) {
    user := auth.UserFromContext(r.Context())

    var req bulkNotificationsRequest
    json.NewDecoder(r.Body).Decode(&req)

    // Parsear estado desejado
    desiredSet := make(map[models.TopicKind]bool)
    for _, raw := range req.EnabledKinds {
        desiredSet[models.TopicKind(strings.ToUpper(raw))] = true
    }

    // Buscar estado atual
    currentKinds, _ := h.notifRepo.GetEnabledKindsByUser(r.Context(), user.ID)
    currentSet := make(map[models.TopicKind]bool)
    for _, k := range currentKinds {
        currentSet[k] = true
    }

    // Calcular diff
    var toEnable, toDisable []models.TopicKind
    for kind := range desiredSet {
        if !currentSet[kind] {
            toEnable = append(toEnable, kind)
        }
    }
    for kind := range currentSet {
        if !desiredSet[kind] {
            toDisable = append(toDisable, kind)
        }
    }

    // Transação única
    h.notifRepo.BulkSetKinds(r.Context(), user.ID, toEnable, toDisable)

    // Async: assinaturas SNS seguem
    h.runAsync(func(ctx context.Context) {
        for _, kind := range toEnable {
            h.userSubSvc.SubscribeToNotificationKind(ctx, userID, kind)
        }
        for _, kind := range toDisable {
            h.userSubSvc.UnsubscribeFromNotificationKind(ctx, userID, kind)
        }
    })

    writeOK(w, bulkNotificationsResponse{Enabled: toEnable, Disabled: toDisable})
}
```

Isso elimina múltiplos round-trips no lado mobile. Em vez de 5 chamadas sequenciais de enable/disable, o cliente envia uma requisição com o estado desejado.

### Lógica de Validação Portada

Várias regras de negócio que viviam no Python precisaram ser portadas:

**Limite de notificações para guest** — convidados só podem habilitar N tipos de notificação:

```go
if user.Role == "GUEST" && h.cfg.MaxGuestNotificationKinds > 0 {
    count, _ := h.notifRepo.CountEnabledByUser(r.Context(), user.ID)
    if count >= h.cfg.MaxGuestNotificationKinds {
        writeError(w, http.StatusForbidden, "guest notification limit reached")
        return
    }
}
```

**Cooldown do time do coração** — usuários não podem trocar de time favorito mais de uma vez a cada 10 dias:

```go
if h.cfg.HeartTeamDeletionCooldown > 0 {
    lastDeletion, _ := h.userRepo.GetLastHeartTeamDeletion(r.Context(), user.ID)
    if lastDeletion != nil {
        cooldownEnd := lastDeletion.Add(h.cfg.HeartTeamDeletionCooldown)
        if time.Now().Before(cooldownEnd) {
            remaining := time.Until(cooldownEnd)
            writeError(w, http.StatusTooManyRequests,
                fmt.Sprintf("heart team deletion on cooldown, %d days remaining",
                    int(remaining.Hours()/24)+1))
            return
        }
    }
}
```

**Exclusão mútua de lembretes** — apenas um intervalo de lembrete pode estar ativo por vez (15min, 30min, 1h, 2h):

```go
if reminderKinds[kind] {
    h.notifRepo.DisableReminderKindsExcept(r.Context(), user.ID, kind)
}
```

Essa validação vive nos handlers REST, não na camada de serviços. Os serviços permanecem agnósticos ao transporte — eles não sabem sobre roles de usuário, cooldowns ou códigos de status HTTP. Os handlers gRPC têm sua própria validação (delegada do Python anteriormente). Isso mantém ambos os adaptadores de protocolo independentes.

## Fase 4: Conectando Tudo

A função main inicializa o provedor JWKS, cria os novos repositórios e registra as rotas REST no chi router existente:

```go
// cmd/server/main.go
func main() {
    // ... setup existente (config, logger, db pool, services) ...

    // Novo: provedor JWKS para autenticação JWT
    jwksProvider := auth.NewJWKSProvider(
        cfg.Auth.FusionAuthURL,
        cfg.Auth.JWKSRefreshInterval,
        logger,
    )
    jwksProvider.Start(ctx)
    authMiddleware := auth.NewMiddleware(jwksProvider, logger)

    // Novo: repositórios para validação REST
    userRepo := repository.NewUserRepository(dbPool, logger)
    favoriteRepo := repository.NewFavoriteRepository(dbPool, logger)
    teamRepo := repository.NewTeamRepository(dbPool, logger)

    // Novo: handler REST (reutiliza serviços existentes)
    restHandler := rest.NewHandler(
        userRepo, favoriteRepo, teamRepo, notificationRepo,
        deviceService, userSubscriptionService, subscriptionService,
        logger, cfg.REST,
    )

    // Registrar no chi router existente
    rest.RegisterRoutes(router, restHandler, authMiddleware)
}
```

O servidor gRPC continua rodando na porta 50052 para serviços internos. O servidor HTTP na porta 8081 agora serve tanto health checks quanto endpoints REST. Mesmo processo, mesmos serviços, dois protocolos.

## A Camada de Serviços: Onde gRPC e REST se Encontram

A decisão de design crítica que faz tudo funcionar: **ambos os adaptadores de protocolo chamam os mesmos serviços**.

Veja como o registro de dispositivo funciona de cada lado:

```go
// Handler REST (internal/rest/device.go)
err := h.deviceService.RegisterDevice(r.Context(), service.RegisterDeviceRequest{
    UserID:      user.ID,
    DeviceToken: req.DeviceToken,
    HeartTeamID: parseUUID(req.HeartTeamID),
})

// Handler gRPC (internal/grpc/handlers/event_handler.go)
err := h.deps.DeviceService.RegisterDevice(ctx, service.RegisterDeviceRequest{
    UserID:      userID,
    DeviceToken: req.DeviceToken,
    HeartTeamID: heartTeamID,
})
```

Mesmo serviço, mesma struct de request, mesmo comportamento. O método `DeviceService.RegisterDevice` lida com o ciclo de vida complexo: deduplicando tokens de dispositivo entre usuários, criando endpoints SNS, assinando tipos de notificação habilitados, assinando o time do coração — tudo em uma chamada. Ambos os handlers gRPC e REST são wrappers finos que parseiam seus respectivos protocolos, chamam o serviço e formatam a resposta.

## O Que Quebrou no Caminho

### Split de versão de módulo Go

Os docs do Swagger carregavam normalmente (`/swagger/index.html` → 200), mas `/swagger/doc.json` retornava 500. Causa raiz: `docs.go` importava `github.com/swaggo/swag/v2` (RC), mas o handler HTTP importava `github.com/swaggo/swag` (v1). Em Go modules, esses são pacotes completamente separados com registros globais independentes. Os docs registravam a spec no v2; o handler lia do v1; não encontrava nada; 500.

Correção: regenerar docs com o CLI v1, fixar `swag` em v1.16.4.

### Endpoint JWKS errado por ambiente

O serviço Go validava JWTs buscando chaves públicas do `/.well-known/jwks.json` do FusionAuth. A configuração padrão apontava para o FusionAuth de produção, mas tokens de dev são emitidos por uma instância diferente do FusionAuth com pares de chaves RSA diferentes. Mesmo formato de `kid` — a verificação de assinatura falhava silenciosamente.

Correção: tornar `fusionauth_url` uma variável de configuração por ambiente.

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
