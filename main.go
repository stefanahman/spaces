// tmux-spaces — one config for tmux sessions on desktop spaces.
//
// A space is one window that the window manager pins to a desktop
// space, reachable by a key: a terminal showing a tmux session with a
// fixed window/pane layout, a terminal running one program directly,
// or an application's window. The config declares all of it; the tool
// creates what is missing and focuses what exists.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
)

// version is set by the release build (-ldflags "-X main.version=…");
// `go install …@vX.Y.Z` builds report the module version instead.
var version = ""

func versionString() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

const usage = `usage: tmux-spaces open <name>    bring the space up (its session or program in a terminal window, or its application), focus it, run its then command
       tmux-spaces focus <name>   same, without the then command
       tmux-spaces key <k>        open the space bound to key k
       tmux-spaces list           every space with its session (or window) and Claude state
       tmux-spaces yabai-rules    the yabai rules that pin the windows to their spaces (eval in yabairc)
       tmux-spaces check          validate the config and this machine
       tmux-spaces config path
       tmux-spaces --version`

// usageError is a bad invocation: printed with the usage text, exit 64.
type usageError string

func (e usageError) Error() string { return string(e) }

// toolDirs is where tmux and yabai are installed when they are not in
// the base system: Homebrew on Apple silicon, and Homebrew on Intel or
// a manual install.
var toolDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}

// toolPath returns path with toolDirs appended when they are missing.
// Hotkey daemons run their commands with the base PATH — Karabiner's
// shell_command gets /usr/bin:/bin:/usr/sbin:/sbin — and being run from
// one is what tmux-spaces is for, so it must find tmux and yabai
// without a login shell in between. Appended, not prepended: the
// user's own PATH still wins.
func toolPath(path string) string {
	dirs := filepath.SplitList(path)
	for _, dir := range toolDirs {
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

func main() {
	os.Setenv("PATH", toolPath(os.Getenv("PATH")))
	args := os.Args[1:]
	if len(args) == 0 {
		exitOn(usageError("a command is required"))
	}
	switch args[0] {
	case "--version", "version":
		fmt.Println("tmux-spaces", versionString())
		return
	case "--help", "-h", "help":
		fmt.Println(usage)
		return
	case "config":
		if len(args) != 2 || args[1] != "path" {
			exitOn(usageError("config: expected `path`"))
		}
		exitOn(configPathCmd(os.Stdout))
		return
	}

	spaces, err := loadSpaces()
	exitOn(err)
	switch args[0] {
	case "open", "focus":
		if len(args) != 2 {
			exitOn(usageError(args[0] + ": expected a space name"))
		}
		sp, ok := find(spaces, args[1])
		if !ok {
			exitOn(fmt.Errorf("no space named %q (see `tmux-spaces list`)", args[1]))
		}
		d, err := newDesktop()
		exitOn(err)
		exitOn(open(d, sp, args[0] == "open", os.Stdout))
	case "key":
		if len(args) != 2 {
			exitOn(usageError("key: expected a key"))
		}
		sp, ok := findKey(spaces, args[1])
		if !ok {
			exitOn(fmt.Errorf("no space bound to key %q", args[1]))
		}
		d, err := newDesktop()
		exitOn(err)
		exitOn(open(d, sp, true, os.Stdout))
	case "list":
		d, _ := newDesktop() // nil off macOS: list still works, command spaces show "?"
		exitOn(list(d, spaces, os.Stdout))
	case "yabai-rules":
		yabaiRules(spaces, os.Stdout)
	case "check":
		d, err := newDesktop()
		exitOn(err)
		exitOn(check(d, spaces, os.Stdout))
	default:
		exitOn(usageError("unknown command " + args[0]))
	}
}

// exitOn prints err and exits: 64 for a usage error (with the usage
// text), 1 otherwise.
func exitOn(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "tmux-spaces: %v\n", err)
	var ue usageError
	if errors.As(err, &ue) {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(64)
	}
	os.Exit(1)
}
