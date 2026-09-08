package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLI builds the binary and checks the verbs that need no tmux or
// desktop: usage and exit codes, config path, yabai-rules from an XDG
// dir, key dispatch failures.
func TestCLI(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin", "spaces")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(root, "spaces", "spaces.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "spaces", "spaces.d", "a.yaml"), []byte("spaces:\n  app: {key: a, space: 3, windows: [shell]}\n  herdr: {key: h, space: 7, command: herdr --session work}\n  cmux: {key: c, space: 8, app: cmux}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, int) {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+root)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}
	cases := []struct {
		args []string
		code int
		want string
	}{
		{nil, 64, "usage:"},
		{[]string{"bogus"}, 64, "unknown command"},
		{[]string{"open"}, 64, "expected a space name"},
		{[]string{"--version"}, 0, "spaces"},
		{[]string{"config", "path"}, 0, filepath.Join(root, "spaces")},
		{[]string{"yabai-rules"}, 0, `yabai -m rule --add app="^Ghostty$" title="^app$" space=^3`},
		{[]string{"yabai-rules"}, 0, `yabai -m rule --add app="^Ghostty$" title="^herdr$" space=^7`},
		{[]string{"yabai-rules"}, 0, `yabai -m rule --add app="^cmux$" space=^8`},
		{[]string{"key", "z"}, 1, `no space bound to key "z"`},
		{[]string{"open", "nope"}, 1, `no space named "nope"`},
	}
	for _, c := range cases {
		out, code := run(c.args...)
		if code != c.code || !strings.Contains(out, c.want) {
			t.Errorf("%v: exit %d, output %q; want exit %d containing %q", c.args, code, out, c.code, c.want)
		}
	}
	// A broken config fails every verb that reads it, with the file named.
	if err := os.WriteFile(filepath.Join(root, "spaces", "spaces.d", "b.yaml"), []byte("spaces:\n  app: {key: b, windows: [shell]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := run("list"); code != 1 || !strings.Contains(out, `space "app" is declared in both`) {
		t.Errorf("duplicate space: exit %d, %q", code, out)
	}
}
