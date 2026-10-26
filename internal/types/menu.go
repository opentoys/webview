package types

// Menu describes a top-level menu (e.g. "File", "Edit").
type Menu struct {
	Label string
	Items []MenuItem
}

// MenuItemRole identifies a standard editing command. Platforms that support a
// native responder chain (notably macOS) dispatch these commands directly to
// the focused control instead of emulating them in page JavaScript.
type MenuItemRole uint8

const (
	MenuItemRoleNone MenuItemRole = iota
	MenuItemRoleUndo
	MenuItemRoleRedo
	MenuItemRoleCut
	MenuItemRoleCopy
	MenuItemRolePaste
	MenuItemRoleSelectAll
)

// MenuItem is a single entry inside a Menu.
// If Separator is true the other fields are ignored.
type MenuItem struct {
	Label     string
	Shortcut  string // e.g. "Ctrl+Z", "Cmd+Shift+Z"; empty = none
	Action    func()
	Role      MenuItemRole
	Separator bool
}
