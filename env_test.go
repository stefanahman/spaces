package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The launch environment drops what a multiplexer must not inherit and
// keeps the rest.
func TestLaunchEnv(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	t.Setenv("TMUX_PANE", "%1")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("CMUX_SURFACE_ID", "s1")
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("SPACES_TEST_KEPT", "yes")
	env := launchEnv()
	for _, dropped := range []string{"TMUX=", "TMUX_PANE=", "CLAUDECODE=", "CLAUDE_CODE_CHILD_SESSION=", "CMUX_SURFACE_ID=", "HERDR_ENV="} {
		if slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, dropped) }) {
			t.Errorf("launch environment carries %s", dropped)
		}
	}
	for _, kept := range []string{"SPACES_TEST_KEPT=yes", "PATH=" + os.Getenv("PATH")} {
		if !slices.Contains(env, kept) {
			t.Errorf("launch environment lacks %s", kept)
		}
	}
}

// A command or app space's `then` runs in the launch environment.
func TestThenRunsInLaunchEnv(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("SPACES_TEST_KEPT", "yes")
	file := filepath.Join(t.TempDir(), "env")
	sp := Space{Name: "x", Command: argv{"sh"}, Then: "env > '" + file + ".part' && mv '" + file + ".part' '" + file + "'"}
	if err := runThenCommand(sp); err != nil {
		t.Fatal(err)
	}
	// then runs in the background, released: wait for what it wrote.
	var got string
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		b, err := os.ReadFile(file)
		if err == nil {
			got = string(b)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("then wrote nothing in 5s: %v", err)
		}
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if !slices.Contains(lines, "SPACES_TEST_KEPT=yes") {
		t.Errorf("then's environment lacks SPACES_TEST_KEPT=yes:\n%s", got)
	}
	for _, dropped := range []string{"TMUX=", "CLAUDECODE="} {
		if slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, dropped) }) {
			t.Errorf("then's environment carries %s:\n%s", dropped, got)
		}
	}
}
