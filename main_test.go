package main

import "testing"

func TestToolPath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// Karabiner's shell_command PATH: both tool dirs appended, in order.
		{"/usr/bin:/bin:/usr/sbin:/sbin", "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin"},
		// A user PATH that already has one of them keeps its order.
		{"/opt/homebrew/bin:/usr/bin", "/opt/homebrew/bin:/usr/bin:/usr/local/bin"},
		// Nothing to add.
		{"/usr/local/bin:/opt/homebrew/bin:/usr/bin", "/usr/local/bin:/opt/homebrew/bin:/usr/bin"},
		// No PATH at all: no leading empty element (that would be the cwd).
		{"", "/opt/homebrew/bin:/usr/local/bin"},
	} {
		if got := toolPath(tc.in); got != tc.want {
			t.Errorf("toolPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
