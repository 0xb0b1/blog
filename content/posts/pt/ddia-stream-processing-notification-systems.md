---
title: "Como os Conceitos de Stream Processing do DDIA se Aplicam a Sistemas de Notificação em Tempo Real"
date: 2026-02-21
description: "Conectando os conceitos de stream processing do Designing Data-Intensive Applications — event streams, fan-out, backpressure, materialized views e semântica exactly-once — às decisões arquiteturais reais em um sistema de notificações esportivas em tempo real construído com Go, gRPC e AWS SNS."
tags:
  [
    "golang",
    "stream-processing",
    "grpc",
    "sistemas-distribuidos",
    "ddia",
    "event-driven",
    "tempo-real",
    "design-de-sistemas",
    "notificacoes",
    "backend",
  ]
---

Quando peguei o _Designing Data-Intensive Applications_ do Martin Kleppmann pela primeira vez, os capítulos sobre stream processing me impactaram de forma diferente. Não porque os conceitos fossem novos — eu já trabalhava com sistemas orientados a eventos e pipelines de dados em tempo real há um tempo — mas porque Kleppmann os enquadra de uma forma que te faz repensar sistemas que você já construiu.

Eu trabalho em uma plataforma esportiva em tempo real que entrega atualizações de placares e push notifications para centenas de milhares de usuários simultâneos. Os desafios são exatamente o que o DDIA descreve: garantias de ordenação, tolerância a falhas, backpressure e a eterna pergunta "o que acontece quando um consumidor fica para trás?" Este post conecta os conceitos de stream processing do DDIA às decisões arquiteturais reais que enfrentei construindo sistemas de notificação em escala.

## O Event Stream: Onde Tudo Começa

A ideia mais poderosa do DDIA sobre stream processing é enganosamente simples: **um event stream é uma sequência ilimitada e ordenada de registros**. Kleppmann dedica atenção significativa a esse conceito porque ele é a fundação de todo o resto — event sourcing, change data capture, stream processing e, sim, sistemas de notificação.

Na nossa arquitetura, cada atualização de placar, cada gol, cada cartão é um evento. Um serviço de dataloader monitora feeds de partidas ao vivo e envia eventos estruturados para nosso backend de notificações via gRPC. Cada evento carrega tudo que o sistema de notificação precisa para decidir o que enviar, para quem e como:

```go
type EventRequest struct {
    MatchID      uuid.UUID
    TopicKind    TopicKind // GOAL, PENALTY, RED_CARD, START, END, ...
    GameTime     string
    TeamGround   TeamGround
    HomeName     TranslatedName
    AwayName     TranslatedName
    PlayerName   *TranslatedName
    HomeScore    int32
    AwayScore    int32
    UniqueKey    string // deduplication identifier
    SilentUpdate bool   // correction to a previous notification
}
```

O campo `UniqueKey` importa mais do que você imagina. O DDIA explica que em um sistema distribuído, eventos podem chegar fora de ordem ou ser entregues mais de uma vez. Nosso dataloader pode enviar o mesmo evento múltiplas vezes — retentativas de rede, replays upstream — e o `UniqueKey` é o que nos permite distinguir um evento genuinamente novo de um duplicado ou de uma correção (como o nome do artilheiro sendo atualizado após revisão do VAR).

Agrupamos eventos por `MatchID` para processamento ordenado dentro de cada partida, enquanto processamos partidas diferentes concorrentemente. Isso espelha a discussão do DDIA sobre processamento particionado: a ordenação é garantida onde importa (dentro de uma partida), mas não pagamos o custo de ordenação global entre streams independentes.

## Fan-Out: De Um Evento para Milhares de Dispositivos

O DDIA descreve dois padrões de mensageria: **load balancing** (cada mensagem vai para um consumidor) e **fan-out** (cada mensagem vai para todos os consumidores). A maioria dos sistemas reais precisa de ambos, e sistemas de notificação são um exemplo perfeito.

Nosso pipeline funciona assim:

1. **Dataloader** envia eventos de placar via gRPC
2. **Handler gRPC** converte protobuf para modelos internos, despacha assincronamente
3. **Processador de eventos** (worker pool) busca o tópico SNS, deduplica, constrói o payload FCM e publica no SNS
4. **AWS SNS** faz fan-out para todos os endpoints de dispositivos inscritos via FCM/APNs

O fan-out acontece na camada do SNS. Cada combinação de partida e tipo de evento tem seu próprio tópico SNS (ex: `prod_{match_id}_GOAL_PORTUGUESE`). Quando um usuário se inscreve em uma partida, o endpoint do dispositivo dele é inscrito nos tópicos SNS relevantes. Quando um evento de gol chega, publicamos uma vez no tópico SNS e o SNS cuida de distribuir a notificação para todos os dispositivos inscritos:

```go
func (p *EventProcessor) processSingleEvent(ctx context.Context, event *EventRequest) error {
    // 1. Look up or auto-create the SNS topic for this match + event type
    topic, err := p.getOrCreateTopic(ctx, event)
    if err != nil {
        return err
    }

    // 2. Deduplicate — is this a new event, a correction, or a duplicate?
    result, _ := p.deduplicator.Check(ctx, event, topic.ARN)
    if result == dedup.ResultDuplicate {
        return nil // Already sent, skip
    }

    // 3. Delay corrections so the original notification is seen first
    if result == dedup.ResultCorrection && event.SilentUpdate {
        time.Sleep(5 * time.Second)
    }

    // 4. Build multi-language FCM v1 payload and publish to SNS
    payload := p.buildNotificationRequest(event)
    return p.deliverer.Deliver(ctx, event, topic, payload)
}
```

Esse é um modelo de fan-out fundamentalmente diferente do que o DDIA descreve com consumer groups do Kafka. Em vez de cada consumidor manter sua própria posição em um log, o SNS atua como uma camada de fan-out push-based — publicamos uma vez, e a plataforma cuida da entrega para potencialmente centenas de milhares de dispositivos. O tradeoff é que perdemos replayability na camada de fan-out (SNS é fire-and-forget), mas ganhamos simplificação massiva na infraestrutura de entrega. O framework do DDIA ajuda a ver esse tradeoff claramente.

## Backpressure: Três Camadas de Defesa

É aqui que a sabedoria do DDIA nos salvou de problemas sérios em produção. Kleppmann discute a tensão fundamental no stream processing: **o que acontece quando um produtor é mais rápido que um consumidor?**

As três opções que ele descreve são: descartar mensagens, armazená-las em buffer (arriscando estouro de memória) ou aplicar backpressure (desacelerar o produtor). Em um sistema de notificação que publica para um serviço externo como AWS SNS, nenhuma dessas é ideal isoladamente. Durante um dia movimentado de jogos — pense em múltiplas partidas de alto perfil simultâneas — nossa taxa de eventos dispara dramaticamente, e o AWS SNS tem limites de taxa.

Nossa solução empilha três padrões inspirados no DDIA:

**Camada 1: Processamento assíncrono limitado.** O handler gRPC retorna imediatamente e despacha trabalho via semáforo limitado. Isso impede que o dataloader seja bloqueado pelo processamento de notificações, enquanto limita goroutines concorrentes:

```go
type EventHandler struct {
    processor EventProcessor
    asyncSem  chan struct{} // capacity: 1000
}

func (h *EventHandler) NotifyEvents(ctx context.Context, req *pb.EventsRequest) (*emptypb.Empty, error) {
    events := convertEvents(req.Events)

    h.asyncSem <- struct{}{} // acquire
    go func() {
        defer func() { <-h.asyncSem }() // release
        h.processor.ProcessEvents(context.Background(), events)
    }()

    return &emptypb.Empty{}, nil
}
```

**Camada 2: Rate limiting adaptativo.** Quando o SNS começa a fazer throttling, recuamos exponencialmente. Quando ele se recupera, aceleramos gradualmente:

```go
type AdaptiveRateLimiter struct {
    currentDelay   time.Duration
    backoffFactor  float64 // 2.0 — double on error
    recoveryFactor float64 // 0.8 — 20% faster on success
    minDelay       time.Duration // 10ms
    maxDelay       time.Duration // 5s
}

func (r *AdaptiveRateLimiter) RecordError(isThrottling bool) {
    factor := r.backoffFactor
    if isThrottling {
        factor *= 1.5 // extra penalty for throttling
    }
    r.currentDelay = min(r.currentDelay * factor, r.maxDelay)
}

func (r *AdaptiveRateLimiter) RecordSuccess() {
    if r.consecutiveSuccess >= r.successThreshold {
        r.currentDelay = max(r.currentDelay * r.recoveryFactor, r.minDelay)
    }
}
```

**Camada 3: Circuit breakers.** Se o SNS está falhando persistentemente, paramos de bombardeá-lo completamente e deixamos ele se recuperar:

```go
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
    if cb.state == StateOpen {
        if time.Now().Before(cb.expiry) {
            return ErrCircuitOpen // Fast-fail, don't even try
        }
        cb.setState(StateHalfOpen) // Timeout elapsed, test recovery
    }

    err := fn()
    if err == nil {
        cb.onSuccess()
    } else {
        cb.onFailure(err)
    }
    return err
}
```

Mantemos instâncias separadas de circuit breaker para entrega e assinaturas. Essa é uma escolha de design crítica: se a API de assinaturas está sendo throttled, isso não pode bloquear a entrega de notificações. O framework do DDIA para pensar sobre isolamento de falhas entre estágios do pipeline informou diretamente essa separação.

## Materialized Views: Cache de Tópicos como Dualidade Stream-Table

Um dos conceitos mais elegantes do DDIA é a **dualidade stream-table**: um stream pode ser visto como o changelog de uma tabela, e uma tabela pode ser vista como o estado materializado de um stream. Isso se aplica diretamente a como lidamos com a resolução de tópicos.

Cada evento de placar precisa ser publicado em um tópico SNS. Os metadados do tópico (ARN, tipo, associação com partida) ficam no PostgreSQL, mas com centenas de eventos por minuto, consultar o banco de dados para cada evento seria brutal.

Em vez disso, mantemos uma materialized view dos tópicos com backing em Redis:

```go
func (p *EventProcessor) getOrCreateTopic(ctx context.Context, event *EventRequest) (*TopicCache, error) {
    // Try cache first (Redis)
    topic, _ := p.topicCache.Get(ctx, event.MatchID, event.TopicKind)
    if topic != nil {
        return topic, nil
    }

    // Cache miss — query PostgreSQL
    dbTopic, err := p.topicRepo.GetByMatchAndKind(ctx, event.MatchID, event.TopicKind)
    if err != nil {
        return nil, err
    }

    // Topic doesn't exist yet — auto-create in SNS + DB
    if dbTopic == nil {
        dbTopic, err = p.createMatchTopic(ctx, event.MatchID, event.TopicKind)
        if err != nil {
            return nil, err
        }
    }

    // Cache the result
    cached := &TopicCache{ARN: dbTopic.ARN, Kind: dbTopic.Kind, MatchID: event.MatchID.String()}
    p.topicCache.Set(ctx, event.MatchID, event.TopicKind, cached)
    return cached, nil
}
```

Essa é a dualidade stream-table na prática, só que sem Kafka. O "stream" é o fluxo de eventos de criação de tópicos (tópico SNS criado, armazenado no PostgreSQL). A "tabela" é nosso cache Redis — uma materialized view otimizada para o padrão de acesso do processador de eventos (busca por matchID + topicKind). Quando os dados subjacentes mudam (tópico deletado externamente, novo tópico criado), invalidamos o cache e deixamos ele se reconstruir a partir da fonte.

O padrão de auto-criação também lida com condições de corrida: quando dois workers tentam criar o mesmo tópico simultaneamente, o segundo captura a violação de unique constraint do PostgreSQL e busca o tópico que o primeiro worker criou. Sem locking distribuído, sem coordenação — apenas escritas idempotentes e uma constraint unique bem escolhida.

## Semântica Exactly-Once: A Realidade Pragmática

O DDIA é refrescantemente honesto sobre processamento exactly-once: é difícil, e na maioria dos sistemas distribuídos, o que você realmente alcança é **effectively-once** através de idempotência. Isso ressoou com nosso sistema de notificação porque enviar uma notificação de gol duplicada é uma experiência terrível para o usuário.

Mas nossos requisitos de deduplicação são mais nuanceados do que um simples "já viu isso antes? pule." No esporte, eventos são corrigidos — o nome do artilheiro pode ser atualizado, uma decisão do VAR pode reverter um pênalti. Precisamos distinguir três casos: eventos **novos**, **correções** de eventos anteriores e **duplicados** verdadeiros.

Lidamos com isso usando um script Lua atômico no Redis:

```go
// Lua script for atomic check-and-set deduplication
// Returns: 0 = duplicate, 1 = new, 2 = correction
const dedupScript = `
    local existing = redis.call('GET', KEYS[1])
    if existing then
        local data = cjson.decode(existing)
        if data.h == ARGV[1] then
            return 0  -- Duplicate: same payload hash
        else
            redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
            return 2  -- Correction: different payload, update and send
        end
    else
        redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
        return 1  -- New event
    end
`
```

O hash do payload é computado a partir dos campos que tornam uma notificação diferente — placar, nome do jogador, tipo de gol, tempo de jogo:

```go
func (d *Deduplicator) computePayloadHash(event *EventRequest) string {
    payload := fmt.Sprintf("%s|%d|%d|%s|%s|%s",
        event.TopicKind, event.HomeScore, event.AwayScore,
        event.PlayerName, event.GoalType, event.GameTime,
    )
    hash := sha256.Sum256([]byte(payload))
    return hex.EncodeToString(hash[:8])
}
```

A chave é composta por `dedup:{unique_key}:{topic_arn}` com TTL de 24 horas. Não precisamos lembrar de cada evento para sempre — apenas tempo suficiente para cobrir retentativas e replays upstream. Em caso de falha do Redis, falhamos aberto: melhor enviar um duplicado do que perder uma notificação de gol.

O ponto do DDIA sobre o tradeoff entre garantias exactly-once e complexidade do sistema é algo que penso frequentemente. Deduplicação perfeita em um sistema distribuído requer coordenação, e coordenação é a inimiga do throughput. A classificação em três vias (novo/correção/duplicado) nos dá precisão onde importa sem a sobrecarga de transações distribuídas.

## Circuit Breakers: Isolamento de Falhas no Pipeline

O DDIA discute tolerância a falhas extensivamente, e um insight chave é que **falhas em uma parte do pipeline não devem se propagar para outras**. No nosso sistema, dependemos do AWS SNS tanto para entrega de notificações quanto para gerenciamento de assinaturas. São cargas de trabalho fundamentalmente diferentes com modos de falha diferentes.

Operações de assinatura (inscrever o dispositivo de um usuário em um tópico de partida) são operações em lote que podem disparar throttling do SNS em horários de pico. Entrega de notificações (publicar um evento de gol em um tópico) é sensível a latência e não pode ser bloqueada por throttling de assinaturas.

Resolvemos isso executando instâncias separadas de circuit breaker:

```go
// In main.go — two SNS clients, two circuit breakers
snsDeliveryBreaker := circuitbreaker.New(circuitbreaker.Config{
    Name:         "sns-delivery",
    FailureRatio: 0.6,
    MinRequests:  10,
    Timeout:      30 * time.Second,
})

snsSubscriptionBreaker := circuitbreaker.New(circuitbreaker.Config{
    Name:         "sns-subscription",
    FailureRatio: 0.6,
    MinRequests:  10,
    Timeout:      30 * time.Second,
})
```

Quando o breaker de assinaturas dispara (porque estamos criando milhares de assinaturas durante um período de pico de registros), a entrega de notificações continua sem ser afetada. O serviço de assinaturas usa seu próprio rate limiter adaptativo para encontrar o throughput máximo que o SNS aceita sem throttling, enquanto a entrega opera a toda velocidade em seu próprio circuito.

Isso mapeia diretamente a discussão do DDIA sobre isolamento entre estágios de stream processing. Cada estágio deve lidar com seu próprio backpressure e falhas independentemente. O dataloader não desacelera quando o serviço de notificação está sobrecarregado — fire-and-forget com concorrência limitada cuida disso. O publicador de notificações não para quando as assinaturas estão sendo throttled — circuit breakers separados cuidam disso. Cada fronteira é explícita e configurável independentemente.

## Tolerância a Falhas: Infraestrutura Auto-Recuperável

O DDIA enfatiza que em um sistema de stream processing bem projetado, **falhas devem ser recuperáveis sem intervenção manual**. Nosso sistema de notificação enfrenta um desafio específico de confiabilidade: tópicos SNS podem ser deletados externamente (mudanças de infraestrutura, deleções acidentais, manutenção da AWS), e quando isso acontece, as notificações falham silenciosamente.

Lidamos com isso com recriação automática de tópicos:

```go
func (p *EventProcessor) processSingleEvent(ctx context.Context, event *EventRequest) error {
    topic, err := p.getOrCreateTopic(ctx, event)
    if err != nil {
        return err
    }

    payload := p.buildNotificationRequest(event)
    err = p.deliverer.Deliver(ctx, event, topic, payload)

    if errors.Is(err, delivery.ErrTopicNotFound) {
        // SNS topic was deleted externally — recreate and retry
        p.topicCache.Delete(ctx, event.MatchID, event.TopicKind)
        newTopic, err := p.recreateTopic(ctx, event)
        if err != nil {
            return err
        }
        return p.deliverer.Deliver(ctx, event, newTopic, payload)
    }

    return err
}
```

A função `recreateTopic` invalida o cache Redis, deleta o registro obsoleto do banco de dados, cria um novo tópico SNS, armazena o novo ARN e faz cache do resultado. Tudo isso acontece de forma transparente — sem alertas, sem intervenção manual, sem notificações perdidas além da que disparou a recriação.

Combinado com o comportamento fail-open da nossa deduplicação (Redis caiu? envia mesmo assim) e o processamento assíncrono limitado (handler gRPC nunca bloqueia), o sistema tem múltiplas camadas de resiliência. Essa é a abordagem de "defesa em profundidade" que o DDIA defende: não dependa de nenhum componente único estar disponível, e projete cada peça para degradar graciosamente.

## O Que o DDIA Me Ensinou Que a Produção Confirmou

Ler os capítulos de stream processing do DDIA depois de construir esses sistemas foi como ter alguém articulando padrões que eu descobri por tentativa e erro. Alguns pontos-chave:

**O event stream é o coração do sistema.** Cada decisão arquitetural flui de tratar eventos como a fonte da verdade. Quando o dataloader envia um evento de gol, ele flui através de deduplicação, resolução de tópico, construção de payload e entrega — cada passo uma transformação daquele evento original. A ênfase de Kleppmann nesse conceito fundamental não é acadêmica — é o conselho mais prático do livro.

**Dados derivados simplificam tudo.** Nosso cache Redis de tópicos é derivado do PostgreSQL. Nossas assinaturas de tópicos SNS são derivadas das preferências dos usuários. Cada sistema downstream possui sua própria representação dos dados, otimizada para seus padrões de acesso específicos. Essa é exatamente a filosofia de dados derivados que o DDIA defende, e funciona lindamente na prática.

**Exactly-once é um espectro, não um binário.** Passamos semanas no início tentando alcançar deduplicação perfeita em todo o pipeline. O DDIA me ajudou a entender que deduplicação atômica com TTLs limitados e política fail-open não é um compromisso — é a escolha pragmática de engenharia. A classificação em três vias novo/correção/duplicado surgiu ao entender que "duplicado" nem sempre é binário.

**Backpressure deve ser projetado, não descoberto.** Cada incidente em produção que tivemos com o sistema de notificação remonta a algum componente sendo sobrecarregado. Semáforos limitados, rate limiters adaptativos e circuit breakers formam três camadas de proteção. O framework do DDIA para pensar sobre o que acontece quando produtores ultrapassam consumidores deve ser a primeira coisa que você projeta, não a última.

## Conclusão

_Designing Data-Intensive Applications_ continua sendo o melhor recurso único para entender os princípios por trás de sistemas de dados modernos. Os capítulos sobre stream processing em particular fornecem um framework mental que mapeia diretamente para sistemas de notificação reais, arquiteturas orientadas a eventos e qualquer sistema onde dados fluem continuamente.

O que mais me surpreendeu é quão bem os conceitos se aplicam mesmo quando você não está usando Kafka ou uma arquitetura tradicional baseada em log. Nosso sistema usa gRPC para ingestão e AWS SNS para entrega — sem log, sem consumer offsets, sem replay. Mas os modelos mentais do DDIA — event streams como fonte da verdade, dados derivados, backpressure como preocupação de primeira classe, isolamento de falhas entre estágios — moldaram cada decisão arquitetural.

Se você está construindo sistemas como esses, encorajo você a ler os capítulos de stream processing com sua própria arquitetura em mente. Trace os paralelos. Questione onde seu sistema diverge dos padrões que o DDIA descreve e pergunte a si mesmo se essa divergência é intencional ou acidental.

Os melhores sistemas não são construídos seguindo padrões cegamente — são construídos por engenheiros que entendem os tradeoffs profundamente o suficiente para tomar decisões informadas. O DDIA te dá essa profundidade.

---
