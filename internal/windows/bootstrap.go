//go:build windows

package windows

import "github.com/opentoys/webview/internal/bridgejs"

func bootstrapJS() string {
	return bridgejs.Transport(`window.chrome.webview.postMessage(JSON.stringify(msg));`)
}
