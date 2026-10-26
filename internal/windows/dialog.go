//go:build windows

package windows

import (
	"fmt"
	"path/filepath"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/opentoys/webview/internal/types"
)

const (
	clsctxInprocServer = 1
	fosPickFolders     = 0x20
	fosForceFilesystem = 0x40
	fosPathMustExist   = 0x800
	fosFileMustExist   = 0x1000
	sigdnFileSysPath   = 0x80058000
	errorCancelledHR   = 0x800704C7
)

var (
	clsidFileOpenDialog = GUID{0xDC1C5A9C, 0xE88A, 0x4DDE, [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidFileOpenDialog   = GUID{0xD57C7288, 0xD4AD, 0x4768, [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
)

type fileOpenDialog struct{ vtbl *fileOpenDialogVtbl }
type fileOpenDialogVtbl struct {
	_IUnknownVtbl
	Show, SetFileTypes, SetFileTypeIndex, GetFileTypeIndex                                     ComProc
	Advise, Unadvise, SetOptions, GetOptions                                                   ComProc
	SetDefaultFolder, SetFolder, GetFolder, GetCurrentSelection                                ComProc
	SetFileName, GetFileName, SetTitle, SetOkButtonLabel, SetFileNameLabel                     ComProc
	GetResult, AddPlace, SetDefaultExtension, Close, SetClientGuid, ClearClientData, SetFilter ComProc
}

type shellItem struct{ vtbl *shellItemVtbl }
type shellItemVtbl struct {
	_IUnknownVtbl
	BindToHandler, GetParent, GetDisplayName, GetAttributes, Compare ComProc
}

func failedHRESULT(hr uintptr) bool { return uint32(hr)&0x80000000 != 0 }

func (p *Platform) ChooseFile() (string, error) { return p.choosePath(false) }
func (p *Platform) ChooseDir() (string, error)  { return p.choosePath(true) }

func (p *Platform) choosePath(directory bool) (string, error) {
	p.mu.Lock()
	ready := p.hwnd != 0 && !p.closed
	p.mu.Unlock()
	if !ready {
		return "", fmt.Errorf("webview: choose path: window is not running")
	}
	var path string
	var err error
	p.MainThread(func() { path, err = p.choosePathUI(directory) })
	return path, err
}

func (p *Platform) choosePathUI(directory bool) (string, error) {
	var dialog *fileOpenDialog
	hr, _, _ := pCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidFileOpenDialog)), uintptr(unsafe.Pointer(&dialog)))
	if failedHRESULT(hr) || dialog == nil {
		return "", fmt.Errorf("webview: choose path: IFileOpenDialog creation failed (HRESULT 0x%08X)", uint32(hr))
	}
	defer dialog.vtbl.Release.Call(uintptr(unsafe.Pointer(dialog)))
	flags := uintptr(fosForceFilesystem | fosPathMustExist | fosFileMustExist)
	if directory {
		flags |= fosPickFolders
	}
	if hr, _, _ = dialog.vtbl.SetOptions.Call(uintptr(unsafe.Pointer(dialog)), flags); failedHRESULT(hr) {
		return "", fmt.Errorf("webview: choose path: SetOptions failed (HRESULT 0x%08X)", uint32(hr))
	}
	if hr, _, _ = dialog.vtbl.Show.Call(uintptr(unsafe.Pointer(dialog)), p.hwnd); failedHRESULT(hr) {
		if uint32(hr) == errorCancelledHR {
			return "", types.ErrDialogCancelled
		}
		return "", fmt.Errorf("webview: choose path: Show failed (HRESULT 0x%08X)", uint32(hr))
	}
	var item *shellItem
	if hr, _, _ = dialog.vtbl.GetResult.Call(uintptr(unsafe.Pointer(dialog)), uintptr(unsafe.Pointer(&item))); failedHRESULT(hr) || item == nil {
		return "", fmt.Errorf("webview: choose path: GetResult failed (HRESULT 0x%08X)", uint32(hr))
	}
	defer item.vtbl.Release.Call(uintptr(unsafe.Pointer(item)))
	var raw *uint16
	if hr, _, _ = item.vtbl.GetDisplayName.Call(uintptr(unsafe.Pointer(item)), sigdnFileSysPath, uintptr(unsafe.Pointer(&raw))); failedHRESULT(hr) || raw == nil {
		return "", fmt.Errorf("webview: choose path: selected item has no filesystem path (HRESULT 0x%08X)", uint32(hr))
	}
	defer pCoTaskMemFree.Call(uintptr(unsafe.Pointer(raw)))
	path := wideToString(raw)
	if path == "" {
		return "", fmt.Errorf("webview: choose path: selected item has an empty filesystem path")
	}
	return filepath.Abs(path)
}

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
