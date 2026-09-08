// Configuration: every *.yaml in $XDG_CONFIG_HOME/spaces/spaces.d/
// plus an optional spaces.yaml next to it, merged. Each file declares
// spaces under a `spaces:` mapping; a name or key declared twice
// anywhere is an error — never a silent override — because the files
// typically come from different sources (one per dotfiles branch).
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Space is one workspace: a window pinned to a desktop space,
// reachable by a key. A terminal window showing a tmux session with a
// fixed layout (windows), or running one program directly (command) —
// for programs that are multiplexers themselves, like herdr — or an
// application's window (app). A command or app space that is itself a
// multiplexer (herdr, cmux) can declare the workspaces to build inside
// it.
type Space struct {
	Name        string               `yaml:"-"`
	Key         string               `yaml:"key"`
	Space       int                  `yaml:"space"` // desktop space; 0 = not pinned
	Cwd         pathList             `yaml:"cwd"`
	Windows     []Window             `yaml:"windows"`
	Command     argv                 `yaml:"command"`     // runs in the terminal instead of a tmux session
	App         string               `yaml:"app"`         // an application, by name, instead of a terminal
	Env         map[string]string    `yaml:"env"`         // environment the application is launched with (app spaces only)
	Multiplexer string               `yaml:"multiplexer"` // herdr or cmux: what renders the workspaces
	Session     string               `yaml:"session"`     // herdr: the session whose socket to use; default: herdr's default socket
	Workspaces  map[string]Workspace `yaml:"workspaces"`  // the work context built inside the multiplexer, by name
	Select      string               `yaml:"select"`      // window selected when the session is created (default: the first), or the workspace shown when the space opens
	Then        string               `yaml:"then"`        // run after `open` has focused the space

	file string // where it was declared, for error messages
}

// Workspace is one workspace inside a herdr or cmux space: its
// directory and its windows — tabs there — with the panes of each.
// The first window is the pane the workspace is created with.
type Workspace struct {
	Key     string   `yaml:"key"` // one of 0-9 a-z: `spaces key <k>` opens the space on this workspace
	Cwd     pathList `yaml:"cwd"`
	Windows []Window `yaml:"windows"`
}

// hasWorkspaces reports whether the space builds workspaces inside a
// multiplexer.
func (sp Space) hasWorkspaces() bool { return len(sp.Workspaces) > 0 }

// workspaceNames lists the workspaces in name order: the order they
// are created in, the same every time.
func (sp Space) workspaceNames() []string {
	names := make([]string, 0, len(sp.Workspaces))
	for name := range sp.Workspaces {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// workspaceKeys lists the keys bound to the space's workspaces, sorted.
func (sp Space) workspaceKeys() []string {
	var keys []string
	for _, ws := range sp.Workspaces {
		if ws.Key != "" {
			keys = append(keys, ws.Key)
		}
	}
	sort.Strings(keys)
	return keys
}

// envList renders env as KEY=VALUE entries, values expanded like paths
// (~ and $VAR), sorted by name so a launch is the same every time.
func (sp Space) envList() []string {
	names := make([]string, 0, len(sp.Env))
	for name := range sp.Env {
		names = append(names, name)
	}
	sort.Strings(names)
	env := make([]string, len(names))
	for i, name := range names {
		env[i] = name + "=" + expand(sp.Env[name])
	}
	return env
}

// isCommand reports whether the space runs its command in the terminal
// itself, with no tmux session.
func (sp Space) isCommand() bool { return len(sp.Command) > 0 }

// isApp reports whether the space is an application's window rather
// than a terminal.
func (sp Space) isApp() bool { return sp.App != "" }

// Window is a tmux window of a space. In YAML a bare string is a
// window of that name running the shell.
type Window struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Cwd     pathList `yaml:"cwd"`
	Panes   []Pane   `yaml:"panes"`
	Split   string   `yaml:"split"` // horizontal (side by side, default) or vertical
}

func (w *Window) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		var name string
		if err := n.Decode(&name); err != nil {
			return err
		}
		*w = Window{Name: name}
		return nil
	}
	type plain Window // no UnmarshalYAML, avoids recursion
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*w = Window(p)
	return nil
}

// Pane is one pane of a window.
type Pane struct {
	Cwd     pathList `yaml:"cwd"`
	Command string   `yaml:"command"`
}

// pathList is a directory, or a list of candidates of which the first
// that exists is used — one config for machines that keep a repo in
// different places.
type pathList []string

func (p *pathList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		var s string
		if err := n.Decode(&s); err != nil {
			return err
		}
		*p = pathList{s}
		return nil
	}
	var list []string
	if err := n.Decode(&list); err != nil {
		return err
	}
	*p = list
	return nil
}

// argv is a command line: a string, split on whitespace, or a list for
// when an argument contains a space. No shell is involved either way:
// the program is resolved on PATH and run with these arguments.
type argv []string

func (a *argv) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		var s string
		if err := n.Decode(&s); err != nil {
			return err
		}
		*a = strings.Fields(s)
		return nil
	}
	var list []string
	if err := n.Decode(&list); err != nil {
		return err
	}
	*a = list
	return nil
}

// resolve returns the first candidate that is a directory, with ~ and
// $VAR expanded. ok is false when none exists (or the list is empty).
func (p pathList) resolve() (dir string, ok bool) {
	for _, c := range p {
		c = expand(c)
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c, true
		}
	}
	return "", false
}

// expand replaces a leading ~ and $VAR references.
func expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = home + p[1:]
		}
	}
	return os.ExpandEnv(p)
}

// configDir is $XDG_CONFIG_HOME/spaces, else ~/.config/spaces.
func configDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "spaces"), nil
}

// configFiles lists the files that are read, in order: spaces.yaml,
// then spaces.d/*.yaml sorted by name. Missing files are fine — unless
// they sit where the tool's former name, tmux-spaces, read them.
func configFiles() ([]string, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	var files []string
	if _, err := os.Stat(filepath.Join(dir, "spaces.yaml")); err == nil {
		files = append(files, filepath.Join(dir, "spaces.yaml"))
	}
	more, err := filepath.Glob(filepath.Join(dir, "spaces.d", "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(more)
	files = append(files, more...)
	if len(files) == 0 {
		old := filepath.Join(filepath.Dir(dir), "tmux-spaces")
		if _, err := os.Stat(old); err == nil {
			return nil, fmt.Errorf("config moved: %s → %s; move the files", old, dir)
		}
	}
	return files, nil
}

// loadSpaces reads and merges every config file.
func loadSpaces() ([]Space, error) {
	files, err := configFiles()
	if err != nil {
		return nil, err
	}
	var sources []source
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source{name: f, data: data})
	}
	return mergeSpaces(sources)
}

type source struct {
	name string
	data []byte
}

// mergeSpaces parses each source and merges them, rejecting a name
// declared twice, or a key bound twice within one multiplexer — a
// space's own key lives in its realm, a workspace's in the space's
// multiplexer, so bf-1 can be key 1 as a tmux space, a herdr
// workspace and a cmux workspace at once. The result is sorted by name.
func mergeSpaces(sources []source) ([]Space, error) {
	byName := map[string]Space{}
	byKey := map[string]map[string]string{} // realm → key → holder
	bind := func(realm, key string, holder keyHolder) error {
		if byKey[realm] == nil {
			byKey[realm] = map[string]string{}
		}
		if other, dup := byKey[realm][key]; dup {
			return fmt.Errorf("key %q is bound to both %s and %s", key, other, holder)
		}
		byKey[realm][key] = holder.String()
		return nil
	}
	for _, src := range sources {
		var file struct {
			Spaces map[string]Space `yaml:"spaces"`
		}
		dec := yaml.NewDecoder(bytes.NewReader(src.data))
		dec.KnownFields(true)
		if err := dec.Decode(&file); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: %w", src.name, err)
		}
		// Deterministic error messages: visit names in order.
		names := make([]string, 0, len(file.Spaces))
		for name := range file.Spaces {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			sp := file.Spaces[name]
			sp.Name = name
			sp.file = src.name
			if prev, dup := byName[name]; dup {
				return nil, fmt.Errorf("space %q is declared in both %s and %s", name, prev.file, src.name)
			}
			if err := sp.validate(); err != nil {
				return nil, fmt.Errorf("%s: space %q: %w", src.name, name, err)
			}
			if sp.Key != "" {
				if err := bind(sp.realm(), sp.Key, keyHolder{space: sp}); err != nil {
					return nil, err
				}
			}
			for _, wsName := range sp.workspaceNames() {
				if key := sp.Workspaces[wsName].Key; key != "" {
					if err := bind(sp.Multiplexer, key, keyHolder{space: sp, workspace: wsName}); err != nil {
						return nil, err
					}
				}
			}
			byName[name] = sp
		}
	}
	out := make([]Space, 0, len(byName))
	for _, sp := range byName {
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// validKey is what a space or a workspace may be bound to: nothing,
// or one of 0-9 a-z.
func validKey(key string) bool {
	return key == "" || (len(key) == 1 && strings.ContainsAny(key, "0123456789abcdefghijklmnopqrstuvwxyz"))
}

// validName is what a space or a window may be called. A name becomes
// a tmux session or window name and a `-t` target, the terminal's
// title and, through `yabai-rules`, part of a regex that yabairc
// evals — so only characters that mean nothing to any of them.
var validName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// validApp is what an application may be called: its name as the
// window manager reports it and `open -a` accepts it. It becomes a
// yabai regex too — see yabaiRegex — so nothing a regex or the shell
// reads specially beyond what yabaiRegex escapes.
var validApp = regexp.MustCompile(`^[A-Za-z0-9 ()._-]+$`)

// validEnvName is what an environment variable may be called.
var validEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validate checks one space's shape; paths are checked by `check`
// and at open time, because they differ per machine.
func (sp Space) validate() error {
	if !validName.MatchString(sp.Name) {
		return fmt.Errorf("name %q: only letters, digits, - and _ are allowed (it is the tmux session name and the window's title)", sp.Name)
	}
	if !validKey(sp.Key) {
		return fmt.Errorf("key %q must be one of 0-9 a-z", sp.Key)
	}
	if sp.Space < 0 {
		return fmt.Errorf("space must be a positive number, got %d", sp.Space)
	}
	kinds := 0
	for _, is := range []bool{len(sp.Windows) > 0, sp.isCommand(), sp.isApp()} {
		if is {
			kinds++
		}
	}
	switch {
	case kinds == 0:
		return errors.New("windows, command or app is required")
	case kinds > 1:
		return errors.New("windows, command and app are exclusive: a space shows a tmux session, runs a program, or is an application")
	case sp.isCommand() && sp.Command[0] == "":
		return errors.New("command names no program")
	case sp.isCommand() && sp.Select != "" && !sp.hasWorkspaces():
		return errors.New("select picks a window; a command space has none")
	case sp.isApp() && !validApp.MatchString(sp.App):
		return fmt.Errorf("app %q: only letters, digits, space, ( ) . - and _ are allowed (it becomes a yabai rule's regex)", sp.App)
	case sp.isApp() && len(sp.Cwd) > 0:
		return errors.New("cwd applies to windows and command; an app space has none")
	case sp.isApp() && sp.Select != "" && !sp.hasWorkspaces():
		return errors.New("select picks a window; an app space has none")
	case len(sp.Env) > 0 && !sp.isApp():
		return errors.New("env applies to app spaces; a tmux window inherits the server's environment, a command runs in Ghostty's")
	case sp.hasWorkspaces() && len(sp.Windows) > 0:
		return errors.New("workspaces are built inside a herdr or cmux space (command or app); a tmux space has windows")
	case sp.hasWorkspaces() && sp.Multiplexer == "":
		return errors.New("multiplexer is required with workspaces: herdr or cmux")
	case sp.Multiplexer != "" && sp.Multiplexer != "herdr" && sp.Multiplexer != "cmux":
		return fmt.Errorf("multiplexer must be herdr or cmux, got %q", sp.Multiplexer)
	case sp.Multiplexer != "" && !sp.hasWorkspaces():
		return errors.New("multiplexer applies to workspaces; declare some")
	case sp.Session != "" && sp.Multiplexer != "herdr":
		return errors.New("session applies to a herdr multiplexer")
	}
	for name := range sp.Env {
		if !validEnvName.MatchString(name) {
			return fmt.Errorf("env: %q is not a variable name", name)
		}
	}
	seen, err := validateWindows(sp.Windows)
	if err != nil {
		return err
	}
	for _, name := range sp.workspaceNames() {
		if !validName.MatchString(name) {
			return fmt.Errorf("workspace %q: only letters, digits, - and _ are allowed", name)
		}
		if !validKey(sp.Workspaces[name].Key) {
			return fmt.Errorf("workspace %q: key %q must be one of 0-9 a-z", name, sp.Workspaces[name].Key)
		}
		if _, err := validateWindows(sp.Workspaces[name].Windows); err != nil {
			return fmt.Errorf("workspace %q: %w", name, err)
		}
	}
	switch {
	case sp.Select == "":
	case sp.hasWorkspaces():
		if _, ok := sp.Workspaces[sp.Select]; !ok {
			return fmt.Errorf("select names workspace %q, which is not declared", sp.Select)
		}
	case !seen[sp.Select]:
		return fmt.Errorf("select names window %q, which is not declared", sp.Select)
	}
	return nil
}

// validateWindows checks a list of windows: named, uniquely, with a
// sensible split and the command where it belongs. Returns the names.
func validateWindows(windows []Window) (map[string]bool, error) {
	seen := map[string]bool{}
	for i, w := range windows {
		if w.Name == "" {
			return nil, fmt.Errorf("windows[%d] has no name", i)
		}
		if !validName.MatchString(w.Name) {
			return nil, fmt.Errorf("window %q: only letters, digits, - and _ are allowed", w.Name)
		}
		if seen[w.Name] {
			return nil, fmt.Errorf("window %q is declared twice", w.Name)
		}
		seen[w.Name] = true
		if w.Split != "" && w.Split != "horizontal" && w.Split != "vertical" {
			return nil, fmt.Errorf("window %q: split must be horizontal or vertical, got %q", w.Name, w.Split)
		}
		if len(w.Panes) > 0 && w.Command != "" {
			return nil, fmt.Errorf("window %q: give the command to the panes, not the window", w.Name)
		}
	}
	return seen, nil
}

// find returns the space with the given name.
func find(spaces []Space, name string) (Space, bool) {
	for _, sp := range spaces {
		if sp.Name == name {
			return sp, true
		}
	}
	return Space{}, false
}
