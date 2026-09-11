// The verbs: open, focus, key, use, list, yabai-rules, check, config path.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
)

// yabaiRegex escapes what an app name may contain that a regex reads
// specially — ( ) . — so the rule matches the name literally. Inside
// yabairc's double-quoted `eval`, a backslash before these survives
// to yabai.
var yabaiRegex = strings.NewReplacer(`.`, `\.`, `(`, `\(`, `)`, `\)`)

// open brings a space up: session and windows, terminal window on its
// desktop space, focus — and, with then, the space's `then` command
// once a client is attached. A command space has no session, and an
// app space no terminal: see openCommand and openApp.
func open(d desktop, sp Space, then bool, out io.Writer) error {
	return openSpace(d, sp, then, "", out)
}

// openSpace is open with a say in which workspace a herdr or cmux
// space shows: selectWS, when a key on that workspace was pressed.
func openSpace(d desktop, sp Space, then bool, selectWS string, out io.Writer) error {
	unlock, err := lockSpace(sp.Name)
	if err != nil {
		return err
	}
	defer unlock()
	switch {
	case sp.isCommand():
		return openCommand(d, sp, then, selectWS, out)
	case sp.isApp():
		return openApp(d, sp, then, selectWS, out)
	}
	created, err := ensureSession(sp)
	if err != nil {
		return err
	}
	if created {
		if err := selectWindow(sp); err != nil {
			return err
		}
	}
	argv, err := attachCommand(sp.Name)
	if err != nil {
		return err
	}
	spawned, err := ensureWindow(d, sp, argv, func() bool { return hasClient(sp.Name) })
	if err != nil {
		return err
	}
	switch {
	case created && spawned:
		fmt.Fprintf(out, "%s: session and window created\n", sp.Name)
	case spawned:
		fmt.Fprintf(out, "%s: window spawned\n", sp.Name)
	default:
		fmt.Fprintf(out, "%s: focused\n", sp.Name)
	}
	if !then || sp.Then == "" {
		return nil
	}
	if spawned && !waitForClient(sp.Name, 3*time.Second) {
		return fmt.Errorf("%s: no client attached within 3s, not running then", sp.Name)
	}
	return runThen(sp)
}

// openCommand brings a command space up: its terminal window running
// the program, on its desktop space, focused — and, with then, the
// space's `then` command. There is no client to wait for: then runs
// once the window is in front. Nor is there one to ask whether the
// space is up while the window manager can't see its window, so a
// hidden window (a locked screen) is spawned again.
func openCommand(d desktop, sp Space, then bool, selectWS string, out io.Writer) error {
	program, err := exec.LookPath(sp.Command[0])
	if err != nil {
		return fmt.Errorf("%s: %s not found on PATH", sp.Name, sp.Command[0])
	}
	argv := append([]string{program}, sp.Command[1:]...)
	spawned, err := ensureWindow(d, sp, argv, func() bool { return false })
	if err != nil {
		return err
	}
	if spawned {
		fmt.Fprintf(out, "%s: window spawned%s\n", sp.Name, selected(selectWS))
	} else {
		fmt.Fprintf(out, "%s: focused%s\n", sp.Name, selected(selectWS))
	}
	if err := renderWorkspaces(sp, selectWS, out); err != nil {
		return err
	}
	if !then || sp.Then == "" {
		return nil
	}
	return runThenCommand(sp)
}

// renderWorkspaces builds the space's workspaces, when it declares
// any, in the multiplexer the window runs, and shows selectWS when
// given.
func renderWorkspaces(sp Space, selectWS string, out io.Writer) error {
	if !sp.hasWorkspaces() {
		return nil
	}
	return render(newDriver(sp.Multiplexer, sp.Session), sp, selectWS, out, os.Stderr)
}

// selected names the workspace a key press lands on, for the status
// line; nothing for a plain open.
func selected(selectWS string) string {
	if selectWS == "" {
		return ""
	}
	return "; " + selectWS + " selected"
}

// openApp brings an app space up: the application's window on its
// desktop space, focused, the application launched first when it has
// none. Applications start slowly, so the wait is longer than a
// terminal's; like a command space there is no client to wait for or
// to ask about a window the window manager can't see. macOS keeps an
// application alive with no windows: `open -a` then activates it,
// which reopens one for most apps, and the wait covers that too.
func openApp(d desktop, sp Space, then bool, selectWS string, out io.Writer) error {
	id, err := d.findAppWindow(sp.App)
	if err != nil {
		return err
	}
	launched := id == ""
	if launched {
		if err := d.launch(sp.App, sp.envList()); err != nil {
			return err
		}
		find := func() (string, error) { return d.findAppWindow(sp.App) }
		if id, err = waitForWindow(find, sp.App+"'s window", 15*time.Second); err != nil {
			return err
		}
		if sp.Space > 0 {
			if err := d.moveToSpace(id, sp.Space); err != nil {
				return err
			}
		}
	}
	if err := d.focus(id); err != nil {
		return err
	}
	if launched {
		fmt.Fprintf(out, "%s: app launched%s\n", sp.Name, selected(selectWS))
	} else {
		fmt.Fprintf(out, "%s: focused%s\n", sp.Name, selected(selectWS))
	}
	if err := renderWorkspaces(sp, selectWS, out); err != nil {
		return err
	}
	if !then || sp.Then == "" {
		return nil
	}
	return runThenCommand(sp)
}

// runThenCommand runs a command or app space's `then` through the
// shell, in the background like runThen, with the launch environment:
// a `then` starts things too. Its output goes where spaces's does:
// there is no session for tmux to show it in.
func runThenCommand(sp Space) error {
	cmd := exec.Command("/bin/sh", "-c", sp.Then)
	cmd.Env = launchEnv()
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: then: %w", sp.Name, err)
	}
	return cmd.Process.Release()
}

// lockSpace serialises open per space. Two opens of the same space at
// once — a double key press — each found no window and each spawned
// one: the duplicate guard only knows about attached sessions, and
// neither had attached yet. An advisory lock held for the whole of
// open makes the second wait, then find what the first created.
func lockSpace(name string) (unlock func(), err error) {
	dir, err := lockDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, name+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("lock %s: %w", f.Name(), err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// lockDir is $XDG_CACHE_HOME/spaces, else the OS cache dir.
func lockDir() (string, error) {
	if base := os.Getenv("XDG_CACHE_HOME"); base != "" {
		return filepath.Join(base, "spaces"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "spaces"), nil
}

// list prints the active multiplexer, then every space with its keys
// (its own, and its workspaces' in brackets), its session state (for
// a command or app space: whether its window is open, and for one with
// workspaces how many of them exist) and, when the windows carry
// tmux-claude-status's @claude-state option, what Claude is doing there.
//
// The formats are `:`-separated, with the name last: a tmux client
// outside a UTF-8 locale (a hotkey daemon's environment) prints
// control characters in command output as `_`, so a tab would not
// survive, while `:` can't appear in a session name.
func list(d desktop, spaces []Space, out io.Writer) error {
	active, err := activeMultiplexer()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "multiplexer: %s\n", active)
	sessions := map[string]sessionInfo{}
	if outStr, err := tmux("list-sessions", "-F", "#{session_attached}:#{session_windows}:#{session_name}"); err == nil {
		for _, line := range strings.Split(outStr, "\n") {
			f := strings.SplitN(line, ":", 3)
			if len(f) != 3 {
				continue
			}
			attached, _ := strconv.Atoi(f[0])
			windows, _ := strconv.Atoi(f[1])
			sessions[f[2]] = sessionInfo{attached: attached > 0, windows: windows}
		}
	}
	claude := map[string]map[string]int{}
	if outStr, err := tmux("list-windows", "-a", "-F", "#{@claude-state}:#{session_name}"); err == nil {
		for _, line := range strings.Split(outStr, "\n") {
			state, name, _ := strings.Cut(line, ":")
			if state == "" {
				continue
			}
			if claude[name] == nil {
				claude[name] = map[string]int{}
			}
			claude[name][state]++
		}
	}

	sorted := append([]Space(nil), spaces...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Space != sorted[j].Space {
			return sorted[i].Space < sorted[j].Space
		}
		return sorted[i].Name < sorted[j].Name
	})
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tKEY\tSPACE\tSESSION\tCLAUDE")
	for _, sp := range sorted {
		space, key := "-", "-"
		if sp.Space > 0 {
			space = strconv.Itoa(sp.Space)
		}
		if sp.Key != "" {
			key = sp.Key
		}
		if keys := sp.workspaceKeys(); len(keys) > 0 {
			key += " (" + strings.Join(keys, " ") + ")"
		}
		session := "-"
		switch {
		case sp.isCommand():
			session = windowState(d, func(d desktop) (string, error) { return d.findWindow(sp.Name) })
		case sp.isApp():
			session = windowState(d, func(d desktop) (string, error) { return d.findAppWindow(sp.App) })
		default:
			if s, ok := sessions[sp.Name]; ok {
				session = fmt.Sprintf("%d windows", s.windows)
				if s.attached {
					session += ", attached"
				}
			}
		}
		if sp.hasWorkspaces() {
			have := "?"
			if n, ok := workspaceCount(newDriver(sp.Multiplexer, sp.Session), sp); ok {
				have = strconv.Itoa(n)
			}
			session += fmt.Sprintf(", %s/%d workspaces", have, len(sp.Workspaces))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", sp.Name, key, space, session, claudeChip(claude[sp.Name]))
	}
	return w.Flush()
}

type sessionInfo struct {
	attached bool
	windows  int
}

// windowState is a command or app space's counterpart of the session
// column: whether its window, as find looks it up, is up. "?" when the
// window manager can't be asked (no desktop backend on this OS).
func windowState(d desktop, find func(desktop) (string, error)) string {
	if d == nil {
		return "?"
	}
	id, err := find(d)
	switch {
	case err != nil:
		return "?"
	case id != "":
		return "window open"
	default:
		return "-"
	}
}

// claudeChip renders state counts the way tmux-claude-status does:
// N⚠ blocked, N~ working, N* done (unread), N idle.
func claudeChip(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}
	var parts []string
	for _, s := range []struct{ state, mark string }{{"blocked", "⚠"}, {"working", "~"}, {"done", "*"}, {"idle", ""}} {
		if n := counts[s.state]; n > 0 {
			parts = append(parts, strconv.Itoa(n)+s.mark)
		}
	}
	return strings.Join(parts, " ")
}

// yabaiRules prints one rule per pinned space, for `eval` in yabairc:
// the space number then has exactly one home, this config.
func yabaiRules(spaces []Space, out io.Writer) {
	sorted := append([]Space(nil), spaces...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Space != sorted[j].Space {
			return sorted[i].Space < sorted[j].Space
		}
		return sorted[i].Name < sorted[j].Name
	})
	for _, sp := range sorted {
		if sp.Space == 0 {
			continue
		}
		if sp.isApp() {
			fmt.Fprintf(out, "yabai -m rule --add app=\"^%s$\" space=^%d\n", yabaiRegex.Replace(sp.App), sp.Space)
			continue
		}
		fmt.Fprintf(out, "yabai -m rule --add app=\"^Ghostty$\" title=\"^%s$\" space=^%d\n", sp.Name, sp.Space)
	}
}

// check reports what would stop a space from opening on this machine:
// missing directories, a command not on PATH, an application not
// installed, desktop spaces that don't exist, duplicate desktop
// spaces, missing tools — and what would spoil the agents in it: a
// running multiplexer started with the wrong environment. Exit status
// 1 when anything is wrong; warnings alone pass.
func check(d desktop, spaces []Space, groups map[string]Group, out io.Writer) error {
	problems := 0
	files, _ := configFiles()
	fmt.Fprintf(out, "config: %d file(s), %d space(s)\n", len(files), len(spaces))
	for _, sp := range spaces {
		if len(sp.Cwd) > 0 {
			if _, ok := sp.Cwd.resolve(); !ok {
				fmt.Fprintf(out, "error: %s: none of cwd %v exists\n", sp.Name, sp.Cwd)
				problems++
			}
		}
		if sp.isCommand() {
			if _, err := exec.LookPath(sp.Command[0]); err != nil {
				fmt.Fprintf(out, "error: %s: command %q not found on PATH\n", sp.Name, sp.Command[0])
				problems++
			}
		}
		if sp.isApp() && !appBundleExists(sp.App) {
			fmt.Fprintf(out, "error: %s: no %s.app in /Applications, ~/Applications or /System/Applications\n", sp.Name, sp.App)
			problems++
		}
		problems += checkWindows(out, sp.Name, sp.Windows)
		for _, name := range sp.workspaceNames() {
			ws := sp.Workspaces[name]
			if len(ws.Cwd) > 0 {
				if _, ok := ws.Cwd.resolve(); !ok {
					fmt.Fprintf(out, "error: %s: workspace %s: none of cwd %v exists\n", sp.Name, name, ws.Cwd)
					problems++
				}
			}
			problems += checkWindows(out, sp.Name+": workspace "+name, ws.Windows)
		}
	}
	bySpace := map[int][]string{}
	for _, sp := range spaces {
		if sp.Space > 0 {
			bySpace[sp.Space] = append(bySpace[sp.Space], sp.Name)
		}
	}
	for n, names := range bySpace {
		if len(names) > 1 {
			fmt.Fprintf(out, "warning: desktop space %d is claimed by %s\n", n, strings.Join(names, ", "))
		}
	}
	// A group nobody joins describes nothing. Usually a renamed
	// workspace, or a `group:` spelled differently from the heading.
	used := map[string]bool{}
	for _, sp := range spaces {
		for _, name := range sp.workspaceNames() {
			if g := sp.Workspaces[name].Group; g != "" {
				used[g] = true
			}
		}
	}
	unused := make([]string, 0, len(groups))
	for name := range groups {
		if !used[name] {
			unused = append(unused, name)
		}
	}
	sort.Strings(unused)
	for _, name := range unused {
		fmt.Fprintf(out, "warning: group %q is declared in %s and no workspace joins it\n", name, groups[name].file)
	}
	// A space lands on its select every time it opens, and landing on
	// a workspace makes it: an on_demand select is a workspace that is
	// born at every open, which is what on_demand was asked not to do.
	for _, sp := range spaces {
		if sp.Select != "" && sp.Workspaces[sp.Select].OnDemand {
			fmt.Fprintf(out, "warning: %s selects %q, which is on_demand: it will be created on every open\n", sp.Name, sp.Select)
		}
	}
	toolsOK := true
	for _, r := range d.requirements() {
		if !r.ok() {
			fmt.Fprintf(out, "error: %s not found\n", r.name)
			problems++
			toolsOK = false
		}
	}
	// A pin to a desktop space that doesn't exist fails open half-way,
	// after the window was spawned. Only asked with the tools present:
	// without them the query can't work, and that is reported above.
	if toolsOK {
		if have, err := d.spaces(); err != nil {
			fmt.Fprintf(out, "error: desktop spaces: %v\n", err)
			problems++
		} else if len(have) == 0 {
			fmt.Fprintf(out, "error: desktop spaces: none reported\n")
			problems++
		} else {
			for _, sp := range spaces {
				if sp.Space > 0 && !slices.Contains(have, sp.Space) {
					fmt.Fprintf(out, "error: %s: desktop space %d does not exist (this desktop has 1..%d)\n", sp.Name, sp.Space, slices.Max(have))
					problems++
				}
			}
		}
	}
	problems += checkMultiplexers(out, spaces)
	if problems > 0 {
		return fmt.Errorf("%d problem(s)", problems)
	}
	fmt.Fprintln(out, "ok")
	return nil
}

// checkWindows reports the windows and panes whose directories are
// missing; returns how many.
func checkWindows(out io.Writer, what string, windows []Window) int {
	problems := 0
	for _, w := range windows {
		if len(w.Cwd) > 0 {
			if _, ok := w.Cwd.resolve(); !ok {
				fmt.Fprintf(out, "error: %s: window %s: none of cwd %v exists\n", what, w.Name, w.Cwd)
				problems++
			}
		}
		for i, p := range w.Panes {
			if len(p.Cwd) > 0 {
				if _, ok := p.Cwd.resolve(); !ok {
					fmt.Fprintf(out, "error: %s: window %s pane %d: none of cwd %v exists\n", what, w.Name, i, p.Cwd)
					problems++
				}
			}
		}
	}
	return problems
}

// configPathCmd prints the directory the config is read from.
func configPathCmd(out io.Writer) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, dir)
	return nil
}
