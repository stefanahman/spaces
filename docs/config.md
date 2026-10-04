# Config

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
    then: tmux display-popup -E -w 88% -h 84% owl pr   # run after `open` has focused the space
  <command-space>:
    command: herdr --session work   # instead of windows: the terminal runs this program, no tmux session
    then: say "herdr is up"         # runs through `sh -c` once the window is in front
  <app-space>:
    app: cmux                       # instead of windows or command: a macOS application, pinned by its name
    env: {CMUX_SOCKET_MODE: allowAll}   # its environment when this space launches it
  <run-space>:
    key: m                          # required: the key is the only way to reach it
    run: mindoro toggle             # a command the key runs, through sh -c — no window, no desktop space
```

The three kinds above open something and pin it; the fourth, a **run
space**, opens nothing. Its key runs a command and returns: a toggle,
a script, anything that belongs on the leader beside the spaces. It
takes `key` and `run` only — no `space`, `cwd`, `windows`, `select`
or `then` — and its key works whatever multiplexer is active. `run`
is waited for: a hotkey has no terminal, so a non-zero exit becomes a
desktop notification with the command's output, where a background
failure would be silent. `spaces check` confirms the command's program
is on PATH.

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
      prs: {cwd: ~/src/app, windows: [{name: prs, command: owl pr}]}   # owl detects the multiplexer it runs in
    select: prs                   # the workspace shown when the space opens
  cmux:
    key: c
    space: 8
    app: cmux
    env: {CMUX_SOCKET_MODE: allowAll}   # cmux takes its socket mode from the environment only
    multiplexer: cmux
    workspaces: *work
    select: prs
```

`open` and `focus` both build the workspaces once the window is up
and before `then`. Workspaces are created in the order the file
declares them, each in the directory of its first window's first pane
(else the window's, else its own). herdr keeps them in the order they
were made, so one made while others exist — `on_demand`, or closed and
made again — is moved to its declared place once its windows are
built: after the nearest workspace declared before it, else in front
of the nearest one declared after it. A move that fails is said on
stderr and costs nothing else. One that exists is never moved: an
order set by hand in the sidebar stays. The first window is the pane a workspace comes with; every
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
after its launch. A run lists its workspaces once and reads each one's
tabs and panes, and under herdr lists them again for each workspace it
makes: a key press costs a fraction of a second.

`multiplexer` and `workspaces` come together, each an error without
the other; `session` applies to herdr. `select` names a workspace.

**Groups.** cmux keeps workspaces in collapsible sidebar groups. A
workspace can name the one it joins, and a top-level `groups:` map
gives that name a look:

```yaml
groups:                       # merged across files like spaces are
  Tooling:  {color: "#8fa1b3", icon: wrench.and.screwdriver}
  Features: {color: "#a3be8c"}

spaces:
  cmux:
    app: cmux
    multiplexer: cmux
    workspaces:
      eden: {key: e, group: Tooling, cwd: ~/src/eden, windows: [shell]}
      prs:  {key: r, group: Tooling, cwd: ~/src/app, windows: [{name: prs, command: owl pr}]}
      bf-1: {key: "1", group: Features, cwd: ~/src/app, windows: [shell]}
```

Both keys are optional. A `group:` naming nothing in `groups:` is made
with cmux's own look; `color` is `#RRGGBB` and `icon` an SF Symbol
name, and a group is created anchored on the first workspace that
joins it. **Only a workspace this run creates is grouped**: dragging
one out of a group in the sidebar is your decision, and the next open
leaves it where you put it. herdr and tmux have no grouping — tmux's
container is the session — so a `group:` is inert there and one config
serves all three.

Groups merge by name across files. The same declaration in two files
is one declaration; two that disagree are an error, as a space's name
or a key bound twice already is. `check` warns about a group no
workspace joins.

**Which app, when several could answer.** `socket:` names the
multiplexer's socket, for the case where the default is not the one
wanted — a nightly cmux beside a stable one binds its own and holds
its own workspaces, while the CLI's default reaches whichever owns
`~/.local/state/cmux/cmux.sock`. Without it a space can focus one app
and build its workspaces in the other:

```yaml
  cmux:
    key: c
    space: 2
    app: cmux NIGHTLY
    socket: /tmp/cmux-nightly.sock
    multiplexer: cmux
```

A process started *inside* a cmux terminal needs none of this: the app
puts its own socket in the environment, and the CLI follows it. This is
for what spaces does from outside — a hotkey. herdr takes the same key,
where `session:` does not name the socket wanted.

**Workspaces that come and go.** A workspace whose program you open,
use and quit — lazygit, a picker — does not have to sit there idling
in between. `on_demand: true` ties its life to that program's:

```yaml
      lazygit: {key: g, on_demand: true, cwd: ~/src/app, windows: [{name: lazygit, command: lazygit}]}
```

It is made when a run lands on it — its own key, or the space's
`select:` — and skipped by every other open, so reaching for a
different key no longer rebuilds it. Its command is typed with the line
that ends the workspace appended, so quitting the program takes the
workspace with it: `exit` under tmux and herdr, which drop a workspace
whose shell has gone, and `cmux workspace close` under cmux, which
keeps the shell alive after the command by design.

Because one program's end is the whole contract, `on_demand` wants
exactly one window, running a command, with no extra panes; anything
else is a config error. A space that *selects* an on_demand workspace
makes it on every open, which is what `on_demand` was asked not to do —
`check` warns about that combination.

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
      prs: {key: r, cwd: ~/src/app, windows: [{name: prs, command: owl pr}]}
    select: prs
```

`spaces use` lists the multiplexers the config declares, the active
one marked, and on a terminal asks which to use — a number or a name;
an empty answer leaves it. `spaces use cmux` sets it outright. A
change is printed and shown as a desktop notification. `spaces key 1`
then opens the holder in the active multiplexer: the tmux space, or
the herdr space on its `app` workspace, which lands on that workspace
instead of the space's `select`. A key bound in only one multiplexer
opens there whatever is active — `r` above, or a space's own key. A
key bound in several with none of them active opens nothing and says
so in a desktop notification, naming them — a hotkey has no terminal
to answer on. Within one multiplexer a key is bound at most once: two tmux
spaces, two workspaces of one cmux space or of two, or a space and one
of its own workspaces on the same key are errors that name both.

A hotkey has no terminal to fail in, so whatever stops `key` — a key
bound in two multiplexers, a multiplexer that refuses, a window that
never came — is shown as a desktop notification as well.
