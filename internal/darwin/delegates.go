//go:build darwin

package darwin

// Objective-C delegate class registration and callbacks.

import (
	"fmt"
	"net/http"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

func registerDelegateClasses() {
	// windowShouldClose: returns whether the window should close when the user
	// clicks the close button. The window is the sender (one argument).
	windowShouldClose := func(id objc.ID, cmd objc.SEL, sender objc.ID) bool {
		return true
	}
	// windowWillClose: signals Close() semantics when the user closes the window
	// via the titlebar button. Runs on the host thread (delegate callbacks are
	// delivered there), so call signalExit, not Close() (which would deadlock on
	// mainThread).
	windowWillClose := func(id objc.ID, cmd objc.SEL, window objc.ID) {
		if p := activePlatform; p != nil {
			p.signalExit()
		}
	}
	// applicationShouldTerminateAfterLastWindowClosed: keeps the app alive after
	// the last window closes so Run() only returns via Close().
	terminateAfterLastWindowClosed := func(id objc.ID, cmd objc.SEL, app objc.ID) bool {
		return false
	}
	var err error
	windowDelegateClass, err = objc.RegisterClass(
		"GoWebviewWindowDelegate",
		objc.GetClass("NSObject"),
		nil,
		nil,
		[]objc.MethodDef{
			{Cmd: objc.RegisterName("windowShouldClose:"), Fn: windowShouldClose},
			{Cmd: objc.RegisterName("windowWillClose:"), Fn: windowWillClose},
			{Cmd: objc.RegisterName("applicationShouldTerminateAfterLastWindowClosed:"), Fn: terminateAfterLastWindowClosed},
		},
	)
	if err != nil {
		probeErr = fmt.Errorf("webview: register class: %w", err)
		return
	}

	// userContentController:didReceiveScriptMessage: is called on the host thread
	// when JS runs postMessage on this class's registered handler. ObjC method
	// closures can't capture Go state, so route through the package-level
	// activePlatform set in setup().
	didReceiveMessage := func(id objc.ID, cmd objc.SEL, controller objc.ID, message objc.ID) {
		p := activePlatform
		if p == nil || p.MessageFunc == nil {
			return
		}
		body := objc.ID(message).Send(bodySel)
		text := goString(body)
		p.MessageFunc(text)
	}
	messageHandlerClass, err = objc.RegisterClass(
		"GoWebviewScriptHandler",
		objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("WKScriptMessageHandler")},
		nil,
		[]objc.MethodDef{
			{Cmd: objc.RegisterName("userContentController:didReceiveScriptMessage:"), Fn: didReceiveMessage},
		},
	)
	if err != nil {
		probeErr = fmt.Errorf("webview: register class: %w", err)
		return
	}

	// WKUIDelegate: handles JS alert/confirm/prompt. Runs on the WebKit
	// delivery thread, which is the host thread (same assumption as
	// MessageFunc/didReceiveScriptMessage). activePlatform is written once
	// in setup() on the host thread and read here on the host thread.
	uiDelegateClass, err = objc.RegisterClass(
		"GoWebviewUIDelegate",
		objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("WKUIDelegate")},
		nil,
		[]objc.MethodDef{
			// Alert: native NSAlert with OK button.
			{Cmd: objc.RegisterName("webView:runJavaScriptAlertPanelWithMessage:initiatedByFrame:completionHandler:"),
				Fn: func(id objc.ID, sel objc.SEL, webView objc.ID, msg objc.ID, frame objc.ID, completion objc.ID) {
					safe := objc.Block(completion).Copy()
					if safe == 0 {
						callBlock(completion)
						return
					}
					defer safe.Release()
					alert := objc.ID(nsAlertClass).Send(allocSel).Send(initSel)
					alert.Send(setMessageTextSel, nsString(goString(msg)))
					alert.Send(addButtonWithTitleSel, nsString("OK"))
					alert.Send(alertRunModalSel)
					callBlock(objc.ID(safe))
				}},
			// Confirm: native NSAlert with OK/Cancel buttons.
			{Cmd: objc.RegisterName("webView:runJavaScriptConfirmPanelWithMessage:initiatedByFrame:completionHandler:"),
				Fn: func(id objc.ID, sel objc.SEL, webView objc.ID, msg objc.ID, frame objc.ID, completion objc.ID) {
					safe := objc.Block(completion).Copy()
					if safe == 0 {
						callBlock(completion, int64(0))
						return
					}
					defer safe.Release()
					alert := objc.ID(nsAlertClass).Send(allocSel).Send(initSel)
					alert.Send(setMessageTextSel, nsString(goString(msg)))
					alert.Send(addButtonWithTitleSel, nsString("OK"))
					alert.Send(addButtonWithTitleSel, nsString("Cancel"))
					var confirm int64
					if alert.Send(alertRunModalSel) == 1000 {
						confirm = 1
					}
					callBlock(objc.ID(safe), confirm)
				}},
			// Prompt: native NSAlert with text field + OK/Cancel.
			{Cmd: objc.RegisterName("webView:runJavaScriptTextInputPanelWithPrompt:defaultText:initiatedByFrame:completionHandler:"),
				Fn: func(id objc.ID, sel objc.SEL, webView objc.ID, prompt objc.ID, defaultText objc.ID, frame objc.ID, completion objc.ID) {
					def := goString(defaultText)
					safe := objc.Block(completion).Copy()
					if safe == 0 {
						callBlock(completion, nsString(def))
						return
					}
					defer safe.Release()
					alert := objc.ID(nsAlertClass).Send(allocSel).Send(initSel)
					alert.Send(setMessageTextSel, nsString(goString(prompt)))
					alert.Send(addButtonWithTitleSel, nsString("OK"))
					alert.Send(addButtonWithTitleSel, nsString("Cancel"))
					tf := objc.ID(nsTextFieldClass).Send(allocSel).Send(initWithFrameOnlySel, rect(0, 0, 300, 24))
					tf.Send(objc.RegisterName("setStringValue:"), nsString(def))
					alert.Send(setAccessoryViewSel, tf)
					alert.Send(windowSel).Send(setInitialFirstResponderSel, tf)
					r := alert.Send(alertRunModalSel)
					if r == 1000 {
						callBlock(objc.ID(safe), tf.Send(stringValueSel))
					} else {
						callBlock(objc.ID(safe), objc.ID(0))
					}
				}},
			// <input type=file> → native NSOpenPanel (see openpanel.go).
			{Cmd: objc.RegisterName("webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:"),
				Fn: runOpenPanel},
		},
	)
	if err != nil {
		probeErr = fmt.Errorf("webview: register class: %w", err)
		return
	}

	// --- Download delegate (WKNavigationDelegate + WKDownloadDelegate) ---

	downloadDidBecome := func(id objc.ID, cmd objc.SEL, webView objc.ID, navAction objc.ID, download objc.ID) {
		// Each WKDownload needs its own delegate instance. alloc+init from the
		// registered class; WebKit retains via setDelegate:.
		inst := objc.ID(downloadDelegateClass).Send(allocSel)
		inst = inst.Send(initSel)
		download.Send(setDelegateSel, inst)
	}

	decideDestination := func(id objc.ID, cmd objc.SEL, download objc.ID, response objc.ID, suggestedFilename objc.ID, completion objc.ID) {
		if completion == 0 {
			return
		}
		safe := objc.Block(completion).Copy()
		if safe == 0 {
			return
		}
		defer safe.Release()
		p := activePlatform
		if p == nil {
			invokeBlock(objc.ID(safe), objc.ID(0))
			return
		}
		p.showSavePanel(download, suggestedFilename, objc.ID(safe))
	}

	downloadDidFinish := func(id objc.ID, cmd objc.SEL, download objc.ID) {
		dest, ok := pendingDownloads.LoadAndDelete(downloadKey(download))
		if !ok {
			return
		}
		destURL := dest.(objc.ID)
		if destURL == 0 {
			return
		}
		// Reveal in Finder.
		ws := objc.ID(nsWorkspaceClass).Send(sharedWorkspaceSel)
		if ws != 0 {
			arr := objc.ID(nsArrayClass).Send(arrayWithObjectsCountSel,
				unsafe.Pointer(&destURL), 1)
			ws.Send(activateFileViewerSel, arr)
		}
	}

	downloadDidFail := func(id objc.ID, cmd objc.SEL, download objc.ID, errObj objc.ID) {
		pendingDownloads.Delete(downloadKey(download))
	}

	// decidePolicyForNavigationResponse turns a response into a download instead
	// of letting the page preview/navigate. If WebKit can show the MIME type
	// (e.g. PDF), download only when the server requests an attachment via
	// Content-Disposition; otherwise (e.g. .zip, unknown type) always download.
	// The closure MUST take the full arg order (id, cmd, webView,
	// navigationResponse, decisionHandler) — a wrong arity makes purego misroute
	// args and WebKit treats the method as unimplemented (silently Allow).
	decidePolicyForNavigationResponse := func(id objc.ID, cmd objc.SEL, webView objc.ID, navigationResponse objc.ID, decisionHandler objc.ID) {
		policy := wkNavigationResponsePolicyAllow
		if objc.ID(navigationResponse).Send(canShowMIMETypeSel) != 0 {
			resp := objc.ID(navigationResponse).Send(responseSel)
			if resp != 0 && objc.ID(resp).Send(respondsToSelectorSel, valueForHTTPHeaderFieldSel) != 0 {
				cd := objc.ID(resp).Send(valueForHTTPHeaderFieldSel, nsString("Content-Disposition"))
				if cd != 0 && strings.Contains(strings.ToLower(goString(cd)), "attachment") {
					policy = wkNavigationResponsePolicyDownload
				}
			}
		} else {
			policy = wkNavigationResponsePolicyDownload
		}
		callBlock(decisionHandler, policy)
	}
	// A completion callback is intentionally limited to the phase: WKNavigation
	// can expose full request URLs and those must not reach the debug log.
	didFinishNavigation := func(id objc.ID, cmd objc.SEL, webView objc.ID, navigation objc.ID) {
		if p := activePlatform; p != nil {
			p.Logger.Log(BackendName, "navigate", map[string]string{"phase": "completed"})
		}
	}

	// Response-stage handoff: when the policy above returns Download, WebKit
	// calls this with the navigation response (not the action) to finish the
	// WKDownload. Give each download its own delegate.
	downloadDidBecomeFromResponse := func(id objc.ID, cmd objc.SEL, webView objc.ID, navigationResponse objc.ID, download objc.ID) {
		inst := objc.ID(downloadDelegateClass).Send(allocSel)
		inst = inst.Send(initSel)
		download.Send(setDelegateSel, inst)
	}

	downloadDelegateClass, err = objc.RegisterClass(
		"GoWebviewDownloadDelegate",
		objc.GetClass("NSObject"),
		[]*objc.Protocol{
			objc.GetProtocol("WKNavigationDelegate"),
			objc.GetProtocol("WKDownloadDelegate"),
		},
		nil,
		[]objc.MethodDef{
			{Cmd: objc.RegisterName("webView:navigationAction:didBecomeDownload:"), Fn: downloadDidBecome},
			{Cmd: objc.RegisterName("webView:navigationResponse:didBecomeDownload:"), Fn: downloadDidBecomeFromResponse},
			{Cmd: objc.RegisterName("webView:decidePolicyForNavigationResponse:decisionHandler:"), Fn: decidePolicyForNavigationResponse},
			{Cmd: objc.RegisterName("webView:didFinishNavigation:"), Fn: didFinishNavigation},
			{Cmd: objc.RegisterName("download:decideDestinationUsingResponse:suggestedFilename:completionHandler:"), Fn: decideDestination},
			{Cmd: objc.RegisterName("downloadDidFinish:"), Fn: downloadDidFinish},
			{Cmd: objc.RegisterName("download:didFailWithError:"), Fn: downloadDidFail},
		},
	)
	if err != nil {
		probeErr = fmt.Errorf("webview: register class: %w", err)
		return
	}

	// Command handler: receives performSelectorOnMainThread: calls for
	// cross-thread dispatch. The ObjC method reads a function from the
	// command channel and executes it on the host thread.
	commandHandlerClass, err = objc.RegisterClass(
		"GoWebviewCommandHandler",
		objc.GetClass("NSObject"),
		nil,
		nil,
		[]objc.MethodDef{
			{Cmd: objc.RegisterName("runCommand:"), Fn: func(id objc.ID, cmd objc.SEL, obj objc.ID) {
				if fn := <-commandChan; fn != nil {
					fn()
				}
			}},
		},
	)
	if err != nil {
		probeErr = fmt.Errorf("webview: register class: %w", err)
		return
	}

	// Menu item action handler: dispatches via a tag→callback map.
	menuCallbackMu.Lock()
	menuCallbacks = map[uintptr]func(){}
	menuCallbackMu.Unlock()
	menuHandlerClass, err = objc.RegisterClass(
		"GoWebviewMenuHandler",
		objc.GetClass("NSObject"),
		nil,
		nil,
		[]objc.MethodDef{
			{Cmd: menuItemSelectedSel, Fn: func(self objc.ID, cmd objc.SEL, sender objc.ID) {
				tag := sender.Send(objc.RegisterName("tag"))
				menuCallbackMu.Lock()
				cb := menuCallbacks[uintptr(tag)]
				menuCallbackMu.Unlock()
				if cb != nil {
					cb()
				}
			}},
		},
	)
	if err != nil {
		probeErr = fmt.Errorf("webview: register class: %w", err)
		return
	}

	// WKURLSchemeHandler: intercepts custom-scheme URL loads.
	startSchemeTask := func(id objc.ID, cmd objc.SEL, webView objc.ID, task objc.ID) {
		// Get the request URL.
		req := objc.ID(task).Send(schemeRequestSel)
		if req == 0 {
			return
		}
		nsURL := objc.ID(req).Send(panelURLSel) // "URL" selector
		if nsURL == 0 {
			return
		}
		urlStr := objc.ID(nsURL).Send(objc.RegisterName("absoluteString"))
		urlGo := goString(urlStr)

		// Extract scheme.
		scheme := ""
		if idx := strings.Index(urlGo, "://"); idx > 0 {
			scheme = urlGo[:idx]
		}
		handler, ok := schemeHandlerInstances[scheme]
		if !ok {
			return
		}

		// Get HTTP method.
		methodGo := http.MethodGet
		if m := objc.ID(req).Send(HTTPMethodSel); m != 0 {
			methodGo = goString(m)
		}

		// Get headers (may be nil).
		headersGo := http.Header{}
		if hdrs := objc.ID(req).Send(allHTTPHeaderFieldsSel); hdrs != 0 {
			headersGo = goMapFromNSDictionary(hdrs)
		}

		// Store task to prevent GC; get numeric ID for the closure.
		taskID := activeSchemeTasks.put(task)

		// Get HTTP body. Only body-bearing methods (POST/PUT/PATCH/DELETE)
		// can carry one; skip the lookup for GET/HEAD/TRACE/OPTIONS.
		var bodyGo []byte
		switch methodGo {
		case "GET", "HEAD", "TRACE", "OPTIONS":
		default:
			if httpBody := objc.ID(req).Send(objc.RegisterName("HTTPBody")); httpBody != 0 {
				bodyLen := int(httpBody.Send(objc.RegisterName("length")))
				if bodyLen > 0 {
					bodyGo = make([]byte, bodyLen)
					copy(bodyGo, unsafe.Slice((*byte)(unsafe.Pointer(httpBody.Send(objc.RegisterName("bytes")))), bodyLen))
				}
			}
		}

		sr := ResourceRequest{
			URL:     urlGo,
			Method:  methodGo,
			Headers: headersGo,
			Body:    bodyGo,
		}

		// failTask sends didFailWithError: to the task on the host thread.
		failTask := func() {
			mainThread(func() {
				t, ok := activeSchemeTasks.get(taskID)
				if !ok {
					return
				}
				activeSchemeTasks.delete(taskID)
				nsErr := objc.ID(nsErrorClass).Send(errorWithDomainSel,
					nsString("NSURLErrorDomain"), int64(-1100), objc.ID(0))
				t.Send(didFailWithErrorSel, nsErr)
			})
		}

		// Recover from handler panics — ObjC callbacks cannot unwind through C.
		defer func() {
			if r := recover(); r != nil {
				failTask()
			}
		}()

		handler(sr, func(resp *ResourceResponse) {
			// This callback may be called from any goroutine.
			// Dispatch to the host thread.
			if resp == nil {
				failTask()
				return
			}
			mainThread(func() {
				t, ok := activeSchemeTasks.get(taskID)
				if !ok {
					return // task was cancelled
				}
				activeSchemeTasks.delete(taskID)
				respondToSchemeTask(t, *resp, nsURL)
			})
		})
	}

	stopSchemeTask := func(id objc.ID, cmd objc.SEL, webView objc.ID, task objc.ID) {
		// Find and remove the task by iterating (task ID not available here).
		// WKURLSchemeTask identity: compare by pointer value.
		activeSchemeTasks.mu.Lock()
		var foundKey uintptr
		for k, v := range activeSchemeTasks.m {
			if v == task {
				foundKey = k
				break
			}
		}
		if foundKey != 0 {
			delete(activeSchemeTasks.m, foundKey)
		}
		activeSchemeTasks.mu.Unlock()
		if foundKey != 0 {
			task.Send(releaseSel)
		}
	}

	schemeHandlerClass, err = objc.RegisterClass(
		"GoWebviewSchemeHandler",
		objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("WKURLSchemeHandler")},
		nil,
		[]objc.MethodDef{
			{Cmd: objc.RegisterName("webView:startURLSchemeTask:"), Fn: startSchemeTask},
			{Cmd: objc.RegisterName("webView:stopURLSchemeTask:"), Fn: stopSchemeTask},
		},
	)
	if err != nil {
		probeErr = fmt.Errorf("webview: register class: %w", err)
		return
	}
}
