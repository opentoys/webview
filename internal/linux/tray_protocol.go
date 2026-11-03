//go:build linux

package linux

import (
	"github.com/opentoys/webview/internal/types"
	"sort"
)

func (t *linuxTray) property(iface, name string) uintptr {
	if iface == menuInterface {
		switch name {
		case "Version":
			return gio.NewUint(3)
		case "TextDirection":
			return gio.NewString("ltr")
		case "Status":
			return gio.NewString("normal")
		case "IconThemePath":
			return vStrings(nil)
		}
		return 0
	}
	if iface != trayInterface {
		return 0
	}
	switch name {
	case "Category":
		return gio.NewString("ApplicationStatus")
	case "Id":
		return gio.NewString("webview")
	case "Title":
		return gio.NewString(t.opts.Tooltip)
	case "Status":
		return gio.NewString("Active")
	case "IconName":
		return gio.NewString("")
	case "IconPixmap":
		return t.pixmap.variant()
	case "ToolTip":
		return vTuple(gio.NewString(""), t.pixmap.variant(), gio.NewString(t.opts.Tooltip), gio.NewString(""))
	case "ItemIsMenu":
		return vBool(t.opts.OnClick == nil)
	case "Menu":
		return gio.NewPath(trayMenuPath)
	}
	return 0
}

// A sorted dictionary makes layout and property results deterministic.
// Values are floating variants and are consumed even when filtered out.
func vProperties(properties map[string]uintptr, names []string) uintptr {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []uintptr{}
	for _, key := range keys {
		include := len(names) == 0
		for _, name := range names {
			if name == key {
				include = true
			}
		}
		if include {
			out = append(out, gio.NewEntry(gio.NewString(key), gio.NewVariant(properties[key])))
		} else {
			gio.Unref(gio.RefSink(properties[key]))
		}
	}
	return vArray("{sv}", out...)
}
func itemProperties(item types.MenuItem) map[string]uintptr {
	if item.Separator {
		return map[string]uintptr{"type": gio.NewString("separator")}
	}
	return map[string]uintptr{"label": gio.NewString(item.Label), "enabled": vBool(true), "visible": vBool(true)}
}
func (t *linuxTray) index(id int32) int {
	for i, v := range t.ids {
		if v == id {
			return i
		}
	}
	return -1
}
func (t *linuxTray) menuProperties(id int32, names []string) uintptr {
	if id == 0 {
		return vProperties(map[string]uintptr{"children-display": gio.NewString("submenu")}, names)
	}
	return vProperties(itemProperties(t.opts.Items[t.index(id)]), names)
}
func (t *linuxTray) layout(id, depth int32, names []string) uintptr {
	children := []uintptr{}
	if id == 0 && depth != 0 {
		for _, child := range t.ids {
			children = append(children, gio.NewVariant(t.layout(child, 0, names)))
		}
	}
	return vTuple(gio.NewInt(id), t.menuProperties(id, names), vArray("v", children...))
}
func (t *linuxTray) event(id int32, event string) (func(), bool) {
	i := t.index(id)
	if i < 0 {
		return nil, id == 0
	}
	if event == "clicked" {
		return t.opts.Items[i].Action, true
	}
	return nil, true
}
func (t *linuxTray) method(iface, method string, p uintptr) (uintptr, []func(), string) {
	unknown := "org.freedesktop.DBus.Error.UnknownMethod"
	if iface == trayInterface {
		switch method {
		case "Activate":
			return vTuple(), []func(){t.opts.OnClick}, ""
		case "ContextMenu", "SecondaryActivate", "Scroll":
			return vTuple(), nil, ""
		}
		return 0, nil, unknown
	}
	if iface != menuInterface {
		return 0, nil, unknown
	}
	switch method {
	case "GetLayout":
		id := vChildInt(p, 0)
		depth := vChildInt(p, 1)
		if id != 0 && t.index(id) < 0 {
			return 0, nil, invalidMenuItem
		}
		names := gio.Child(p, 2)
		defer gio.Unref(names)
		return vTuple(gio.NewUint(t.revision), t.layout(id, depth, vReadStrings(names))), nil, ""
	case "GetGroupProperties":
		ids := gio.Child(p, 0)
		defer gio.Unref(ids)
		names := gio.Child(p, 1)
		defer gio.Unref(names)
		requested := vReadInts(ids)
		// DBusMenu defines an empty list as all items, including the root.
		if len(requested) == 0 {
			requested = append([]int32{0}, t.ids...)
		}
		out := []uintptr{}
		for _, id := range requested {
			if id != 0 && t.index(id) < 0 {
				continue
			}
			out = append(out, vTuple(gio.NewInt(id), t.menuProperties(id, vReadStrings(names))))
		}
		return vTuple(vArray("(ia{sv})", out...)), nil, ""
	case "GetProperty":
		id := vChildInt(p, 0)
		name := vChildString(p, 1)
		if id != 0 && t.index(id) < 0 {
			return 0, nil, invalidMenuItem
		}
		var props map[string]uintptr
		if id == 0 {
			props = map[string]uintptr{"children-display": gio.NewString("submenu")}
		} else {
			props = itemProperties(t.opts.Items[t.index(id)])
		}
		value := props[name]
		for key, v := range props {
			if key != name {
				gio.Unref(gio.RefSink(v))
			}
		}
		if value == 0 {
			return 0, nil, "com.canonical.dbusmenu.Error.InvalidProperty"
		}
		return vTuple(gio.NewVariant(value)), nil, ""
	case "Event":
		fn, valid := t.event(vChildInt(p, 0), vChildString(p, 1))
		if !valid {
			return 0, nil, invalidMenuItem
		}
		return vTuple(), []func(){fn}, ""
	case "EventGroup":
		events := gio.Child(p, 0)
		defer gio.Unref(events)
		failed := []int32{}
		callbacks := []func(){}
		for i := uintptr(0); i < gio.Count(events); i++ {
			e := gio.Child(events, i)
			id := vChildInt(e, 0)
			fn, valid := t.event(id, vChildString(e, 1))
			gio.Unref(e)
			if !valid {
				failed = append(failed, id)
			} else if fn != nil {
				callbacks = append(callbacks, fn)
			}
		}
		return vTuple(vInts(failed)), callbacks, ""
	case "AboutToShow":
		id := vChildInt(p, 0)
		if id != 0 && t.index(id) < 0 {
			return 0, nil, invalidMenuItem
		}
		return vTuple(vBool(false)), nil, ""
	case "AboutToShowGroup":
		ids := gio.Child(p, 0)
		defer gio.Unref(ids)
		failed := []int32{}
		for _, id := range vReadInts(ids) {
			if id != 0 && t.index(id) < 0 {
				failed = append(failed, id)
			}
		}
		return vTuple(vInts(nil), vInts(failed)), nil, ""
	}
	return 0, nil, unknown
}

const trayXML = `
<node>
 <interface name="org.kde.StatusNotifierItem">
  <method name="ContextMenu"><arg type="i" direction="in"/><arg type="i" direction="in"/></method>
  <method name="Activate"><arg type="i" direction="in"/><arg type="i" direction="in"/></method>
  <method name="SecondaryActivate"><arg type="i" direction="in"/><arg type="i" direction="in"/></method>
  <method name="Scroll"><arg type="i" direction="in"/><arg type="s" direction="in"/></method>
  <property name="Category" type="s" access="read"/>
  <property name="Id" type="s" access="read"/>
  <property name="Title" type="s" access="read"/>
  <property name="Status" type="s" access="read"/>
  <property name="IconName" type="s" access="read"/>
  <property name="IconPixmap" type="a(iiay)" access="read"/>
  <property name="ToolTip" type="(sa(iiay)ss)" access="read"/>
  <property name="ItemIsMenu" type="b" access="read"/>
  <property name="Menu" type="o" access="read"/>
  <signal name="NewIcon"/><signal name="NewToolTip"/><signal name="NewTitle"/>
 </interface>
 <interface name="com.canonical.dbusmenu">
  <method name="GetLayout"><arg type="i" direction="in"/><arg type="i" direction="in"/><arg type="as" direction="in"/><arg type="u" direction="out"/><arg type="(ia{sv}av)" direction="out"/></method>
  <method name="GetGroupProperties"><arg type="ai" direction="in"/><arg type="as" direction="in"/><arg type="a(ia{sv})" direction="out"/></method>
  <method name="GetProperty"><arg type="i" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="out"/></method>
  <method name="Event"><arg type="i" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="in"/><arg type="u" direction="in"/></method>
  <method name="EventGroup"><arg type="a(isvu)" direction="in"/><arg type="ai" direction="out"/></method>
  <method name="AboutToShow"><arg type="i" direction="in"/><arg type="b" direction="out"/></method>
  <method name="AboutToShowGroup"><arg type="ai" direction="in"/><arg type="ai" direction="out"/><arg type="ai" direction="out"/></method>
  <property name="Version" type="u" access="read"/>
  <property name="TextDirection" type="s" access="read"/>
  <property name="Status" type="s" access="read"/>
  <property name="IconThemePath" type="as" access="read"/>
  <signal name="LayoutUpdated"><arg type="u"/><arg type="i"/></signal>
 </interface>
</node>
`
