//go:build linux

package linux

import "github.com/opentoys/webview/internal/bridgejs"

func bootstrapJS() string {
	return bridgejs.Transport(`window.webkit.messageHandlers.webviewBridge.postMessage(JSON.stringify(msg));`)
}
