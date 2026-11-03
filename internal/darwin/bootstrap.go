//go:build darwin

package darwin

import "github.com/opentoys/webview/internal/bridgejs"

func bootstrapJS(names []string) string {
	return bridgejs.Transport(`window.webkit.messageHandlers.webviewBridge.postMessage(JSON.stringify(msg));`) + bridgejs.Proxies(bridgejs.Functions(names))
}
