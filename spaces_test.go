package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stefanahman/mux"
)

// unsetenv removes a variable for the rest of the test; t.Setenv can
// only set it, and an empty value is not always the same as none.
func unsetenv(t *testing.T, name string) {
	t.Helper()
	if old, ok := os.LookupEnv(name); ok {
		t.Cleanup(func() { os.Setenv(name, old) })
	}
	os.Unsetenv(name)
}

// startTmux runs a private tmux server for the test: its own socket
// directory (Unix socket paths are short-limited and t.TempDir is
// long), its own minimal config (/bin/sh in every window — the
// developer's shell would write history into $HOME on exit), and the
// inherited $TMUX cleared so a test run from inside tmux never reaches
// the developer's server.
func startTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	sockDir, err := os.MkdirTemp("", "spaces")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	conf := filepath.Join(sockDir, "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g default-shell /bin/sh\nset -s exit-empty off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", sockDir)
	t.Setenv("TMUX", "")
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir()) // open's lock files stay out of the developer's cache
	if _, err := tmux("-L", "default", "-f", conf, "start-server"); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(sockDir, fmt.Sprintf("tmux-%d", os.Getuid()), "default")
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", socket, "kill-server").Run() })
}

// fakeDesktop records what open asks of the window manager. A spawned
// window, or a launched app's, "appears" a couple of polls later,
// like a real one.
type fakeDesktop struct {
	calls   []string
	present map[string]bool // terminal windows, by title
	apps    map[string]bool // applications with a window, by name
	pending int             // polls left before a spawned window is reported
	indexes []int           // the desktop spaces `check` can pin to
}

func newFakeDesktop() *fakeDesktop {
	return &fakeDesktop{present: map[string]bool{}, apps: map[string]bool{}}
}

func (d *fakeDesktop) findWindow(title string) (string, error) {
	return d.appears(d.present[title], "w-"+title)
}

func (d *fakeDesktop) findAppWindow(app string) (string, error) {
	return d.appears(d.apps[app], "a-"+app)
}

func (d *fakeDesktop) appears(present bool, id string) (string, error) {
	if !present {
		return "", nil
	}
	if d.pending > 0 {
		d.pending--
		return "", nil
	}
	return id, nil
}

func (d *fakeDesktop) launch(app string, env []string) error {
	d.calls = append(d.calls, strings.TrimSpace("launch "+app+" "+strings.Join(env, " ")))
	d.apps[app] = true
	d.pending = 2
	return nil
}

func (d *fakeDesktop) spawn(title, cwd string, argv []string) error {
	program := append([]string{filepath.Base(argv[0])}, argv[1:]...)
	d.calls = append(d.calls, fmt.Sprintf("spawn %s cwd=%s argv=%s", title, filepath.Base(cwd), strings.Join(program, " ")))
	d.present[title] = true
	d.pending = 2
	return nil
}

func (d *fakeDesktop) moveToSpace(id string, space int) error {
	d.calls = append(d.calls, fmt.Sprintf("move %s -> %d", id, space))
	return nil
}

func (d *fakeDesktop) focus(id string) error {
	d.calls = append(d.calls, "focus "+id)
	return nil
}

func (d *fakeDesktop) spaces() ([]int, error) { return d.indexes, nil }

func (d *fakeDesktop) requirements() []requirement {
	return []requirement{{"tmux", func() bool { return true }}}
}

func (d *fakeDesktop) notify(title, text string) error {
	d.calls = append(d.calls, "notify "+title+" "+text)
	return nil
}

// activeWindow is the session's current window. (display-message -t
// <session> prints nothing without a client, so ask list-windows.)
func activeWindow(t *testing.T, session string) string {
	t.Helper()
	out, err := tmux("list-windows", "-t", target(session, ""), "-F", "#{window_active} #{window_name}")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out, "\n") {
		if name, ok := strings.CutPrefix(line, "1 "); ok {
			return name
		}
	}
	return ""
}

func TestOpenCreatesTheSpace(t *testing.T) {
	startTmux(t)
	root, err := filepath.EvalSymlinks(t.TempDir()) // tmux reports resolved paths
	if err != nil {
		t.Fatal(err)
	}
	app, eden := filepath.Join(root, "app"), filepath.Join(root, "eden")
	for _, d := range []string{app, eden} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(root, "then.ran")
	sp := Space{
		Name: "bf-1", Key: "1", Space: 3, Cwd: pathList{app},
		Windows: []Window{
			{Name: "shell"},
			{Name: "eden", Panes: []Pane{{Cwd: pathList{eden}}, {Cwd: pathList{app}, Command: "sleep 30"}}},
			{Name: "nvim", Command: "sleep 30", Cwd: pathList{eden}},
		},
		Select: "nvim",
		Then:   "touch " + marker,
	}
	d := newFakeDesktop()
	var out strings.Builder
	if err := open(d, sp, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "session and window created") {
		t.Errorf("output: %q", out.String())
	}

	if got, want := windowNames("bf-1"), []string{"shell", "eden", "nvim"}; !reflect.DeepEqual(got, want) {
		t.Errorf("windows = %v, want %v", got, want)
	}
	for _, opt := range []struct{ name, want string }{{"set-titles", "on"}, {"set-titles-string", "#S"}} {
		if v, _ := tmux("show-options", "-t", "bf-1", "-v", opt.name); v != opt.want {
			t.Errorf("%s = %q, want %q", opt.name, v, opt.want)
		}
	}
	if panes, _ := tmux("list-panes", "-t", target("bf-1", "eden"), "-F", "#{pane_current_path}"); panes != eden+"\n"+app {
		t.Errorf("eden panes = %q", panes)
	}
	if cwd, _ := tmux("display-message", "-p", "-t", target("bf-1", "nvim"), "#{pane_current_path}"); cwd != eden {
		t.Errorf("nvim cwd = %q, want %q", cwd, eden)
	}
	if active := activeWindow(t, "bf-1"); active != "nvim" {
		t.Errorf("selected window = %q, want nvim", active)
	}
	want := []string{"spawn bf-1 cwd=app argv=tmux attach-session -t =bf-1", "move w-bf-1 -> 3", "focus w-bf-1"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Errorf("desktop calls = %v, want %v", d.calls, want)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("then ran on focus")
	}
}

func TestOpenThenNeedsAClientAfterSpawn(t *testing.T) {
	startTmux(t)
	sp := Space{Name: "popup", Windows: []Window{{Name: "scratch"}}, Then: "true"}
	err := open(newFakeDesktop(), sp, true, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "no client attached") {
		t.Errorf("got %v, want the no-client error (the fake never attaches one)", err)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	startTmux(t)
	root := t.TempDir()
	sp := Space{Name: "eden", Space: 11, Cwd: pathList{root}, Windows: []Window{{Name: "shell"}, {Name: "nvim", Command: "sleep 30"}}}
	d := newFakeDesktop()
	if err := open(d, sp, false, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	// The user renamed nothing but closed nvim and moved to shell.
	if _, err := tmux("kill-window", "-t", target("eden", "nvim")); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("select-window", "-t", target("eden", "shell")); err != nil {
		t.Fatal(err)
	}
	d.calls = nil

	var out strings.Builder
	if err := open(d, sp, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "eden: focused") {
		t.Errorf("output: %q", out.String())
	}
	if got, want := windowNames("eden"), []string{"shell", "nvim"}; !reflect.DeepEqual(got, want) {
		t.Errorf("windows = %v, want the missing one re-added", got)
	}
	if active := activeWindow(t, "eden"); active != "shell" {
		t.Errorf("re-open moved the current window to %q", active)
	}
	if want := []string{"focus w-eden"}; !reflect.DeepEqual(d.calls, want) {
		t.Errorf("desktop calls = %v, want %v (no spawn, no move)", d.calls, want)
	}
}

func TestOpenUnpinnedSpaceIsNotMoved(t *testing.T) {
	startTmux(t)
	sp := Space{Name: "scratch", Windows: []Window{{Name: "shell"}}}
	d := newFakeDesktop()
	if err := open(d, sp, false, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	for _, c := range d.calls {
		if strings.HasPrefix(c, "move") {
			t.Errorf("unpinned space was moved: %v", d.calls)
		}
	}
}

func TestOpenRunsThenWhenTheWindowExists(t *testing.T) {
	startTmux(t)
	root := t.TempDir()
	marker := filepath.Join(root, "then.ran")
	sp := Space{Name: "pr-reviews", Windows: []Window{{Name: "scratch"}}, Then: "touch " + marker}
	d := newFakeDesktop()
	d.present["pr-reviews"] = true // the window already exists: no spawn, no client wait
	if err := open(d, sp, true, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, marker)
	if err := open(d, sp, false, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
}

// waitForFile waits for a `then` command, which runs in the
// background, to leave its marker.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not appear: then did not run", filepath.Base(path))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestOpenRunsAProgramInsteadOfASession(t *testing.T) {
	startTmux(t)
	root := t.TempDir()
	marker := filepath.Join(root, "then.ran")
	sp := Space{Name: "herdr", Key: "h", Space: 7, Cwd: pathList{root}, Command: argv{"sleep", "30"}, Then: "touch " + marker}
	d := newFakeDesktop()
	var out strings.Builder
	if err := open(d, sp, true, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "herdr: window spawned") {
		t.Errorf("output: %q", out.String())
	}
	if windowNames("herdr") != nil {
		t.Error("a tmux session was created for a command space")
	}
	want := []string{"spawn herdr cwd=" + filepath.Base(root) + " argv=sleep 30", "move w-herdr -> 7", "focus w-herdr"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Errorf("desktop calls = %v, want %v", d.calls, want)
	}
	waitForFile(t, marker) // no client to wait for: then ran once the window was up

	d.calls = nil
	out.Reset()
	if err := open(d, sp, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "herdr: focused") || !reflect.DeepEqual(d.calls, []string{"focus w-herdr"}) {
		t.Errorf("second open: %q, desktop calls %v", out.String(), d.calls)
	}
}

func TestOpenLaunchesAnApplication(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	marker := filepath.Join(root, "then.ran")
	t.Setenv("MODE", "allowAll")
	sp := Space{Name: "cmux", Key: "c", Space: 8, App: "cmux", Then: "touch " + marker,
		Env: map[string]string{"CMUX_SOCKET_MODE": "$MODE", "CMUX_HOME": "~/cmux"}}
	d := newFakeDesktop()
	var out strings.Builder
	if err := open(d, sp, true, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cmux: app launched") {
		t.Errorf("output: %q", out.String())
	}
	home, _ := os.UserHomeDir()
	// env entries sorted by name, values expanded like paths
	want := []string{"launch cmux CMUX_HOME=" + home + "/cmux CMUX_SOCKET_MODE=allowAll", "move a-cmux -> 8", "focus a-cmux"}
	if !reflect.DeepEqual(d.calls, want) {
		t.Errorf("desktop calls = %v, want %v", d.calls, want)
	}
	waitForFile(t, marker)

	d.calls = nil
	out.Reset()
	if err := open(d, sp, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cmux: focused") || !reflect.DeepEqual(d.calls, []string{"focus a-cmux"}) {
		t.Errorf("second open: %q, desktop calls %v", out.String(), d.calls)
	}
}

func TestOpenReportsAMissingProgram(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	sp := Space{Name: "ghost", Command: argv{"no-such-program-spaces"}}
	d := newFakeDesktop()
	err := open(d, sp, false, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("got %v, want the PATH error", err)
	}
	if len(d.calls) != 0 {
		t.Errorf("desktop was touched: %v", d.calls)
	}
}

func TestOpenRefusesToDuplicateAnAttachedSession(t *testing.T) {
	startTmux(t)
	sp := Space{Name: "seen", Windows: []Window{{Name: "shell"}}}
	d := newFakeDesktop()
	if err := open(d, sp, false, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	// A terminal attaches through a real pty, then the window manager
	// stops seeing the window (a locked screen does exactly this).
	attach := exec.Command("tmux", "attach-session", "-t", target("seen", ""))
	attach.Env = append(os.Environ(), "TERM=xterm")
	ptmx, err := pty.Start(attach)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = attach.Process.Kill(); _, _ = attach.Process.Wait(); _ = ptmx.Close() })
	if !waitForClient("seen", 3*time.Second) {
		t.Fatal("client did not attach")
	}
	d.present["seen"] = false
	d.calls = nil

	err = open(d, sp, false, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "no window titled") {
		t.Errorf("got %v, want the blind-window-manager error", err)
	}
	for _, c := range d.calls {
		if strings.HasPrefix(c, "spawn") {
			t.Errorf("spawned a duplicate: %v", d.calls)
		}
	}
}

func TestOpenReportsMissingCwd(t *testing.T) {
	startTmux(t)
	sp := Space{Name: "ghost", Cwd: pathList{"/nonexistent/one", "/nonexistent/two"}, Windows: []Window{{Name: "shell"}}}
	err := open(newFakeDesktop(), sp, false, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "none of cwd") {
		t.Errorf("got %v, want a cwd error", err)
	}
	if windowNames("ghost") != nil {
		t.Error("session was created despite the error")
	}
}

func TestList(t *testing.T) {
	startTmux(t)
	// A hotkey daemon's environment: no locale and no TMUX (an empty
	// TMUX still counts as "inside tmux" to the client). The client
	// then prints control characters in command output as `_`.
	for _, v := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		t.Setenv(v, "")
	}
	unsetenv(t, "TMUX")
	sp := Space{Name: "bf-1", Key: "1", Space: 3, Windows: []Window{{Name: "shell"}, {Name: "nvim"}}}
	if err := open(newFakeDesktop(), sp, false, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("set-option", "-w", "-t", target("bf-1", "shell"), "@claude-state", "blocked"); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := list(newFakeDesktop(), []Space{sp, {Name: "eden", Key: "e", Space: 11, Windows: []Window{{Name: "shell"}}}}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "bf-1  1    3      2 windows  1⚠", "eden  e    11     -          -"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list lacks %q:\n%s", want, out.String())
		}
	}

	// A command space has no session: the column says whether its
	// window is up, and "?" where there is no window manager to ask.
	d := newFakeDesktop()
	d.present["herdr"] = true
	commands := []Space{{Name: "herdr", Key: "h", Space: 7, Command: argv{"herdr"}}, {Name: "cmux", Command: argv{"cmux"}}}
	out.Reset()
	if err := list(d, commands, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"herdr  h    7      window open  -", "cmux   -    -      -            -"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if err := list(nil, commands, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "herdr  h    7      ?") {
		t.Errorf("list without a desktop:\n%s", out.String())
	}

	// An app space is asked of the desktop by the application's name.
	d.apps["Slack"] = true
	apps := []Space{{Name: "slack", Key: "s", Space: 13, App: "Slack"}, {Name: "zed", App: "Zed"}}
	out.Reset()
	if err := list(d, apps, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"slack  s    13     window open  -", "zed    -    -      -            -"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list lacks %q:\n%s", want, out.String())
		}
	}
}

func TestLockSpaceSerialises(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	unlock, err := lockSpace("bf-1")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(os.Getenv("XDG_CACHE_HOME"), "spaces", "bf-1.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatalf("a second open while the first holds the lock: %v, want EWOULDBLOCK", err)
	}
	unlock()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("lock after release: %v", err)
	}
}

func TestCheck(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no config files of the developer's
	stubDrivers(t, func(kind, _ string) error { return errors.New(kind + ": not running") })
	d := newFakeDesktop()
	d.indexes = []int{1, 2, 3}
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	shell := []Window{{Name: "shell"}}
	spaces := []Space{
		{Name: "a", Space: 2, Cwd: pathList{dir}, Windows: shell},
		{Name: "b", Space: 9, Cwd: pathList{dir}, Windows: shell},
		{Name: "c", Cwd: pathList{missing}, Windows: shell},
		{Name: "d", Space: 2, Cwd: pathList{dir}, Windows: shell},
		{Name: "e", Command: argv{"no-such-program-spaces", "--flag"}},
		{Name: "f", Cwd: pathList{dir}, Command: argv{"sh"}},
		{Name: "g", Space: 3, App: "No Such App (spaces)"},
		{Name: "h", Command: argv{"sh"}, Multiplexer: "herdr", Workspaces: map[string]Workspace{
			"ok":   {Cwd: pathList{dir}, Windows: []Window{{Name: "w", Cwd: pathList{dir}}}},
			"lost": {Cwd: pathList{missing}, Windows: []Window{{Name: "w", Panes: []Pane{{}, {Cwd: pathList{missing}}}}}},
		}},
	}
	var out strings.Builder
	err := check(d, spaces, nil, &out)
	for _, want := range []string{
		"error: b: desktop space 9 does not exist (this desktop has 1..3)",
		"error: c: none of cwd [" + missing + "] exists",
		"error: e: command \"no-such-program-spaces\" not found on PATH",
		"error: g: no No Such App (spaces).app in /Applications, ~/Applications or /System/Applications",
		"error: h: workspace lost: none of cwd [" + missing + "] exists",
		"error: h: workspace lost: window w pane 1: none of cwd [" + missing + "] exists",
		"warning: desktop space 2 is claimed by a, d",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("check output lacks %q:\n%s", want, out.String())
		}
	}
	for _, fine := range []string{"error: a:", "error: d:", "error: f:"} {
		if strings.Contains(out.String(), fine) {
			t.Errorf("check output has %q, which is not a problem:\n%s", fine, out.String())
		}
	}
	if strings.Contains(out.String(), "workspace ok") {
		t.Errorf("check complains about the workspace that is fine:\n%s", out.String())
	}
	if err == nil || err.Error() != "6 problem(s)" {
		t.Errorf("check returned %v, want 6 problem(s)", err)
	}
}

// pingDriver is a multiplexer that only answers Ping — all `check`
// asks of one.
type pingDriver struct {
	mux.Driver
	err error
}

func (p pingDriver) Ping() error { return p.err }

// stubDrivers makes every multiplexer answer Ping with what ping says
// of its kind and session, for the test's duration.
func stubDrivers(t *testing.T, ping func(kind, session string) error) {
	t.Helper()
	real := newDriver
	newDriver = func(kind, session string) mux.Driver { return pingDriver{err: ping(kind, session)} }
	t.Cleanup(func() { newDriver = real })
}

// tainted is Ping's error for a multiplexer that runs, started with
// the wrong environment: mux.ErrTainted behind its message.
type tainted string

func (e tainted) Error() string      { return string(e) }
func (tainted) Is(target error) bool { return target == mux.ErrTainted }

// check audits tmux and each herdr session and cmux the spaces
// declare, once each: a multiplexer started with the wrong
// environment is a problem, one that isn't running is not.
func TestCheckAuditsMultiplexers(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var asked []string
	stubDrivers(t, func(kind, session string) error {
		asked = append(asked, strings.TrimSpace(kind+" "+session))
		switch kind {
		case "tmux":
			return tainted("tmux: the server was started from inside a Claude Code session (CLAUDECODE): agents started in it run as child sessions and save no transcript; restart it from a hotkey or a plain shell")
		case "cmux":
			return tainted("cmux: the app was launched with TMUX in its environment (from a shell inside tmux): its shell integration hands CMUX_SURFACE_ID to tmux before every command and the Claude Code hooks never engage; relaunch cmux from a hotkey or Spotlight")
		}
		return errors.New("dial unix /nowhere/herdr.sock: connect: no such file or directory")
	})
	d := newFakeDesktop()
	d.indexes = []int{1, 2, 3}
	dir := t.TempDir()
	ws := map[string]Workspace{"w": {Cwd: pathList{dir}}}
	spaces := []Space{
		{Name: "a", Space: 1, Cwd: pathList{dir}, Windows: []Window{{Name: "shell"}}},
		{Name: "h", Space: 2, Command: argv{"sh"}, Multiplexer: "herdr", Session: "work", Workspaces: ws},
		{Name: "c", Space: 3, Command: argv{"sh"}, Multiplexer: "cmux", Workspaces: ws},
		{Name: "c2", Command: argv{"sh"}, Multiplexer: "cmux", Workspaces: ws},
	}
	var out strings.Builder
	err := check(d, spaces, nil, &out)
	if want := []string{"tmux", "herdr work", "cmux"}; !reflect.DeepEqual(asked, want) {
		t.Errorf("asked %v, want %v", asked, want)
	}
	for _, want := range []string{
		"error: tmux: the server was started from inside a Claude Code session (CLAUDECODE): agents started in it run as child sessions",
		"error: cmux: the app was launched with TMUX in its environment",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("check output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "herdr") {
		t.Errorf("check reports the herdr that isn't running:\n%s", out.String())
	}
	if err == nil || err.Error() != "2 problem(s)" {
		t.Errorf("check returned %v, want 2 problem(s)", err)
	}
}

// TestCheckWarnsOnUnusedGroup: a group nobody joins is a warning, not
// an error — the config is still valid, it just describes nothing.
func TestCheckWarnsOnUnusedGroup(t *testing.T) {
	d := &fakeDesktop{}
	d.indexes = []int{1, 2, 3}
	dir := t.TempDir()
	spaces := []Space{{
		Name: "c", Space: 1, App: "Ghostty", Multiplexer: "cmux",
		Workspaces: map[string]Workspace{
			"joined": {Cwd: pathList{dir}, Group: "Tooling"},
			"loose":  {Cwd: pathList{dir}},
		},
	}}
	groups := map[string]Group{
		"Tooling": {Color: "#8fa1b3", file: "macos.yaml"},
		"Ghosts":  {Icon: "hammer", file: "bardo.yaml"},
	}
	var out strings.Builder
	_ = check(d, spaces, groups, &out)
	if want := `warning: group "Ghosts" is declared in bardo.yaml and no workspace joins it`; !strings.Contains(out.String(), want) {
		t.Errorf("check said %q, want %q", out.String(), want)
	}
	if strings.Contains(out.String(), `"Tooling"`) {
		t.Errorf("a group a workspace joins is not warned about:\n%s", out.String())
	}
}
