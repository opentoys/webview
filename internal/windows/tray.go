//go:build windows

package windows

import (
	"bytes"
	"image/png"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/opentoys/webview/internal/types"
)

var shellTray = syscall.NewLazyDLL("shell32.dll").NewProc("Shell_NotifyIconW")
var trayGDI = syscall.NewLazyDLL("gdi32.dll")
var createTrayBitmap = trayGDI.NewProc("CreateBitmap")
var deleteTrayObject = trayGDI.NewProc("DeleteObject")
var createTrayIcon = user32.NewProc("CreateIconIndirect")
var destroyTrayIcon = user32.NewProc("DestroyIcon")
var destroyPopup = user32.NewProc("DestroyMenu")
var trackPopup = user32.NewProc("TrackPopupMenu")
var getCursorPosition = user32.NewProc("GetCursorPos")
var registerTrayMessage = user32.NewProc("RegisterWindowMessageW")

const trayMessage = WM_APP + 73

type notifyIconData struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                GUID
	BalloonIcon         uintptr
}
type trayIconInfo struct {
	IsIcon             int32
	XHotspot, YHotspot uint32
	Mask, Color        uintptr
}

func pngTrayIcon(data []byte) (uintptr, error) {
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	bounds := image.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	pixels := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := image.At(x+bounds.Min.X, y+bounds.Min.Y).RGBA()
			i := ((height-1-y)*width + x) * 4
			pixels[i] = byte(b >> 8)
			pixels[i+1] = byte(g >> 8)
			pixels[i+2] = byte(r >> 8)
			pixels[i+3] = byte(a >> 8)
		}
	}
	color, _, _ := createTrayBitmap.Call(uintptr(width), uintptr(height), 1, 32, uintptr(unsafe.Pointer(&pixels[0])))
	runtime.KeepAlive(pixels)
	if color == 0 {
		return 0, types.ErrMenuUnsupported
	}
	defer deleteTrayObject.Call(color)
	maskBits := make([]byte, ((width+15)/16)*2*height)
	mask, _, _ := createTrayBitmap.Call(uintptr(width), uintptr(height), 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	runtime.KeepAlive(maskBits)
	if mask == 0 {
		return 0, types.ErrMenuUnsupported
	}
	defer deleteTrayObject.Call(mask)
	info := trayIconInfo{IsIcon: 1, Mask: mask, Color: color}
	icon, _, _ := createTrayIcon.Call(uintptr(unsafe.Pointer(&info)))
	if icon == 0 {
		return 0, types.ErrMenuUnsupported
	}
	return icon, nil
}
func (p *Platform) SetSystray(opts types.SystrayOptions) error {
	if len(opts.Icon) == 0 {
		return types.ErrMenuUnsupported
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return types.ErrMenuUnsupported
	}
	ready := p.ready.Load() != 0
	if !ready {
		p.trayOptions = &opts
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	var err error
	p.MainThread(func() {
		p.trayOptions = &opts
		err = p.applySystray()
	})
	return err
}
func (p *Platform) RemoveSystray() error {
	p.mu.Lock()
	ready := p.ready.Load() != 0
	closed := p.closed
	if !ready || closed {
		p.trayOptions = nil
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	p.MainThread(func() {
		p.trayOptions = nil
		p.removeSystray()
	})
	return nil
}
func (p *Platform) trayData() notifyIconData {
	return notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Window: p.hwnd, ID: 1}
}
func (p *Platform) applySystray() error {
	if p.trayOptions == nil {
		return nil
	}
	opts := p.trayOptions
	icon, err := pngTrayIcon(opts.Icon)
	if err != nil {
		return err
	}
	data := p.trayData()
	data.Flags = 1 | 2 | 4
	data.Callback = trayMessage
	data.Icon = icon
	copy(data.Tip[:127], utf16.Encode([]rune(opts.Tooltip)))
	operation := uintptr(0)
	if p.trayAdded {
		operation = 1
	}
	result, _, _ := shellTray.Call(operation, uintptr(unsafe.Pointer(&data)))
	if result == 0 {
		destroyTrayIcon.Call(icon)
		return types.ErrMenuUnsupported
	}
	if p.trayIcon != 0 {
		destroyTrayIcon.Call(p.trayIcon)
	}
	p.trayIcon = icon
	p.trayAdded = true
	p.trayItems = p.trayActions.Replace(opts.Items)
	return nil
}
func (p *Platform) removeSystray() {
	p.trayActions.Close()
	if p.trayAdded {
		data := p.trayData()
		shellTray.Call(2, uintptr(unsafe.Pointer(&data)))
		p.trayAdded = false
	}
	if p.trayIcon != 0 {
		destroyTrayIcon.Call(p.trayIcon)
		p.trayIcon = 0
	}
	p.trayItems = nil
}
func (p *Platform) showTrayMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer destroyPopup.Call(menu)
	items := append([]types.MenuItem(nil), p.trayItems...)
	for i, item := range items {
		if item.Separator {
			pAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
			continue
		}
		title := item.Label
		if item.Shortcut != "" {
			title += "\t" + item.Shortcut
		}
		label := utf16PtrFromStr(title)
		pAppendMenuW.Call(menu, MF_STRING, uintptr(i+1), uintptr(unsafe.Pointer(label)))
		runtime.KeepAlive(label)
	}
	var point struct{ X, Y int32 }
	getCursorPosition.Call(uintptr(unsafe.Pointer(&point)))
	pSetForegroundWindow.Call(p.hwnd)
	selected, _, _ := trackPopup.Call(menu, 0x100|0x2, uintptr(point.X), uintptr(point.Y), 0, p.hwnd, 0)
	pPostMessageW.Call(p.hwnd, 0, 0, 0)
	if selected > 0 && int(selected) <= len(items) {
		if fn := items[selected-1].Action; fn != nil {
			fn()
		}
	}
}
