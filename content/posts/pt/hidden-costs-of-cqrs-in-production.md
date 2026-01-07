---
title: "Os Custos Ocultos do CQRS em Produção"
date: 2025-11-09
description: "O que os tutoriais não contam sobre consistência eventual, debug de estado distribuído e a complexidade operacional que vem com a separação de leituras e escritas."
tags:
  [
    "golang",
    "arquitetura",
    "CQRS",
    "sistemas-distribuidos",
    "producao",
    "licoes-aprendidas",
  ]
---

Todo tutorial de CQRS mostra a separação elegante: comandos vão aqui, queries vão ali, e seu sistema escala lindamente. O que não mostram é o incidente às 3h da manhã onde um cliente insiste que fez um pagamento que seu modelo de leitura diz não existir.

Este post não é sobre se CQRS é bom ou ruim—é sobre os custos que só se tornam visíveis depois que você se comprometeu com o padrão em produção.

## O Imposto da Consistência Eventual

O trade-off mais discutido é a consistência eventual, mas discussões raramente cobrem como isso realmente se manifesta em produção.

### O Problema "Cadê Meus Dados?"

```go
// Isso parece razoável em um tutorial
func (h *OrderHandler) CreateOrder(ctx context.Context, cmd CreateOrderCommand) error {
    // Escreve no lado de comando
    if err := h.commandStore.Save(ctx, order); err != nil {
        return err
    }

    // Publica evento para o modelo de leitura
    return h.eventBus.Publish(ctx, OrderCreatedEvent{OrderID: order.ID})
}

// Mas então o usuário imediatamente tenta ver seu pedido...
func (h *OrderHandler) GetOrder(ctx context.Context, orderID string) (*OrderView, error) {
    // Lê do lado de query - pode não estar lá ainda!
    return h.queryStore.FindByID(ctx, orderID)
}
```

O gap entre escrita e leitura pode ser milissegundos ou minutos dependendo da sua infraestrutura. Aqui está o que aprendemos:

### Estratégia 1: Consistência Read-Your-Writes

```go
type OrderService struct {
    commandStore CommandStore
    queryStore   QueryStore
    cache        *ConsistencyCache // Cache write-through de curta duração
}

func (s *OrderService) CreateOrder(ctx context.Context, cmd CreateOrderCommand) (*OrderView, error) {
    order, err := s.commandStore.Save(ctx, cmd)
    if err != nil {
        return nil, err
    }

    // Cache a view imediatamente para o usuário que criou
    view := orderToView(order)
    s.cache.SetWithUserScope(ctx, userID(ctx), order.ID, view, 30*time.Second)

    // Projeção assíncrona ainda acontece
    go s.eventBus.Publish(context.Background(), OrderCreatedEvent{OrderID: order.ID})

    return view, nil
}

func (s *OrderService) GetOrder(ctx context.Context, orderID string) (*OrderView, error) {
    // Verifica cache do usuário primeiro
    if view, ok := s.cache.GetWithUserScope(ctx, userID(ctx), orderID); ok {
        return view, nil
    }

    return s.queryStore.FindByID(ctx, orderID)
}
```

### Estratégia 2: Limites de Consistência Explícitos

Às vezes a resposta é ser honesto com os usuários:

```go
type OrderResponse struct {
    Order       *OrderView `json:"order"`
    Consistency string     `json:"consistency"` // "confirmed" ou "pending"
}

func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
    order, err := h.service.CreateOrder(r.Context(), cmd)
    if err != nil {
        // trata erro
    }

    json.NewEncoder(w).Encode(OrderResponse{
        Order:       order,
        Consistency: "pending", // UI pode mostrar indicador "Processando..."
    })
}
```

## O Pesadelo do Debug

Quando seu modelo de leitura mostra dados diferentes do modelo de escrita, por onde você começa?

### Problemas de Replay de Eventos

```go
// A projeção que parecia ok em desenvolvimento
func (p *OrderProjection) Handle(event OrderCreatedEvent) error {
    return p.db.Exec(`
        INSERT INTO order_views (id, customer_id, total, status)
        VALUES ($1, $2, $3, $4)
    `, event.OrderID, event.CustomerID, event.Total, "created")
}

// Mas em produção, eventos podem chegar fora de ordem ou ser repetidos
// O que acontece se OrderUpdatedEvent chegar antes de OrderCreatedEvent?
```

### Construindo Ferramentas de Debug

Aprendemos a construir essas ferramentas cedo, não depois do primeiro incidente:

```go
// Monitor de lag de projeção
type ProjectionLagMonitor struct {
    commandStore CommandStore
    queryStore   QueryStore
}

func (m *ProjectionLagMonitor) CheckLag(ctx context.Context, entityID string) (*LagReport, error) {
    commandVersion, err := m.commandStore.GetVersion(ctx, entityID)
    if err != nil {
        return nil, err
    }

    queryVersion, err := m.queryStore.GetProjectedVersion(ctx, entityID)
    if err != nil {
        return nil, err
    }

    return &LagReport{
        EntityID:       entityID,
        CommandVersion: commandVersion,
        QueryVersion:   queryVersion,
        Lag:            commandVersion - queryVersion,
        Status:         lagStatus(commandVersion - queryVersion),
    }, nil
}

// Verificador de consistência para validação em batch
func (m *ProjectionLagMonitor) VerifyConsistency(ctx context.Context) (*ConsistencyReport, error) {
    // Amostra entidades e compara estado command vs query
    // Alerta sobre drift além do threshold aceitável
}
```

## A Complexidade Operacional

### Mais Infraestrutura, Mais Problemas

CQRS tipicamente significa:
- Bancos de dados separados (ou pelo menos schemas) para leituras e escritas
- Um message broker para eventos
- Workers de projeção que precisam de monitoramento
- Orquestração de deploy mais complexa

```yaml
# Seu deployment ficou mais complexo
services:
  command-api:
    depends_on:
      - postgres-write
      - kafka

  query-api:
    depends_on:
      - postgres-read
      - elasticsearch  # Talvez você adicionou isso para busca

  projection-worker:
    depends_on:
      - postgres-write
      - postgres-read
      - kafka
    replicas: 3  # Precisa coordenação para processamento ordenado
```

### Desafios do Worker de Projeção

```go
// Workers de projeção precisam coordenação cuidadosa
type ProjectionWorker struct {
    consumer     kafka.Consumer
    projection   Projection
    checkpointer Checkpointer
}

func (w *ProjectionWorker) Run(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return w.gracefulShutdown()
        default:
            msg, err := w.consumer.Consume(ctx)
            if err != nil {
                return err
            }

            // E se a projeção falhar? Retry? Dead letter?
            if err := w.projection.Handle(msg); err != nil {
                // Esta decisão afeta suas garantias de consistência
                if isRetryable(err) {
                    w.consumer.Nack(msg)
                    continue
                }
                // Falha permanente - e agora?
                w.deadLetter.Send(msg, err)
            }

            // Checkpoint após processamento bem sucedido
            w.checkpointer.Save(msg.Offset)
        }
    }
}
```

## Quando CQRS Realmente Compensa

Depois de viver com esses custos, aqui está quando eles valem a pena:

1. **Padrões de leitura/escrita genuinamente diferentes**: Suas escritas precisam de consistência forte e validação complexa, enquanto leituras precisam de dados desnormalizados de múltiplos agregados.

2. **Requisitos de auditoria**: Você precisa responder "como chegamos aqui?" para compliance.

3. **Assimetria de escala**: 100x mais leituras que escritas, e você precisa escalar independentemente.

4. **Fronteiras de time**: Times separados podem ser donos dos lados de comando e query.

## Quando Evitar CQRS

- Suas leituras e escritas são similares
- Seu time é pequeno e não pode arcar com o overhead operacional
- Você não tem assimetria de escala genuína
- Você não está preparado para construir as ferramentas de debug

## Pontos-Chave

1. **Consistência eventual é um problema de UX**, não apenas técnico. Planeje para isso na sua UI.

2. **Construa observabilidade cedo**. Monitoramento de lag de projeção, verificadores de consistência e ferramentas de replay devem fazer parte da implementação inicial.

3. **A complexidade é front-loaded**. Você paga o imposto arquitetural independente da escala.

4. **Comece com CRUD**, migre para CQRS quando tiver evidência que precisa. "Talvez precisemos escalar" não é evidência.

O padrão é poderoso quando você precisa. O erro é adotá-lo antes disso.
