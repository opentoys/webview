//go:build windows

package windows

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
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
	iidFileDialog       = GUID{0x42F85136, 0xDB7E, 0x439C, [8]byte{0x85, 0xF1, 0xE4, 0x07, 0x5D, 0x13, 0x5F, 0xC8}}
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

func showItemDialog(kind types.DialogKind, opts types.FileDialogOptions) (string, error) {
	directory := kind == types.DialogDirectory
	filters := opts.Filters
	clsid := clsidFileOpenDialog
	if kind == types.DialogSave {
		clsid = GUID{0xC0B4E2F3, 0xBA21, 0x4773, [8]byte{0x8D, 0xBA, 0x33, 0x5E, 0xC9, 0x46, 0xEB, 0x8B}}
	}
	var dialog *fileOpenDialog
	hr, _, _ := pCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsid)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidFileDialog)), uintptr(unsafe.Pointer(&dialog)))
	if failedHRESULT(hr) || dialog == nil {
		return "", fmt.Errorf("%w: IFileDialog creation failed (HRESULT 0x%08X)", types.ErrDialogUnsupported, uint32(hr))
	}
	defer dialog.vtbl.Release.Call(uintptr(unsafe.Pointer(dialog)))
	standaloneDialog = dialog
	defer func() { standaloneDialog = nil }()
	flags := uintptr(fosForceFilesystem | fosPathMustExist)
	if kind == types.DialogSave {
		flags |= 2
	} else {
		flags |= fosFileMustExist
	}
	if directory {
		flags |= fosPickFolders
	}
	if hr, _, _ = dialog.vtbl.SetOptions.Call(uintptr(unsafe.Pointer(dialog)), flags); failedHRESULT(hr) {
		return "", fmt.Errorf("webview: choose path: SetOptions failed (HRESULT 0x%08X)", uint32(hr))
	}
	ft := makeWindowsFileTypes(filters)
	defer runtime.KeepAlive(ft)
	if !directory && len(filters) > 0 {
		if len(ft) > 0 {
			dialog.vtbl.SetFileTypes.Call(uintptr(unsafe.Pointer(dialog)), uintptr(len(ft)), uintptr(unsafe.Pointer(&ft[0])))
		}
	}
	if opts.Title != "" {
		title := utf16PtrFromStr(opts.Title)
		dialog.vtbl.SetTitle.Call(uintptr(unsafe.Pointer(dialog)), uintptr(unsafe.Pointer(title)))
		runtime.KeepAlive(title)
	}
	if kind == types.DialogSave && opts.Filename != "" {
		name := utf16PtrFromStr(opts.Filename)
		dialog.vtbl.SetFileName.Call(uintptr(unsafe.Pointer(dialog)), uintptr(unsafe.Pointer(name)))
		runtime.KeepAlive(name)
	}
	if kind == types.DialogSave {
		extension := strings.TrimPrefix(filepath.Ext(opts.Filename), ".")
		if extension == "" && len(filters) == 1 {
			for _, ext := range filters[0].Extensions {
				ext = strings.TrimPrefix(strings.TrimSpace(ext), ".")
				if ext != "" && ext != "*" {
					extension = ext
					break
				}
			}
		}
		if extension != "" {
			ext := utf16PtrFromStr(extension)
			dialog.vtbl.SetDefaultExtension.Call(uintptr(unsafe.Pointer(dialog)), uintptr(unsafe.Pointer(ext)))
			runtime.KeepAlive(ext)
		}
	}
	if opts.Directory != "" {
		name := utf16PtrFromStr(opts.Directory)
		var folder *shellItem
		iid := GUID{0x43826D1E, 0xE718, 0x42EE, [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
		hr, _, _ = createDialogShellItem.Call(uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&folder)))
		runtime.KeepAlive(name)
		if !failedHRESULT(hr) && folder != nil {
			dialog.vtbl.SetFolder.Call(uintptr(unsafe.Pointer(dialog)), uintptr(unsafe.Pointer(folder)))
			folder.vtbl.Release.Call(uintptr(unsafe.Pointer(folder)))
		}
	}
	if hr, _, _ = dialog.vtbl.Show.Call(uintptr(unsafe.Pointer(dialog)), 0); failedHRESULT(hr) {
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
	filter := windowsFilter(opts.Filters)

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
	} else if len(opts.Filters) == 1 {
		for _, ext := range opts.Filters[0].Extensions {
			ext = strings.TrimPrefix(strings.TrimSpace(ext), ".")
			if ext != "" && ext != "*" {
				defExt = utf16PtrFromStr(ext)
				break
			}
		}
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

type comdlgFileType struct{ name, spec *uint16 }

func makeWindowsFileTypes(filters []FileFilter) []comdlgFileType { // IFileOpenDialog COMDLG_FILTERSPEC layout
	var out []comdlgFileType
	for _, f := range filters {
		name := f.Name
		if name == "" {
			name = "Files"
		}
		spec := filterSpec(f)
		if spec != "" {
			n := utf16PtrFromStr(name)
			s := utf16PtrFromStr(spec)
			out = append(out, comdlgFileType{n, s})
		}
	}
	return out
}

func filterSpec(f FileFilter) string {
	var parts []string
	for _, e := range f.Extensions {
		e = strings.TrimSpace(e)
		if e == "" || e == "*" {
			return "*.*"
		}
		e = strings.TrimPrefix(e, ".")
		if e == "" {
			continue
		}
		parts = append(parts, "*."+e)
	}
	for _, m := range f.MIMETypes {
		for _, e := range windowsMIMEExtensions(m) {
			parts = append(parts, e)
		}
	}
	if len(parts) == 0 {
		return "*.*"
	}
	return strings.Join(parts, ";")
}

func windowsMIMEExtensions(mime string) []string {
	common := map[string][]string{
		"image/*":   {"*.png", "*.jpg", "*.jpeg", "*.gif", "*.webp", "*.svg"},
		"audio/*":   {"*.mp3", "*.wav", "*.ogg", "*.flac"},
		"video/*":   {"*.mp4", "*.webm", "*.ogv"},
		"text/*":    {"*.txt", "*.html", "*.htm", "*.css", "*.md"},
		"image/png": {"*.png"}, "image/jpeg": {"*.jpg", "*.jpeg"},
		"application/pdf": {"*.pdf"},
	}
	return common[strings.ToLower(strings.TrimSpace(mime))]
}

func windowsFilter(filters []FileFilter) []uint16 {
	if len(filters) == 0 {
		return utf16Filter("All Files (*.*)", "*.*")
	}
	var out []uint16
	for _, f := range filters {
		name := f.Name
		if name == "" {
			name = "Files"
		}
		spec := filterSpec(f)
		for _, s := range []string{name, spec} {
			out = append(out, utf16.Encode([]rune(s))...)
			out = append(out, 0)
		}
	}
	out = append(out, 0)
	return out
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

var standaloneDialog *fileOpenDialog // STA only
var createDialogShellItem = syscall.NewLazyDLL("shell32.dll").NewProc("SHCreateItemFromParsingName")

func PrepareDialogs() error {
	if err := startApplication(); err != nil {
		return fmt.Errorf("%w: %v", types.ErrDialogUnsupported, err)
	}
	return nil
}
func DialogOnUI() bool {
	select {
	case <-applicationReady:
	default:
		return false
	}
	tid, _, _ := pGetCurrentThreadId.Call()
	return tid == applicationTID
}
func ShowDialog(kind types.DialogKind, opts types.FileDialogOptions, stopped func() bool) (string, error) {
	type result struct {
		path string
		err  error
	}
	results := make(chan result, 1)
	applicationThread(func() {
		var path string
		var err error
		defer func() { results <- result{path, err} }()
		if !stopped() {
			path, err = showItemDialog(kind, opts)
		}
	})
	select {
	case r := <-results:
		return r.path, r.err
	case <-applicationDone:
		return "", types.ErrDialogCancelled
	}
}
func CancelDialog() {
	select {
	case <-applicationReady:
	default:
		return
	}
	if applicationErr != nil {
		return
	}
	applicationThread(func() {
		if standaloneDialog != nil {
			standaloneDialog.vtbl.Close.Call(uintptr(unsafe.Pointer(standaloneDialog)), errorCancelledHR)
		}
	})
}
