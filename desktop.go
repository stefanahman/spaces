// The desktop side of a space: the terminal window that shows the
// session or runs the program, or the application's window, and the
// window manager that pins it to a space and focuses it. One
// implementation today (yabai + Ghostty on macOS); the
// interface is the seam for others (Hyprland + a terminal on Linux).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

type desktop interface {
	// findWindow returns the id of the terminal window titled title,
	// or "" when there is none.
	findWindow(title string) (string, error)
	// findAppWindow returns the id of the application's first window,
	// or "" when it has none.
	findAppWindow(app string) (string, error)
	// spawn opens a new terminal window with that title, in cwd,
	// running argv.
	spawn(title, cwd string, argv []string) error
	// launch starts the application, or activates it when it runs.
	launch(app string) error
	// moveToSpace pins the window to a desktop space.
	moveToSpace(id string, space int) error
	// focus brings the window to the front.
	focus(id string) error
	// spaces lists the desktop spaces by the index `space` pins to,
	// for `check`.
	spaces() ([]int, error)
	// requirements lists the tools this desktop needs, for `check`.
	requirements() []requirement
}

type requirement struct {
	name string
	ok   func() bool
}

// newDesktop picks the implementation for this OS.
func newDesktop() (desktop, error) {
	switch runtime.GOOS {
	case "darwin":
		return yabaiGhostty{}, nil
	default:
		return nil, fmt.Errorf("no desktop backend for %s yet (macOS with yabai and Ghostty is the only one)", runtime.GOOS)
	}
}

// yabaiGhostty drives Ghostty windows through yabai.
type yabaiGhostty struct{}

// yabaiWindow is the subset of `yabai -m query --windows` we read.
type yabaiWindow struct {
	ID    int    `json:"id"`
	App   string `json:"app"`
	Title string `json:"title"`
}

func (yabaiGhostty) findWindow(title string) (string, error) {
	return findYabaiWindow(func(w yabaiWindow) bool { return w.App == "Ghostty" && w.Title == title })
}

func (yabaiGhostty) findAppWindow(app string) (string, error) {
	return findYabaiWindow(func(w yabaiWindow) bool { return w.App == app })
}

// findYabaiWindow returns the id of the first window yabai reports
// that matches, or "".
func findYabaiWindow(match func(yabaiWindow) bool) (string, error) {
	out, err := runOut(exec.Command("yabai", "-m", "query", "--windows"))
	if err != nil {
		return "", err
	}
	var windows []yabaiWindow
	if err := json.Unmarshal([]byte(out), &windows); err != nil {
		return "", fmt.Errorf("yabai -m query --windows: %w", err)
	}
	for _, w := range windows {
		if match(w) {
			return strconv.Itoa(w.ID), nil
		}
	}
	return "", nil
}

// launch opens the application through LaunchServices. On an app
// that already runs (macOS keeps one alive with no windows) this
// activates it, which reopens a window for most apps.
func (yabaiGhostty) launch(app string) error {
	_, err := runOut(exec.Command("open", "-a", app))
	return err
}

// spawn opens Ghostty through LaunchServices (the only way to start a
// new Ghostty window from the CLI on macOS). argv runs directly in the
// new window, with no shell in between, so pass absolute paths: the
// window's PATH is LaunchServices', not the shell's.
func (yabaiGhostty) spawn(title, cwd string, argv []string) error {
	args := []string{"-na", "Ghostty", "--args", "--title=" + title}
	if cwd != "" {
		args = append(args, "--working-directory="+cwd)
	}
	args = append(append(args, "-e"), argv...)
	_, err := runOut(exec.Command("open", args...))
	return err
}

func (yabaiGhostty) moveToSpace(id string, space int) error {
	_, err := runOut(exec.Command("yabai", "-m", "window", id, "--space", strconv.Itoa(space)))
	return err
}

func (yabaiGhostty) focus(id string) error {
	_, err := runOut(exec.Command("yabai", "-m", "window", "--focus", id))
	return err
}

// yabaiSpace is the subset of `yabai -m query --spaces` we read: the
// mission-control index, which is what `--space N` and the rules use.
type yabaiSpace struct {
	Index int `json:"index"`
}

func (yabaiGhostty) spaces() ([]int, error) {
	out, err := runOut(exec.Command("yabai", "-m", "query", "--spaces"))
	if err != nil {
		return nil, err
	}
	var spaces []yabaiSpace
	if err := json.Unmarshal([]byte(out), &spaces); err != nil {
		return nil, fmt.Errorf("yabai -m query --spaces: %w", err)
	}
	indexes := make([]int, len(spaces))
	for i, s := range spaces {
		indexes[i] = s.Index
	}
	return indexes, nil
}

func (yabaiGhostty) requirements() []requirement {
	onPath := func(name string) func() bool {
		return func() bool { _, err := exec.LookPath(name); return err == nil }
	}
	return []requirement{
		{"tmux", onPath("tmux")},
		{"yabai", onPath("yabai")},
		{"Ghostty.app", func() bool { return appBundleExists("Ghostty") }},
	}
}

// appBundleExists reports whether <name>.app is installed in
// /Applications or ~/Applications, where `open -a` and `check` look.
func appBundleExists(name string) bool {
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/Applications", filepath.Join(home, "Applications")} {
		if _, err := os.Stat(filepath.Join(dir, name+".app")); err == nil {
			return true
		}
	}
	return false
}

// attachCommand is what a spawned terminal runs: tmux by absolute
// path, attaching to the session.
func attachCommand(session string) ([]string, error) {
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return nil, errors.New("tmux not found on PATH")
	}
	return []string{tmuxPath, "attach-session", "-t", target(session, "")}, nil
}

// ensureWindow finds the space's terminal window or spawns one running
// argv, pins it to the space's desktop space when it is new, and
// focuses it. attached tells whether a terminal already shows the
// space (a tmux client): then a window the window manager can't find
// is hidden — screen locked, or the title changed by hand — not
// missing, and spawning would only add a duplicate. Returns whether a
// window was spawned.
func ensureWindow(d desktop, sp Space, argv []string, attached func() bool) (spawned bool, err error) {
	id, err := d.findWindow(sp.Name)
	if err != nil {
		return false, err
	}
	if id == "" && attached() {
		return false, fmt.Errorf("%s: a terminal is attached to the session but no window titled %q is visible to the window manager (screen locked?)", sp.Name, sp.Name)
	}
	if id == "" {
		cwd, ok := sp.Cwd.resolve()
		if !ok && len(sp.Cwd) > 0 {
			return false, fmt.Errorf("space %q: none of cwd %v exists", sp.Name, sp.Cwd)
		}
		if err := d.spawn(sp.Name, cwd, argv); err != nil {
			return false, err
		}
		find := func() (string, error) { return d.findWindow(sp.Name) }
		if id, err = waitForWindow(find, "terminal window "+sp.Name, 5*time.Second); err != nil {
			return true, err
		}
		if sp.Space > 0 {
			if err := d.moveToSpace(id, sp.Space); err != nil {
				return true, err
			}
		}
		spawned = true
	}
	return spawned, d.focus(id)
}

// waitForWindow polls find until the window manager reports the
// window; what names it in the error.
func waitForWindow(find func() (string, error), what string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		id, err := find()
		if err != nil {
			return "", err
		}
		if id != "" {
			return id, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("%s did not appear within %s", what, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
