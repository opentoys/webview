//go:build windows

package windows

import (
	"fmt"
	"runtime"
	"strings"
	"unsafe"
)

// Native file save dialog via the Common Item Dialog COM API
// (IFileSaveDialog). Adapted from docs/glaze/dialog_windows.go to this
// project's ComProc/syscall idiom. SaveFile is application-modal: Show runs
// its own message loop, so it must run on the UI thread (via MainThread).

// --- CLSIDs / IIDs ---------------------------------------------------------

var (
	clsidFileSaveDialog = GUID{0xC0B4E2F3, 0xBA21, 0x4773, [8]byte{0x8D, 0xBA, 0x33, 0x5E, 0xC9, 0x46, 0xEB, 0x8B}}
	iidFileSaveDialog   = GUID{0x84BCCD23, 0x5FDE, 0x4CDB, [8]byte{0xAE, 0xA4, 0xAF, 0x64, 0xB8, 0x3D, 0x78, 0xAB}}
	iidShellItem        = GUID{0x43826D1E, 0xE718, 0x42EE, [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

const (
	clsctxInprocServer = 0x1

	// FILEOPENDIALOGOPTIONS bits.
	fosOverwritePrompt = 0x00000002
	fosForceFilesystem = 0x00000040

	// SIGDN_FILESYSPATH for IShellItem::GetDisplayName.
	sigdnFileSysPath = 0x80058000

	// HRESULT_FROM_WIN32(ERROR_CANCELLED): the user dismissed the dialog.
	hrCancelled = 0x800704C7
)

// --- COM vtable layouts (exact IDL order) ----------------------------------

// iFileDialogVtbl covers the IFileDialog portion shared by IFileSaveDialog.
// The IFileSaveDialog-specific methods (SetSaveAsItem, SetProperties, ...)
// follow and are omitted — none are called.
type iFileDialogVtbl struct {
	_IUnknownVtbl
	Show                ComProc // 3
	SetFileTypes        ComProc // 4
	SetFileTypeIndex    ComProc // 5
	GetFileTypeIndex    ComProc // 6
	Advise              ComProc // 7
	Unadvise            ComProc // 8
	SetOptions          ComProc // 9
	GetOptions          ComProc // 10
	SetDefaultFolder    ComProc // 11
	SetFolder           ComProc // 12
	GetFolder           ComProc // 13
	GetCurrentSelection ComProc // 14
	SetFileName         ComProc // 15
	GetFileName         ComProc // 16
	SetTitle            ComProc // 17
	SetOkButtonLabel    ComProc // 18
	SetFileNameLabel    ComProc // 19
	GetResult           ComProc // 20
	AddPlace            ComProc // 21
	SetDefaultExtension ComProc // 22
	Close               ComProc // 23
	SetClientGuid       ComProc // 24
	ClearClientData     ComProc // 25
	SetFilter           ComProc // 26
}

type iShellItemVtbl struct {
	_IUnknownVtbl
	BindToHandler  ComProc // 3
	GetParent      ComProc // 4
	GetDisplayName ComProc // 5
	GetAttributes  ComProc // 6
	Compare        ComProc // 7
}

type iFileDialog struct{ vtbl *iFileDialogVtbl }
type iShellItem struct{ vtbl *iShellItemVtbl }

// comdlgFilterSpec mirrors COMDLG_FILTERSPEC.
type comdlgFilterSpec struct {
	pszName *uint16
	pszSpec *uint16
}

func (d *iFileDialog) Show(parent uintptr) uintptr {
	r, _, _ := d.vtbl.Show.Call(uintptr(unsafe.Pointer(d)), parent)
	return r
}

func (d *iFileDialog) SetOptions(fos uint32) {
	d.vtbl.SetOptions.Call(uintptr(unsafe.Pointer(d)), uintptr(fos))
}

func (d *iFileDialog) SetTitle(s string) {
	p := utf16PtrFromStr(s)
	d.vtbl.SetTitle.Call(uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
}

func (d *iFileDialog) SetFileName(s string) {
	p := utf16PtrFromStr(s)
	d.vtbl.SetFileName.Call(uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
}

func (d *iFileDialog) SetFolder(si *iShellItem) {
	d.vtbl.SetFolder.Call(uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(si)))
}

func (d *iFileDialog) SetFileTypes(n uint32, specs *comdlgFilterSpec) {
	d.vtbl.SetFileTypes.Call(uintptr(unsafe.Pointer(d)), uintptr(n), uintptr(unsafe.Pointer(specs)))
}

func (d *iFileDialog) GetResult(out **iShellItem) uintptr {
	r, _, _ := d.vtbl.GetResult.Call(uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(out)))
	return r
}

func (d *iFileDialog) Release() {
	d.vtbl.Release.Call(uintptr(unsafe.Pointer(d)))
}

func (s *iShellItem) GetDisplayName(sigdn uintptr, out **uint16) uintptr {
	r, _, _ := s.vtbl.GetDisplayName.Call(uintptr(unsafe.Pointer(s)), sigdn, uintptr(unsafe.Pointer(out)))
	return r
}

func (s *iShellItem) Release() {
	s.vtbl.Release.Call(uintptr(unsafe.Pointer(s)))
}

// SaveFile shows a modal save-file dialog and returns the chosen path, or ""
// when cancelled. It runs the dialog on the UI thread and blocks until the
// user dismisses it.
func (p *Platform) SaveFile(opts FileDialogOptions) (string, error) {
	type outcome struct {
		path string
		err  error
	}
	ch := make(chan outcome, 1)
	p.MainThread(func() {
		path, err := p.saveFileDialog(opts)
		ch <- outcome{path, err}
	})
	r := <-ch
	return r.path, r.err
}

// saveFileDialog does the actual IFileSaveDialog work. Runs on the UI thread.
func (p *Platform) saveFileDialog(opts FileDialogOptions) (string, error) {
	var dlg *iFileDialog
	r, _, _ := pCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileSaveDialog)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidFileSaveDialog)),
		uintptr(unsafe.Pointer(&dlg)),
	)
	if r != S_OK || dlg == nil {
		return "", fmt.Errorf("webview: CoCreateInstance(IFileSaveDialog) failed: 0x%X", r)
	}
	defer dlg.Release()

	dlg.SetOptions(fosForceFilesystem | fosOverwritePrompt)

	if opts.Title != "" {
		dlg.SetTitle(opts.Title)
	}
	if opts.Filename != "" {
		dlg.SetFileName(opts.Filename)
	}
	if opts.Directory != "" {
		var psi *iShellItem
		dirPtr := utf16PtrFromStr(opts.Directory)
		hr, _, _ := pSHCreateItemFromParsingName.Call(
			uintptr(unsafe.Pointer(dirPtr)),
			0,
			uintptr(unsafe.Pointer(&iidShellItem)),
			uintptr(unsafe.Pointer(&psi)),
		)
		runtime.KeepAlive(dirPtr)
		if hr == S_OK && psi != nil {
			dlg.SetFolder(psi)
			psi.Release()
		}
	}

	specs := buildFilterSpecs(opts.Filters)
	if len(specs) == 0 {
		specs = []comdlgFilterSpec{{
			pszName: utf16PtrFromStr("All Files"),
			pszSpec: utf16PtrFromStr("*.*"),
		}}
	}
	dlg.SetFileTypes(uint32(len(specs)), &specs[0])

	hr := dlg.Show(p.hwnd)
	runtime.KeepAlive(specs)
	if hr != S_OK {
		if hr == hrCancelled {
			return "", nil
		}
		return "", fmt.Errorf("webview: file save dialog Show failed: 0x%X", hr)
	}

	var item *iShellItem
	if r := dlg.GetResult(&item); r != S_OK || item == nil {
		return "", nil
	}
	defer item.Release()

	var pwstr *uint16
	if r := item.GetDisplayName(sigdnFileSysPath, &pwstr); r != S_OK || pwstr == nil {
		return "", nil
	}
	path := wideToString(pwstr)
	pCoTaskMemFree.Call(uintptr(unsafe.Pointer(pwstr)))
	return path, nil
}

// buildFilterSpecs turns FileFilters into a COMDLG_FILTERSPEC array. The
// returned slice holds the backing UTF-16 pointers; the caller must keep it
// alive across SetFileTypes and Show.
func buildFilterSpecs(filters []FileFilter) []comdlgFilterSpec {
	var specs []comdlgFilterSpec
	for _, f := range filters {
		var patterns []string
		wildcard := false
		for _, e := range f.Extensions {
			if e == "" || e == "*" {
				wildcard = true
				break
			}
			patterns = append(patterns, "*."+strings.TrimPrefix(e, "."))
		}
		spec := "*.*"
		if !wildcard && len(patterns) > 0 {
			spec = strings.Join(patterns, ";")
		}
		name := f.Name
		if name == "" {
			name = spec
		}
		specs = append(specs, comdlgFilterSpec{
			pszName: utf16PtrFromStr(name),
			pszSpec: utf16PtrFromStr(spec),
		})
	}
	return specs
}
