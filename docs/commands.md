# Commands

| | |
|---|---|
| `open <name>` | ensure the session and its windows (or the program), find or spawn the terminal window — or find or launch the application — pin it to `space` when new, focus it, build its workspaces, then run `then` |
| `focus <name>` | the same without `then` — for other tools that just need the space in front |
| `key <k>` | `open` what `k` is bound to — a space, or a herdr/cmux space on one of its workspaces — the active multiplexer deciding when several are; exit 1 when none is, or when a choice is needed |
| `use [tmux\|herdr\|cmux]` | pick, or set, the active multiplexer; without an argument, off a terminal, just list them with the active one marked |
| `list` | the active multiplexer, then name, key (a space's own, and its workspaces' in brackets), space, session state (for a command or app space: whether its window is open, and how many of its workspaces exist — `on_demand` ones counted apart, since absent is their resting state), and the [claude-status](https://github.com/stefanahman/claude-status) chip of its windows (`1⚠ 2~ 1* 3`: blocked, working, done, idle) |
| `yabai-rules` | one `yabai -m rule` per pinned space — `eval` it in your yabairc so the space number has one home |
| `check` | missing directories (workspaces' too), commands, applications and tools, desktop spaces that don't exist or are claimed twice, a group no workspace joins, an `on_demand` workspace a space selects, and a running multiplexer that was started with the wrong environment (see *Clean launches* in [wiring.md](wiring.md)) |
| `config path` | the directory it reads |

Exit status: 64 for a bad invocation, 1 for a failure.
