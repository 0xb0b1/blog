---
title: "Construindo um MMORPG do Zero com Go - Parte 1: A Fundação"
date: 2025-11-13
description: "Parte 1 da série 'Construindo um MMORPG'"
tags: ["golang", "websockets", "desenvolvimento-de-jogos", "multiplayer"]
---

## Aviso Rápido: Não Faço Ideia do Que Estou Fazendo

Ok, isso não é totalmente verdade. Eu sei programar - sou um engenheiro backend com anos de experiência construindo servidores e APIs.

**Mas desenvolvimento de jogos?** Nunca mexi. Nem um tutorial de Unity. Minha experiência com game dev literalmente consiste em:

- Jogar muitos jogos
- Pensar "eu conseguiria fazer isso" enquanto jogo
- Nunca realmente tentar até agora

**Este projeto é 100% por diversão.** Não estou tentando me tornar um desenvolvedor de jogos profissional. Não estou lançando uma startup. Eu só pensei "não seria legal construir um MMORPG?" e decidi ver o que acontece.

**Por que compartilhar essa jornada?**
Porque é muito mais divertido aprender publicamente! Além disso, talvez alguém esteja no mesmo barco - desenvolvedor experiente, zero conhecimento de game dev, curioso sobre o que é possível.

**Aviso justo:** Vou cometer erros de novato. Vou refatorar código quando perceber que há uma maneira melhor. Vou pesquisar "como fazer um jogo" mais vezes do que gostaria de admitir.

Mas essa é a parte divertida, certo? Vamos descobrir isso juntos. 🚀

## Introdução

Você já quis construir seu próprio jogo multiplayer? Não apenas uma prova de conceito simples, mas um jogo real e funcional que várias pessoas podem jogar juntas em tempo real?

Estou construindo um MMORPG 2D de artes marciais com uma twist única: **em vez de grindar níveis infinitamente, jogadores exploram um mundo perigoso para descobrir técnicas de combate ocultas**. Imagine encontrar uma caverna secreta nas profundezas das montanhas e aprender uma antiga técnica do Golpe do Dragão que muda como você luta. Essa é a fantasia central.

Pense: **Dark Souls encontra MapleStory, com a exploração de Tibia e as guerras de guild de Knight Age**, mas construído inteiramente em Go.

Ao final desta série, teremos:

- ⚔️ Combate baseado em combos (jogo de luta encontra MMORPG)
- 🗺️ Sistema de descoberta de técnicas ocultas
- 🏰 Guerras territoriais de guild
- 📊 Progressão de personagem e nivelamento
- 👥 Recursos sociais (guilds, grupos, comércio)
- 🎯 Dungeons, bosses e segredos
- 🏆 Arenas PvP e rankings

Mas estamos começando simples. Neste primeiro post, vou mostrar como construir a **fundação**: um jogo multiplayer funcional onde jogadores podem se mover e ver uns aos outros em tempo real.

## Por Que Go? (E Por Que Realmente Funciona para MMORPGs)

Você pode estar se perguntando: "Por que Go para desenvolvimento de jogos? Unity ou Godot não são o padrão?"

Aqui está a questão - para **jogos multiplayer server-heavy**, Go é na verdade **superior** a engines de jogos tradicionais. Deixe-me explicar por quê.

### O Problema do Servidor MMORPG

MMORPGs têm desafios técnicos únicos:

```
Jogo Single-Player Tradicional:
- 1 jogador
- Processamento local
- Sem sincronização de rede

MMORPG:
- 1.000+ jogadores simultâneos
- Sincronização constante de estado
- 20+ atualizações por segundo
- Operações de banco de dados
- Cálculos de combate em tempo real
```

**Unity/Unreal/Godot** são construídos para renderização, não concorrência massiva. Suas soluções de servidor frequentemente envolvem:

- Camadas de rede complexas
- Pesadelos de threading
- Gargalos de performance em escala
- Alto uso de recursos por jogador

**Go** foi literalmente projetado para resolver este problema.

### Por Que Go É Perfeito para Servidores Multiplayer

**1. Goroutines = Concorrência Sem Esforço**

```go
// Cada jogador ganha sua própria goroutine
func (s *GameServer) HandleClient(client *Client) {
    go client.readMessages()   // Leitura não-bloqueante
    go client.writeMessages()  // Escrita não-bloqueante
}

// 1.000 jogadores = 2.000 goroutines
// Custo de memória: ~2KB por goroutine
// Overhead de CPU: Mínimo
```

Compare com threads:

- Goroutine Go: ~2KB memória
- Thread OS: ~2MB memória
- **1.000x mais eficiente**

**2. Networking Integrado**

```go
// Servidor WebSocket em Go
http.HandleFunc("/ws", handleWebSocket)
http.ListenAndServe(":8080", nil)

// É isso. Pronto para produção.
```

Sem bibliotecas externas para networking básico. Está na biblioteca padrão.

**3. Performance Onde Importa**

Para um MMORPG, o servidor é o gargalo, não o cliente:

```
Servidor deve:
- Processar 1.000 ações de jogador/seg
- Atualizar estado do mundo 20x/seg
- Validar combate (anti-cheat)
- Gerenciar queries de banco
- Lidar com IA de centenas de NPCs

Cliente deve:
- Renderizar a 60 FPS
- Enviar input do jogador
- Tocar animações
```

**Go se destaca no trabalho do servidor.** Para renderização 2D no cliente, Ebitengine é mais que capaz.

## O Jogo: Shadow Arts Online (Nome de Trabalho)

Antes de mergulharmos em código, deixe-me compartilhar o que torna este jogo especial.

### Conceito Central

**"Um MMORPG de artes marciais baseado em skill onde descobrir técnicas ocultas importa mais que grindar níveis."**

Em vez de comprar habilidades de NPCs como todo outro MMORPG, jogadores **exploram o mundo para encontrá-las**:

- 🗻 Cavernas ocultas em picos de montanhas
- 🐉 Pergaminhos antigos dropados por dragões
- 🥋 NPCs mestres que ensinam técnicas secretas
- 📜 Templos cheios de puzzles com movimentos lendários

**Exemplo:** Você está explorando os Picos Gelados no nível 50. Nota uma rachadura suspeita na parede. Dentro, encontra um pergaminho ensinando "Fúria do Dragão" - um poderoso ataque pesado envolto em fogo. Nenhum marcador de quest te disse. Nenhum guia spoilou. Você descobriu.

Essa é a fantasia central: **exploração e descoberta**, não grindar os mesmos mobs por horas.

## O Tech Stack

```
┌─────────────────────────────────────────────┐
│              CLIENTE (Ebitengine)           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
│  │  Render  │  │  Input   │  │ Network  │  │
│  │  Engine  │  │ Handler  │  │  Client  │  │
│  └──────────┘  └──────────┘  └──────────┘  │
└─────────────────┬───────────────────────────┘
                  │
                  │ WebSocket (JSON)
                  │
┌─────────────────▼───────────────────────────┐
│              SERVIDOR (Go)                   │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
│  │WebSocket │  │   Game   │  │ Database │  │
│  │  Handler │  │   Loop   │  │ (Postgres)│  │
│  └──────────┘  └──────────┘  └──────────┘  │
└─────────────────────────────────────────────┘
```

**Tecnologias:**

- **Linguagem**: Go 1.21+
- **Game Engine**: Ebitengine v2.6
- **Networking**: Gorilla WebSocket
- **Banco de Dados**: PostgreSQL (opcional para MVP)
- **Protocolo**: WebSocket com mensagens JSON

## O Que Estamos Construindo Hoje

Neste primeiro post, estamos construindo o **MVP** (Produto Mínimo Viável) - a fundação sobre a qual todo o resto se constrói:

1. ✅ Servidor WebSocket que aceita múltiplas conexões
2. ✅ Game loop rodando a 20 ticks por segundo
3. ✅ Cliente que conecta e renderiza o jogo
4. ✅ Sincronização de posição em tempo real
5. ✅ Sistema básico de chat
6. ✅ Notificações de entrada/saída de jogadores

**Sim, são apenas quadrados coloridos se movendo.** Mas aqui está o importante: a **arquitetura está pronta** para tudo que precisamos:

- Sistema de combate? ✓ Servidor já valida ações
- Sistema de combo? ✓ Fila de input pronta para estender
- Técnicas ocultas? ✓ Servidor pode enviar desbloqueios de técnicas
- Guerras de guild? ✓ Coordenação multi-jogador funciona
- Dungeons? ✓ Sistema de zonas pronto para instanciar

## Estrutura do Projeto

Antes de mergulharmos no código, vamos ver como o projeto está organizado:

```
multiplayer-game/
├── client/              # Cliente do jogo
│   ├── main.go         # Ponto de entrada
│   ├── game.go         # Lógica de jogo & renderização
│   └── network.go      # Cliente WebSocket
├── server/             # Servidor do jogo
│   ├── main.go         # Ponto de entrada
│   ├── server.go       # Lógica central do servidor
│   └── database.go     # Funções de banco de dados
├── shared/             # Código compartilhado
│   └── protocol.go     # Protocolo de rede
└── go.mod              # Dependências
```

Esta separação é crucial:

- **Shared**: Tipos de mensagem usados por cliente e servidor
- **Server**: Lógica de jogo autoritativa (previne cheating!)
- **Client**: Renderização e tratamento de input

## O Game Loop

Isso é crucial - o servidor precisa continuamente atualizar e transmitir estado do jogo:

```go
func (s *GameServer) Run() {
    ticker := time.NewTicker(50 * time.Millisecond) // 20 TPS
    defer ticker.Stop()

    for {
        select {
        case client := <-s.register:
            s.clients[client.id] = client

        case client := <-s.unregister:
            delete(s.clients, client.id)

        case <-ticker.C:
            s.broadcastWorldState()
        }
    }
}
```

**20 TPS** (Ticks Por Segundo) é nossa taxa de atualização. Por que 20?

- Rápido o suficiente para gameplay responsivo
- Baixo o suficiente para não sobrecarregar a rede
- Padrão da indústria para muitos jogos

## O Que Vem a Seguir?

Construímos uma fundação sólida, mas quadrados coloridos não são exatamente empolgantes. Na **Parte 2**, vamos transformar isso em algo que realmente parece e se sente como um jogo de artes marciais:

1. 🎨 **Sprites Gráficos** - Personagens artistas marciais com animações suaves
2. 🏃 **Movimentação Suave** - Interpolação (chega de teleporte!)
3. 🧱 **Detecção de Colisão** - Não dá para atravessar paredes
4. 💬 **UI de Chat Melhor** - Campo de entrada de texto real

**Parte 3** é onde fica realmente interessante - vamos adicionar **nossas primeiras mecânicas de combate**:

- Ataque básico com combos (Leve → Leve → Pesado)
- Detecção de hit e dano
- IA de inimigos (eles vão revidar!)
- Screen shake e efeitos de hit (fazer parecer BOM)

**Parte 4** introduz o que torna nosso jogo único:

- **Sistema de Descoberta de Técnicas** - Encontre sua primeira técnica oculta
- Exploração de caverna secreta
- Pergaminhos antigos e NPCs mestres
- A sensação de descobrir algo que nenhum guia te contou

E isso é apenas o começo! O roadmap completo inclui:

- Inventário e equipamentos (Parte 5)
- Skills e habilidades (Parte 6)
- Dungeons e bosses (Parte 7)
- Sistema de guild (Parte 8)
- Guerras territoriais de guild (Parte 9)
- E muito mais!

**Próxima semana:** Fazendo parecer um jogo de verdade com sprites e animações. Te vejo lá! 🥋

## Conclusão

Conseguimos! Neste primeiro post, construímos uma fundação funcional de multiplayer para nosso MMORPG de artes marciais:

✅ Networking em tempo real com WebSocket
✅ Servidor de jogo com handling de cliente concorrente
✅ Cliente de jogo com renderização e input
✅ Sincronização de posição
✅ Sistema de chat

Esta é a fundação **sobre a qual todo o resto se constrói**. A arquitetura é sólida, o código é limpo, e funciona!

**Mas aqui está o que mais me empolga:** Isso não é apenas um exercício técnico. Estamos construindo algo único - um jogo onde exploração e skill importam mais que grinding. Um jogo onde encontrar uma técnica oculta em uma caverna secreta se sente mais recompensador do que matar 1000 slimes.

**Estou cometendo erros no caminho?** Definitivamente. **Vou refatorar coisas depois?** Com certeza. **Isso faz parte da jornada?** Pode apostar.

No próximo post, vamos fazer parecer um jogo de artes marciais de verdade com animações de personagem suaves e movimentação polida. Depois vamos adicionar o sistema de combate que torna a descoberta de técnicas significativa.

**A jornada de quadrados coloridos a um MMORPG jogável começa agora.** E estamos aprendendo juntos, um commit de cada vez. 🥋

---

**Achou útil?** Me conte nos comentários o que você gostaria de ver em seguida, ou compartilhe sua própria jornada de game dev!

**Encontrou um erro ou tem uma sugestão?** É exatamente o que eu preciso! Deixe um comentário e me ajude a melhorar. Afinal, este é um projeto de aprendizado.

**GitHub**: [https://github.com/0xb0b1]
