//go:build windows

package windows

// Native Win32 menu construction.

import (
	"runtime"
	"unsafe"
)

func (p *Platform) setupMainMenu() {
	editMenu, _, _ := pCreatePopupMenu.Call()
	if editMenu == 0 {
		return
	}

	appendEditItem := func(label string, id uintptr) {
		putf16 := utf16PtrFromStr(label)
		pAppendMenuW.Call(editMenu, MF_STRING, id, uintptr(unsafe.Pointer(putf16)))
		runtime.KeepAlive(putf16)
	}

	appendEditItem("Undo\tCtrl+Z", cmdUndo)
	appendEditItem("Redo\tCtrl+Y", cmdRedo)
	pAppendMenuW.Call(editMenu, MF_SEPARATOR, 0, 0)
	appendEditItem("Cut\tCtrl+X", cmdCut)
	appendEditItem("Copy\tCtrl+C", cmdCopy)
	appendEditItem("Paste\tCtrl+V", cmdPaste)
	pAppendMenuW.Call(editMenu, MF_SEPARATOR, 0, 0)
	appendEditItem("Select All\tCtrl+A", cmdSelectAll)

	menuBar, _, _ := pCreateMenu.Call()
	if menuBar == 0 {
		return
	}

	editLabel := utf16PtrFromStr("Edit")
	pAppendMenuW.Call(menuBar, MF_POPUP, editMenu, uintptr(unsafe.Pointer(editLabel)))
	runtime.KeepAlive(editLabel)
	pSetMenu.Call(p.hwnd, menuBar)
}

// applyMenus builds and installs a Win32 menu bar from the given Menu slice.
func (p *Platform) applyMenus(menus []Menu) {
	p.menuCallbacks = make(map[uintptr]func())
	p.nextMenuCmdID = cmdCustomBase

	menuBar, _, _ := pCreateMenu.Call()
	if menuBar == 0 {
		return
	}

	for _, m := range menus {
		submenu, _, _ := pCreatePopupMenu.Call()
		if submenu == 0 {
			continue
		}
		for _, mi := range m.Items {
			if mi.Separator {
				pAppendMenuW.Call(submenu, MF_SEPARATOR, 0, 0)
				continue
			}
			label := mi.Label
			if mi.Shortcut != "" {
				label += "\t" + mi.Shortcut
			}
			cmdID := p.nextMenuCmdID
			p.nextMenuCmdID++
			if mi.Action != nil {
				p.menuCallbacks[cmdID] = mi.Action
			}
			putf16 := utf16PtrFromStr(label)
			pAppendMenuW.Call(submenu, MF_STRING, cmdID, uintptr(unsafe.Pointer(putf16)))
			runtime.KeepAlive(putf16)
		}
		topLabel := utf16PtrFromStr(m.Label)
		pAppendMenuW.Call(menuBar, MF_POPUP, submenu, uintptr(unsafe.Pointer(topLabel)))
		runtime.KeepAlive(topLabel)
	}

	pSetMenu.Call(p.hwnd, menuBar)
}
