//go:build darwin

package darwin

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// OpenPanelParams is the info a runOpenPanelWithParameters: handler receives.
type OpenPanelParams struct {
	AllowsMultipleSelection bool
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
	// Recover from any panic in the panel path (e.g. purego ABI mismatch) and
	// still call WebKit's completion with nil, preventing the "completion handler
	// was not called" crash.
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "openpanel: panic in showOpenPanel: %v\n", r)
				if completion != 0 {
					callBlock(completion, objc.ID(0))
				}
			}
		}()
		p.showOpenPanel(OpenPanelParams{AllowsMultipleSelection: allowsMulti}, objc.Block(completion))
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
// block is invoked directly with panel.URLs on return — no Go-side block
// round-trip, which purego's block cache cannot serve for AppKit-invoked
// blocks (AppKit copies them to a different address than the stored key).
func (p *Platform) showOpenPanel(params OpenPanelParams, completion objc.Block) {
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
			callBlock(objc.ID(completion), openPanelResult(paths, ok))
		}
		return
	}
	// NSOpenPanel has no public init; openPanel returns a configured instance.
	panel := objc.ID(nsOpenPanelClass).Send(openPanelSel)
	panel.Send(setCanChooseFilesSel, true)
	panel.Send(setCanChooseDirectoriesSel, false)
	panel.Send(setAllowsMultipleSelectionSel, params.AllowsMultipleSelection)
	panel.Send(setAllowedFileTypesSel, objc.ID(0)) // nil = all files
	// Default to the user's home directory (homeDirectoryForCurrentUser → NSURL).
	fm := objc.ID(nsFileManagerClass).Send(defaultManagerSel)
	home := objc.ID(fm).Send(homeDirectoryForCurrentUserSel)
	panel.Send(setDirectoryURLSel, home)

	// NSModalResponseOK = 1. Run the panel modally; on OK, forward the selected
	// URLs (NSArray<NSURL>) straight to WebKit's completion block.
	result := panel.Send(runModalSel)
	if result != 0 {
		callBlock(objc.ID(completion), panel.Send(URLsSel))
	} else {
		callBlock(objc.ID(completion), objc.ID(0))
	}
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

// completeOpenPanel invokes the WKOpenPanel completion block. ok=false → nil
// (cancel: WebKit emits no FileList, input stays empty). callBlock matches the
// dialog completion path (WebKit hands raw blocks that are not in purego's
// block cache, so Block.Invoke would panic).
//
// MUST be called on the host thread — WebKit asserts the completion handler is
// invoked before the delegate method returns. showOpenPanel already ensures this
// (done-channel blocks the host thread until the handler calls its callback).
func completeOpenPanel(completion objc.Block, paths []string, ok bool) {
	callBlock(objc.ID(completion), openPanelResult(paths, ok))
}
