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
	ID        string // Required for systray and context menu entries, including separators.
	Label     string
	Shortcut  string // e.g. "Ctrl+Z", "Cmd+Shift+Z"; empty = none
	Action    func()
	Role      MenuItemRole
	Separator bool
}

// SystrayOptions configures a native notification-area icon and its menu.
type SystrayOptions struct {
	Title   string // Visible tray text on macOS; ignored on unsupported platforms.
	Icon    []byte // PNG image; optional on macOS when Title is nonempty.
	Tooltip string
	Items   []MenuItem
}

// ContextMenuConfig is the internal snapshot sent to native menu backends.
type ContextMenuConfig struct {
	ReplaceDefault bool
	Items          []MenuItem
	RemoveDefault  []MenuItemRole
}
