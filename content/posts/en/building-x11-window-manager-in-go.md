---
title: "Building a Tiling Window Manager in Pure Go"
date: 2026-01-05
description: "How I built gowm, a minimal X11 tiling window manager from scratch using Go"
tags: ["golang", "x11", "window-manager", "linux", "xmonad"]
---

## Why Build a Window Manager?

I've been using xmonad for years. It's fantastic - powerful, configurable, rock-solid. But there's always been this itch: **what if I could build my own?**

Not because xmonad is lacking. It's not. But because:

1. I wanted to truly understand how X11 window management works
2. Go is my language of choice, and I wanted to see if it could handle this
3. Building something from scratch teaches you things you can't learn any other way

So I built **gowm** - a minimal, pure Go tiling window manager inspired by xmonad.

## The Result

After a few coding sessions, I have a fully functional tiling window manager with:

- **Multiple layouts**: Tall (master/stack), Full (monocle), Grid
- **9 workspaces** with instant switching
- **EWMH compliance** for compatibility with bars and apps
- **Strut support** for panels (eww, polybar, etc.)
- **Scratchpad** for a quick dropdown terminal
- **Mouse support** for floating window move/resize
- **Window rules** for auto-floating specific apps
- **IPC socket** for external control
- **Catppuccin Frappe** color scheme (because aesthetics matter)

All in about **3,600 lines of Go code**.

## The Stack

```
gowm/
├── main.go          - Entry point, event loop
├── wm.go            - Core WindowManager
├── config.go        - Compile-time configuration
├── layout_*.go      - Tiling layouts
├── ewmh.go          - EWMH/ICCCM compliance
├── actions.go       - Keybinding actions
├── scratchpad.go    - Dropdown terminal
├── mouse.go         - Move/resize floating windows
├── rules.go         - Window matching rules
├── ipc.go           - Unix socket for external control
└── ...
```

The only external dependencies are:
- `github.com/jezek/xgb` - Pure Go X11 protocol implementation
- `github.com/jezek/xgbutil` - Utilities for xgb

No C bindings. No CGO. Pure Go.

## How X11 Window Management Works

The core concept is surprisingly simple: **SubstructureRedirect**.

When you call `ChangeWindowAttributes` on the root window with `EventMaskSubstructureRedirect`, you're telling X11: "I want to manage all windows. Send me events when apps try to map, configure, or destroy windows."

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

If another WM is already running, this fails with `BadAccess`. Only one window manager can exist per display.

## The Event Loop

The heart of any window manager is the event loop:

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
    // ... more events
    }
}
```

When an app wants to display a window, X11 sends us a `MapRequestEvent`. We then decide how to manage it - add it to a workspace, tile it, maybe float it based on rules.

## Tiling Logic

The tiling algorithm is where things get interesting. For the classic "Tall" layout:

```go
func (l *TallLayout) DoLayout(clients []*Client, area Rect) {
    n := len(clients)
    if n == 0 {
        return
    }

    if n == 1 {
        // Single window takes full area
        clients[0].X = area.X
        clients[0].Y = area.Y
        clients[0].Width = area.Width
        clients[0].Height = area.Height
        return
    }

    // Master area (left side)
    masterWidth := int16(float64(area.Width) * l.masterRatio)

    // Master window
    clients[0].X = area.X
    clients[0].Y = area.Y
    clients[0].Width = uint16(masterWidth)
    clients[0].Height = area.Height

    // Stack area (right side)
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

One master window on the left, stack of windows on the right. Clean and efficient.

## EWMH: Speaking the Same Language

For your WM to work with modern apps and status bars, you need **EWMH** (Extended Window Manager Hints) compliance.

This means setting properties on the root window like:
- `_NET_SUPPORTED` - what features your WM supports
- `_NET_CLIENT_LIST` - list of managed windows
- `_NET_CURRENT_DESKTOP` - active workspace
- `_NET_ACTIVE_WINDOW` - focused window

And respecting client requests like:
- `_NET_WM_STATE_FULLSCREEN` - app wants fullscreen
- `_NET_WM_WINDOW_TYPE_DIALOG` - should float
- `_NET_WM_STRUT_PARTIAL` - panel reserving screen space

Without EWMH, your eww bar won't know which workspace you're on, and Steam won't be able to go fullscreen properly.

## Keybindings

Keybindings in X11 require translating keysyms to keycodes:

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

Keysyms are symbolic constants (`XK_Return`, `XK_space`), while keycodes are hardware-specific. The X server provides a mapping between them.

## The Scratchpad

One feature I couldn't live without: a dropdown terminal that appears with a single keypress.

```go
func (wm *WindowManager) toggleScratchpad() {
    if wm.scratchpad.Visible {
        // Hide it
        xproto.UnmapWindow(wm.conn, wm.scratchpad.Window)
        wm.scratchpad.Visible = false
    } else {
        if wm.scratchpad.Window == 0 {
            // Spawn terminal with special class
            spawn("kitty --class scratchpad")
        } else {
            // Show and position it
            xproto.MapWindow(wm.conn, wm.scratchpad.Window)
            // Center on screen...
        }
        wm.scratchpad.Visible = true
    }
}
```

Press `Super+Grave`, terminal drops down. Press again, it disappears. Simple but incredibly useful.

## Steam Games and Fullscreen

Getting games to work properly was tricky. Steam games often use `_NET_WM_STATE_FULLSCREEN`, and you need to:

1. Detect the fullscreen request
2. Remove borders
3. Resize to full screen dimensions
4. Raise above everything (including your status bar)
5. Set the state property so panels know to hide

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

CS2 needed special handling - I had to add `steam_app_730` to the floating rules.

## What I Learned

Building a window manager taught me:

1. **X11 is old but well-designed** - The protocol is from 1987 but still makes sense
2. **Go works great for system software** - No GC pauses, clean concurrency, fast compilation
3. **EWMH is essential** - Without it, nothing works properly with modern apps
4. **Small details matter** - Focus handling, border colors, struts - they all affect usability

## Try It Yourself

The code is on GitHub: [github.com/0xb0b1/gowm](https://github.com/0xb0b1/gowm)

To build and test (without replacing your current WM):

```bash
# Install Xephyr for nested X server
sudo pacman -S xorg-server-xephyr  # Arch
sudo apt install xserver-xephyr    # Debian/Ubuntu

# Clone and build
git clone https://github.com/0xb0b1/gowm
cd gowm
go build -o gowm .

# Test in Xephyr
Xephyr :1 -screen 1920x1080 &
DISPLAY=:1 ./gowm &
DISPLAY=:1 kitty &  # Open a terminal in Xephyr
```

## What's Next?

The basics work great. Future improvements could include:

- Multi-monitor support
- More layouts (spiral, columns)
- Per-workspace layouts
- Hot-reload configuration
- Better urgency handling

But honestly? It already does everything I need. And that's the beauty of building your own tools - you can stop when *you're* satisfied.

Happy hacking!
