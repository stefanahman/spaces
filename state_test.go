package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestActiveMultiplexerState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if got, err := activeMultiplexer(); err != nil || got != "tmux" {
		t.Errorf("no state: %q, %v; want tmux", got, err)
	}
	if err := setActiveMultiplexer("cmux"); err != nil {
		t.Fatal(err)
	}
	path, _ := stateFile()
	if data, _ := os.ReadFile(path); string(data) != "cmux\n" || !strings.HasSuffix(path, filepath.Join("spaces", "multiplexer")) {
		t.Errorf("state file %s holds %q", path, data)
	}
	if got, err := activeMultiplexer(); err != nil || got != "cmux" {
		t.Errorf("after set: %q, %v", got, err)
	}
	if err := os.WriteFile(path, []byte("screen\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := activeMultiplexer(); err == nil || !strings.Contains(err.Error(), "not a multiplexer") {
		t.Errorf("bad state: %v", err)
	}
}

// terminal decides what `use` takes stdin for.
func terminal(t *testing.T, is bool) {
	t.Helper()
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return is }
	t.Cleanup(func() { stdinIsTerminal = orig })
}

func TestUse(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	shell := []Window{{Name: "shell"}}
	work := map[string]Workspace{"bf-1": {Windows: shell}}
	spaces := []Space{
		{Name: "bf-1", Key: "1", Windows: shell},
		{Name: "cmux", Key: "c", App: "cmux", Multiplexer: "cmux", Workspaces: work},
	}
	if got := declaredMultiplexers(spaces); !reflect.DeepEqual(got, []string{"tmux", "cmux"}) {
		t.Errorf("declared = %v", got)
	}
	d := newFakeDesktop()
	var out strings.Builder
	use := func(arg, input string) error {
		out.Reset()
		return useCmd(d, spaces, arg, strings.NewReader(input), &out)
	}

	// Not a terminal: the list, the active one marked, nothing asked.
	terminal(t, false)
	if err := use("", "2\n"); err != nil || out.String() != "* 1) tmux\n  2) cmux\n" || len(d.calls) != 0 {
		t.Errorf("listing: %q, %v, calls %v", out.String(), err, d.calls)
	}

	// A terminal: the list, then a number or a name; herdr is not on
	// the list, but a name is taken whether declared or not.
	terminal(t, true)
	if err := use("", "2\n"); err != nil || out.String() != "* 1) tmux\n  2) cmux\nuse: cmux\n" {
		t.Errorf("picked 2: %q, %v", out.String(), err)
	}
	if err := use("", "herdr\n"); err != nil || !strings.HasSuffix(out.String(), "use: herdr\n") || !strings.Contains(out.String(), "* 2) cmux") {
		t.Errorf("picked herdr: %q, %v", out.String(), err)
	}
	if active, _ := activeMultiplexer(); active != "herdr" {
		t.Errorf("active = %q", active)
	}
	// An empty answer, or none, leaves it; a bad one is a usage error.
	for _, input := range []string{"\n", ""} {
		if err := use("", input); err != nil || !strings.HasSuffix(out.String(), "use: ") {
			t.Errorf("answer %q: %q, %v", input, out.String(), err)
		}
	}
	for _, input := range []string{"9\n", "screen\n"} {
		var ue usageError
		if err := use("", input); err == nil || !errors.As(err, &ue) {
			t.Errorf("answer %q: %v, want a usage error", input, err)
		}
	}
	if active, _ := activeMultiplexer(); active != "herdr" {
		t.Errorf("active changed to %q", active)
	}

	// A name sets it outright; a change notifies, a repeat does not.
	if err := use("cmux", ""); err != nil || out.String() != "cmux\n" {
		t.Errorf("use cmux: %q, %v", out.String(), err)
	}
	if err := use("cmux", ""); err != nil {
		t.Fatal(err)
	}
	if got := d.calls; !reflect.DeepEqual(got, []string{"notify spaces cmux", "notify spaces herdr", "notify spaces cmux"}) {
		t.Errorf("notifications %v", got)
	}
	if err := use("screen", ""); err == nil || !strings.Contains(err.Error(), "expected tmux, herdr or cmux") {
		t.Errorf("use screen: %v", err)
	}
	if err := useCmd(d, nil, "", strings.NewReader(""), &out); err == nil || !strings.Contains(err.Error(), "no multiplexer is declared") {
		t.Errorf("use with nothing declared: %v", err)
	}
	// Off macOS there is no desktop: the switch still works.
	out.Reset()
	if err := useCmd(nil, spaces, "tmux", strings.NewReader(""), &out); err != nil || out.String() != "tmux\n" {
		t.Errorf("use without a desktop: %q, %v", out.String(), err)
	}
}
