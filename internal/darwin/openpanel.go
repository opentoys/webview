//go:build darwin

package darwin

import (
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// acceptMu protects acceptVal, written by the __accept__ bridge callback
// (main thread) and read by readAcceptAndFilter (main thread).
var (
	acceptMu  sync.Mutex
	acceptVal string
)

// SetAccept stores the <input accept> value captured by the JS click listener.
// Called from the __accept__ bridge function.
func SetAccept(v string) {
	acceptMu.Lock()
	acceptVal = v
	acceptMu.Unlock()
}

// OpenPanelParams is the info a runOpenPanelWithParameters: handler receives.
type OpenPanelParams struct {
	AllowsMultipleSelection bool
	AllowsDirectories       bool
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
// completionHandler: implementation.
func runOpenPanel(id objc.ID, cmd objc.SEL, webView objc.ID, paramsObj objc.ID, frame objc.ID, completion objc.ID) {
	p := activePlatform
	if p == nil {
		return
	}
	allowsMulti := objc.ID(paramsObj).Send(allowsMultipleSelectionSel) != 0
	allowsDirs := objc.ID(paramsObj).Send(allowsDirectoriesSel) != 0

	if completion == 0 {
		return
	}
	safe := objc.Block(completion).Copy()
	if safe == 0 {
		return
	}
	defer safe.Release()

	func() {
		defer func() {
			if r := recover(); r != nil {
				invokeBlock(objc.ID(safe), objc.ID(0))
			}
		}()
		p.showOpenPanel(webView, OpenPanelParams{
			AllowsMultipleSelection: allowsMulti,
			AllowsDirectories:       allowsDirs,
		}, objc.ID(safe))
	}()
}

// showOpenPanel presents the native NSOpenPanel for <input type=file>.
func (p *Platform) showOpenPanel(wv objc.ID, params OpenPanelParams, completion objc.ID) {
	if p.OpenPanelFunc != nil {
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

	panel := objc.ID(nsOpenPanelClass).Send(openPanelSel)
	panel.Send(setCanChooseFilesSel, true)
	panel.Send(setCanChooseDirectoriesSel, params.AllowsDirectories)
	panel.Send(setAllowsMultipleSelectionSel, params.AllowsMultipleSelection)

	fm := objc.ID(nsFileManagerClass).Send(defaultManagerSel)
	home := objc.ID(fm).Send(homeDirectoryForCurrentUserSel)
	panel.Send(setDirectoryURLSel, home)

	// Read <input accept> stored by the __accept__ bridge callback and
	// apply content-type filter BEFORE showing the panel.
	readAcceptAndFilter(panel)

	// NSModalResponseOK = 1.
	result := panel.Send(runModalSel)
	if result != 0 {
		urls := panel.Send(URLsSel)
		invokeBlock(completion, urls)
	} else {
		invokeBlock(completion, objc.ID(0))
	}
}

// readAcceptAndFilter reads the <input accept> value stored by the __accept__
// bridge callback, builds UTType objects, and sets panel.allowedContentTypes
// — all BEFORE runModal is called. No JS eval, no NSRunLoop needed.
func readAcceptAndFilter(panel objc.ID) {
	if panel.Send(respondsToSelectorSel, setAllowedContentTypesSel) == 0 {
		return
	}
	acceptMu.Lock()
	accept := acceptVal
	acceptVal = "" // reset for next time
	acceptMu.Unlock()

	types := buildUTTypes(parseAccept(accept))
	if types != 0 {
		panel.Send(setAllowedContentTypesSel, types)
	}
}

// parseAccept splits an HTML accept attribute value into individual entries.
// E.g. "image/png,.pdf,.jpg" → ["image/png", ".pdf", ".jpg"]
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

// buildUTTypes converts accept values to an NSArray<UTType>. Handles MIME
// types ("image/png"), file extensions (".png"). Returns 0 if empty.
func buildUTTypes(accepts []string) objc.ID {
	if len(accepts) == 0 {
		return 0
	}
	utTypeClass := objc.GetClass("UTType")
	if utTypeClass == 0 {
		return 0
	}
	typeWithMIMESel := objc.RegisterName("typeWithMIMEType:")
	typeWithExtSel := objc.RegisterName("typeWithFilenameExtension:")

	// Wildcard MIME → UTType via typeWithIdentifier:.
	wildcardUTI := map[string]string{
		"image/*": "public.image",
		"video/*": "public.movie",
		"audio/*": "public.audio",
		"text/*":  "public.text",
	}
	typeWithIdentifierSel := objc.RegisterName("typeWithIdentifier:")

	arr := objc.ID(nsMutableArrayClass).Send(arrayInstanceSel)
	for _, a := range accepts {
		var ut objc.ID
		if strings.HasPrefix(a, ".") {
			ext := strings.TrimPrefix(a, ".")
			ut = objc.ID(utTypeClass).Send(typeWithExtSel, nsString(ext))
		} else if uti, ok := wildcardUTI[a]; ok {
			ut = objc.ID(utTypeClass).Send(typeWithIdentifierSel, nsString(uti))
		} else if strings.Contains(a, "/") {
			ut = objc.ID(utTypeClass).Send(typeWithMIMESel, nsString(a))
		} else {
			continue
		}
		if ut != 0 {
			arr.Send(addObjectSel, ut)
		}
	}
	countSel := objc.RegisterName("count")
	if arr.Send(countSel) == 0 {
		return 0
	}
	return arr
}

// pathURLs builds an NSArray<NSURL> from absolute paths, or 0 (nil) for empty.
func pathURLs(paths []string) objc.ID {
	if len(paths) == 0 {
		return 0
	}
	ids := make([]objc.ID, len(paths))
	for i, p := range paths {
		ids[i] = objc.ID(nsURLClass).Send(fileURLWithPathSel, nsString(p))
	}
	return objc.ID(nsArrayClass).Send(arrayWithObjectsCountSel,
		unsafe.Pointer(&ids[0]), len(paths))
}

// openPanelResult builds the NSArray<NSURL> completion value for chosen paths,
// or 0 (nil) on cancel.
func openPanelResult(paths []string, ok bool) objc.ID {
	if !ok {
		return 0
	}
	return pathURLs(paths)
}
