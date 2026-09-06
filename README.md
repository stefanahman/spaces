# tmux-spaces

One config for your tmux sessions on desktop spaces. A *space* is a
tmux session with a fixed window and pane layout, shown in exactly one
terminal window that the window manager pins to a desktop space, and
reachable by a key. Declare it once; `tmux-spaces open` creates what's
missing and focuses what exists.

```yaml
# ~/.config/tmux-spaces/spaces.yaml
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
```

```sh
tmux-spaces open app     # session `app` with windows shell + nvim, a Ghostty window on space 3, focused
tmux-spaces open app     # again: everything exists → just focus it
tmux-spaces key 1        # what a hotkey runs
tmux-spaces list         # every space, its session state and what Claude Code is doing in it
```

Today's backend is **macOS with [yabai](https://github.com/koekeishiya/yabai)
and [Ghostty](https://ghostty.org)**; the window-manager side sits behind
a small interface so a Hyprland one can follow.

## Install

```sh
go install github.com/stefanahman/tmux-spaces@latest
```

or from a checkout, `make install BIN=~/.local/bin`. Needs tmux, yabai
and Ghostty; Go 1.25 to build. `tmux-spaces check` tells you what's
missing.

## Config

`$XDG_CONFIG_HOME/tmux-spaces/spaces.yaml` and every
`$XDG_CONFIG_HOME/tmux-spaces/spaces.d/*.yaml` are merged. A space or a
key declared twice is an error, never a silent override — the files
usually come from different places (one per dotfiles branch, one per
machine).

```yaml
spaces:
  <name>:                     # the tmux session name and the terminal window's title
    key: r                    # one of 0-9 a-z, for `tmux-spaces key r`; optional
    space: 9                  # desktop space the window is pinned to; omit to leave it where it opens
    cwd: ~/src/app            # default directory for windows, panes and the terminal; a string or a list of candidates
    windows:                  # created in this order the first time; later runs add what's missing by name
      - scratch
      - name: work
        cwd: ~/src/app/backend
        command: nvim         # runs instead of the shell; the window closes when it exits
      - name: pair
        split: horizontal     # side by side (default) or vertical
        panes:
          - {cwd: ~/src/eden}
          - {cwd: ~/src/eden-private-branches, command: nvim}
    select: work              # window shown when the terminal is spawned; default: the first
    then: tmux display-popup -E -w 88% -h 84% pr-owl   # run after `open` has focused the space
```

`~` and `$VAR` are expanded. `then` runs through `tmux run-shell` inside
the session, in the background, after a client is attached — so
`display-popup` works straight from a hotkey. Give it absolute paths:
it runs with tmux's PATH, not your shell's.

## Commands

| | |
|---|---|
| `open <name>` | ensure the session and its windows, find or spawn the terminal window, pin it to `space` when new, focus it, then run `then` |
| `focus <name>` | the same without `then` — for other tools that just need the space in front |
| `key <k>` | `open` the space bound to `k`; exit 1 when none is |
| `list` | name, key, space, session state, and the [tmux-claude-status](https://github.com/stefanahman/tmux-claude-status) chip of its windows (`1⚠ 2~ 1* 3`: blocked, working, done, idle) |
| `yabai-rules` | one `yabai -m rule` per pinned space — `eval` it in your yabairc so the space number has one home |
| `check` | missing directories, tools, and desktop spaces claimed twice |
| `config path` | the directory it reads |

Exit status: 64 for a bad invocation, 1 for a failure.

## Wiring it up

**Titles.** `open` sets `set-titles on` and `set-titles-string '#S'` on
the session, so the terminal's title is always the session name —
whatever the shells inside do with OSC title sequences. That's how the
window is found again, and what the yabai rule keys on.

**yabai.** In `yabairc`:

```sh
eval "$($HOME/.local/bin/tmux-spaces yabai-rules)"   # absolute path: yabai starts with a minimal PATH
```

Rules fire when a window is *created*; `open` also moves a freshly
spawned window itself, so the first open lands right even before yabai
reloads.

**Hotkeys.** With [skhd](https://github.com/koekeishiya/skhd):

```
rctrl + ralt + rcmd - r : $HOME/.local/bin/tmux-spaces open pr-reviews
```

With a Karabiner-Elements leader (`Hyper+X`, then a key), each key's
`shell_command` is `$HOME/.local/bin/tmux-spaces key <k>` — the keys
themselves live in the config.

**pr-owl.** `hooks.after_open: ~/.local/bin/tmux-spaces focus pr-reviews`
brings the review terminal to the front after every
[pr-owl](https://github.com/stefanahman/pr-owl) open, and a `pr-reviews`
space with `then: tmux display-popup … pr-owl` is the hotkey that opens
the popup from anywhere.

## Hacking

```sh
make test   # a private tmux server per test; the window manager is a recording fake
make lint
```

The macOS backend is `desktop.go`; a Linux one implements the same
four-method interface (find the window by title, spawn a terminal
attached to the session, move it to a workspace, focus it).

## License

MIT
