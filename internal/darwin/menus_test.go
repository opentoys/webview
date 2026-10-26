//go:build darwin

package darwin

import "testing"

func TestMenuRoleAction(t *testing.T) {
	tests := []struct {
		role MenuItemRole
		want string
	}{
		{MenuItemRoleUndo, "undo:"},
		{MenuItemRoleRedo, "redo:"},
		{MenuItemRoleCut, "cut:"},
		{MenuItemRoleCopy, "copy:"},
		{MenuItemRolePaste, "paste:"},
		{MenuItemRoleSelectAll, "selectAll:"},
		{0, ""},
	}
	for _, tt := range tests {
		if got := menuRoleAction(tt.role); got != tt.want {
			t.Errorf("menuRoleAction(%d) = %q, want %q", tt.role, got, tt.want)
		}
	}
}
