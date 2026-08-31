//go:build linux

package linux

import (
	"fmt"
	"os"

	"github.com/ebitengine/purego"
)

// --- download interception (native save dialog) ------------------------------

// downloadDecidePolicyFn is no longer connected. Downloads are handled
// exclusively via the network-session "download-started" signal, which only
// fires for actual downloads and avoids the GLib assertion crash that
// decide-policy triggers on WebKitNavigationPolicyDecision objects.
// Kept as a no-op to avoid breaking connectDownloadCallbacks().
func downloadDecidePolicyFn() uintptr {
	if downloadDecidePolicy != 0 {
		return downloadDecidePolicy
	}
	downloadDecidePolicy = purego.NewCallback(func(webview, decision, decisionType, userData uintptr) uintptr {
		return 0
	})
	return downloadDecidePolicy
}

// downloadDecideDestFn handles a WebKitDownload's "decide-destination" signal.
// We show our native save dialog seeded with the suggested name and return TRUE
// to let WebKit handle the download to the chosen path.
func downloadDecideDestFn() uintptr {
	if downloadDecideDest != 0 {
		return downloadDecideDest
	}
	downloadDecideDest = purego.NewCallback(func(download, suggested, userData uintptr) uintptr {
		fmt.Fprintf(os.Stderr, "webview: decide-destination download=%x suggested=%x\n", download, suggested)
		name := "download"
		if suggested != 0 {
			if n := cstr(suggested); n != "" {
				name = n
			}
		}
		fmt.Fprintf(os.Stderr, "webview: decide-destination name=%q\n", name)
		p := lookupPlatform(userData)
		if p == nil {
			fmt.Fprintf(os.Stderr, "webview: decide-destination lookupPlatform nil\n")
			return 0
		}
		// download-decide-destination fires on the GLib thread pool, not GTK main
		// thread. showSaveDialog must run on GTK thread or it panics. Dispatch
		// the dialog to the main thread and return TRUE to pause the download
		// until the dialog completes.
		dialogName := name
		gIdleAddFull(gPriorityInIdle, purego.NewCallback(func(data uintptr) uintptr {
			regMu.Lock()
			p := registry[data]
			regMu.Unlock()
			if p == nil {
				return gSourceRemove
			}
			fmt.Fprintf(os.Stderr, "webview: showSaveDialog(%q) on GTK thread\n", dialogName)
			_, ok := p.showSaveDialog(dialogName)
			if !ok {
				fmt.Fprintf(os.Stderr, "webview: download cancelled\n")
				return gSourceRemove
			}
			// Dialog done; let WebKit proceed with the selected path.
			return gSourceRemove
		}), userData, 0)
		return 1 // TRUE: we handle destination asynchronously
	})
	return downloadDecideDest
}

var (
	downloadDecidePolicy uintptr
	downloadDecideDest   uintptr
	downloadStartedVar   uintptr
	downloadFinishedVar  uintptr
	downloadFailedVar    uintptr
	dialogResponse       uintptr
)

func downloadFinishedFn() uintptr {
	if downloadFinishedVar != 0 {
		return downloadFinishedVar
	}
	downloadFinishedVar = purego.NewCallback(func(download, userData uintptr) uintptr {
		gObjectUnref(download)
		return 0
	})
	return downloadFinishedVar
}

func downloadFailedFn() uintptr {
	if downloadFailedVar != 0 {
		return downloadFailedVar
	}
	downloadFailedVar = purego.NewCallback(func(download, errorPtr, userData uintptr) uintptr {
		gObjectUnref(download)
		return 0
	})
	return downloadFailedVar
}

// downloadStartedFn handles the WebKitNetworkSession "download-started" signal:
// (session, download, user_data). We take a reference on the download object
// and wire up decide-destination / finished / failed callbacks, then let WebKit
// proceed. Returning TRUE would suppress the default handling — we return 0 to
// let WebKit continue normally after our hooks are attached.
func downloadStartedFn() uintptr {
	if downloadStartedVar != 0 {
		return downloadStartedVar
	}
	downloadStartedVar = purego.NewCallback(func(session, download, userData uintptr) uintptr {
		// Guard against nil download (can happen on non-download navigation).
		if download == 0 {
			fmt.Fprintf(os.Stderr, "webview: download-started nil download, skip\n")
			return 0
		}
		fmt.Fprintf(os.Stderr, "webview: download-started download=%x\n", download)
		gObjectRef(download)
		gSignalConnectData(download, "decide-destination", downloadDecideDestFn(), userData, 0, 0)
		gSignalConnectData(download, "finished", downloadFinishedFn(), download, 0, 0)
		gSignalConnectData(download, "failed", downloadFailedFn(), download, 0, 0)
		return 0
	})
	return downloadStartedVar
}

func connectDownloadCallbacks() {
	// Build callbacks eagerly so the decide-policy / download-started hooks can
	// reference them.
	downloadDecidePolicyFn()
	downloadDecideDestFn()
	downloadStartedFn()
	downloadFinishedFn()
	downloadFailedFn()
}
