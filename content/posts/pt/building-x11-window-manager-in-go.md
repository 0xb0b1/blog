---
title: "Construindo um Window Manager Tiling em Go Puro"
date: 2026-01-05
description: "Como eu construi o gowm, um window manager X11 tiling minimalista do zero usando Go"
tags: ["golang", "x11", "window-manager", "linux", "xmonad"]
---

## Por Que Construir um Window Manager?

Uso xmonad há anos. É fantástico - poderoso, configurável, sólido como uma rocha. Mas sempre teve essa coceira: **e se eu pudesse construir o meu próprio?**

Não porque o xmonad seja insuficiente. Não é. Mas porque:

1. Eu queria entender de verdade como o gerenciamento de janelas X11 funciona
2. Go é minha linguagem preferida, e eu queria ver se ela daria conta do recado
3. Construir algo do zero te ensina coisas que você não aprende de nenhuma outra forma

Então eu construí o **gowm** - um window manager tiling minimalista, em Go puro, inspirado no xmonad.

## O Resultado

Depois de algumas sessões de código, tenho um window manager tiling totalmente funcional com:

- **Múltiplos layouts**: Tall (master/stack), Full (monocle), Grid
- **9 workspaces** com troca instantânea
- **Conformidade EWMH** para compatibilidade com barras e apps
- **Suporte a struts** para painéis (eww, polybar, etc.)
- **Scratchpad** para um terminal dropdown rápido
- **Suporte a mouse** para mover/redimensionar janelas flutuantes
- **Regras de janela** para auto-float de apps específicos
- **Socket IPC** para controle externo
- **Tema Catppuccin Frappe** (porque estética importa)

Tudo em cerca de **3.600 linhas de código Go**.

## A Stack

```
gowm/
├── main.go          - Ponto de entrada, loop de eventos
├── wm.go            - Core do WindowManager
├── config.go        - Configuração em tempo de compilação
├── layout_*.go      - Layouts de tiling
├── ewmh.go          - Conformidade EWMH/ICCCM
├── actions.go       - Ações de keybindings
├── scratchpad.go    - Terminal dropdown
├── mouse.go         - Mover/redimensionar janelas flutuantes
├── rules.go         - Regras de matching de janelas
├── ipc.go           - Socket Unix para controle externo
└── ...
```

As únicas dependências externas são:
- `github.com/jezek/xgb` - Implementação do protocolo X11 em Go puro
- `github.com/jezek/xgbutil` - Utilitários para xgb

Sem bindings C. Sem CGO. Go puro.

## Como Funciona o Gerenciamento de Janelas X11

O conceito central é surpreendentemente simples: **SubstructureRedirect**.

Quando você chama `ChangeWindowAttributes` na janela root com `EventMaskSubstructureRedirect`, você está dizendo ao X11: "Eu quero gerenciar todas as janelas. Me envie eventos quando apps tentarem mapear, configurar ou destruir janelas."

```go
func (wm *WindowManager) becomeWM() error {
    return xproto.ChangeWindowAttributesChecked(
        wm.conn,
        wm.root,
        xproto.CwEventMask,
        []uint32{
            xproto.EventMaskSubstructureRedirect |
            xproto.EventMaskSubstructureNotify |
            xproto.EventMaskEnterWindow |
            xproto.EventMaskPropertyChange,
        },
    ).Check()
}
```

Se outro WM já estiver rodando, isso falha com `BadAccess`. Apenas um window manager pode existir por display.

## O Loop de Eventos

O coração de qualquer window manager é o loop de eventos:

```go
for {
    ev, err := wm.conn.WaitForEvent()
    if err != nil {
        log.Printf("Error: %v", err)
        continue
    }

    switch e := ev.(type) {
    case xproto.MapRequestEvent:
        wm.manageWindow(e.Window)
    case xproto.UnmapNotifyEvent:
        wm.handleUnmapNotify(e)
    case xproto.DestroyNotifyEvent:
        wm.unmanageWindow(e.Window)
    case xproto.ConfigureRequestEvent:
        wm.handleConfigureRequest(e)
    case xproto.KeyPressEvent:
        wm.handleKeyPress(e)
    case xproto.EnterNotifyEvent:
        wm.handleEnterNotify(e)
    // ... mais eventos
    }
}
```

Quando um app quer exibir uma janela, o X11 nos envia um `MapRequestEvent`. Então decidimos como gerenciá-la - adicionar a um workspace, fazer tiling, talvez deixar flutuante baseado em regras.

## Lógica de Tiling

O algoritmo de tiling é onde as coisas ficam interessantes. Para o layout clássico "Tall":

```go
func (l *TallLayout) DoLayout(clients []*Client, area Rect) {
    n := len(clients)
    if n == 0 {
        return
    }

    if n == 1 {
        // Janela única ocupa toda a área
        clients[0].X = area.X
        clients[0].Y = area.Y
        clients[0].Width = area.Width
        clients[0].Height = area.Height
        return
    }

    // Área master (lado esquerdo)
    masterWidth := int16(float64(area.Width) * l.masterRatio)

    // Janela master
    clients[0].X = area.X
    clients[0].Y = area.Y
    clients[0].Width = uint16(masterWidth)
    clients[0].Height = area.Height

    // Área stack (lado direito)
    stackX := area.X + masterWidth
    stackWidth := area.Width - uint16(masterWidth)
    stackHeight := area.Height / uint16(n-1)

    for i := 1; i < n; i++ {
        clients[i].X = stackX
        clients[i].Y = area.Y + int16(uint16(i-1)*stackHeight)
        clients[i].Width = stackWidth
        clients[i].Height = stackHeight
    }
}
```

Uma janela master na esquerda, pilha de janelas na direita. Limpo e eficiente.

## EWMH: Falando a Mesma Língua

Para seu WM funcionar com apps modernos e barras de status, você precisa de conformidade **EWMH** (Extended Window Manager Hints).

Isso significa definir propriedades na janela root como:
- `_NET_SUPPORTED` - quais recursos seu WM suporta
- `_NET_CLIENT_LIST` - lista de janelas gerenciadas
- `_NET_CURRENT_DESKTOP` - workspace ativo
- `_NET_ACTIVE_WINDOW` - janela focada

E respeitar requisições de clientes como:
- `_NET_WM_STATE_FULLSCREEN` - app quer tela cheia
- `_NET_WM_WINDOW_TYPE_DIALOG` - deve flutuar
- `_NET_WM_STRUT_PARTIAL` - painel reservando espaço na tela

Sem EWMH, sua barra eww não vai saber em qual workspace você está, e o Steam não vai conseguir entrar em tela cheia corretamente.

## Keybindings

Keybindings no X11 requerem traduzir keysyms para keycodes:

```go
func (wm *WindowManager) grabKeys() {
    for _, kb := range wm.config.Keybindings {
        codes := wm.keysymToKeycodes(kb.Keysym)
        for _, code := range codes {
            xproto.GrabKey(
                wm.conn,
                true,
                wm.root,
                kb.Mod,
                code,
                xproto.GrabModeAsync,
                xproto.GrabModeAsync,
            )
        }
    }
}
```

Keysyms são constantes simbólicas (`XK_Return`, `XK_space`), enquanto keycodes são específicos do hardware. O servidor X fornece um mapeamento entre eles.

## O Scratchpad

Uma feature que eu não conseguiria viver sem: um terminal dropdown que aparece com um único pressionamento de tecla.

```go
func (wm *WindowManager) toggleScratchpad() {
    if wm.scratchpad.Visible {
        // Esconde
        xproto.UnmapWindow(wm.conn, wm.scratchpad.Window)
        wm.scratchpad.Visible = false
    } else {
        if wm.scratchpad.Window == 0 {
            // Spawna terminal com classe especial
            spawn("kitty --class scratchpad")
        } else {
            // Mostra e posiciona
            xproto.MapWindow(wm.conn, wm.scratchpad.Window)
            // Centraliza na tela...
        }
        wm.scratchpad.Visible = true
    }
}
```

Pressione `Super+Grave`, terminal desce. Pressione de novo, desaparece. Simples mas incrivelmente útil.

## Jogos Steam e Tela Cheia

Fazer jogos funcionarem corretamente foi complicado. Jogos Steam frequentemente usam `_NET_WM_STATE_FULLSCREEN`, e você precisa:

1. Detectar a requisição de tela cheia
2. Remover bordas
3. Redimensionar para dimensões de tela cheia
4. Elevar acima de tudo (incluindo sua barra de status)
5. Definir a propriedade de estado para que painéis saibam que devem esconder

```go
case _NET_WM_STATE_ADD:
    client.Floating = true
    client.X = 0
    client.Y = 0
    client.Width = wm.screen.WidthInPixels
    client.Height = wm.screen.HeightInPixels
    wm.setFullscreenState(client.Window, true)
    xproto.ConfigureWindow(wm.conn, client.Window,
        xproto.ConfigWindowX|xproto.ConfigWindowY|
        xproto.ConfigWindowWidth|xproto.ConfigWindowHeight|
        xproto.ConfigWindowBorderWidth|xproto.ConfigWindowStackMode,
        []uint32{0, 0, uint32(client.Width), uint32(client.Height), 0, xproto.StackModeAbove})
```

CS2 precisou de tratamento especial - tive que adicionar `steam_app_730` às regras de flutuante.

## O Que Aprendi

Construir um window manager me ensinou:

1. **X11 é antigo mas bem projetado** - O protocolo é de 1987 mas ainda faz sentido
2. **Go funciona muito bem para software de sistema** - Sem pausas de GC, concorrência limpa, compilação rápida
3. **EWMH é essencial** - Sem ele, nada funciona direito com apps modernos
4. **Pequenos detalhes importam** - Gerenciamento de foco, cores de borda, struts - tudo afeta a usabilidade

## Teste Você Mesmo

O código está no GitHub: [github.com/0xb0b1/gowm](https://github.com/0xb0b1/gowm)

Para buildar e testar (sem substituir seu WM atual):

```bash
# Instale Xephyr para servidor X aninhado
sudo pacman -S xorg-server-xephyr  # Arch
sudo apt install xserver-xephyr    # Debian/Ubuntu

# Clone e builde
git clone https://github.com/0xb0b1/gowm
cd gowm
go build -o gowm .

# Teste no Xephyr
Xephyr :1 -screen 1920x1080 &
DISPLAY=:1 ./gowm &
DISPLAY=:1 kitty &  # Abra um terminal no Xephyr
```

## Próximos Passos

O básico funciona muito bem. Melhorias futuras podem incluir:

- Suporte multi-monitor
- Mais layouts (espiral, colunas)
- Layouts por workspace
- Configuração hot-reload
- Melhor tratamento de urgência

Mas honestamente? Já faz tudo que preciso. E essa é a beleza de construir suas próprias ferramentas - você pode parar quando *você* estiver satisfeito.

Happy hacking!
