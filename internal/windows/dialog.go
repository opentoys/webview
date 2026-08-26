//go:build windows

package windows

import (
	"path/filepath"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// Native "Save As" dialog via the classic GetSaveFileNameW common dialog.
// Unlike IFileSaveDialog (Common Item Dialog), this shows no places bar /
// recent-downloads list — just the file picker. It must run on the UI thread
// (via MainThread) because it pumps its own message loop.

const (
	ofnOverwritePrompt = 0x00000002
	ofnExplorer        = 0x00080000
)

// openfilenameW mirrors the Win32 OPENFILENAMEW structure.
type openfilenameW struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	Flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	FlagsEx           uint32
}

// saveFileDialog shows the classic Save dialog and returns the chosen path.
// An empty path with no error means the user cancelled. Runs on the UI thread.
func (p *Platform) saveFileDialog(opts FileDialogOptions) (string, error) {
	const maxPath = 8192
	buf := make([]uint16, maxPath)
	if opts.Filename != "" {
		copy(buf, syscall.StringToUTF16(opts.Filename))
	}

	// "Description\0*.ext\0\0" double-NUL terminated filter. Built manually
	// because syscall.StringToUTF16Ptr rejects embedded NULs.
	filter := utf16Filter("All Files (*.*)", "*.*")

	var title *uint16
	if opts.Title != "" {
		title = utf16PtrFromStr(opts.Title)
	}
	var initialDir *uint16
	if opts.Directory != "" {
		initialDir = utf16PtrFromStr(opts.Directory)
	}
	var defExt *uint16
	if ext := filepath.Ext(opts.Filename); ext != "" {
		defExt = utf16PtrFromStr(ext[1:]) // strip leading dot
	}

	ofn := openfilenameW{
		lStructSize:     uint32(unsafe.Sizeof(openfilenameW{})),
		hwndOwner:       p.hwnd,
		lpstrFilter:     &filter[0],
		nFilterIndex:    1,
		lpstrFile:       &buf[0],
		nMaxFile:        maxPath,
		lpstrInitialDir: initialDir,
		lpstrTitle:      title,
		Flags:           ofnOverwritePrompt | ofnExplorer,
		lpstrDefExt:     defExt,
	}

	runtime.KeepAlive(buf)
	runtime.KeepAlive(filter)
	runtime.KeepAlive(title)
	runtime.KeepAlive(initialDir)
	runtime.KeepAlive(defExt)

	ret, _, _ := pGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ret == 0 {
		// User cancelled (or dialog failed). CommDlgExtendedError would
		// distinguish, but a cancelled download and a failed dialog both
		// mean "no path".
		return "", nil
	}
	return syscall.UTF16ToString(buf), nil
}

// utf16Filter builds a double-NUL-terminated OPENFILENAMEW filter in UTF-16:
// "Description\0*.ext\0\0". syscall.StringToUTF16Ptr rejects embedded NULs, so
// we encode the segments and join them with NULs manually.
func utf16Filter(name, spec string) []uint16 {
	parts := [][]uint16{
		utf16.Encode([]rune(name)),
		utf16.Encode([]rune(spec)),
	}
	var out []uint16
	for _, p := range parts {
		out = append(out, p...)
		out = append(out, 0)
	}
	out = append(out, 0)
	return out
}
