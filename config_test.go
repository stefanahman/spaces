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
`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, sp := range spaces {
		names = append(names, sp.Name)
	}
	if want := []string{"bf-1", "eden", "pr-reviews", "unpinned"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	bf, _ := find(spaces, "bf-1")
	if bf.Key != "1" || bf.Space != 3 || bf.file != "a.yaml" {
		t.Errorf("bf-1 = %+v", bf)
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
		"window twice":    {{"a.yaml", []byte("spaces:\n  x: {windows: [shell, shell]}\n")}},
		"nameless window": {{"a.yaml", []byte("spaces:\n  x: {windows: [{command: nvim}]}\n")}},
		"bad split":       {{"a.yaml", []byte("spaces:\n  x: {windows: [{name: w, split: diagonal}]}\n")}},
		"command+panes":   {{"a.yaml", []byte("spaces:\n  x: {windows: [{name: w, command: nvim, panes: [{}, {}]}]}\n")}},
		"select unknown":  {{"a.yaml", []byte("spaces:\n  x: {windows: [shell], select: nvim}\n")}},
		"name with colon": {{"a.yaml", []byte("spaces:\n  a:b: {windows: [shell]}\n")}},
		"negative space":  {{"a.yaml", []byte("spaces:\n  x: {space: -1, windows: [shell]}\n")}},
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
	dir := filepath.Join(root, "tmux-spaces")
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

func TestYabaiRules(t *testing.T) {
	spaces := []Space{
		{Name: "eden", Space: 11},
		{Name: "unpinned"},
		{Name: "bf-2", Space: 4},
		{Name: "bf-1", Space: 3},
		{Name: "pr-reviews", Space: 9},
	}
	var out strings.Builder
	yabaiRules(spaces, &out)
	want := `yabai -m rule --add app="^Ghostty$" title="^bf-1$" space=^3
yabai -m rule --add app="^Ghostty$" title="^bf-2$" space=^4
yabai -m rule --add app="^Ghostty$" title="^pr-reviews$" space=^9
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
