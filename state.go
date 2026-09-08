// The active multiplexer: which of tmux, herdr and cmux a key opens
// when the same key is bound in more than one of them. One word in a
// state file, switched with `use` — from a hotkey, to try another
// multiplexer with the keys one already knows.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// multiplexers, in the order `use` lists them.
var multiplexers = []string{"tmux", "herdr", "cmux"}

// stateFile is $XDG_STATE_HOME/spaces/multiplexer, else
// ~/.local/state/spaces/multiplexer.
func stateFile() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "spaces", "multiplexer"), nil
}

// activeMultiplexer reads the state; absent means tmux.
func activeMultiplexer() (string, error) {
	path, err := stateFile()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "tmux", nil
	}
	if err != nil {
		return "", err
	}
	kind := strings.TrimSpace(string(data))
	if !slices.Contains(multiplexers, kind) {
		return "", fmt.Errorf("%s: %q is not a multiplexer (tmux, herdr, cmux)", path, kind)
	}
	return kind, nil
}

// setActiveMultiplexer records the state.
func setActiveMultiplexer(kind string) error {
	path, err := stateFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(kind+"\n"), 0o644)
}

// declaredMultiplexers lists the multiplexers the config has anything
// in, in order: tmux for a session space, herdr and cmux for a space
// that builds workspaces there.
func declaredMultiplexers(spaces []Space) []string {
	have := map[string]bool{}
	for _, sp := range spaces {
		switch {
		case len(sp.Windows) > 0:
			have["tmux"] = true
		case sp.Multiplexer != "":
			have[sp.Multiplexer] = true
		}
	}
	var out []string
	for _, m := range multiplexers {
		if have[m] {
			out = append(out, m)
		}
	}
	return out
}

// stdinIsTerminal reports whether stdin is a terminal, in which case
// `use` without an argument asks; a variable, so tests can decide. An
// isatty check, not a character-device one: /dev/null is a character
// device too.
var stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// useCmd is `spaces use [tmux|herdr|cmux]`. With a name it records
// that one. Without, it lists the multiplexers the config declares,
// the active one marked, and — on a terminal — reads the choice, a
// number or a name; an empty line leaves things as they are. A change
// is printed and shown as a desktop notification.
func useCmd(d desktop, spaces []Space, arg string, in io.Reader, out io.Writer) error {
	active, err := activeMultiplexer()
	if err != nil {
		return err
	}
	kind := arg
	if arg == "" {
		declared := declaredMultiplexers(spaces)
		if len(declared) == 0 {
			return errors.New("use: no multiplexer is declared (no tmux space, no workspaces)")
		}
		for i, m := range declared {
			mark := " "
			if m == active {
				mark = "*"
			}
			fmt.Fprintf(out, "%s %d) %s\n", mark, i+1, m)
		}
		if !stdinIsTerminal() {
			return nil
		}
		fmt.Fprint(out, "use: ")
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		answer := strings.TrimSpace(line)
		if answer == "" {
			return nil
		}
		if n, err := strconv.Atoi(answer); err == nil {
			if n < 1 || n > len(declared) {
				return usageError(fmt.Sprintf("use: %d is not on the list", n))
			}
			kind = declared[n-1]
		} else {
			kind = answer
		}
	}
	if !slices.Contains(multiplexers, kind) {
		return usageError("use: expected tmux, herdr or cmux, got " + kind)
	}
	if err := setActiveMultiplexer(kind); err != nil {
		return err
	}
	fmt.Fprintln(out, kind)
	if kind != active && d != nil {
		return d.notify("spaces", kind)
	}
	return nil
}
