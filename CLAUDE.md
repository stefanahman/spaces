# spaces — agent notes

spaces keeps one config for windows on desktop spaces and the hotkeys
that reach them (README.md); it imports mux for herdr and cmux
workspaces. It ships as a Homebrew cask, `stefanahman/tap/spaces`.

A change is done when it is committed in coherent pieces, pushed with
CI green, installed where the hotkeys find it, documented, and — at a
milestone — tagged so the cask other machines install catches up. Do
all of it and say which steps you did.

## Build and test

```sh
make test      # go test ./... — a private tmux server per test
make lint      # gofmt, go vet
make build     # the binary at the repo root, for a quick try
go run honnef.co/go/tools/cmd/staticcheck@2026.2.1 ./...     # CI's analysis job
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...        # the rest of it
```

Check exit codes, not output. `make test && make lint` green is not CI
green: the `analysis` job runs the two commands above, and the `go` job
also runs `goreleaser check`, which is what keeps a broken
`.goreleaser.yaml` from surfacing at tag time.

Every test is hermetic: a private tmux server per test, and the window
manager is a recording fake — the macOS backend behind that interface is
`desktop.go` (find a window by title or an app by name, spawn a
terminal, move a window to a space, focus it, list the spaces for
`check`). Verify against the live machine read-only: `spaces check`,
`spaces list`, `spaces yabai-rules`. Never open spaces, launch Ghostty,
herdr or cmux from the agent's shell — an app launched from a Claude
session inherits its markers; the user presses the hotkey.

## Commit

Conventional commits, lower-case subject, a body that says why.
Smallest coherent commits, dependencies before consumers. No
Co-Authored-By trailers.

## Push, and install the checkout build

```sh
git push origin main
make install BIN=~/.eden/bin     # the launchers search ~/.eden/bin before the cask
spaces --version                 # says which build answered: the checkout stamps git describe, the cask a bare version
```

CI (`.github/workflows/ci.yml`): `go` (lint, test, `goreleaser check`)
and `analysis` (staticcheck, govulncheck). `gh run list --workflow ci
--branch main --limit 1` shows it.

Karabiner and skhd prefix `~/.eden/bin` on PATH, so a hotkey runs the
checkout build while `spaces` typed in a login shell runs the cask —
after a release, install again so the two agree.

## Document

README.md is the reference for the config keys and the verbs; a new
key or verb lands there in the same commit series.

Three consumers live outside this repo, and a renamed verb or key
breaks them silently: Karabiner runs `spaces key <k>` for every leader
chord, yabairc `eval`s `spaces yabai-rules` for the window pins, and the
space definitions themselves are `.config/spaces/spaces.d/*.yaml` in
Stefan's eden checkout. None is yours to edit from here: say so in the
handoff.

## Ship at a milestone

Not after every change. Before tagging: tree clean, no rebase in
progress, HEAD pushed, CI green at HEAD.

```sh
git tag -a v0.8.2 -m "spaces 0.8.2: <the batch, one line>"
git push origin v0.8.2
```

The tag runs `release.yml`: goreleaser builds the macOS binaries,
publishes the GitHub release and rewrites the cask in
stefanahman/homebrew-tap, which needs the `HOMEBREW_TAP_GITHUB_TOKEN`
secret — a release that fails at the cask step usually means that token
expired. Then, in the background:

```sh
gh run list --workflow release --branch v0.8.2 --json status,conclusion   # until completed success
brew update && brew upgrade --cask spaces
/opt/homebrew/bin/spaces --version
```

A change in mux comes first: tag mux, `GOPROXY=direct go get
github.com/stefanahman/mux@vX.Y.Z && go mod tidy`, one commit `build:
mux vX.Y.Z — <why>`, then ship.
