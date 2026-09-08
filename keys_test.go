package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stefanahman/mux"
	"github.com/stefanahman/mux/muxtest"
)

// threeWays is bf-1 bound to key 1 three times, once per multiplexer,
// with pr-owl on r in herdr and cmux only, and a plain app space on s.
const threeWays = `
spaces:
  bf-1: {key: "1", space: 3, windows: [shell]}
  herdr:
    key: h
    space: 7
    command: herdr --session work
    multiplexer: herdr
    session: work
    workspaces:
      bf-1: {key: "1", windows: [shell]}
      pr-owl: {key: r, windows: [{name: pr-owl, command: pr-owl}]}
    select: pr-owl
  cmux:
    key: c
    space: 8
    app: cmux
    multiplexer: cmux
    workspaces:
      bf-1: {key: "1", windows: [shell]}
      pr-owl: {key: r, windows: [{name: pr-owl, command: pr-owl}]}
    select: pr-owl
  slack: {key: s, space: 13, app: Slack}
`

func TestKeysOnWorkspaces(t *testing.T) {
	spaces, err := mergeSpaces([]source{{"a.yaml", []byte(threeWays)}})
	if err != nil {
		t.Fatal(err)
	}
	herdr, _ := find(spaces, "herdr")
	if herdr.Workspaces["bf-1"].Key != "1" || herdr.Workspaces["pr-owl"].Key != "r" || !reflect.DeepEqual(herdr.workspaceKeys(), []string{"1", "r"}) {
		t.Errorf("herdr workspace keys: %+v", herdr.Workspaces)
	}
	var got []string
	for _, h := range keyHolders(spaces, "1") {
		got = append(got, h.realm+":"+h.String())
	}
	if want := []string{`tmux:space "bf-1"`, `herdr:workspace "bf-1" of "herdr"`, `cmux:workspace "bf-1" of "cmux"`}; !reflect.DeepEqual(got, want) {
		t.Errorf("holders of 1 = %v, want %v", got, want)
	}

	// The active multiplexer's holder wins; a key bound once opens
	// whatever is active; one bound in several, none of them active,
	// asks for a choice.
	cases := []struct {
		key, active, want, err string
	}{
		{"1", "tmux", `space "bf-1"`, ""},
		{"1", "herdr", `workspace "bf-1" of "herdr"`, ""},
		{"1", "cmux", `workspace "bf-1" of "cmux"`, ""},
		{"r", "tmux", "", "key \"r\" is bound in herdr and cmux; `spaces use` one of them"},
		{"r", "cmux", `workspace "pr-owl" of "cmux"`, ""},
		{"h", "cmux", `space "herdr"`, ""},
		{"s", "herdr", `space "slack"`, ""},
		{"z", "tmux", "", `no space bound to key "z"`},
	}
	for _, c := range cases {
		h, err := resolveKey(spaces, c.key, c.active)
		switch {
		case c.err != "" && (err == nil || err.Error() != c.err):
			t.Errorf("key %s with %s active: err %v, want %q", c.key, c.active, err, c.err)
		case c.err == "" && (err != nil || h.String() != c.want):
			t.Errorf("key %s with %s active: %v, %v; want %s", c.key, c.active, h, err, c.want)
		}
	}

	// Within one multiplexer a key is bound once, the holders named.
	rejects := map[string]string{
		"two tmux spaces":               "spaces:\n  a: {key: x, windows: [shell]}\n  b: {key: x, windows: [shell]}\n",
		"two workspaces of one space":   "spaces:\n  h: {command: herdr, multiplexer: herdr, workspaces: {a: {key: x, windows: [w]}, b: {key: x, windows: [w]}}}\n",
		"workspaces of two cmux spaces": "spaces:\n  c1: {app: cmux, multiplexer: cmux, workspaces: {a: {key: x, windows: [w]}}}\n  c2: {app: cmux2, multiplexer: cmux, workspaces: {b: {key: x, windows: [w]}}}\n",
		"a space and its own workspace": "spaces:\n  h: {key: x, command: herdr, multiplexer: herdr, workspaces: {a: {key: x, windows: [w]}}}\n",
		"two plain app spaces":          "spaces:\n  a: {key: x, app: A}\n  b: {key: x, app: B}\n",
		"bad workspace key":             "spaces:\n  h: {command: herdr, multiplexer: herdr, workspaces: {a: {key: xy, windows: [w]}}}\n",
	}
	for name, yaml := range rejects {
		_, err := mergeSpaces([]source{{"a.yaml", []byte(yaml)}})
		if err == nil {
			t.Errorf("%s: expected an error", name)
		} else if strings.Contains(name, "two") && !strings.Contains(err.Error(), "bound to both") {
			t.Errorf("%s: %v, want the two holders named", name, err)
		}
	}
	_, err = mergeSpaces([]source{{"a.yaml", []byte("spaces:\n  h: {command: herdr, multiplexer: herdr, workspaces: {a: {key: x, windows: [w]}, b: {key: x, windows: [w]}}}\n")}})
	if err == nil || !strings.Contains(err.Error(), `key "x" is bound to both workspace "a" of "h" and workspace "b" of "h"`) {
		t.Errorf("message: %v", err)
	}
}

func TestKeyOpensTheWorkspaceOfTheActiveMultiplexer(t *testing.T) {
	quick(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fake := muxtest.NewFakeHerdr(t)
	for _, v := range []string{"HERDR_ENV", "HERDR_WORKSPACE_ID", "HERDR_SESSION"} {
		t.Setenv(v, "")
	}
	orig := newDriver
	newDriver = func(string, string) mux.Driver { return mux.NewHerdr(fake.Socket()) }
	t.Cleanup(func() { newDriver = orig })
	shell := []Window{{Name: "shell"}}
	spaces := []Space{
		{Name: "bf-1", Key: "1", Space: 3, Windows: shell},
		{Name: "herdr", Key: "h", Space: 7, Command: argv{"sleep", "30"}, Multiplexer: "herdr", Select: "pr-owl", Workspaces: map[string]Workspace{
			"bf-1":   {Key: "1", Windows: shell},
			"pr-owl": {Key: "r", Windows: []Window{{Name: "pr-owl"}}},
		}},
	}
	d := newFakeDesktop()
	var out strings.Builder

	// herdr active: key 1 is bf-1 inside herdr — the space opens on
	// that workspace, not on its select.
	if err := setActiveMultiplexer("herdr"); err != nil {
		t.Fatal(err)
	}
	if err := keyCmd(func() (desktop, error) { return d, nil }, spaces, "1", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "herdr: window spawned; bf-1 selected\nherdr: 2 workspaces created") {
		t.Errorf("output: %q", out.String())
	}
	if bf := fake.Workspace("bf-1"); bf == nil || fake.Focused() != bf.ID {
		t.Errorf("focused %q, want bf-1", fake.Focused())
	}
	// Key r is bound in herdr only: it opens there whatever is active.
	if err := setActiveMultiplexer("tmux"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := keyCmd(func() (desktop, error) { return d, nil }, spaces, "r", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "herdr: focused; pr-owl selected") || fake.Focused() != fake.Workspace("pr-owl").ID {
		t.Errorf("output %q, focused %q", out.String(), fake.Focused())
	}
	// cmux active, key 1 bound in tmux and herdr: nothing opens, the
	// press is answered with a notification, and it is not an error.
	if err := setActiveMultiplexer("cmux"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	d.calls = nil
	if err := keyCmd(func() (desktop, error) { return d, nil }, spaces, "1", &out); err != nil {
		t.Errorf("a key bound elsewhere is not an error: %v", err)
	}
	if want := []string{"notify spaces key \"1\" is bound in tmux and herdr; `spaces use` one of them"}; !reflect.DeepEqual(d.calls, want) {
		t.Errorf("desktop calls = %v, want only the notification", d.calls)
	}
	if !strings.Contains(out.String(), "bound in tmux and herdr") {
		t.Errorf("output %q", out.String())
	}
	// Without a desktop to notify with, the reason is the error.
	if err := keyCmd(func() (desktop, error) { return nil, errors.New("no desktop") }, spaces, "1", &out); err == nil || !strings.Contains(err.Error(), "bound in tmux and herdr") {
		t.Errorf("ambiguous key without a desktop: %v", err)
	}
}

// A hotkey has no terminal: every failure behind it is shown on the
// desktop as well as returned.
func TestKeyNotifiesEveryFailure(t *testing.T) {
	quick(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, v := range []string{"HERDR_ENV", "HERDR_WORKSPACE_ID", "HERDR_SESSION"} {
		t.Setenv(v, "")
	}
	orig := newDriver
	newDriver = func(string, string) mux.Driver { return mux.NewHerdr("/nonexistent/herdr.sock") }
	t.Cleanup(func() { newDriver = orig })
	spaces := []Space{{Name: "herdr", Key: "h", Space: 7, Command: argv{"sleep", "30"}, Multiplexer: "herdr", Select: "bf-1", Workspaces: map[string]Workspace{
		"bf-1": {Key: "1", Windows: []Window{{Name: "shell"}}},
	}}}
	if err := setActiveMultiplexer("herdr"); err != nil {
		t.Fatal(err)
	}
	d := newFakeDesktop()
	var out strings.Builder

	// Nothing bound: the error, and the notification with it.
	err := keyCmd(func() (desktop, error) { return d, nil }, spaces, "z", &out)
	if err == nil || !strings.Contains(err.Error(), `no space bound to key "z"`) {
		t.Errorf("unbound key: %v", err)
	}
	if want := []string{`notify spaces no space bound to key "z"`}; !reflect.DeepEqual(d.calls, want) {
		t.Errorf("desktop calls = %v, want %v", d.calls, want)
	}

	// The multiplexer refuses: the window came, the rendering did not;
	// the last thing the desktop hears is the reason.
	d.calls = nil
	err = keyCmd(func() (desktop, error) { return d, nil }, spaces, "1", &out)
	if err == nil || !strings.Contains(err.Error(), "herdr") {
		t.Errorf("unreachable multiplexer: %v", err)
	}
	if last := d.calls[len(d.calls)-1]; !strings.HasPrefix(last, "notify spaces herdr") {
		t.Errorf("desktop calls = %v, want the failure notified last", d.calls)
	}

	// No desktop at all: only the error.
	if err := keyCmd(func() (desktop, error) { return nil, errors.New("no desktop") }, spaces, "z", &out); err == nil || !strings.Contains(err.Error(), `no space bound`) {
		t.Errorf("without a desktop: %v", err)
	}
}

func TestListShowsTheMultiplexerAndWorkspaceKeys(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	quick(t)
	orig := newDriver
	newDriver = func(string, string) mux.Driver { return mux.NewHerdr("/nonexistent/herdr.sock") }
	t.Cleanup(func() { newDriver = orig })
	if err := setActiveMultiplexer("herdr"); err != nil {
		t.Fatal(err)
	}
	d := newFakeDesktop()
	d.present["herdr"] = true
	spaces := []Space{
		{Name: "herdr", Key: "h", Space: 7, Command: argv{"herdr"}, Multiplexer: "herdr", Workspaces: map[string]Workspace{
			"bf-1": {Key: "1", Windows: []Window{{Name: "shell"}}}, "pr-owl": {Key: "r", Windows: []Window{{Name: "pr-owl"}}}, "eden": {Windows: []Window{{Name: "shell"}}},
		}},
		{Name: "cmux", Space: 8, App: "cmux", Multiplexer: "cmux", Workspaces: map[string]Workspace{"bf-1": {Key: "1", Windows: []Window{{Name: "shell"}}}}},
	}
	var out strings.Builder
	if err := list(d, spaces, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"multiplexer: herdr\n", "herdr  h (1 r)  7      window open, ?/3 workspaces  -", "cmux   - (1)    8      -, ?/1 workspaces            -"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list lacks %q:\n%s", want, out.String())
		}
	}
}
