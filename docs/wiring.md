# Wiring it up

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
rctrl + ralt + rcmd - r : /opt/homebrew/bin/spaces open reviews
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

**Clean launches.** Everything spaces starts — the tmux server, a
Ghostty window and the program in it, an application (`open` hands the
caller's whole environment to what it launches) and a `then` command —
gets the environment spaces runs in, less what a multiplexer must not
inherit: `TMUX` and `TMUX_PANE`, `CMUX_*`, `HERDR_*`, and Claude Code's
session markers (`CLAUDECODE`, `CLAUDE_*`). A cmux launched with `TMUX`
set hands its surface id to tmux before every command and never
engages its Claude Code hooks; a tmux server, herdr server or cmux
started from inside a Claude Code session runs every agent in it as a
child session, which saves no transcript. So a space opens the same
from a hotkey, a plain shell, a tmux pane or an agent's shell. `check`
asks the multiplexers that run — tmux, and the herdr sessions and cmux
the spaces declare — how they were started, and reports one that was
started some other way with those variables in its environment; quit
it and open its space again.

**Claude state.** The `CLAUDE` column of `list` reads the
`@claude-state` window option that
[claude-status](https://github.com/stefanahman/claude-status)
maintains from Claude Code's hooks; without the plugin the column is
empty and everything else works.

**owl.** `reviews` is [owl](https://github.com/stefanahman/owl)'s
review session, one tmux window per pull request, beside `features` for
issues and `projects`. A space of that name with `then: tmux
display-popup … owl pr` is the hotkey that opens its list from
anywhere, and owl's `hooks.after_open: {tmux: spaces focus
"$OWL_SESSION"}` brings that terminal to the front after every open —
the variable, so one hook serves all three.
