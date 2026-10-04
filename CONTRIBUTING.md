# Contributing

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

To install a build from a checkout: `make install BIN=~/.local/bin`.
