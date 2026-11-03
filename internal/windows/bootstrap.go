//go:build windows

package windows

import "github.com/opentoys/webview/internal/bridgejs"

func bootstrapJS(names []string) string {
	return bridgejs.Transport(`window.chrome.webview.postMessage(JSON.stringify(msg));`) + bridgejs.Proxies(bridgejs.Functions(names))
}
