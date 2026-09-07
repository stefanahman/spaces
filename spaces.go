// The verbs: open, focus, key, list, yabai-rules, check.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
)

// open brings a space up: session and windows, terminal window on its
// desktop space, focus — and, with then, the space's `then` command
// once a client is attached.
func open(d desktop, sp Space, then bool, out io.Writer) error {
	unlock, err := lockSpace(sp.Name)
	if err != nil {
		return err
	}
	defer unlock()
	created, err := ensureSession(sp)
	if err != nil {
		return err
	}
	if created {
		if err := selectWindow(sp); err != nil {
			return err
		}
	}
	spawned, err := ensureWindow(d, sp)
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

// lockDir is $XDG_CACHE_HOME/tmux-spaces, else the OS cache dir.
func lockDir() (string, error) {
	if base := os.Getenv("XDG_CACHE_HOME"); base != "" {
		return filepath.Join(base, "tmux-spaces"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "tmux-spaces"), nil
}

// list prints every space with its session state and, when the
// windows carry tmux-claude-status's @claude-state option, what Claude
// is doing there.
//
// The formats are `:`-separated, with the name last: a tmux client
// outside a UTF-8 locale (a hotkey daemon's environment) prints
// control characters in command output as `_`, so a tab would not
// survive, while `:` can't appear in a session name.
func list(spaces []Space, out io.Writer) error {
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
		session := "-"
		if s, ok := sessions[sp.Name]; ok {
			session = fmt.Sprintf("%d windows", s.windows)
			if s.attached {
				session += ", attached"
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", sp.Name, key, space, session, claudeChip(claude[sp.Name]))
	}
	return w.Flush()
}

type sessionInfo struct {
	attached bool
	windows  int
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
		fmt.Fprintf(out, "yabai -m rule --add app=\"^Ghostty$\" title=\"^%s$\" space=^%d\n", sp.Name, sp.Space)
	}
}

// check reports what would stop a space from opening on this machine:
// missing directories, duplicate desktop spaces, missing tools. Exit
// status 1 when anything is wrong; warnings alone pass.
func check(d desktop, spaces []Space, out io.Writer) error {
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
		for _, w := range sp.Windows {
			if len(w.Cwd) > 0 {
				if _, ok := w.Cwd.resolve(); !ok {
					fmt.Fprintf(out, "error: %s: window %s: none of cwd %v exists\n", sp.Name, w.Name, w.Cwd)
					problems++
				}
			}
			for i, p := range w.Panes {
				if len(p.Cwd) > 0 {
					if _, ok := p.Cwd.resolve(); !ok {
						fmt.Fprintf(out, "error: %s: window %s pane %d: none of cwd %v exists\n", sp.Name, w.Name, i, p.Cwd)
						problems++
					}
				}
			}
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
	for _, r := range d.requirements() {
		if !r.ok() {
			fmt.Fprintf(out, "error: %s not found\n", r.name)
			problems++
		}
	}
	if problems > 0 {
		return fmt.Errorf("%d problem(s)", problems)
	}
	fmt.Fprintln(out, "ok")
	return nil
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
