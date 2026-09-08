// The environment of what spaces starts.
package main

import (
	"os"

	"github.com/stefanahman/mux"
)

// launchEnv is the environment for everything spaces starts — the
// tmux server, a Ghostty window and the program in it, an application
// (`open` hands the caller's whole environment to what it launches)
// and a `then` command: the caller's, less what a multiplexer must
// not inherit (mux.CleanEnv): tmux's, cmux's and herdr's own
// variables, and Claude Code's session markers. A cmux launched with
// TMUX set hands its surface id to tmux before every command and never
// engages its Claude Code hooks; a server or app started from inside a
// Claude Code session runs every agent as a child session, which
// saves no transcript. So a space opens the same from a hotkey, a
// plain shell, a tmux pane or an agent's shell.
func launchEnv() []string { return mux.CleanEnv(os.Environ()) }
