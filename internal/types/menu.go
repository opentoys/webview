package types

// Menu describes a top-level menu (e.g. "File", "Edit").
type Menu struct {
	Label string
	Items []MenuItem
}

// MenuItem is a single entry inside a Menu.
// If Separator is true the other fields are ignored.
type MenuItem struct {
	Label     string
	Shortcut  string // e.g. "Ctrl+Z", "Cmd+Shift+Z"; empty = none
	Action    func()
	Separator bool
}
