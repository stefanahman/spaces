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
make test      # go test ./...
make lint      # gofmt, go vet
```

Check exit codes, not output. Verify against the live machine
read-only: `spaces check`, `spaces list`, `spaces yabai-rules`. Never
open spaces, launch Ghostty, herdr or cmux from the agent's shell — an
app launched from a Claude session inherits its markers; the user
presses the hotkey.

## Commit

Conventional commits, lower-case subject, a body that says why.
Smallest coherent commits, dependencies before consumers. No
Co-Authored-By trailers.

## Push, and install the checkout build

```sh
git push origin main
make install BIN=~/.eden/bin     # the launchers search ~/.eden/bin before the cask
```

CI is `ci.yml`; `gh run list --workflow ci --branch main --limit 1`.

## Document

README.md is the reference for the config keys and the verbs; a new
key or verb lands there in the same commit series.

## Ship at a milestone

Not after every change. Before tagging: tree clean, no rebase in
progress, HEAD pushed, CI green at HEAD.

```sh
git tag -a v0.8.2 -m "spaces 0.8.2: <the batch, one line>"
git push origin v0.8.2
```

The tag runs `release.yml` (goreleaser: binaries, the GitHub release,
the cask in stefanahman/homebrew-tap). Then, in the background:

```sh
gh run list --workflow release --branch v0.8.2 --json status,conclusion   # until completed success
brew update && brew upgrade --cask spaces
/opt/homebrew/bin/spaces --version
```

A change in mux comes first: tag mux, `GOPROXY=direct go get
github.com/stefanahman/mux@vX.Y.Z && go mod tidy`, one commit `build:
mux vX.Y.Z — <why>`, then ship.
