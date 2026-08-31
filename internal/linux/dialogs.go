//go:build linux

package linux

import (
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

const (
	gtkFileChooserActionOpen = 0
	gtkFileChooserActionSave = 1
	gtkResponseAccept        = -3
	// WebKitPolicyDecisionTypeResponse: a WebKitResponsePolicyDecision.
	// Navigation decisions (type=0) are WebKitNavigationPolicyDecision — do NOT call get_response() on them.
	webkitPolicyDecisionTypeResponse = 2
)

// showSaveDialog presents a modal GtkFileChooserNative save dialog on the GTK
// thread and returns the chosen path. The second result is false when the user
// cancels. Blocks the caller until the dialog is dismissed. The caller
// (download-started signal) already runs on the GTK main thread, so the chooser
// is run directly here — never block waiting for a dispatched idle or it deadlocks.
func (p *gtk) showSaveDialog(suggested string) (string, bool) {
	dlg := gtkFileChooserNativeNew("", p.window, gtkFileChooserActionSave, "_Save", "_Cancel")
	if dlg == 0 {
		return "", false
	}
	defer gObjectUnref(dlg)
	if suggested != "" {
		gtkFileChooserSetCurrentName(dlg, suggested)
	}
	var response int
	if gtkNativeDialogRun != nil {
		response = gtkNativeDialogRun(dlg)
	} else {
		response = runFallbackDialog(dlg)
	}
	if response != gtkResponseAccept {
		return "", false
	}
	path := p.savePathFn(p, dlg)
	if path == "" {
		return "", false
	}
	return path, true
}

var (
	dialogRespMu     sync.Mutex
	dialogRespSeq    int
	dialogRespStates = map[int]*dialogResp{}
)

type dialogResp struct {
	response int
	done     bool
}

// <input type=file> accept attribute captured by the __accept__ bridge callback
// (see platform_linux.go). Read synchronously by the run-file-chooser handler
// before showing the native dialog.
var (
	fileAcceptMu sync.Mutex
	fileAccept   string
)

// SetFileAccept stores the HTML accept attribute value for the next file input.
func SetFileAccept(v string) {
	fileAcceptMu.Lock()
	fileAccept = v
	fileAcceptMu.Unlock()
}

// runFallbackDialog shows a GtkFileChooserNative and blocks until the user
// responds by manually iterating the GLib main context. Used when
// gtk_native_dialog_run is not available on this GTK build.
func runFallbackDialog(dlg uintptr) int {
	dialogRespMu.Lock()
	dialogRespSeq++
	token := dialogRespSeq
	st := &dialogResp{}
	dialogRespStates[token] = st
	dialogRespMu.Unlock()
	defer func() {
		dialogRespMu.Lock()
		delete(dialogRespStates, token)
		dialogRespMu.Unlock()
	}()
	gSignalConnectData(dlg, "response", dialogResponseFn(), uintptr(token), 0, 0)
	gtkNativeDialogShow(dlg)
	gtkNativeDialogSetModal(dlg, true)
	for {
		dialogRespMu.Lock()
		done := st.done
		dialogRespMu.Unlock()
		if done {
			break
		}
		gMainContextIteration(0, true)
	}
	gtkNativeDialogHide(dlg)
	return st.response
}

func dialogResponseFn() uintptr {
	if dialogResponse != 0 {
		return dialogResponse
	}
	dialogResponse = purego.NewCallback(func(dialog, responseID, token uintptr) uintptr {
		dialogRespMu.Lock()
		st := dialogRespStates[int(token)]
		dialogRespMu.Unlock()
		if st != nil {
			st.response = int(responseID)
			st.done = true
		}
		return 0
	})
	return dialogResponse
}

func connectDialogCallback() {}

// runFileChooserFn handles the WebKitWebView "run-file-chooser" signal
// (webview, request, user_data). It shows a native GtkFileChooserNative open
// dialog honoring the <input accept> attribute, then hands the chosen GFiles
// back to WebKit via webkit_file_chooser_request_select_files. Returning TRUE
// means we handled the chooser ourselves (suppresses WebKit's default).
func runFileChooserFn() uintptr {
	if runFileChooser != 0 {
		return runFileChooser
	}
	runFileChooser = purego.NewCallback(func(webview, request, userData uintptr) uintptr {
		p := lookupPlatform(userData)
		if p == nil || request == 0 {
			return 0
		}
		multiple := webkitFileChooserRequestGetSelectMultiple(request)
		paths, ok := p.showOpenDialog(multiple)
		if !ok {
			webkitFileChooserRequestCancel(request)
			return 1
		}
		// Build a GList of GFile* (transfer none) for the request.
		var list uintptr
		for _, path := range paths {
			file := gFileNewForPath(path)
			list = gListAppend(list, file)
		}
		webkitFileChooserRequestSelectFiles(request, list)
		gListFree(list) // GList container only; WebKit refs each GFile.
		return 1
	})
	return runFileChooser
}

var runFileChooser uintptr

// showOpenDialog presents a modal GtkFileChooserNative open dialog on the GTK
// main thread and applies accept filters read from the captured <input accept>
// attribute. Returns the chosen absolute paths and false on cancel.
func (p *gtk) showOpenDialog(multiple bool) ([]string, bool) {
	dlg := gtkFileChooserNativeNew("", p.window, gtkFileChooserActionOpen, "_Open", "_Cancel")
	if dlg == 0 {
		return nil, false
	}
	defer gObjectUnref(dlg)
	if multiple {
		gtkFileChooserSetSelectMultiple(dlg, true)
	}
	addAcceptFilter(dlg)
	var response int
	if gtkNativeDialogRun != nil {
		response = gtkNativeDialogRun(dlg)
	} else {
		response = runFallbackDialog(dlg)
	}
	if response != gtkResponseAccept {
		return nil, false
	}
	if p.openFilesFn == nil {
		return nil, false
	}
	return p.openFilesFn(p, dlg), true
}

func pgtk4SavePath(p *gtk, dlg uintptr) string {
	file := gtkFileChooserGetFile(dlg) // transfer full
	if file == 0 {
		return ""
	}
	defer gObjectUnref(file)
	cs := gFileGetPath(file)
	if cs == 0 {
		return ""
	}
	return cstr(cs)
}

// pgtk4OpenFiles converts the GTK4 chooser's GList of GFile objects into
// filesystem paths and releases the transferred objects.
func pgtk4OpenFiles(p *gtk, dlg uintptr) []string {
	files := gtkFileChooserGetFiles(dlg)
	if files == 0 {
		return nil
	}
	defer gListFree(files)
	n := int(gListLength(files))
	var out []string
	for i := uint(0); i < uint(n); i++ {
		file := gListNthData(files, i)
		if file == 0 {
			continue
		}
		cs := gFileGetPath(file)
		if cs != 0 {
			if s := cstr(cs); s != "" {
				out = append(out, s)
			}
			gFree(cs)
		}
		gObjectUnref(file)
	}
	return out
}

// parseAccept splits an HTML accept attribute value into individual entries.
// E.g. "image/png,.pdf,.jpg" -> ["image/png", ".pdf", ".jpg"].
func parseAccept(accept string) []string {
	if accept == "" {
		return nil
	}
	var out []string
	for _, s := range strings.Split(accept, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// addAcceptFilter reads the captured accept attribute and adds a single
// GtkFileFilter built from extensions and MIME-type-derived patterns. When
// empty, no filter is added (all files selectable).
//
// gtk_file_filter_add_mime_type depends on the system shared-mime-info
// database which may be absent or incomplete on some Linux setups. We
// convert MIME types to file-extension glob patterns instead, which are
// always reliable.
func addAcceptFilter(dlg uintptr) {
	fileAcceptMu.Lock()
	accept := fileAccept
	fileAccept = ""
	fileAcceptMu.Unlock()
	entries := parseAccept(accept)
	if len(entries) == 0 {
		return
	}
	filter := gtkFileFilterNew()
	if filter == 0 {
		return
	}
	gtkFileFilterSetName(filter, "Selected files")
	for _, e := range entries {
		switch {
		case strings.HasPrefix(e, "."):
			gtkFileFilterAddPattern(filter, "*"+e)
		case strings.Contains(e, "/*"):
			for _, ext := range mimeToExtensions(e) {
				gtkFileFilterAddPattern(filter, ext)
			}
		case strings.Contains(e, "/"):
			gtkFileFilterAddMimeType(filter, e)
		default:
			// Bare extension without dot — normalize.
			gtkFileFilterAddPattern(filter, "*."+e)
		}
	}
	gtkFileChooserAddFilter(dlg, filter)
}

// mimeToExtensions maps a MIME type (or wildcard like "image/*") to common
// file-extension glob patterns. Unknown types fall back to "*".
func mimeToExtensions(mime string) []string {
	if exts, ok := commonMimes[strings.TrimSuffix(mime, "/*")]; ok {
		return exts
	}
	return []string{"*"}
}

var commonMimes = map[string][]string{
	"image":       {"*.png", "*.jpg", "*.jpeg", "*.gif", "*.webp", "*.svg", "*.bmp", "*.ico"},
	"audio":       {"*.mp3", "*.wav", "*.ogg", "*.flac"},
	"video":       {"*.mp4", "*.webm", "*.ogv"},
	"application": {"*.pdf", "*.zip", "*.json", "*.xml", "*.js", "*.mjs", "*.cjs"},
	"text":        {"*.txt", "*.html", "*.htm", "*.css", "*.js", "*.mjs", "*.md", "*.markdown"},
}
