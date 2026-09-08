// The tmux side of a space: the session, its windows and panes, and
// the session options that keep the terminal's title equal to the
// session name.
package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// tmux runs a tmux command and returns trimmed stdout. Errors carry
// tmux's stderr, which is where the useful message is. The client
// gets the launch environment: a server it starts keeps the client's
// environment for every window it will ever open, and with TMUX
// dropped it is the default server that is addressed — the one the
// spaces live in — not the server of a tmux this runs inside.
func tmux(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	cmd.Env = launchEnv()
	return runOut(cmd)
}

func runOut(cmd *exec.Cmd) (string, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s: %s", strings.Join(cmd.Args, " "), msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// target builds an exact-match `-t` argument. Without the `=` prefix
// tmux falls back to prefix matching, so `bf-1` would resolve to
// `bf-10` when `bf-1` itself doesn't exist.
func target(session, window string) string {
	if window == "" {
		return "=" + session
	}
	return "=" + session + ":=" + window
}

// windowNames lists the windows of a session; nil when it doesn't exist.
func windowNames(session string) []string {
	out, err := tmux("list-windows", "-t", target(session, ""), "-F", "#{window_name}")
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// ensureSession creates the space's session on first use and adds any
// window that is missing by name on later runs. Windows that exist are
// never touched — they hold the user's work. Returns whether the
// session was created now.
func ensureSession(sp Space) (created bool, err error) {
	cwd, ok := sp.Cwd.resolve()
	if !ok && len(sp.Cwd) > 0 {
		return false, fmt.Errorf("space %q: none of cwd %v exists", sp.Name, sp.Cwd)
	}
	existing := windowNames(sp.Name)
	created = existing == nil
	for i, w := range sp.Windows {
		if slices.Contains(existing, w.Name) {
			continue
		}
		wcwd, wcmd, err := windowCwdCommand(sp, w, cwd)
		if err != nil {
			return false, err
		}
		args := []string{"new-window", "-d", "-t", target(sp.Name, ""), "-n", w.Name}
		if created && i == 0 {
			args = []string{"new-session", "-d", "-s", sp.Name, "-n", w.Name}
		}
		if wcwd != "" {
			args = append(args, "-c", wcwd)
		}
		if wcmd != "" {
			args = append(args, wcmd)
		}
		if _, err := tmux(args...); err != nil {
			return false, err
		}
		if err := addPanes(sp, w, cwd); err != nil {
			return false, err
		}
	}
	// The terminal shows the session name as its title, whatever the
	// shells inside do, so the window can always be found by title.
	// (set-option is the one command that rejects the `=` prefix on a
	// session target; the exact name wins over prefix matches anyway.)
	for _, opt := range [][]string{{"set-titles", "on"}, {"set-titles-string", "#S"}} {
		if _, err := tmux(append([]string{"set-option", "-t", sp.Name}, opt...)...); err != nil {
			return false, err
		}
	}
	return created, nil
}

// windowCwdCommand resolves a window's cwd (its own, else the space's)
// and its command. Panes carry their own; the window then runs the
// shell in the first pane.
func windowCwdCommand(sp Space, w Window, spaceCwd string) (cwd, cmd string, err error) {
	cwd = spaceCwd
	if len(w.Cwd) > 0 {
		var ok bool
		if cwd, ok = w.Cwd.resolve(); !ok {
			return "", "", fmt.Errorf("space %q window %q: none of cwd %v exists", sp.Name, w.Name, w.Cwd)
		}
	}
	cmd = w.Command
	if len(w.Panes) > 0 {
		if p := w.Panes[0]; len(p.Cwd) > 0 {
			var ok bool
			if cwd, ok = p.Cwd.resolve(); !ok {
				return "", "", fmt.Errorf("space %q window %q: none of pane cwd %v exists", sp.Name, w.Name, p.Cwd)
			}
		}
		cmd = w.Panes[0].Command
	}
	return cwd, cmd, nil
}

// addPanes splits a freshly created window for panes[1:] and lays
// them out evenly; the first pane keeps the focus.
func addPanes(sp Space, w Window, spaceCwd string) error {
	if len(w.Panes) < 2 {
		return nil
	}
	dir, layout := "-h", "even-horizontal"
	if w.Split == "vertical" {
		dir, layout = "-v", "even-vertical"
	}
	for i, p := range w.Panes[1:] {
		cwd := spaceCwd
		if len(p.Cwd) > 0 {
			var ok bool
			if cwd, ok = p.Cwd.resolve(); !ok {
				return fmt.Errorf("space %q window %q pane %d: none of cwd %v exists", sp.Name, w.Name, i+1, p.Cwd)
			}
		}
		args := []string{"split-window", "-d", dir, "-t", target(sp.Name, w.Name)}
		if cwd != "" {
			args = append(args, "-c", cwd)
		}
		if p.Command != "" {
			args = append(args, p.Command)
		}
		if _, err := tmux(args...); err != nil {
			return err
		}
	}
	_, err := tmux("select-layout", "-t", target(sp.Name, w.Name), layout)
	return err
}

// selectWindow makes the space's `select` window (default: the first)
// the session's current one — done before a terminal attaches, so
// that is what it shows.
func selectWindow(sp Space) error {
	name := sp.Select
	if name == "" {
		name = sp.Windows[0].Name
	}
	_, err := tmux("select-window", "-t", target(sp.Name, name))
	return err
}

// hasClient reports whether a client is attached to the session.
func hasClient(session string) bool {
	out, err := tmux("list-clients", "-t", target(session, ""), "-F", "#{client_tty}")
	return err == nil && out != ""
}

// waitForClient waits until a client is attached, so that a command
// which needs one (display-popup) can run right after a spawn.
func waitForClient(session string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if hasClient(session) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// runThen runs the space's `then` command through tmux, targeting the
// session, so it runs as if from a pane there (display-popup works),
// and in the background: a popup must not block `open`.
func runThen(sp Space) error {
	if sp.Then == "" {
		return nil
	}
	_, err := tmux("run-shell", "-b", "-t", target(sp.Name, ""), sp.Then)
	return err
}
