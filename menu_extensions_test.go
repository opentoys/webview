package webview

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"reflect"
	"runtime"
	"testing"

	"github.com/opentoys/webview/internal/chrome"
	"github.com/opentoys/webview/internal/types"
)

type menuPlatformStub struct {
	dialogPlatformStub
	context types.ContextMenuConfig
	tray    SystrayOptions
	calls   int
	err     error
}

func (p *menuPlatformStub) ApplyContextMenu(opts types.ContextMenuConfig) error {
	if p.err != nil {
		return p.err
	}
	p.context = opts
	p.calls++
	return nil
}
func (p *menuPlatformStub) SetSystray(opts SystrayOptions) error { p.tray = opts; return p.err }
func (p *menuPlatformStub) RemoveSystray() error                 { p.tray = SystrayOptions{}; return p.err }
func menuTestIcon(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestContextMenuConfiguration(t *testing.T) {
	p := &menuPlatformStub{}
	w := &W{p: p}
	items := []MenuItem{{ID: "first", Label: "One"}, {ID: "sep", Separator: true}}
	if err := w.SetContextMenu(items); err != nil {
		t.Fatal(err)
	}
	items[0].Label = "mutated"

	if p.context.Items[0].Label != "One" || !p.context.ReplaceDefault {
		t.Fatal("configuration aliases caller data")
	}
	if err := w.AddContextMenuItem(MenuItem{ID: "last", Label: "Last"}); err != nil {
		t.Fatal(err)
	}
	if err := w.AddContextMenuItem(MenuItem{ID: "first"}); err == nil {
		t.Fatal("duplicate ID accepted")
	}
	if err := w.RemoveContextMenuItem("first"); err != nil {
		t.Fatal(err)
	}
	if ids := []string{p.context.Items[0].ID, p.context.Items[1].ID}; !reflect.DeepEqual(ids, []string{"sep", "last"}) {
		t.Fatal(ids)
	}
	calls := p.calls
	if err := w.RemoveContextMenuItem("missing"); err != nil || p.calls != calls {
		t.Fatal("missing removal changed configuration")
	}
	if err := w.SetContextMenu(nil); err != nil {
		t.Fatal(err)
	}
	if len(p.context.Items) != 0 || len(p.context.RemoveDefault) != 0 || !p.context.ReplaceDefault {
		t.Fatal("empty replacement retained entries")
	}
}
func TestMenuValidationAndErrors(t *testing.T) {
	p := &menuPlatformStub{}
	w := &W{p: p}
	for _, items := range [][]MenuItem{{{Label: "empty"}}, {{Separator: true}}, {{ID: "x"}, {ID: "x"}}} {
		if err := w.SetContextMenu(items); err == nil {
			t.Fatalf("accepted %#v", items)
		}
		if err := w.SetSystray(SystrayOptions{Icon: menuTestIcon(t), Items: items}); err == nil {
			t.Fatalf("tray accepted %#v", items)
		}
	}
	if err := w.RemoveContextMenuItemRole(MenuItemRoleNone); err == nil {
		t.Fatal("invalid role accepted")
	}
	if err := w.SetSystray(SystrayOptions{Icon: []byte("bad PNG")}); err == nil {
		t.Fatal("invalid PNG accepted")
	}
	if err := w.SetContextMenu([]MenuItem{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	p.err = ErrMenuUnsupported
	if err := w.AddContextMenuItem(MenuItem{ID: "b"}); !errors.Is(err, ErrMenuUnsupported) {
		t.Fatal(err)
	}
	p.err = nil
	if err := w.AddContextMenuItem(MenuItem{ID: "b"}); err != nil {
		t.Fatal("failed update changed saved configuration", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.AddContextMenuItem(MenuItem{ID: "c"}); err == nil {
		t.Fatal("update after Close accepted")
	}
}
func TestSystraySnapshotsAndRemoval(t *testing.T) {
	p := &menuPlatformStub{}
	w := &W{p: p}
	opts := SystrayOptions{Icon: menuTestIcon(t), Title: "Visible title", Tooltip: "Tray", Items: []MenuItem{{ID: "quit", Label: "Quit"}}}
	if err := w.SetSystray(opts); err != nil {
		t.Fatal(err)
	}
	opts.Icon[0] = 0
	opts.Items[0].Label = "changed"
	if p.tray.Icon[0] != 137 || p.tray.Items[0].Label != "Quit" || p.tray.Title != "Visible title" {
		t.Fatal("tray config not copied")
	}
	for range 2 {
		if err := w.RemoveSystray(); err != nil {
			t.Fatal(err)
		}
	}
}
func TestChromeMenuExtensionsUnsupported(t *testing.T) {
	w := &W{p: chrome.New(chrome.Options{})}
	errorsToCheck := []error{w.SetSystray(SystrayOptions{}), w.RemoveSystray(), w.SetContextMenu(nil), w.AddContextMenuItem(MenuItem{ID: "x"}), w.RemoveContextMenuItem("x"), w.RemoveContextMenuItemRole(MenuItemRoleCopy)}
	for _, err := range errorsToCheck {
		if !errors.Is(err, ErrMenuUnsupported) {
			t.Fatalf("got %v", err)
		}
	}
}

func TestContextMenuAppendAndRemoveRoles(t *testing.T) {
	p := &menuPlatformStub{}
	w := &W{p: p}
	if err := w.AddContextMenuItem(MenuItem{ID: "keep", Label: "Keep"}); err != nil {
		t.Fatal(err)
	}
	if p.context.ReplaceDefault {
		t.Fatal("initial append replaced defaults")
	}
	if err := w.AddContextMenuItem(MenuItem{ID: "copy", Role: MenuItemRoleCopy}); err != nil {
		t.Fatal(err)
	}
	if err := w.RemoveContextMenuItemRole(MenuItemRoleCopy, MenuItemRoleCut, MenuItemRoleCopy); err != nil {
		t.Fatal(err)
	}
	if p.context.ReplaceDefault || len(p.context.Items) != 1 || p.context.Items[0].ID != "keep" {
		t.Fatalf("%+v", p.context)
	}
	if !reflect.DeepEqual(p.context.RemoveDefault, []MenuItemRole{MenuItemRoleCopy, MenuItemRoleCut}) {
		t.Fatal(p.context.RemoveDefault)
	}
	calls := p.calls
	if err := w.RemoveContextMenuItemRole(MenuItemRolePaste, MenuItemRoleNone); err == nil {
		t.Fatal("invalid role accepted")
	}
	if p.calls != calls {
		t.Fatal("invalid role partially applied")
	}
	if err := w.RemoveContextMenuItemRole(MenuItemRoleCopy); err != nil {
		t.Fatal(err)
	}
	if len(p.context.RemoveDefault) != 2 {
		t.Fatal("role removal not idempotent")
	}
	if err := w.SetContextMenu([]MenuItem{{ID: "replacement", Role: MenuItemRoleCopy}}); err != nil {
		t.Fatal(err)
	}
	if !p.context.ReplaceDefault || len(p.context.RemoveDefault) != 0 {
		t.Fatal("replacement did not reset previous removals")
	}
	if err := w.RemoveContextMenuItemRole(MenuItemRoleCopy); err != nil {
		t.Fatal(err)
	}
	if len(p.context.Items) != 0 || !p.context.ReplaceDefault {
		t.Fatal("role removal changed replacement mode")
	}
	if err := w.AddContextMenuItem(MenuItem{ID: "new"}); err != nil {
		t.Fatal(err)
	}
	if !p.context.ReplaceDefault || len(p.context.Items) != 1 {
		t.Fatal("append restored defaults")
	}
	if err := w.RemoveContextMenuItem("new"); err != nil {
		t.Fatal(err)
	}
	if !p.context.ReplaceDefault || len(p.context.Items) != 0 {
		t.Fatal("removal restored defaults")
	}
}

func TestTitleOnlySystray(t *testing.T) {
	p := &menuPlatformStub{}
	w := &W{p: p}
	err := w.SetSystray(SystrayOptions{Title: "Text only"})
	if runtime.GOOS == "darwin" {
		if err != nil {
			t.Fatal(err)
		}
		if p.tray.Title != "Text only" || len(p.tray.Icon) != 0 {
			t.Fatal("title-only config not forwarded")
		}
	} else if !errors.Is(err, ErrMenuUnsupported) {
		t.Fatalf("title-only error=%v", err)
	}
	if err := w.SetSystray(SystrayOptions{}); err == nil {
		t.Fatal("empty tray accepted")
	}
	if err := w.SetSystray(SystrayOptions{Title: "Title", Icon: []byte("invalid PNG")}); err == nil {
		t.Fatal("nonempty invalid icon accepted")
	}
}
