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
	sockDir, err := os.MkdirTemp("", "tmux-spaces")
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
// window "appears" a couple of polls later, like a real one.
type fakeDesktop struct {
	calls   []string
	present map[string]bool
	pending int // polls left before a spawned window is reported
}

func newFakeDesktop() *fakeDesktop { return &fakeDesktop{present: map[string]bool{}} }

func (d *fakeDesktop) findWindow(title string) (string, error) {
	if !d.present[title] {
		return "", nil
	}
	if d.pending > 0 {
		d.pending--
		return "", nil
	}
	return "w-" + title, nil
}

func (d *fakeDesktop) spawn(title, cwd string, argv []string) error {
	d.calls = append(d.calls, fmt.Sprintf("spawn %s cwd=%s argv=%s", title, filepath.Base(cwd), strings.Join(argv[1:], " ")))
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

func (d *fakeDesktop) requirements() []requirement {
	return []requirement{{"tmux", func() bool { return true }}}
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
	want := []string{"spawn bf-1 cwd=app argv=attach-session -t =bf-1", "move w-bf-1 -> 3", "focus w-bf-1"}
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
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("then did not run")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := open(d, sp, false, &strings.Builder{}); err != nil {
		t.Fatal(err)
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
	if err := list([]Space{sp, {Name: "eden", Key: "e", Space: 11, Windows: []Window{{Name: "shell"}}}}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "bf-1  1    3      2 windows  1⚠", "eden  e    11     -          -"} {
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
	f, err := os.Open(filepath.Join(os.Getenv("XDG_CACHE_HOME"), "tmux-spaces", "bf-1.lock"))
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
