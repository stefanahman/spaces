package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stefanahman/mux"
	"github.com/stefanahman/mux/muxtest"
)

// TestMain lets the test binary stand in for the cmux CLI (see
// muxtest.FakeCmuxMain).
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "cmux" {
		os.Exit(muxtest.FakeCmuxMain(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// workContext is the layout the eden config declares: shells and nvim
// tabs for bf-1, eden with trunk beside branches and an nvim tab, and
// pr-owl running.
func workContext(t *testing.T, muxKind string) (map[string]Workspace, map[string]string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]string{}
	for _, d := range []string{"bardo", "eden", "branches"} {
		dirs[d] = filepath.Join(root, d)
		if err := os.Mkdir(dirs[d], 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return map[string]Workspace{
		"bf-1": {Cwd: pathList{dirs["bardo"]}, Windows: []Window{{Name: "shell"}, {Name: "nvim", Command: "nvim"}}},
		"eden": {Cwd: pathList{dirs["eden"]}, Windows: []Window{
			{Name: "eden", Panes: []Pane{{}, {Cwd: pathList{dirs["branches"]}}}},
			{Name: "nvim", Command: "nvim", Cwd: pathList{dirs["branches"]}},
		}},
		"pr-owl": {Cwd: pathList{dirs["bardo"]}, Windows: []Window{{Name: "pr-owl", Command: "pr-owl --mux " + muxKind}}},
	}, dirs
}

// quick makes the waits short: the fakes answer at once, and a pane
// running something else is left alone without the grace period.
func quick(t *testing.T) {
	t.Helper()
	ping := pingTimeout
	pingTimeout = time.Second
	t.Cleanup(func() { pingTimeout = ping })
}

func TestRenderOnHerdr(t *testing.T) {
	quick(t)
	fake := muxtest.NewFakeHerdr(t)
	for _, v := range []string{"HERDR_ENV", "HERDR_WORKSPACE_ID", "HERDR_SESSION"} {
		t.Setenv(v, "")
	}
	d := mux.NewHerdr(fake.Socket())
	workspaces, dirs := workContext(t, "herdr")
	sp := Space{Name: "herdr", Command: argv{"herdr"}, Multiplexer: "herdr", Workspaces: workspaces, Select: "pr-owl"}
	var out, errOut strings.Builder
	if err := render(d, sp, "", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "herdr: 3 workspaces created") || errOut.Len() > 0 {
		t.Errorf("out %q, err %q", out.String(), errOut.String())
	}
	bf := fake.Workspace("bf-1")
	if bf == nil || bf.Cwd != dirs["bardo"] || !reflect.DeepEqual(bf.Tabs, []string{"1", "nvim"}) || len(bf.Panes) != 2 {
		t.Fatalf("bf-1 = %+v", bf)
	}
	if got := fake.Typed(bf.Panes[1]); !reflect.DeepEqual(got, []string{"nvim<enter>"}) {
		t.Errorf("bf-1 nvim tab typed %v", got)
	}
	if got := fake.Typed(bf.Panes[0]); len(got) != 0 {
		t.Errorf("bf-1 shell typed %v, want nothing", got)
	}
	eden := fake.Workspace("eden")
	if eden == nil || eden.Cwd != dirs["eden"] || !reflect.DeepEqual(eden.Tabs, []string{"1", "nvim"}) || len(eden.Panes) != 3 {
		t.Fatalf("eden = %+v", eden)
	}
	// The split lives in the first tab, in branches; the nvim tab in branches too.
	if fake.PaneCwd(eden.Panes[1]) != dirs["branches"] || fake.PaneCwd(eden.Panes[2]) != dirs["branches"] {
		t.Errorf("eden pane cwds: split %q, tab %q", fake.PaneCwd(eden.Panes[1]), fake.PaneCwd(eden.Panes[2]))
	}
	if got := fake.Typed(eden.Panes[2]); !reflect.DeepEqual(got, []string{"nvim<enter>"}) {
		t.Errorf("eden nvim tab typed %v", got)
	}
	owl := fake.Workspace("pr-owl")
	if got := fake.Typed(owl.Pane()); !reflect.DeepEqual(got, []string{"pr-owl --mux herdr<enter>"}) {
		t.Errorf("pr-owl typed %v", got)
	}
	if fake.Focused() != owl.ID {
		t.Errorf("focused %q, want pr-owl %q", fake.Focused(), owl.ID)
	}

	// The programs run now, as they would: a second render adds and
	// types nothing.
	fake.SetForeground(bf.Panes[1], "nvim")
	fake.SetForeground(eden.Panes[2], "nvim")
	fake.SetForeground(owl.Pane(), "pr-owl")
	typed := len(fake.Typed(bf.Panes[1])) + len(fake.Typed(eden.Panes[2])) + len(fake.Typed(owl.Pane()))
	out.Reset()
	if err := render(d, sp, "", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "herdr: 3 workspaces in place") || errOut.Len() > 0 {
		t.Errorf("second render: out %q, err %q", out.String(), errOut.String())
	}
	if again := len(fake.Typed(bf.Panes[1])) + len(fake.Typed(eden.Panes[2])) + len(fake.Typed(owl.Pane())); again != typed {
		t.Errorf("second render typed %d more lines", again-typed)
	}
	if bf := fake.Workspace("bf-1"); len(bf.Panes) != 2 || len(fake.Workspace("eden").Panes) != 3 {
		t.Errorf("second render added panes: bf-1 %v, eden %v", bf.Panes, fake.Workspace("eden").Panes)
	}

	// pr-owl exited and vim runs in its pane: left alone, and said so.
	fake.SetForeground(owl.Pane(), "vim")
	if err := render(d, sp, "", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "herdr: workspace pr-owl window pr-owl is running vim, not pr-owl; leaving it alone") {
		t.Errorf("stderr %q", errOut.String())
	}
	if got := fake.Typed(owl.Pane()); len(got) != 1 {
		t.Errorf("typed into a busy pane: %v", got)
	}
}

func TestRenderOnCmux(t *testing.T) {
	quick(t)
	fake := muxtest.InstallFakeCmux(t)
	fake.AddWorkspace("", "HOME")
	d := mux.Cmux{}
	workspaces, dirs := workContext(t, "cmux")
	sp := Space{Name: "cmux", App: "cmux", Multiplexer: "cmux", Workspaces: workspaces, Select: "pr-owl"}
	var out, errOut strings.Builder
	if err := render(d, sp, "", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cmux: 3 workspaces created") || errOut.Len() > 0 {
		t.Errorf("out %q, err %q", out.String(), errOut.String())
	}
	bf, _ := fake.Workspace("bf-1")
	if bf.Cwd != dirs["bardo"] || len(bf.Panes) != 1 || len(bf.Panes[0].Surfaces) != 2 || bf.Panes[0].Surfaces[1].Title != "nvim" {
		t.Fatalf("bf-1 = %+v", bf)
	}
	// A fresh surface is moved to its directory, then its command is
	// typed, as it is.
	nvimTab := bf.Panes[0].Surfaces[1].ID
	if got := fake.Typed(nvimTab); !reflect.DeepEqual(got, []string{"cd '" + dirs["bardo"] + "'", "<enter>", "nvim", "<enter>"}) {
		t.Errorf("bf-1 nvim tab typed %v", got)
	}
	eden, _ := fake.Workspace("eden")
	if len(eden.Panes) != 2 || len(eden.Panes[0].Surfaces) != 2 {
		t.Fatalf("eden = %+v", eden)
	}
	if got := fake.Typed(eden.Panes[1].Surfaces[0].ID); !reflect.DeepEqual(got, []string{"cd '" + dirs["branches"] + "'", "<enter>"}) {
		t.Errorf("eden split typed %v", got)
	}
	owl, _ := fake.Workspace("pr-owl")
	root := owl.Panes[0].Surfaces[0].ID
	if got := fake.Typed(root); !reflect.DeepEqual(got, []string{"pr-owl --mux cmux", "<enter>"}) {
		t.Errorf("pr-owl typed %v", got)
	}
	if fake.State().Selected != owl.ID {
		t.Errorf("selected %q, want pr-owl %q", fake.State().Selected, owl.ID)
	}

	// The programs run now — as cmux tells: ps on the tty of the
	// surface a workspace was created with, the title of a tab made
	// through the API.
	fake.SetTitle(nvimTab, "nvim")
	edenWS, _ := fake.Workspace("eden")
	fake.SetTitle(edenWS.Panes[0].Surfaces[1].ID, "cd /x/eden-private-branches && nvim")
	fake.SetForeground(owl.Panes[0].Surfaces[0].TTY, "-/bin/zsh", "/x/bin/pr-owl")
	typed := len(fake.Typed(nvimTab)) + len(fake.Typed(root))
	out.Reset()
	if err := render(d, sp, "", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cmux: 3 workspaces in place") || errOut.Len() > 0 {
		t.Errorf("second render: out %q, err %q", out.String(), errOut.String())
	}
	if again := len(fake.Typed(nvimTab)) + len(fake.Typed(root)); again != typed {
		t.Errorf("second render typed %d more lines", again-typed)
	}
	if bf, _ := fake.Workspace("bf-1"); len(bf.Panes[0].Surfaces) != 2 {
		t.Errorf("second render added surfaces: %+v", bf.Panes)
	}
	fake.SetForeground(owl.Panes[0].Surfaces[0].TTY, "-/bin/zsh", "vim")
	if err := render(d, sp, "", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "cmux: workspace pr-owl window pr-owl is running vim, not pr-owl; leaving it alone") {
		t.Errorf("stderr %q", errOut.String())
	}
}

func TestRenderWaitsForTheMultiplexer(t *testing.T) {
	quick(t)
	pingTimeout = 300 * time.Millisecond
	sp := Space{Name: "herdr", Command: argv{"herdr"}, Multiplexer: "herdr", Workspaces: map[string]Workspace{"x": {}}}
	err := render(mux.NewHerdr("/nonexistent/herdr.sock"), sp, "", &strings.Builder{}, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "herdr: not answering after 300ms") {
		t.Errorf("got %v", err)
	}
}

func TestRenderReportsAMissingCwd(t *testing.T) {
	quick(t)
	fake := muxtest.NewFakeHerdr(t)
	sp := Space{Name: "herdr", Command: argv{"herdr"}, Multiplexer: "herdr", Workspaces: map[string]Workspace{"x": {Cwd: pathList{"/nonexistent/one"}}}}
	err := render(mux.NewHerdr(fake.Socket()), sp, "", &strings.Builder{}, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "herdr: workspace x: none of cwd [/nonexistent/one] exists") {
		t.Errorf("got %v", err)
	}
	if fake.Workspace("x") != nil {
		t.Error("workspace was created despite the error")
	}
}

func TestProgramOf(t *testing.T) {
	for command, want := range map[string]string{"nvim": "nvim", "pr-owl --mux herdr": "pr-owl", "PR_OWL_CONFIG=~/x.yaml /usr/local/bin/pr-owl": "pr-owl", "$HOME/.eden/bin/claude -c": "claude"} {
		if got := programOf(command); got != want {
			t.Errorf("programOf(%q) = %q, want %q", command, got, want)
		}
	}
}

// open builds the workspaces once the window is up, for an app space
// and for a command space alike.
func TestOpenBuildsTheWorkspaces(t *testing.T) {
	quick(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fake := muxtest.NewFakeHerdr(t)
	for _, v := range []string{"HERDR_ENV", "HERDR_WORKSPACE_ID", "HERDR_SESSION"} {
		t.Setenv(v, "")
	}
	orig := newDriver
	newDriver = func(string, string) mux.Driver { return mux.NewHerdr(fake.Socket()) }
	t.Cleanup(func() { newDriver = orig })
	workspaces, _ := workContext(t, "herdr")

	sp := Space{Name: "cmux", Space: 8, App: "cmux", Multiplexer: "herdr", Workspaces: workspaces, Select: "pr-owl"}
	d := newFakeDesktop()
	var out strings.Builder
	if err := open(d, sp, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cmux: app launched\ncmux: 3 workspaces created") {
		t.Errorf("output: %q", out.String())
	}
	if fake.Workspace("pr-owl") == nil || fake.Focused() != fake.Workspace("pr-owl").ID {
		t.Error("the workspaces were not built and selected")
	}

	sp = Space{Name: "herdr", Space: 7, Command: argv{"sleep", "30"}, Multiplexer: "herdr", Workspaces: map[string]Workspace{"bf-2": workspaces["bf-1"]}}
	out.Reset()
	if err := open(d, sp, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "herdr: window spawned\nherdr: 1 workspaces created") || fake.Workspace("bf-2") == nil {
		t.Errorf("output: %q", out.String())
	}
}

func TestListCountsTheWorkspaces(t *testing.T) {
	quick(t)
	fake := muxtest.NewFakeHerdr(t)
	for _, v := range []string{"HERDR_ENV", "HERDR_WORKSPACE_ID", "HERDR_SESSION"} {
		t.Setenv(v, "")
	}
	orig := newDriver
	newDriver = func(_, session string) mux.Driver {
		if session == "gone" {
			return mux.NewHerdr("/nonexistent/herdr.sock")
		}
		return mux.NewHerdr(fake.Socket())
	}
	t.Cleanup(func() { newDriver = orig })
	workspaces, _ := workContext(t, "herdr")
	drv := mux.NewHerdr(fake.Socket())
	if _, err := drv.Create("bf-1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := drv.Create("scratch", ""); err != nil { // the user's own, not counted
		t.Fatal(err)
	}
	d := newFakeDesktop()
	d.apps["cmux"] = true
	spaces := []Space{
		{Name: "cmux", Key: "c", Space: 8, App: "cmux", Multiplexer: "herdr", Workspaces: workspaces},
		{Name: "gone", Key: "g", Space: 9, App: "gone", Multiplexer: "herdr", Session: "gone", Workspaces: workspaces},
	}
	var out strings.Builder
	if err := list(d, spaces, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cmux  c    8      window open, 1/3 workspaces  -", "gone  g    9      -, ?/3 workspaces            -"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list lacks %q:\n%s", want, out.String())
		}
	}
}

// TestRenderGroupsFreshWorkspaces: a workspace this run creates joins
// its group with the declared style; one that was already there is
// left where the user put it, and a workspace with no group is never
// grouped. Under herdr and tmux the same config groups nothing.
func TestRenderGroupsFreshWorkspaces(t *testing.T) {
	quick(t)
	fake := muxtest.InstallFakeCmux(t)
	fake.AddWorkspace("", "HOME")
	// A workspace that exists before this run, ungrouped: the user
	// dragged it out, and an open must not put it back.
	fake.AddWorkspace("prs", "WS-PRS")
	d := mux.Cmux{}
	dir := t.TempDir()
	sp := Space{Name: "cmux", App: "cmux", Multiplexer: "cmux", Workspaces: map[string]Workspace{
		"eden":  {Cwd: pathList{dir}, Group: "Tooling", style: Group{Color: "#8fa1b3", Icon: "wrench.and.screwdriver"}},
		"prs":   {Cwd: pathList{dir}, Group: "Tooling", style: Group{Color: "#8fa1b3", Icon: "wrench.and.screwdriver"}},
		"bf-1":  {Cwd: pathList{dir}, Group: "Features"},
		"loose": {Cwd: pathList{dir}},
	}}
	var out, errOut strings.Builder
	if err := render(d, sp, "", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	tooling, ok := fake.Group("Tooling")
	if !ok {
		t.Fatal("no Tooling group")
	}
	eden, _ := fake.Workspace("eden")
	if !reflect.DeepEqual(tooling.MemberIDs, []string{eden.ID}) {
		t.Errorf("Tooling holds %v, want only the fresh eden %q — prs was already there", tooling.MemberIDs, eden.ID)
	}
	if tooling.AnchorID != eden.ID {
		t.Errorf("Tooling is anchored on %q, want %q", tooling.AnchorID, eden.ID)
	}
	if tooling.Color != "#8fa1b3" || tooling.Icon != "wrench.and.screwdriver" {
		t.Errorf("Tooling style = %q %q", tooling.Color, tooling.Icon)
	}
	// A group with no declaration is made without a style.
	features, ok := fake.Group("Features")
	if !ok {
		t.Fatal("no Features group")
	}
	if features.Color != "" || features.Icon != "" {
		t.Errorf("Features style = %q %q, want none", features.Color, features.Icon)
	}
	if len(fake.Groups()) != 2 {
		t.Errorf("groups = %+v, want Tooling and Features only — a workspace with no group joins none", fake.Groups())
	}

	// A second run creates nothing, so it groups nothing.
	before := len(fake.Calls())
	out.Reset()
	if err := render(d, sp, "", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	for _, call := range fake.Calls()[before:] {
		if strings.HasPrefix(call, "workspace-group create") || strings.HasPrefix(call, "workspace-group add") {
			t.Errorf("a second run wrote to the groups: %q", call)
		}
	}
}

// TestRenderGroupsAreCmuxOnly: the same config under herdr issues no
// grouping call — herdr has none, and mux.Group is a no-op there.
func TestRenderGroupsAreCmuxOnly(t *testing.T) {
	quick(t)
	fake := muxtest.NewFakeHerdr(t)
	dir := t.TempDir()
	sp := Space{Name: "herdr", Command: argv{"herdr"}, Multiplexer: "herdr", Workspaces: map[string]Workspace{
		"eden": {Cwd: pathList{dir}, Group: "Tooling", style: Group{Color: "#8fa1b3"}},
	}}
	var out, errOut strings.Builder
	if err := render(mux.NewHerdr(fake.Socket()), sp, "", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1 workspaces created") {
		t.Errorf("out %q", out.String())
	}
}
