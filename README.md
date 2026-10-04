# spaces

Hotkeys for tmux, herdr and cmux workspaces, pinned to desktop spaces.

[![ci](https://github.com/stefanahman/spaces/actions/workflows/ci.yml/badge.svg)](https://github.com/stefanahman/spaces/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/stefanahman/spaces)](https://github.com/stefanahman/spaces/releases)
[![license](https://img.shields.io/github/license/stefanahman/spaces)](LICENSE)

A space is one window on one desktop space, reached by one key. The
window is a terminal showing a tmux session with fixed windows, a
program that is its own multiplexer (herdr), or an application (cmux).
Declare it once; `spaces open` creates what's missing and focuses what
exists, so a key always lands in the same place.

```yaml
# ~/.config/spaces/spaces.yaml
spaces:
  app:
    key: "1"
    space: 3
    cwd: ~/src/app
    windows:
      - shell                        # a bare string: a window of that name, running your shell
      - {name: nvim, command: nvim}
  notes:
    key: n
    space: 4
    cwd: [~/notes, ~/Documents/notes]  # a list: the first directory that exists
    windows: [shell]
  herdr:
    key: h
    space: 7
    cwd: ~/src/app
    command: herdr --session work      # no tmux: the window runs this program itself
  cmux:
    key: c
    space: 8
    app: cmux                          # no terminal either: an application, by name
```

```sh
spaces open app     # session `app` with windows shell + nvim, a Ghostty window on space 3, focused
spaces open app     # again: everything exists → just focus it
spaces key 1        # what a hotkey runs

It runs on macOS with [yabai](https://github.com/koekeishiya/yabai)
and [Ghostty](https://ghostty.org). The window-manager side sits
behind a small interface, so a Hyprland one can follow.

## Install

```sh
brew install --cask stefanahman/tap/spaces
brew install tmux koekeishiya/formulae/yabai
brew install --cask ghostty
```

yabai needs its accessibility permission. `spaces check` names
anything missing. [docs/install.md](docs/install.md) has `go install`
and the files spaces keeps.

## Quick start

1. Write `~/.config/spaces/spaces.yaml`, as in the example above.
2. Run `spaces check`, then `spaces open <name>`.
3. Bind a hotkey to `spaces key <k>` and add `spaces yabai-rules` to
   your yabairc: [docs/wiring.md](docs/wiring.md).

## Docs

- [Config](docs/config.md): every key, workspaces inside herdr and
  cmux, groups, on-demand workspaces, switching multiplexer
- [Commands](docs/commands.md): every verb and exit status
- [Wiring it up](docs/wiring.md): window titles, yabai, hotkeys, clean
  launches, Claude state, owl
- [Install](docs/install.md): other ways to install, prerequisites,
  files it keeps

See also: [owl](https://github.com/stefanahman/owl) ·
[mux](https://github.com/stefanahman/mux) ·
[mcp-defer](https://github.com/stefanahman/mcp-defer) ·
[claude-status](https://github.com/stefanahman/claude-status) ·
[mindoro](https://github.com/stefanahman/mindoro) ·
[eden](https://github.com/stefanahman/eden)

## License

MIT
