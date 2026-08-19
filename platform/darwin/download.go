//go:build darwin

package darwin

import (
	"sync"

	"github.com/ebitengine/purego/objc"
)

// pendingDownloads maps WKDownload objc.ID (as uintptr) to the chosen
// destination NSURL. Written by showSavePanel, read/deleted by
// downloadDidFinish/downloadDidFail. sync.Map: sparse access, one entry
// per active download, no contention in practice.
var pendingDownloads sync.Map

func downloadKey(download objc.ID) uintptr {
	return uintptr(download)
}

// showSavePanel presents NSSavePanel for a download, or routes to
// DownloadFunc when the app has overridden it. Runs on the host thread.
func (p *Platform) showSavePanel(download, suggestedFilename, completion objc.ID) {
	name := goString(suggestedFilename)
	if name == "" {
		name = "download"
	}

	if p.DownloadFunc != nil {
		var (
			path string
			done = make(chan struct{})
		)
		p.DownloadFunc(name, func(savePath string) {
			path = savePath
			close(done)
		})
		<-done
		if path != "" {
			url := objc.ID(nsURLClass).Send(fileURLWithPathSel, nsString(path))
			pendingDownloads.Store(downloadKey(download), url)
			invokeBlock(completion, url)
		} else {
			invokeBlock(completion, objc.ID(0))
		}
		return
	}

	panel := objc.ID(nsSavePanelClass).Send(savePanelSel)
	panel.Send(setNameFieldStringValueSel, nsString(name))
	fm := objc.ID(nsFileManagerClass).Send(defaultManagerSel)
	home := objc.ID(fm).Send(homeDirectoryForCurrentUserSel)
	panel.Send(setDirectoryURLSel, home)

	result := panel.Send(runModalSel)
	if result != 0 {
		url := panel.Send(panelURLSel)
		pendingDownloads.Store(downloadKey(download), objc.ID(url))
		invokeBlock(completion, url)
	} else {
		invokeBlock(completion, objc.ID(0))
	}
}
