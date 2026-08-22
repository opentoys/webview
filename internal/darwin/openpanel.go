//go:build darwin

package darwin

import (
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// OpenPanelParams is the info a runOpenPanelWithParameters: handler receives.
type OpenPanelParams struct {
	AllowsMultipleSelection bool
	AllowsDirectories       bool
	// AllowedFileTypes limits selectable files to these extensions (without
	// leading dot, e.g. "png", "jpg"). Empty means all files.
	AllowedFileTypes []string
}

// invokeBlock calls a WebKit-provided completion block via its invoke function
// pointer (purego.SyscallN).
func invokeBlock(block objc.ID, arg objc.ID) {
	if block == 0 {
		return
	}
	invoke := blockInvoke(uintptr(block))
	if invoke == 0 {
		return
	}
	r, _, _ := purego.SyscallN(invoke, uintptr(block), uintptr(arg))
	_ = r
}

// runOpenPanel is the WKUIDelegate runOpenPanelWithParameters:initiatedByFrame:
// completionHandler: implementation. Runs on the host thread (delegate
// callbacks are delivered there, same as dialog/script-message handlers).
// Building the panel, showing the sheet, and calling completion all happen on
// the host thread, so activePlatform is current and no lock is needed.
func runOpenPanel(id objc.ID, cmd objc.SEL, webView objc.ID, paramsObj objc.ID, frame objc.ID, completion objc.ID) {
	p := activePlatform
	if p == nil {
		return
	}
	allowsMulti := objc.ID(paramsObj).Send(allowsMultipleSelectionSel) != 0
	allowsDirs := objc.ID(paramsObj).Send(allowsDirectoriesSel) != 0
	// _Block_copy the completion IMMEDIATELY — before anything else.
	// WebKit provides a stack block that is only guaranteed alive for the
	// duration of this delegate callback. runModal pumps a nested event loop
	// that drains autorelease pools, which can free the original block.
	// _Block_copy promotes a stack block to the heap (or increments the
	// refcount of a heap block).
	if completion == 0 {
		return
	}
	safe := objc.Block(completion).Copy()
	if safe == 0 {
		return
	}
	defer safe.Release()

	// Recover from any panic in the panel path and still call WebKit's
	// completion with nil, preventing the "completion handler was not called"
	// crash.
	func() {
		defer func() {
			if r := recover(); r != nil {
				invokeBlock(objc.ID(safe), objc.ID(0))
			}
		}()
		p.showOpenPanel(OpenPanelParams{
			AllowsMultipleSelection: allowsMulti,
			AllowsDirectories:       allowsDirs,
		}, objc.ID(safe))
	}()
}

// showOpenPanel presents the native NSOpenPanel for <input type=file>, or
// routes to OpenPanelFunc when the app has overridden it. Runs on the host
// thread (called from runOpenPanel, a WKUIDelegate callback).
//
// The default path runs the panel modally with runModal (the webview_go
// reference approach). runModal pumps a nested event loop on the host thread,
// so it works under the manual host loop; the page is frozen while the panel
// is up, which is the standard modal-dialog behavior. WebKit's completion
// block is invoked via NSInvocation with panel.URLs on return.
func (p *Platform) showOpenPanel(params OpenPanelParams, completion objc.ID) {
	if p.OpenPanelFunc != nil {
		// The handler may call cb synchronously (host thread) or from another
		// goroutine. Block until cb fires so the delegate method can call the
		// WebKit completion before returning — WebKit asserts it is called.
		var (
			paths []string
			ok    bool
			done  = make(chan struct{})
		)
		p.OpenPanelFunc(params, func(p []string, o bool) {
			paths, ok = p, o
			close(done)
		})
		<-done
		if completion != 0 {
			invokeBlock(completion, openPanelResult(paths, ok))
		}
		return
	}
	// completion is already a heap block (_Block_copy'd in runOpenPanel).
	// NSOpenPanel has no public init; openPanel returns a configured instance.
	panel := objc.ID(nsOpenPanelClass).Send(openPanelSel)
	configureOpenPanel(panel, true, params.AllowsDirectories, params.AllowsMultipleSelection, params.AllowedFileTypes)
	// Default to the user's home directory (homeDirectoryForCurrentUser → NSURL).
	fm := objc.ID(nsFileManagerClass).Send(defaultManagerSel)
	home := objc.ID(fm).Send(homeDirectoryForCurrentUserSel)
	panel.Send(setDirectoryURLSel, home)

	// NSModalResponseOK = 1. Run the panel modally; on OK, forward the selected
	// URLs (NSArray<NSURL>) to WebKit's completion block.
	result := panel.Send(runModalSel)
	if result != 0 {
		// Retain URLs before invoking completion: the panel may be freed
		// when the autorelease pool drains after runModal.
		urls := panel.Send(URLsSel)
		invokeBlock(completion, urls)
	} else {
		invokeBlock(completion, objc.ID(0))
	}
}

// configureOpenPanel applies the open-panel settings shared by showOpenPanel
// and the programmatic dialog API.
func configureOpenPanel(panel objc.ID, canFiles, canDirs, multiple bool, allowedTypes []string) {
	panel.Send(setCanChooseFilesSel, canFiles)
	panel.Send(setCanChooseDirectoriesSel, canDirs)
	panel.Send(setAllowsMultipleSelectionSel, multiple)
	types := allowedFileTypes(allowedTypes)
	if types != 0 {
		panel.Send(setAllowedFileTypesSel, types)
	}
}

// allowedFileTypes builds an NSMutableArray<NSString*> of bare extensions for
// setAllowedFileTypes:. Returns 0 (nil) when exts is empty (no restriction).
func allowedFileTypes(exts []string) objc.ID {
	if len(exts) == 0 {
		return 0
	}
	arr := objc.ID(nsMutableArrayClass).Send(arrayInstanceSel)
	for _, e := range exts {
		e = strings.TrimPrefix(e, ".")
		if e == "" || e == "*" {
			return 0 // wildcard = no restriction
		}
		arr.Send(addObjectSel, nsString(e))
	}
	return arr
}

// pathURLs builds an NSArray<NSURL> from absolute paths, or 0 (nil) for empty.
// fileURLWithPath: returns file-system URLs; WebKit converts these into the
// input's FileList.
func pathURLs(paths []string) objc.ID {
	if len(paths) == 0 {
		return 0
	}
	ids := make([]objc.ID, len(paths))
	for i, p := range paths {
		ids[i] = objc.ID(nsURLClass).Send(fileURLWithPathSel, nsString(p))
	}
	return objc.ID(nsArrayClass).Send(arrayWithObjectsCountSel,
		unsafe.Pointer(&ids[0]), len(ids))
}

// openPanelResult builds the NSArray<NSURL> completion value for chosen paths,
// or 0 (nil) on cancel. Pure; separated so it is unit-testable without a block.
func openPanelResult(paths []string, ok bool) objc.ID {
	if !ok {
		return 0
	}
	return pathURLs(paths)
}
