// The desktop side of a space: the terminal window that shows the
// session, and the window manager that pins it to a space and focuses
// it. One implementation today (yabai + Ghostty on macOS); the
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
	// spawn opens a new terminal window with that title, in cwd,
	// running argv.
	spawn(title, cwd string, argv []string) error
	// moveToSpace pins the window to a desktop space.
	moveToSpace(id string, space int) error
	// focus brings the window to the front.
	focus(id string) error
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
	out, err := runOut(exec.Command("yabai", "-m", "query", "--windows"))
	if err != nil {
		return "", err
	}
	var windows []yabaiWindow
	if err := json.Unmarshal([]byte(out), &windows); err != nil {
		return "", fmt.Errorf("yabai -m query --windows: %w", err)
	}
	for _, w := range windows {
		if w.App == "Ghostty" && w.Title == title {
			return strconv.Itoa(w.ID), nil
		}
	}
	return "", nil
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

func (yabaiGhostty) requirements() []requirement {
	onPath := func(name string) func() bool {
		return func() bool { _, err := exec.LookPath(name); return err == nil }
	}
	return []requirement{
		{"tmux", onPath("tmux")},
		{"yabai", onPath("yabai")},
		{"Ghostty.app", func() bool {
			home, _ := os.UserHomeDir()
			for _, dir := range []string{"/Applications", filepath.Join(home, "Applications")} {
				if _, err := os.Stat(filepath.Join(dir, "Ghostty.app")); err == nil {
					return true
				}
			}
			return false
		}},
	}
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

// ensureWindow finds the space's terminal window or spawns one, pins
// it to the space's desktop space when it is new, and focuses it.
// Returns whether a window was spawned.
func ensureWindow(d desktop, sp Space) (spawned bool, err error) {
	id, err := d.findWindow(sp.Name)
	if err != nil {
		return false, err
	}
	if id == "" {
		cwd, _ := sp.Cwd.resolve()
		argv, err := attachCommand(sp.Name)
		if err != nil {
			return false, err
		}
		if err := d.spawn(sp.Name, cwd, argv); err != nil {
			return false, err
		}
		if id, err = waitForWindow(d, sp.Name, 5*time.Second); err != nil {
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

// waitForWindow polls until the window manager reports the window.
func waitForWindow(d desktop, title string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		id, err := d.findWindow(title)
		if err != nil {
			return "", err
		}
		if id != "" {
			return id, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("terminal window %q did not appear within %s", title, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
