//go:build darwin

package darwin

import (
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

const (
	nsFilenamesPboardType = "NSFilenamesPboardType"
	nsFileURLPboardType   = "public.file-url"
)

func (p *Platform) applyFileDropTarget(view objc.ID, enabled bool) {
	if enabled {
		// AppKit targets the deepest registered view. Temporarily remove the
		// WebView's registration and restore it exactly when disabled.
		if p.webview != 0 && p.webviewDragTypes == 0 {
			p.webviewDragTypes = p.webview.Send(objc.RegisterName("registeredDraggedTypes"))
			p.webview.Send(objc.RegisterName("unregisterDraggedTypes"))
		}
		types := []objc.ID{nsString(nsFilenamesPboardType), nsString(nsFileURLPboardType)}
		arr := objc.ID(nsArrayClass).Send(arrayWithObjectsCountSel, unsafe.Pointer(&types[0]), len(types))
		view.Send(objc.RegisterName("registerForDraggedTypes:"), arr)
		if p.webview != 0 {
			p.webview.Send(objc.RegisterName("registerForDraggedTypes:"), arr)
		}
		return
	}
	view.Send(objc.RegisterName("unregisterDraggedTypes"))
	if p.webview != 0 && p.webviewDragTypes != 0 {
		p.webview.Send(objc.RegisterName("registerForDraggedTypes:"), p.webviewDragTypes)
		p.webviewDragTypes = 0
	}
}

func (p *Platform) fileDropEnabled() bool {
	p.mu.Lock()
	ok := !p.closed && p.filesDropped != nil
	p.mu.Unlock()
	return ok
}

func (p *Platform) pathsFromDraggingPasteboard(pb objc.ID) []string {
	if pb == 0 {
		return nil
	}
	// Modern Finder advertises public.file-url. Asking the pasteboard for NSURL
	// objects handles that format and filters non-file URLs without parsing a
	// URI. NSFileNamesPboardType remains as a fallback for older senders.
	urlClass := objc.ID(objc.GetClass("NSURL"))
	classes := objc.ID(nsArrayClass).Send(arrayWithObjectsCountSel, unsafe.Pointer(&urlClass), 1)
	arr := pb.Send(objc.RegisterName("readObjectsForClasses:options:"), classes, objc.ID(0))
	if arr != 0 {
		n := int(arr.Send(objc.RegisterName("count")))
		paths := make([]string, 0, n)
		objectAt := objc.RegisterName("objectAtIndex:")
		for i := 0; i < n; i++ {
			url := arr.Send(objectAt, uintptr(i))
			if url.Send(objc.RegisterName("isFileURL")) != 0 {
				if path := goString(url.Send(objc.RegisterName("path"))); path != "" {
					paths = append(paths, path)
				}
			}
		}
		if len(paths) != 0 {
			return paths
		}
	}

	arr = pb.Send(objc.RegisterName("propertyListForType:"), nsString(nsFilenamesPboardType))
	if arr == 0 {
		return nil
	}
	n := int(arr.Send(objc.RegisterName("count")))
	paths := make([]string, 0, n)
	objectAt := objc.RegisterName("objectAtIndex:")
	for i := 0; i < n; i++ {
		if path := goString(arr.Send(objectAt, uintptr(i))); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}
