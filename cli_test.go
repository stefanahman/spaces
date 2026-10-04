package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	if err := os.WriteFile(filepath.Join(root, "spaces", "spaces.d", "a.yaml"), []byte("spaces:\n  app: {key: a, space: 3, windows: [shell]}\n  herdr: {key: h, space: 7, command: herdr --session work, multiplexer: herdr, session: work, workspaces: {bf-1: {windows: [shell, {name: nvim, command: nvim}]}}, select: bf-1}\n  cmux: {key: c, space: 8, app: cmux}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The binary reaches the desktop by name: osascript for a
	// notification, yabai and open for windows. Stubs stand first on its
	// PATH and log each call, so `key z` sends its notification to the
	// log instead of the developer's screen, and nothing here touches
	// their windows.
	desk := filepath.Join(root, "desk")
	calls := filepath.Join(root, "desk.log")
	if err := os.MkdirAll(desk, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"osascript", "yabai", "open"} {
		stub := "#!/bin/sh\necho \"" + name + " $*\" >> " + calls + "\n"
		if err := os.WriteFile(filepath.Join(desk, name), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) (string, int) {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+root, "XDG_STATE_HOME="+root, "PATH="+desk+string(os.PathListSeparator)+os.Getenv("PATH"))
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
		{[]string{"use"}, 0, "* 1) tmux\n  2) herdr\n"}, // stdin is no terminal here: the list, nothing asked
		{[]string{"use", "screen"}, 64, "expected tmux, herdr or cmux"},
		{[]string{"open", "nope"}, 1, `no space named "nope"`},
	}
	for _, c := range cases {
		out, code := run(c.args...)
		if code != c.code || !strings.Contains(out, c.want) {
			t.Errorf("%v: exit %d, output %q; want exit %d containing %q", c.args, code, out, c.code, c.want)
		}
	}
	// On macOS the unbound key is said as a notification — once, to the
	// stub — and no verb above went near a window.
	logged, _ := os.ReadFile(calls)
	if runtime.GOOS == "darwin" {
		if got := strings.Count(string(logged), "osascript "); got != 1 || !strings.Contains(string(logged), `no space bound to key`) {
			t.Errorf("notifications: %q, want one for the unbound key", logged)
		}
	}
	if strings.Contains(string(logged), "yabai ") || strings.Contains(string(logged), "open ") {
		t.Errorf("a verb touched the desktop: %q", logged)
	}
	// A broken config fails every verb that reads it, with the file named.
	if err := os.WriteFile(filepath.Join(root, "spaces", "spaces.d", "b.yaml"), []byte("spaces:\n  app: {key: b, windows: [shell]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := run("list"); code != 1 || !strings.Contains(out, `space "app" is declared in both`) {
		t.Errorf("duplicate space: exit %d, %q", code, out)
	}
}
