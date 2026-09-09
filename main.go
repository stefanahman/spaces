// spaces — one config for your windows on desktop spaces: tmux
// sessions, programs, applications.
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

const usage = `usage: spaces open <name>    bring the space up (its session or program in a terminal window, or its application), focus it, build its workspaces, run its then command
       spaces focus <name>   same, without the then command
       spaces key <k>        open what key k is bound to: a space, or a herdr/cmux space on one of its workspaces; the active multiplexer decides when several are
       spaces list           every space with its session (or window) and Claude state
       spaces use [tmux|herdr|cmux]   pick, or set, the active multiplexer: where a key bound in several of them lands
       spaces yabai-rules    the yabai rules that pin the windows to their spaces (eval in yabairc)
       spaces check          validate the config and this machine
       spaces config path
       spaces --version`

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
// one is what spaces is for, so it must find tmux and yabai
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
		fmt.Println("spaces", versionString())
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

	spaces, groups, err := loadSpaces()
	exitOn(err)
	switch args[0] {
	case "open", "focus":
		if len(args) != 2 {
			exitOn(usageError(args[0] + ": expected a space name"))
		}
		sp, ok := find(spaces, args[1])
		if !ok {
			exitOn(fmt.Errorf("no space named %q (see `spaces list`)", args[1]))
		}
		d, err := newDesktop()
		exitOn(err)
		exitOn(open(d, sp, args[0] == "open", os.Stdout))
	case "key":
		if len(args) != 2 {
			exitOn(usageError("key: expected a key"))
		}
		exitOn(keyCmd(newDesktop, spaces, args[1], os.Stdout))
	case "list":
		d, _ := newDesktop() // nil off macOS: list still works, command spaces show "?"
		exitOn(list(d, spaces, os.Stdout))
	case "use":
		if len(args) > 2 {
			exitOn(usageError("use: expected at most one of tmux, herdr, cmux"))
		}
		arg := ""
		if len(args) == 2 {
			arg = args[1]
		}
		d, _ := newDesktop() // nil off macOS: the switch works, the notification is skipped
		exitOn(useCmd(d, spaces, arg, os.Stdin, os.Stdout))
	case "yabai-rules":
		yabaiRules(spaces, os.Stdout)
	case "check":
		d, err := newDesktop()
		exitOn(err)
		exitOn(check(d, spaces, groups, os.Stdout))
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
	fmt.Fprintf(os.Stderr, "spaces: %v\n", err)
	var ue usageError
	if errors.As(err, &ue) {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(64)
	}
	os.Exit(1)
}
