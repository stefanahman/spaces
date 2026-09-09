// The workspaces inside a herdr or cmux space: declared once, built
// where they are missing, left alone where they exist. herdr brings
// its workspaces back across restarts as shells without their
// programs, cmux likewise, and neither knows a startup layout — so the
// config is the declaration, and every open completes it.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stefanahman/mux"
)

// newDriver is the multiplexer of that kind — the one a space builds
// its workspaces in, or one `check` audits; session is herdr's. A
// variable, so tests can put a fake in its place.
var newDriver = func(kind, session string) mux.Driver {
	switch kind {
	case "tmux":
		return mux.Tmux{}
	case "herdr":
		socket := ""
		if session != "" {
			home, _ := os.UserHomeDir()
			socket = filepath.Join(home, ".config", "herdr", "sessions", session, "herdr.sock")
		}
		return mux.NewHerdr(socket)
	case "cmux":
		return mux.NewCmux()
	}
	return nil
}

// How long the multiplexer gets to answer after its launch, and how
// long a busy pane gets to turn out idle before it is left alone.
var (
	pingTimeout = 20 * time.Second
)

// render builds the space's workspaces in its multiplexer and shows
// the selected one — selectWS when given (a key on that workspace),
// the space's `select` otherwise. What exists is kept: workspaces are
// found by name, tabs and panes by position in the layout, and a
// command is typed only into a pane that runs nothing but its shell.
// A workspace this run creates joins its `group:` where the
// multiplexer has them — cmux does, tmux and herdr do not, and there
// the call is a no-op.
func render(d mux.Driver, sp Space, selectWS string, out, errOut io.Writer) error {
	if err := waitForPing(d, pingTimeout); err != nil {
		return fmt.Errorf("%s: %s: %w", sp.Name, sp.Multiplexer, err)
	}
	existing, err := d.Workspaces()
	if err != nil {
		return fmt.Errorf("%s: %w", sp.Name, err)
	}
	byName := map[string]mux.Workspace{}
	for _, ws := range existing {
		byName[ws.Name] = ws
	}
	created := 0
	for _, name := range sp.workspaceNames() {
		w := sp.Workspaces[name]
		ws, ok := byName[name]
		fresh := !ok
		if !ok {
			cwd, err := w.createCwd(sp.Name, name)
			if err != nil {
				return err
			}
			if ws, err = d.Create(name, cwd); err != nil {
				return fmt.Errorf("%s: workspace %s: %w", sp.Name, name, err)
			}
			byName[name] = ws
			created++
			// Only what this run created. Dragging a workspace out of
			// a group in the sidebar is a decision, and the next open
			// must not undo it — so an existing workspace is left
			// where the user put it, group or none.
			if w.Group != "" {
				if err := mux.Group(d, w.Group, ws, mux.GroupStyle{Color: w.style.Color, Icon: w.style.Icon}); err != nil {
					return fmt.Errorf("%s: workspace %s: group %s: %w", sp.Name, name, w.Group, err)
				}
			}
		}
		if err := renderWindows(d, sp.Name, ws, w, fresh, errOut); err != nil {
			return err
		}
	}
	if selectWS == "" {
		selectWS = sp.Select
	}
	if selectWS != "" {
		if err := d.Select(byName[selectWS]); err != nil {
			return fmt.Errorf("%s: select %s: %w", sp.Name, selectWS, err)
		}
	}
	switch {
	case created == len(sp.Workspaces):
		fmt.Fprintf(out, "%s: %d workspaces created\n", sp.Name, created)
	case created > 0:
		fmt.Fprintf(out, "%s: %d of %d workspaces created\n", sp.Name, created, len(sp.Workspaces))
	default:
		fmt.Fprintf(out, "%s: %d workspaces in place\n", sp.Name, len(sp.Workspaces))
	}
	return nil
}

// waitForPing gives the multiplexer time to come up after its launch.
func waitForPing(d mux.Driver, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := d.Ping()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("not answering after %s: %w", timeout, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// createCwd is the directory the workspace is created in: the first
// window's first pane's, else the first window's, else the workspace's.
func (w Workspace) createCwd(space, name string) (string, error) {
	candidates := w.Cwd
	if len(w.Windows) > 0 {
		if win := w.Windows[0]; len(win.Panes) > 0 && len(win.Panes[0].Cwd) > 0 {
			candidates = win.Panes[0].Cwd
		} else if len(win.Cwd) > 0 {
			candidates = win.Cwd
		}
	}
	return resolveCwd(candidates, fmt.Sprintf("%s: workspace %s", space, name))
}

// resolveCwd resolves a declared directory; none declared is "" (the
// multiplexer's own default), none existing is an error.
func resolveCwd(candidates pathList, what string) (string, error) {
	if len(candidates) == 0 {
		return "", nil
	}
	dir, ok := candidates.resolve()
	if !ok {
		return "", fmt.Errorf("%s: none of cwd %v exists", what, candidates)
	}
	return dir, nil
}

// renderWindows completes the workspace's layout: a tab per window
// after the first (the root the workspace came with), a pane per
// declared pane after a tab's first, then the commands.
func renderWindows(d mux.Driver, space string, ws mux.Workspace, w Workspace, fresh bool, errOut io.Writer) error {
	tabs, err := d.Layout(ws)
	if err != nil {
		return fmt.Errorf("%s: workspace %s: %w", space, ws.Name, err)
	}
	// The panes this run created: new shells, typed into without a
	// look. fresh says the whole workspace is.
	isFresh := map[mux.Pane]bool{}
	for i, win := range w.Windows {
		what := fmt.Sprintf("%s: workspace %s window %s", space, ws.Name, win.Name)
		var tab mux.Tab
		if i < len(tabs) {
			tab = tabs[i]
		} else {
			cwd, err := resolveCwd(firstNonEmpty(win.Cwd, w.Cwd), what)
			if err != nil {
				return err
			}
			pane, err := d.AddTab(ws, win.Name, cwd)
			if err != nil {
				return fmt.Errorf("%s: %w", what, err)
			}
			tab = mux.Tab{Name: win.Name, Panes: []mux.Pane{pane}}
			isFresh[pane] = true
		}
		if len(tab.Panes) == 0 {
			return fmt.Errorf("%s: the tab has no pane", what)
		}
		for j := len(tab.Panes); j < len(win.Panes); j++ {
			cwd, err := resolveCwd(firstNonEmpty(win.Panes[j].Cwd, win.Cwd, w.Cwd), fmt.Sprintf("%s pane %d", what, j))
			if err != nil {
				return err
			}
			dir := mux.Right
			if win.Split == "vertical" {
				dir = mux.Down
			}
			pane, err := d.Split(ws, tab.Panes[0], dir, cwd)
			if err != nil {
				return fmt.Errorf("%s pane %d: %w", what, j, err)
			}
			tab.Panes = append(tab.Panes, pane)
			isFresh[pane] = true
		}
		if win.Command != "" {
			if err := runUnless(d, ws, tab.Panes[0], win.Command, what, fresh || isFresh[tab.Panes[0]], errOut); err != nil {
				return err
			}
		}
		for j, p := range win.Panes {
			if p.Command != "" && j < len(tab.Panes) {
				if err := runUnless(d, ws, tab.Panes[j], p.Command, fmt.Sprintf("%s pane %d", what, j), fresh || isFresh[tab.Panes[j]], errOut); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// firstNonEmpty is the first declared directory list of several.
func firstNonEmpty(lists ...pathList) pathList {
	for _, l := range lists {
		if len(l) > 0 {
			return l
		}
	}
	return nil
}

// runUnless types the command into the pane unless its program already
// runs there. A pane this run created is a new shell: the line is
// typed straight away and waits in the pty for the prompt. An existing
// pane is looked at once, and typed into only when it runs nothing but
// a shell — a pane running anything else is left alone, and said so:
// keystrokes into a TUI are commands to it. Under cmux the processes
// are the pane's, not the tab's, which is as fine as cmux tells.
func runUnless(d mux.Driver, ws mux.Workspace, pane mux.Pane, command, what string, fresh bool, errOut io.Writer) error {
	program := programOf(command)
	if !fresh {
		names, err := d.Processes(ws, pane)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		other := ""
		for _, n := range names {
			if n == program {
				return nil
			}
			if !mux.IsShell(n) {
				other = n
			}
		}
		if other != "" {
			fmt.Fprintf(errOut, "spaces: %s is running %s, not %s; leaving it alone\n", what, other, program)
			return nil
		}
	}
	if err := d.Run(ws, pane, command); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// programOf is the name a command line's program shows up under: its
// first word past any VAR=value assignments, without a directory.
func programOf(command string) string {
	for _, word := range strings.Fields(command) {
		if strings.Contains(word, "=") && !strings.HasPrefix(word, "=") {
			continue
		}
		return filepath.Base(word)
	}
	return command
}

// checkMultiplexers asks the multiplexers the spaces run in — tmux,
// and each herdr session and cmux a space declares, once — how they
// were started, for `check`: a server or app that inherited a Claude
// Code session's markers runs every agent as a child session, and a
// cmux launched with TMUX set never engages its hooks (see mux's
// Ping). Prints each as an error and returns how many. A multiplexer
// that isn't running, or won't answer, is not a problem here.
func checkMultiplexers(out io.Writer, spaces []Space) int {
	drivers := []mux.Driver{newDriver("tmux", "")}
	seen := map[string]bool{}
	for _, sp := range spaces {
		if sp.Multiplexer == "" || seen[sp.Multiplexer+"\x00"+sp.Session] {
			continue
		}
		seen[sp.Multiplexer+"\x00"+sp.Session] = true
		drivers = append(drivers, newDriver(sp.Multiplexer, sp.Session))
	}
	problems := 0
	for _, d := range drivers {
		err := d.Ping()
		if err == nil {
			continue
		}
		// A multiplexer that isn't running is nothing to report; one
		// running with the wrong environment is.
		if errors.Is(err, mux.ErrTainted) {
			fmt.Fprintf(out, "error: %v\n", err)
			problems++
		}
	}
	return problems
}

// workspaceCount is how many of the space's workspaces exist, for
// `list`; ok is false when the multiplexer can't be asked.
func workspaceCount(d mux.Driver, sp Space) (have int, ok bool) {
	if d == nil || d.Ping() != nil {
		return 0, false
	}
	existing, err := d.Workspaces()
	if err != nil {
		return 0, false
	}
	for _, ws := range existing {
		if _, declared := sp.Workspaces[ws.Name]; declared {
			have++
		}
	}
	return have, true
}
