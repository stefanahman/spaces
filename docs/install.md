# Install

```sh
brew install --cask stefanahman/tap/spaces
go install github.com/stefanahman/spaces@latest   # with Go 1.26
```

or from a checkout, `make install BIN=~/.local/bin`. The cask puts the
binary in Homebrew's bin (`/opt/homebrew/bin/spaces`); the paths in
[wiring.md](wiring.md) assume that — use `~/.local/bin/spaces` after `make install`.

It needs tmux, [yabai](https://github.com/koekeishiya/yabai) with its
accessibility permission granted, and [Ghostty](https://ghostty.org):

```sh
brew install tmux koekeishiya/formulae/yabai
brew install --cask ghostty
```

`spaces check` tells you what's missing. For workspaces inside herdr
or cmux ([config.md](config.md)), the herdr or cmux install is the prerequisite; cmux's
cask links its `cmux` command, which spaces drives.

Files it keeps: the config in `$XDG_CONFIG_HOME/spaces/`, the active
multiplexer in `$XDG_STATE_HOME/spaces/multiplexer`, per-space locks in
`$XDG_CACHE_HOME/spaces/` (with `~/.config`, `~/.local/state` and the
OS cache dir as the defaults).

spaces was called tmux-spaces until its spaces stopped being only tmux
sessions. The tap maps the old cask name to the new one.
