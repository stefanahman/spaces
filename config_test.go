package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMergeSpaces(t *testing.T) {
	t.Setenv("HOME", "/home/owl")
	spaces, err := mergeSpaces([]source{
		{"a.yaml", []byte(`
spaces:
  bf-1:
    key: "1"
    space: 3
    cwd: ~/src/app
    windows: [shell, {name: nvim, command: nvim}]
  pr-reviews:
    key: r
    space: 9
    cwd: [~/nope, $HOME/src/app]
    windows: [scratch]
    then: tmux display-popup -E pr-owl
`)},
		{"b.yaml", []byte(`
spaces:
  eden:
    key: e
    space: 11
    windows:
      - name: eden
        panes:
          - {cwd: ~/src/eden}
          - {cwd: ~/src/eden-private-branches, command: nvim}
      - {name: nvim, command: nvim, cwd: ~/src/eden-private-branches}
    select: nvim
  unpinned:
    windows: [shell]
  herdr:
    key: h
    space: 7
    cwd: ~/src/app
    command: herdr --session work
  note:
    command: [sh, -c, "echo hi there"]
  cmux:
    key: c
    space: 8
    app: cmux
    env: {CMUX_SOCKET_MODE: allowAll, CMUX_HOME: ~/cmux}
    then: ~/.local/bin/cmux-setup
`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, sp := range spaces {
		names = append(names, sp.Name)
	}
	if want := []string{"bf-1", "cmux", "eden", "herdr", "note", "pr-reviews", "unpinned"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	bf, _ := find(spaces, "bf-1")
	if bf.Key != "1" || bf.Space != 3 || bf.file != "a.yaml" || bf.isCommand() {
		t.Errorf("bf-1 = %+v", bf)
	}
	if herdr, _ := find(spaces, "herdr"); !herdr.isCommand() || !reflect.DeepEqual(herdr.Command, argv{"herdr", "--session", "work"}) || herdr.Space != 7 {
		t.Errorf("herdr = %+v", herdr)
	}
	if note, _ := find(spaces, "note"); !reflect.DeepEqual(note.Command, argv{"sh", "-c", "echo hi there"}) {
		t.Errorf("note (a list keeps an argument with a space) = %+v", note)
	}
	cmux, _ := find(spaces, "cmux")
	if !cmux.isApp() || cmux.App != "cmux" || cmux.Key != "c" || cmux.Space != 8 || cmux.Then == "" {
		t.Errorf("cmux = %+v", cmux)
	}
	if want := []string{"CMUX_HOME=/home/owl/cmux", "CMUX_SOCKET_MODE=allowAll"}; !reflect.DeepEqual(cmux.envList(), want) {
		t.Errorf("cmux env = %v, want %v (sorted, ~ expanded)", cmux.envList(), want)
	}
	if want := []Window{{Name: "shell"}, {Name: "nvim", Command: "nvim"}}; !reflect.DeepEqual(bf.Windows, want) {
		t.Errorf("bf-1 windows = %+v, want %+v", bf.Windows, want)
	}
	pr, _ := find(spaces, "pr-reviews")
	if want := (pathList{"~/nope", "$HOME/src/app"}); !reflect.DeepEqual(pr.Cwd, want) {
		t.Errorf("pr-reviews cwd = %v, want %v", pr.Cwd, want)
	}
	eden, _ := find(spaces, "eden")
	if len(eden.Windows[0].Panes) != 2 || eden.Windows[0].Panes[1].Command != "nvim" || eden.Select != "nvim" {
		t.Errorf("eden = %+v", eden)
	}
	if sp, ok := findKey(spaces, "e"); !ok || sp.Name != "eden" {
		t.Errorf("findKey(e) = %+v, %v", sp, ok)
	}
	if _, ok := findKey(spaces, "z"); ok {
		t.Error("findKey(z) should miss")
	}
}

func TestWorkspaces(t *testing.T) {
	t.Setenv("HOME", "/home/owl")
	spaces, err := mergeSpaces([]source{{"bardo.yaml", []byte(`
spaces:
  herdr:
    key: h
    space: 7
    command: herdr --session work
    multiplexer: herdr
    session: work
    workspaces:
      bf-1: {cwd: ~/Development/bardo/bardo-system, windows: [shell, {name: nvim, command: nvim}]}
      eden:
        cwd: ~/Development/eden
        windows:
          - {name: eden, panes: [{}, {cwd: ~/Development/eden-private-branches}]}
          - {name: nvim, command: nvim, cwd: ~/Development/eden-private-branches}
      pr-owl: {cwd: ~/Development/bardo/bardo-system, windows: [{name: pr-owl, command: pr-owl --mux herdr}]}
    select: pr-owl
  cmux:
    key: c
    space: 8
    app: cmux
    env: {CMUX_SOCKET_MODE: allowAll}
    multiplexer: cmux
    workspaces:
      pr-owl: {cwd: ~/Development/bardo/bardo-system, windows: [{name: pr-owl, command: pr-owl --mux cmux}]}
`)}})
	if err != nil {
		t.Fatal(err)
	}
	herdr, _ := find(spaces, "herdr")
	if herdr.Multiplexer != "herdr" || herdr.Session != "work" || len(herdr.Workspaces) != 3 || herdr.Select != "pr-owl" || !herdr.hasWorkspaces() {
		t.Errorf("herdr = %+v", herdr)
	}
	if got := herdr.workspaceNames(); !reflect.DeepEqual(got, []string{"bf-1", "eden", "pr-owl"}) {
		t.Errorf("workspace names = %v", got)
	}
	eden := herdr.Workspaces["eden"]
	if len(eden.Windows) != 2 || len(eden.Windows[0].Panes) != 2 || eden.Windows[1].Command != "nvim" || !reflect.DeepEqual(eden.Windows[1].Cwd, pathList{"~/Development/eden-private-branches"}) {
		t.Errorf("eden = %+v", eden)
	}
	if bf := herdr.Workspaces["bf-1"]; !reflect.DeepEqual(bf.Windows, []Window{{Name: "shell"}, {Name: "nvim", Command: "nvim"}}) {
		t.Errorf("bf-1 windows = %+v", bf.Windows)
	}
	cmux, _ := find(spaces, "cmux")
	if cmux.Multiplexer != "cmux" || cmux.Workspaces["pr-owl"].Windows[0].Command != "pr-owl --mux cmux" || cmux.Select != "" {
		t.Errorf("cmux = %+v", cmux)
	}
}

func TestMergeSpacesRejects(t *testing.T) {
	cases := map[string][]source{
		"name twice": {
			{"a.yaml", []byte("spaces:\n  x: {key: a, windows: [shell]}\n")},
			{"b.yaml", []byte("spaces:\n  x: {key: b, windows: [shell]}\n")},
		},
		"key twice": {
			{"a.yaml", []byte("spaces:\n  x: {key: a, windows: [shell]}\n  y: {key: a, windows: [shell]}\n")},
		},
		"unknown field":   {{"a.yaml", []byte("spaces:\n  x: {key: a, windows: [shell], colour: red}\n")}},
		"bad key":         {{"a.yaml", []byte("spaces:\n  x: {key: ab, windows: [shell]}\n")}},
		"upper key":       {{"a.yaml", []byte("spaces:\n  x: {key: A, windows: [shell]}\n")}},
		"no windows":      {{"a.yaml", []byte("spaces:\n  x: {key: a}\n")}},
		"windows+command": {{"a.yaml", []byte("spaces:\n  x: {windows: [shell], command: herdr}\n")}},
		"blank command":   {{"a.yaml", []byte("spaces:\n  x: {command: ' '}\n")}},
		"blank program":   {{"a.yaml", []byte("spaces:\n  x: {command: ['']}\n")}},
		"select+command":  {{"a.yaml", []byte("spaces:\n  x: {command: herdr, select: w}\n")}},
		"app+windows":     {{"a.yaml", []byte("spaces:\n  x: {app: Slack, windows: [shell]}\n")}},
		"app+command":     {{"a.yaml", []byte("spaces:\n  x: {app: Slack, command: slack}\n")}},
		"app+cwd":         {{"a.yaml", []byte("spaces:\n  x: {app: Slack, cwd: ~/src}\n")}},
		"app+select":      {{"a.yaml", []byte("spaces:\n  x: {app: Slack, select: w}\n")}},
		"app with pipe":   {{"a.yaml", []byte("spaces:\n  x: {app: 'a|b'}\n")}},
		"app with quote":  {{"a.yaml", []byte("spaces:\n  x: {app: 'a\"b'}\n")}},
		"app with dollar": {{"a.yaml", []byte("spaces:\n  x: {app: 'a$b'}\n")}},
		"env on windows":  {{"a.yaml", []byte("spaces:\n  x: {windows: [shell], env: {A: b}}\n")}},
		"env on command":  {{"a.yaml", []byte("spaces:\n  x: {command: herdr, env: {A: b}}\n")}},
		"env bad name":    {{"a.yaml", []byte("spaces:\n  x: {app: Slack, env: {'1A': b}}\n")}},
		"env dashed name": {{"a.yaml", []byte("spaces:\n  x: {app: Slack, env: {A-B: b}}\n")}},
		"window twice":    {{"a.yaml", []byte("spaces:\n  x: {windows: [shell, shell]}\n")}},
		"nameless window": {{"a.yaml", []byte("spaces:\n  x: {windows: [{command: nvim}]}\n")}},
		"bad split":       {{"a.yaml", []byte("spaces:\n  x: {windows: [{name: w, split: diagonal}]}\n")}},
		"command+panes":   {{"a.yaml", []byte("spaces:\n  x: {windows: [{name: w, command: nvim, panes: [{}, {}]}]}\n")}},
		"select unknown":  {{"a.yaml", []byte("spaces:\n  x: {windows: [shell], select: nvim}\n")}},
		// Workspaces live inside a herdr or cmux space, named as such.
		"workspaces on a tmux space":     {{"a.yaml", []byte("spaces:\n  x: {windows: [shell], multiplexer: herdr, workspaces: {a: {}}}\n")}},
		"workspaces without multiplexer": {{"a.yaml", []byte("spaces:\n  x: {app: cmux, workspaces: {a: {}}}\n")}},
		"multiplexer without workspaces": {{"a.yaml", []byte("spaces:\n  x: {app: cmux, multiplexer: cmux}\n")}},
		"unknown multiplexer":            {{"a.yaml", []byte("spaces:\n  x: {app: cmux, multiplexer: screen, workspaces: {a: {}}}\n")}},
		"session on cmux":                {{"a.yaml", []byte("spaces:\n  x: {app: cmux, multiplexer: cmux, session: work, workspaces: {a: {}}}\n")}},
		"select of no workspace":         {{"a.yaml", []byte("spaces:\n  x: {app: cmux, multiplexer: cmux, workspaces: {a: {}}, select: b}\n")}},
		"workspace name with space":      {{"a.yaml", []byte("spaces:\n  x: {app: cmux, multiplexer: cmux, workspaces: {'a b': {}}}\n")}},
		"workspace window twice":         {{"a.yaml", []byte("spaces:\n  x: {app: cmux, multiplexer: cmux, workspaces: {a: {windows: [w, w]}}}\n")}},
		"workspace pane and command":     {{"a.yaml", []byte("spaces:\n  x: {app: cmux, multiplexer: cmux, workspaces: {a: {windows: [{name: w, command: c, panes: [{}, {}]}]}}}\n")}},
		"name with colon":                {{"a.yaml", []byte("spaces:\n  a:b: {windows: [shell]}\n")}},
		"negative space":                 {{"a.yaml", []byte("spaces:\n  x: {space: -1, windows: [shell]}\n")}},
		// Names end up in a yabai regex that yabairc evals, in tmux
		// targets and in a window title: letters, digits, - and _ only.
		"name with quote":    {{"a.yaml", []byte("spaces:\n  'a\"b': {windows: [shell]}\n")}},
		"name with subshell": {{"a.yaml", []byte("spaces:\n  '$(x)': {windows: [shell]}\n")}},
		"name with plus":     {{"a.yaml", []byte("spaces:\n  a+b: {windows: [shell]}\n")}},
		"name with space":    {{"a.yaml", []byte("spaces:\n  'a b': {windows: [shell]}\n")}},
		"window with quote":  {{"a.yaml", []byte("spaces:\n  x: {windows: ['a\"b']}\n")}},
		"window with space":  {{"a.yaml", []byte("spaces:\n  x: {windows: ['a b']}\n")}},
	}
	for name, srcs := range cases {
		if _, err := mergeSpaces(srcs); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if spaces, err := mergeSpaces([]source{{"a.yaml", []byte("spaces:\n  my_space-1: {windows: [w_1]}\n")}}); err != nil || len(spaces) != 1 || spaces[0].Name != "my_space-1" {
		t.Errorf("underscores and dashes are allowed: got %v, %v", spaces, err)
	}
	for _, empty := range []string{"", "# nothing\n", "spaces: {}\n"} {
		if spaces, err := mergeSpaces([]source{{"a.yaml", []byte(empty)}}); err != nil || len(spaces) != 0 {
			t.Errorf("%q: got %v, %v", empty, spaces, err)
		}
	}
}

func TestPathListResolve(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("REPOS", filepath.Join(root, "repos"))
	if err := os.MkdirAll(filepath.Join(root, "repos", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		list pathList
		want string
		ok   bool
	}{
		{pathList{"~/nope", "$REPOS/app"}, filepath.Join(root, "repos", "app"), true},
		{pathList{"~/repos/app"}, filepath.Join(root, "repos", "app"), true},
		{pathList{"~/file"}, "", false}, // a file, not a directory
		{pathList{"~/nope"}, "", false},
		{nil, "", false},
	}
	for _, c := range cases {
		got, ok := c.list.resolve()
		if got != c.want || ok != c.ok {
			t.Errorf("%v.resolve() = %q, %v; want %q, %v", c.list, got, ok, c.want, c.ok)
		}
	}
}

func TestConfigFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	dir := filepath.Join(root, "spaces")
	if err := os.MkdirAll(filepath.Join(dir, "spaces.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"spaces.d/b.yaml", "spaces.d/a.yaml", "spaces.yaml", "spaces.d/notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("spaces: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := configFiles()
	if err != nil {
		t.Fatal(err)
	}
	var rel []string
	for _, f := range files {
		rel = append(rel, strings.TrimPrefix(f, dir+string(filepath.Separator)))
	}
	if want := []string{"spaces.yaml", "spaces.d/a.yaml", "spaces.d/b.yaml"}; !reflect.DeepEqual(rel, want) {
		t.Errorf("configFiles() = %v, want %v", rel, want)
	}
	if spaces, err := loadSpaces(); err != nil || len(spaces) != 0 {
		t.Errorf("loadSpaces() = %v, %v", spaces, err)
	}
}

// The tool read ~/.config/tmux-spaces before it was called spaces; a
// machine still keeping its config there gets told, not an empty list.
func TestConfigMovedFromTmuxSpaces(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	old := filepath.Join(root, "tmux-spaces", "spaces.d")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "a.yaml"), []byte("spaces:\n  app: {windows: [shell]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadSpaces()
	if err == nil || !strings.Contains(err.Error(), "config moved: "+filepath.Join(root, "tmux-spaces")+" → "+filepath.Join(root, "spaces")+"; move the files") {
		t.Errorf("old config dir only: err = %v", err)
	}
	// An empty new directory beside it is the same situation.
	if err := os.MkdirAll(filepath.Join(root, "spaces", "spaces.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSpaces(); err == nil || !strings.Contains(err.Error(), "config moved") {
		t.Errorf("empty new dir beside the old one: err = %v", err)
	}
	// Once a file lives in the new place, the old directory is history.
	if err := os.WriteFile(filepath.Join(root, "spaces", "spaces.d", "a.yaml"), []byte("spaces:\n  app: {windows: [shell]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if spaces, err := loadSpaces(); err != nil || len(spaces) != 1 {
		t.Errorf("moved config: %v, %v", spaces, err)
	}
}

func TestYabaiRules(t *testing.T) {
	spaces := []Space{
		{Name: "eden", Space: 11},
		{Name: "unpinned"},
		{Name: "bf-2", Space: 4},
		{Name: "bf-1", Space: 3},
		{Name: "pr-reviews", Space: 9},
		{Name: "backstage", Space: 10, App: "Bardo Backstage (Beta)"},
		{Name: "cmux", Space: 8, App: "cmux"},
		{Name: "unpinned-app", App: "Slack"},
	}
	var out strings.Builder
	yabaiRules(spaces, &out)
	// An app rule keys on the name, escaped for the regex; the
	// backslashes are what yabairc's double-quoted eval hands yabai.
	want := `yabai -m rule --add app="^Ghostty$" title="^bf-1$" space=^3
yabai -m rule --add app="^Ghostty$" title="^bf-2$" space=^4
yabai -m rule --add app="^cmux$" space=^8
yabai -m rule --add app="^Ghostty$" title="^pr-reviews$" space=^9
yabai -m rule --add app="^Bardo Backstage \(Beta\)$" space=^10
yabai -m rule --add app="^Ghostty$" title="^eden$" space=^11
`
	if out.String() != want {
		t.Errorf("yabaiRules =\n%s\nwant\n%s", out.String(), want)
	}
}

func TestClaudeChip(t *testing.T) {
	if got := claudeChip(map[string]int{"blocked": 1, "working": 2, "done": 1, "idle": 3}); got != "1⚠ 2~ 1* 3" {
		t.Errorf("chip = %q", got)
	}
	if got := claudeChip(nil); got != "-" {
		t.Errorf("empty chip = %q", got)
	}
}
