# spaces

One config for your windows on desktop spaces. A *space* is exactly
one window that the window manager pins to a desktop space, reachable
by a key. Usually a terminal showing a tmux session with a
fixed window and pane layout — or, for a program that is a multiplexer
itself, running that program directly — or an application's window,
pinned by the application's name. Declare it once; `spaces open`
creates what's missing and focuses what exists. (Formerly tmux-spaces:
the spaces stopped being only tmux sessions.)

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
spaces list         # every space, its session state and what Claude Code is doing in it
```

Today's backend is **macOS with [yabai](https://github.com/koekeishiya/yabai)
and [Ghostty](https://ghostty.org)**; the window-manager side sits behind
a small interface so a Hyprland one can follow.

## Install

```sh
brew install --cask stefanahman/tap/spaces
go install github.com/stefanahman/spaces@latest   # with Go 1.26
```

or from a checkout, `make install BIN=~/.local/bin`. The cask puts the
binary in Homebrew's bin (`/opt/homebrew/bin/spaces`); the paths below
assume that — use `~/.local/bin/spaces` after `make install`.

It needs tmux, [yabai](https://github.com/koekeishiya/yabai) with its
accessibility permission granted, and [Ghostty](https://ghostty.org):

```sh
brew install tmux koekeishiya/formulae/yabai
brew install --cask ghostty
```

`spaces check` tells you what's missing. For workspaces inside herdr
or cmux (below), the herdr or cmux install is the prerequisite; cmux's
cask links its `cmux` command, which spaces drives.

Files it keeps: the config in `$XDG_CONFIG_HOME/spaces/`, the active
multiplexer in `$XDG_STATE_HOME/spaces/multiplexer`, per-space locks in
`$XDG_CACHE_HOME/spaces/` (with `~/.config`, `~/.local/state` and the
OS cache dir as the defaults).

## Config

`$XDG_CONFIG_HOME/spaces/spaces.yaml` and every
`$XDG_CONFIG_HOME/spaces/spaces.d/*.yaml` are merged. A space declared
twice is an error, and so is a key bound twice within one multiplexer
(see *Switching multiplexer*) — never a silent override, since the
files usually come from different places (one per dotfiles branch,
one per machine).

The three kinds of space, every key annotated (a schema, not a file:
the names are placeholders):

```yaml
spaces:
  <tmux-space>:               # letters, digits, - and _: the tmux session name and the terminal window's title
    key: r                    # one of 0-9 a-z, for `spaces key r`; optional
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
          - {cwd: ~/src/app}
          - {cwd: ~/src/docs, command: nvim}
    select: work              # window selected when the session is created; default: the first
    then: tmux display-popup -E -w 88% -h 84% pr-owl   # run after `open` has focused the space
  <command-space>:
    command: herdr --session work   # instead of windows: the terminal runs this program, no tmux session
    then: say "herdr is up"         # runs through `sh -c` once the window is in front
  <app-space>:
    app: cmux                       # instead of windows or command: a macOS application, pinned by its name
    env: {CMUX_SOCKET_MODE: allowAll}   # its environment when this space launches it
```

`~` and `$VAR` are expanded. `then` runs through `tmux run-shell` inside
the session, in the background, after a client is attached — so
`display-popup` works straight from a hotkey. tmux expands the command
with its format engine first: `#{…}` and `#S` are substituted, and a
literal `#` must be written `##`. Give it absolute paths: it runs with
tmux's PATH, not your shell's.

**`command` instead of `windows`.** For a program that multiplexes on
its own — [herdr](https://herdr.dev), say — a tmux session underneath
would only add a prefix key and a status bar. Such a space names the
program instead: a string split on whitespace, or a list when an
argument contains a space. It is resolved on PATH and run in the
terminal window with no shell in between. Its `then` runs through
`sh -c`, in the background, as soon as the window is in front; with no
session for tmux to show its output in, it goes where spaces's
does. `windows`, `select` and `command` don't mix.

**`app`.** A space can also be an application — its name as the
window manager reports it and `open -a` accepts it, `Slack` or `Visual
Studio Code`. `open` focuses the application's first window, and
launches the application when it has none (macOS keeps an application
alive with no windows; `open -a` then activates it, which reopens one
for most apps). `then` runs as for a command space. No `cwd`, no
`select`; `windows`, `command` and `app` don't mix.

`env` is for an application that takes its configuration from the
environment — cmux opens its socket to outside processes only when
launched with `CMUX_SOCKET_MODE=allowAll`, and ignores the setting file
for that. Each entry becomes an `--env KEY=VALUE` to `open -a`, sorted
by name; values expand `~` and `$VAR`. The variables reach only the
launch this space performs: the same application started from the
Dock or Spotlight does not have them, and an application that is
already running is only activated. `env` is an error on `windows` and
`command` spaces, where it would not do what it says — a tmux window
inherits the server's environment, a command runs in Ghostty's.

**Workspaces inside an app.** A space whose program or application is
a multiplexer itself — herdr, cmux — can declare the workspaces to
build inside it, with the same `windows` and `panes` schema a tmux
space uses. Declared once, they are built where they are missing and
left alone where they exist: neither herdr nor cmux knows a startup
layout, and both bring their workspaces back across a restart as
shells without their programs. The building goes through
[mux](https://github.com/stefanahman/mux)'s drivers. A YAML anchor
declares the context once for both:

```yaml
spaces:
  herdr:
    key: h
    space: 7
    command: herdr --session work
    multiplexer: herdr            # what renders `workspaces`: herdr or cmux
    session: work                 # herdr only: its socket is ~/.config/herdr/sessions/<session>/herdr.sock
    workspaces: &work
      app: {cwd: ~/src/app, windows: [shell, {name: nvim, command: nvim}]}
      docs:
        cwd: ~/src/docs
        windows:
          - {name: docs, panes: [{}, {cwd: ~/src/docs/site}]}
          - {name: nvim, command: nvim, cwd: ~/src/docs/site}
      pr-owl: {cwd: ~/src/app, windows: [{name: pr-owl, command: pr-owl}]}   # pr-owl detects the multiplexer it runs in
    select: pr-owl                # the workspace shown when the space opens
  cmux:
    key: c
    space: 8
    app: cmux
    env: {CMUX_SOCKET_MODE: allowAll}   # cmux takes its socket mode from the environment only
    multiplexer: cmux
    workspaces: *work
    select: pr-owl
```

`open` and `focus` both build the workspaces once the window is up
and before `then`. Workspaces are created in name order, each in the
directory of its first window's first pane (else the window's, else
its own). The first window is the pane a workspace comes with; every
window after it is a tab — a herdr tab, a cmux surface — and every
pane after a window's first a split off it (`split: vertical` splits
downward). What exists is found by name (workspaces) or by position
(tabs, panes) and never renamed or closed. A command is typed only
into a pane that runs nothing but its shell: a program already
running is left running, and a pane running anything else is left
alone and said so on stderr — keystrokes into a program are commands
to it. A pane this run created is a new shell and is typed into
without a look; the line waits in the pty for the prompt. Under cmux
the look is `ps` on the surface's tty where cmux knows it (the
surface a workspace was created with) and the tab's title otherwise —
cmux's shell integration names the running program there, or the
directory at a prompt. The multiplexer gets 20 seconds to answer
after its launch, and is read three times per run, not per workspace:
a key press costs a fraction of a second.

`multiplexer` and `workspaces` come together, each an error without
the other; `session` applies to herdr. `select` names a workspace.

**Switching multiplexer.** A workspace inside a herdr or cmux space
can carry a `key:` too, and one key may be bound once per multiplexer
— bf-1 as a tmux space on key 1, as a herdr workspace on key 1, as a
cmux workspace on key 1. Which one a press opens is the *active
multiplexer*: one word in `$XDG_STATE_HOME/spaces/multiplexer`
(`~/.local/state/spaces/multiplexer`), `tmux` when absent.

```yaml
spaces:
  app:
    key: "1"                      # the tmux space
    space: 3
    windows: [shell, {name: nvim, command: nvim}]
  herdr:
    key: h
    space: 7
    command: herdr --session work
    multiplexer: herdr
    session: work
    workspaces:
      app: {key: "1", cwd: ~/src/app, windows: [shell, {name: nvim, command: nvim}]}   # the same key, in herdr
      pr-owl: {key: r, cwd: ~/src/app, windows: [{name: pr-owl, command: pr-owl}]}
    select: pr-owl
```

`spaces use` lists the multiplexers the config declares, the active
one marked, and on a terminal asks which to use — a number or a name;
an empty answer leaves it. `spaces use cmux` sets it outright. A
change is printed and shown as a desktop notification. `spaces key 1`
then opens the holder in the active multiplexer: the tmux space, or
the herdr space on its `app` workspace, which lands on that workspace
instead of the space's `select`. A key bound in only one multiplexer
opens there whatever is active — `r` above, or a space's own key. A
key bound in several with none of them active is refused, naming
them. Within one multiplexer a key is bound at most once: two tmux
spaces, two workspaces of one cmux space or of two, or a space and one
of its own workspaces on the same key are errors that name both.

## Commands

| | |
|---|---|
| `open <name>` | ensure the session and its windows (or the program), find or spawn the terminal window — or find or launch the application — pin it to `space` when new, focus it, build its workspaces, then run `then` |
| `focus <name>` | the same without `then` — for other tools that just need the space in front |
| `key <k>` | `open` what `k` is bound to — a space, or a herdr/cmux space on one of its workspaces — the active multiplexer deciding when several are; exit 1 when none is, or when a choice is needed |
| `use [tmux\|herdr\|cmux]` | pick, or set, the active multiplexer; without an argument, off a terminal, just list them with the active one marked |
| `list` | the active multiplexer, then name, key (a space's own, and its workspaces' in brackets), space, session state (for a command or app space: whether its window is open, and how many of its workspaces exist), and the [tmux-claude-status](https://github.com/stefanahman/tmux-claude-status) chip of its windows (`1⚠ 2~ 1* 3`: blocked, working, done, idle) |
| `yabai-rules` | one `yabai -m rule` per pinned space — `eval` it in your yabairc so the space number has one home |
| `check` | missing directories (workspaces' too), commands, applications and tools, desktop spaces that don't exist or are claimed twice |
| `config path` | the directory it reads |

Exit status: 64 for a bad invocation, 1 for a failure.

## Wiring it up

**Titles.** `open` sets `set-titles on` and `set-titles-string '#S'` on
the session, so the terminal's title is always the session name —
whatever the shells inside do with OSC title sequences. That's how the
window is found again, and what the yabai rule keys on. A command
space has no session to set options on, and needs none: Ghostty keeps
the `--title` it is started with and ignores title sequences from the
program inside. An app space is keyed on the application's name
instead — `app="^cmux$"`, with `(`, `)` and `.` escaped for the regex —
so the rule pins whatever window the application opens.

**yabai.** In `yabairc`:

```sh
eval "$(/opt/homebrew/bin/spaces yabai-rules)"   # absolute path: yabai starts with a minimal PATH
```

Rules fire when a window is *created*; `open` also moves a freshly
spawned window itself, so the first open lands right even before yabai
reloads.

**Hotkeys.** With [skhd](https://github.com/koekeishiya/skhd):

```
rctrl + ralt + rcmd - r : /opt/homebrew/bin/spaces open pr-reviews
```

With a Karabiner-Elements leader (a modifier chord, then a key), each
key's `shell_command` is `/opt/homebrew/bin/spaces key <k>` — the keys
themselves live in the config, so the Karabiner rules never change. One
such rule, for the key `1` after a leader that set the variable
`spaces_leader`:

```json
{ "type": "basic",
  "from": { "key_code": "1" },
  "conditions": [{ "type": "variable_if", "name": "spaces_leader", "value": 1 }],
  "to": [{ "set_variable": { "name": "spaces_leader", "value": 0 } },
         { "shell_command": "/opt/homebrew/bin/spaces key 1" }] }
```

Hotkey daemons run commands with the base PATH (Karabiner's
`shell_command` gets `/usr/bin:/bin:/usr/sbin:/sbin`), so spaces
appends `/opt/homebrew/bin` and `/usr/local/bin` to its own and finds
tmux and yabai there without a login shell in between; your PATH still
comes first. The tmux server it starts inherits that PATH, which is
one more reason `then` wants absolute paths.

**Claude state.** The `CLAUDE` column of `list` reads the
`@claude-state` window option that
[tmux-claude-status](https://github.com/stefanahman/tmux-claude-status)
maintains from Claude Code's hooks; without the plugin the column is
empty and everything else works.

**pr-owl.** `pr-reviews` is [pr-owl](https://github.com/stefanahman/pr-owl)'s
review session, one tmux window per pull request. A `pr-reviews` space
with `then: tmux display-popup … pr-owl` is the hotkey that opens its
popup from anywhere, and pr-owl's `hooks.after_open: {tmux: spaces
focus pr-reviews}` brings that terminal to the front after every open.

## Hacking

```sh
make test   # a private tmux server per test; the window manager is a recording fake
make lint
```

The macOS backend is `desktop.go`; a Linux one implements the same
small interface: find the terminal window by title or an application's
by name, spawn a terminal running a command (tmux attaching to the
session, or the space's program), launch an application, move a window
to a desktop space, focus it, and — for `check` — list the desktop
spaces and the tools it needs.

## License

MIT
