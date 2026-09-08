// Keys: a space's own, and those of the workspaces inside a herdr or
// cmux space. One key may be bound once per multiplexer — bf-1 as a
// tmux space, as a herdr workspace and as a cmux workspace — and the
// active multiplexer decides which one a press opens.
package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// keyHolder is what a key opens: a space, or a workspace of one.
type keyHolder struct {
	space     Space
	workspace string // "" for the space itself
	realm     string // the multiplexer the binding lives in; "" for a plain command or app space
}

func (h keyHolder) String() string {
	if h.workspace != "" {
		return fmt.Sprintf("workspace %q of %q", h.workspace, h.space.Name)
	}
	return fmt.Sprintf("space %q", h.space.Name)
}

// realm is the multiplexer a space's own key is bound in: tmux for a
// session space, the space's multiplexer when it builds workspaces,
// none for a plain command or app space.
func (sp Space) realm() string {
	if len(sp.Windows) > 0 {
		return "tmux"
	}
	return sp.Multiplexer
}

// keyHolders lists what is bound to key, multiplexers in their order,
// plain spaces last.
func keyHolders(spaces []Space, key string) []keyHolder {
	var holders []keyHolder
	for _, sp := range spaces {
		if sp.Key == key {
			holders = append(holders, keyHolder{space: sp, realm: sp.realm()})
		}
		for _, name := range sp.workspaceNames() {
			if sp.Workspaces[name].Key == key {
				holders = append(holders, keyHolder{space: sp, workspace: name, realm: sp.Multiplexer})
			}
		}
	}
	rank := func(realm string) int {
		for i, m := range multiplexers {
			if m == realm {
				return i
			}
		}
		return len(multiplexers)
	}
	sort.SliceStable(holders, func(i, j int) bool { return rank(holders[i].realm) < rank(holders[j].realm) })
	return holders
}

// resolveKey picks the holder a press opens: the one in the active
// multiplexer, else the only one there is.
func resolveKey(spaces []Space, key, active string) (keyHolder, error) {
	holders := keyHolders(spaces, key)
	for _, h := range holders {
		if h.realm == active {
			return h, nil
		}
	}
	switch len(holders) {
	case 0:
		return keyHolder{}, fmt.Errorf("no space bound to key %q", key)
	case 1:
		return holders[0], nil
	}
	var realms []string
	for _, h := range holders {
		realm := h.realm
		if realm == "" {
			realm = "a plain space"
		}
		realms = append(realms, realm)
	}
	return keyHolder{}, fmt.Errorf("key %q is bound in %s; `spaces use` one of them", key, strings.Join(realms, " and "))
}

// keyCmd is `spaces key <k>`: open what the key resolves to — a space,
// or a space on one of its workspaces — then the space's then.
func keyCmd(d desktop, spaces []Space, key string, out io.Writer) error {
	active, err := activeMultiplexer()
	if err != nil {
		return err
	}
	h, err := resolveKey(spaces, key, active)
	if err != nil {
		return err
	}
	return openSpace(d, h.space, true, h.workspace, out)
}
