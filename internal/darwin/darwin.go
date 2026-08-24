//go:build darwin

package darwin

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/opentoys/webview/internal/types"
)

var (
	errNoWindow  = errors.New("darwin: failed to alloc NSWindow")
	errNoWebView = errors.New("darwin: failed to alloc WKWebView")
)

// Re-export shared types from types.
type SizeHint = types.SizeHint

const (
	SizeNone  = types.SizeNone
	SizeMin   = types.SizeMin
	SizeMax   = types.SizeMax
	SizeFixed = types.SizeFixed
)

type ResourceRequest = types.ResourceRequest
type ResourceResponse = types.ResourceResponse
type ResourceHandler = types.ResourceHandler
type Menu = types.Menu
type MenuItem = types.MenuItem

// NSApplicationActivationPolicyRegular = 0.
const activationRegular = 0

// NSWindow styleMask bits (NSWindowStyleMask).
const (
	styleTitled    = 1 << 0
	styleClosable  = 1 << 1
	styleResizable = 1 << 3
)

// NSBackingStoreBuffered = 2.
const backingBuffered = 2

// NSAutoresizingMaskOptions: NSViewWidthSizable|NSViewMaxXMargin|
// NSViewHeightSizable|NSViewMaxYMargin = 2|4|16|32 = 54. Keeps the Web
// Inspector pane (a sibling view) from pushing the webview out of bounds.
const webviewAutoresizingMask = 54

// windowDelegateClass is registered once at package init; re-registering the
// same name panics.
var windowDelegateClass objc.Class

// messageHandlerClass receives JS postMessage calls. Registered once at package
// init, like windowDelegateClass.
var messageHandlerClass objc.Class

// uiDelegateClass handles JS alert/confirm/prompt dialogs via WKUIDelegate.
// Registered once at package init; one instance per Platform.
var uiDelegateClass objc.Class

// downloadDelegateClass handles WKNavigationDelegate (didBecomeDownload:)
// and WKDownloadDelegate (decideDestination/didFinish/didFail). Registered
// once at package init; the navigation delegate is one per Platform, and
// per-download delegate instances are created in didBecomeDownload:.
var downloadDelegateClass objc.Class

// commandHandlerClass receives performSelectorOnMainThread: calls for
// cross-thread dispatch to the host thread. Registered once at package init.
var commandHandlerClass objc.Class

// schemeHandlerClass implements WKURLSchemeHandler for custom URL schemes.
// Registered once at package init; one instance per scheme.
var schemeHandlerClass objc.Class

// menuHandlerClass receives menu item actions via performSelector:withObject:.
// Registered once at package init; uses a tag→callback map for dispatch.
var menuHandlerClass objc.Class

// commandChan carries closures from any goroutine to be executed on the host
// thread (via performSelectorOnMainThread:YES).
var commandChan = make(chan func(), 64)

// scriptHandler keeps the active message handler instance alive. addScriptMessageHandler:
// does not retain its handler, so it must outlive the UCC or messages stop.
var scriptHandler objc.ID

// schemeHandlerInstances maps scheme name → Go handler. Written once per
// scheme in setup(), read from ObjC callbacks on the host thread.
var schemeHandlerInstances = map[string]ResourceHandler{}

var menuCallbackMu sync.Mutex
var menuCallbacks = map[uintptr]func(){}

// schemeTaskStore holds WKURLSchemeTask objects by numeric ID, preventing GC
// before the async Go handler calls back.
type schemeTaskStore struct {
	mu   sync.Mutex
	m    map[uintptr]objc.ID
	next uintptr
}

func (s *schemeTaskStore) put(task objc.ID) uintptr {
	// Retain the task so it stays alive for the async callback. WebKit may
	// release its reference after stopURLSchemeTask: fires.
	task.Send(retainSel)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	s.m[s.next] = task
	return s.next
}

func (s *schemeTaskStore) get(id uintptr) (objc.ID, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.m[id]
	return t, ok
}

func (s *schemeTaskStore) delete(id uintptr) {
	s.mu.Lock()
	t, ok := s.m[id]
	if ok {
		delete(s.m, id)
	}
	s.mu.Unlock()
	if ok && t != 0 {
		t.Send(releaseSel)
	}
}

var activeSchemeTasks = &schemeTaskStore{m: map[uintptr]objc.ID{}}

// Cached ObjC classes (avoids repeated hash-table lookups in GetClass).
var (
	nsStringClass          objc.Class
	nsURLClass             objc.Class
	nsURLRequestClass      objc.Class
	nsWindowClass          objc.Class
	nsViewClass            objc.Class
	nsAppClass             objc.Class
	nsAutoreleasePoolClass objc.Class
	wkUCCClass             objc.Class
	wkWebViewConfigClass   objc.Class
	wkWebViewClass         objc.Class
	nsMenuClass            objc.Class
	nsMenuItemClass        objc.Class
	wkDataStoreClass       objc.Class
	wkOpenPanelParamsClass objc.Class
	nsOpenPanelClass       objc.Class
	nsFileManagerClass     objc.Class
	nsArrayClass           objc.Class
	nsMutableArrayClass    objc.Class
	nsSavePanelClass       objc.Class
	nsWorkspaceClass       objc.Class
	wkDownloadClass        objc.Class
	nsUserDefaultsClass    objc.Class
	nsHTTPURLResponseClass objc.Class
	nsDataClass            objc.Class
	nsDictionaryClass      objc.Class
	nsAlertClass           objc.Class
	nsTextFieldClass       objc.Class
	nsErrorClass           objc.Class
	wkUserScriptClass      objc.Class
)

// Cached ObjC selectors (avoids repeated hash-table lookups in RegisterName).
var (
	allocSel                                              objc.SEL
	initSel                                               objc.SEL
	newSel                                                objc.SEL
	UTF8StringSel                                         objc.SEL
	stringWithUTF8Sel                                     objc.SEL
	bodySel                                               objc.SEL
	setTitleSel                                           objc.SEL
	loadRequestSel                                        objc.SEL
	URLWithStringSel                                      objc.SEL
	requestWithURLSel                                     objc.SEL
	evaluateJSSel                                         objc.SEL
	loadHTMLStringSel                                     objc.SEL
	orderOutSel                                           objc.SEL
	setDelegateSel                                        objc.SEL
	initWithContentRectSel                                objc.SEL
	setContentViewSel                                     objc.SEL
	centerSel                                             objc.SEL
	makeKeyAndOrderFrontSel                               objc.SEL
	addScriptMessageHandlerSel                            objc.SEL
	setUserContentControllerSel                           objc.SEL
	initWithFrameSel                                      objc.SEL
	initWithFrameOnlySel                                  objc.SEL
	setUIDelegateSel                                      objc.SEL
	sharedApplicationSel                                  objc.SEL
	setActivationPolicySel                                objc.SEL
	activateIgnoringOtherAppsSel                          objc.SEL
	finishLaunchingSel                                    objc.SEL
	stopSel                                               objc.SEL
	performSelectorOnMainThreadWithObjectWaitUntilDoneSel objc.SEL
	windowWillCloseSel                                    objc.SEL
	initWithTitleSel                                      objc.SEL
	initWithTitleOnlySel                                  objc.SEL
	autoreleaseSel                                        objc.SEL
	separatorItemSel                                      objc.SEL
	setSubmenuSel                                         objc.SEL
	setMainMenuSel                                        objc.SEL
	setKeyEquivalentModifierMaskSel                       objc.SEL
	setTagSel                                             objc.SEL
	setTargetSel                                          objc.SEL
	setKeyEquivalentSel                                   objc.SEL
	addItemSel                                            objc.SEL
	menuItemSelectedSel                                   objc.SEL
	setWebsiteDataStoreSel                                objc.SEL
	nonPersistentDataStoreSel                             objc.SEL
	defaultDataStoreSel                                   objc.SEL
	allowsMultipleSelectionSel                            objc.SEL
	allowsDirectoriesSel                                  objc.SEL
	allowedContentTypesSel                                objc.SEL
	openPanelSel                                          objc.SEL
	setCanChooseFilesSel                                  objc.SEL
	setCanChooseDirectoriesSel                            objc.SEL
	setAllowedContentTypesSel                             objc.SEL
	setAllowsMultipleSelectionSel                         objc.SEL
	setDirectoryURLSel                                    objc.SEL
	setAllowedFileTypesSel                                objc.SEL
	runModalSel                                           objc.SEL
	defaultManagerSel                                     objc.SEL
	homeDirectoryForCurrentUserSel                        objc.SEL
	fileURLWithPathSel                                    objc.SEL
	arrayWithObjectsCountSel                              objc.SEL
	URLsSel                                               objc.SEL
	setNavigationDelegateSel                              objc.SEL
	setNameFieldStringValueSel                            objc.SEL
	suggestedFilenameSel                                  objc.SEL
	sharedWorkspaceSel                                    objc.SEL
	activateFileViewerSel                                 objc.SEL
	savePanelSel                                          objc.SEL
	panelURLSel                                           objc.SEL
	preferencesSel                                        objc.SEL
	setValueForKeySel                                     objc.SEL
	numberWithBoolSel                                     objc.SEL
	standardUserDefaultsSel                               objc.SEL
	setBoolForKeySel                                      objc.SEL
	setInspectableSel                                     objc.SEL
	addSubviewSel                                         objc.SEL
	setAutoresizesSubviewsSel                             objc.SEL
	setAutoresizingMaskSel                                objc.SEL
	setURLSchemeHandlerForURLSchemeSel                    objc.SEL
	schemeRequestSel                                      objc.SEL // [task request]
	HTTPMethodSel                                         objc.SEL // [request HTTPMethod]
	allHTTPHeaderFieldsSel                                objc.SEL // [request allHTTPHeaderFields]
	didReceiveResponseSel                                 objc.SEL // [task didReceiveResponse:]
	didReceiveDataSel                                     objc.SEL // [task didReceiveData:]
	schemeFinishSel                                       objc.SEL // [task didFinish]
	initWithURLStatusCodeHTTPVersionHeaderFieldsSel       objc.SEL
	dataWithBytesLengthSel                                objc.SEL
	dictionaryWithObjectsForKeysCountSel                  objc.SEL
	retainSel                                             objc.SEL
	releaseSel                                            objc.SEL
	respondsToSelectorSel                                 objc.SEL
	setMessageTextSel                                     objc.SEL
	setInformativeTextSel                                 objc.SEL
	addButtonWithTitleSel                                 objc.SEL
	alertRunModalSel                                      objc.SEL
	setAccessoryViewSel                                   objc.SEL
	nsTextFieldSel                                        objc.SEL
	stringValueSel                                        objc.SEL
	setInitialFirstResponderSel                           objc.SEL
	windowSel                                             objc.SEL
	errorWithDomainSel                                    objc.SEL // [NSError errorWithDomain:code:userInfo:]
	didFailWithErrorSel                                   objc.SEL // [task didFailWithError:]
	arrayInstanceSel                                      objc.SEL // [NSMutableArray array]
	addObjectSel                                          objc.SEL // [array addObject:]
	setMessageSel                                         objc.SEL // [panel setMessage:]
	addUserScriptSel                                      objc.SEL // [ucc addUserScript:]
	removeAllUserScriptsSel                               objc.SEL // [ucc removeAllUserScripts]
	initWithSourceInjectionTimeForMainFrameOnlySel        objc.SEL // [WKUserScript initWithSource:injectionTime:forMainFrameOnly:]
)

// activePlatform is the Platform whose webview is currently set up. Process-
// global because handler methods are registered per-class, not per-instance.
// Written once in setup() on the host thread; read from the host thread the
// same way, so no lock is needed.
var activePlatform *Platform

func init() {
	// AppKit and WebKit are not linked into a CGO_ENABLED=0 binary, so load them
	// explicitly before looking up their classes.
	for _, fw := range []string{
		"/System/Library/Frameworks/Cocoa.framework/Cocoa",
		"/System/Library/Frameworks/WebKit.framework/WebKit",
	} {
		if _, err := purego.Dlopen(fw, purego.RTLD_GLOBAL|purego.RTLD_LAZY); err != nil {
			panic(err)
		}
	}

	// Cache frequently used ObjC classes and selectors.
	nsStringClass = objc.GetClass("NSString")
	nsURLClass = objc.GetClass("NSURL")
	nsURLRequestClass = objc.GetClass("NSURLRequest")
	nsWindowClass = objc.GetClass("NSWindow")
	nsViewClass = objc.GetClass("NSView")
	nsAppClass = objc.GetClass("NSApplication")
	nsAutoreleasePoolClass = objc.GetClass("NSAutoreleasePool")
	wkUCCClass = objc.GetClass("WKUserContentController")
	wkWebViewConfigClass = objc.GetClass("WKWebViewConfiguration")
	wkWebViewClass = objc.GetClass("WKWebView")
	nsMenuClass = objc.GetClass("NSMenu")
	nsMenuItemClass = objc.GetClass("NSMenuItem")
	wkDataStoreClass = objc.GetClass("WKWebsiteDataStore")
	wkOpenPanelParamsClass = objc.GetClass("WKOpenPanelParameters")
	nsOpenPanelClass = objc.GetClass("NSOpenPanel")
	nsFileManagerClass = objc.GetClass("NSFileManager")
	nsArrayClass = objc.GetClass("NSArray")
	nsMutableArrayClass = objc.GetClass("NSMutableArray")
	nsSavePanelClass = objc.GetClass("NSSavePanel")
	nsWorkspaceClass = objc.GetClass("NSWorkspace")
	wkDownloadClass = objc.GetClass("WKDownload")
	nsUserDefaultsClass = objc.GetClass("NSUserDefaults")
	nsHTTPURLResponseClass = objc.GetClass("NSHTTPURLResponse")
	nsDataClass = objc.GetClass("NSData")
	nsDictionaryClass = objc.GetClass("NSDictionary")
	nsAlertClass = objc.GetClass("NSAlert")
	nsTextFieldClass = objc.GetClass("NSTextField")
	nsErrorClass = objc.GetClass("NSError")
	wkUserScriptClass = objc.GetClass("WKUserScript")

	allocSel = objc.RegisterName("alloc")
	initSel = objc.RegisterName("init")
	newSel = objc.RegisterName("new")
	UTF8StringSel = objc.RegisterName("UTF8String")
	stringWithUTF8Sel = objc.RegisterName("stringWithUTF8String:")
	bodySel = objc.RegisterName("body")
	setTitleSel = objc.RegisterName("setTitle:")
	loadRequestSel = objc.RegisterName("loadRequest:")
	URLWithStringSel = objc.RegisterName("URLWithString:")
	requestWithURLSel = objc.RegisterName("requestWithURL:")
	evaluateJSSel = objc.RegisterName("evaluateJavaScript:completionHandler:")
	loadHTMLStringSel = objc.RegisterName("loadHTMLString:baseURL:")
	orderOutSel = objc.RegisterName("orderOut:")
	setDelegateSel = objc.RegisterName("setDelegate:")
	initWithContentRectSel = objc.RegisterName("initWithContentRect:styleMask:backing:defer:")
	setContentViewSel = objc.RegisterName("setContentView:")
	centerSel = objc.RegisterName("center")
	makeKeyAndOrderFrontSel = objc.RegisterName("makeKeyAndOrderFront:")
	addScriptMessageHandlerSel = objc.RegisterName("addScriptMessageHandler:name:")
	setUserContentControllerSel = objc.RegisterName("setUserContentController:")
	initWithFrameSel = objc.RegisterName("initWithFrame:configuration:")
	initWithFrameOnlySel = objc.RegisterName("initWithFrame:")
	setUIDelegateSel = objc.RegisterName("setUIDelegate:")
	sharedApplicationSel = objc.RegisterName("sharedApplication")
	setActivationPolicySel = objc.RegisterName("setActivationPolicy:")
	activateIgnoringOtherAppsSel = objc.RegisterName("activateIgnoringOtherApps:")
	finishLaunchingSel = objc.RegisterName("finishLaunching")
	stopSel = objc.RegisterName("stop:")
	performSelectorOnMainThreadWithObjectWaitUntilDoneSel = objc.RegisterName("performSelectorOnMainThread:withObject:waitUntilDone:")
	windowWillCloseSel = objc.RegisterName("windowWillClose:")
	initWithTitleSel = objc.RegisterName("initWithTitle:action:keyEquivalent:")
	initWithTitleOnlySel = objc.RegisterName("initWithTitle:")
	autoreleaseSel = objc.RegisterName("autorelease")
	retainSel = objc.RegisterName("retain")
	releaseSel = objc.RegisterName("release")
	respondsToSelectorSel = objc.RegisterName("respondsToSelector:")
	separatorItemSel = objc.RegisterName("separatorItem")
	setSubmenuSel = objc.RegisterName("setSubmenu:")
	setMainMenuSel = objc.RegisterName("setMainMenu:")
	setKeyEquivalentModifierMaskSel = objc.RegisterName("setKeyEquivalentModifierMask:")
	setTagSel = objc.RegisterName("setTag:")
	setTargetSel = objc.RegisterName("setTarget:")
	setKeyEquivalentSel = objc.RegisterName("setKeyEquivalent:")
	addItemSel = objc.RegisterName("addItem:")
	menuItemSelectedSel = objc.RegisterName("menuItemSelected:")
	setWebsiteDataStoreSel = objc.RegisterName("setWebsiteDataStore:")
	nonPersistentDataStoreSel = objc.RegisterName("nonPersistentDataStore")
	defaultDataStoreSel = objc.RegisterName("defaultDataStore")
	allowsMultipleSelectionSel = objc.RegisterName("allowsMultipleSelection")
	allowsDirectoriesSel = objc.RegisterName("allowsDirectories")
	allowedContentTypesSel = objc.RegisterName("allowedContentTypes")
	openPanelSel = objc.RegisterName("openPanel")
	setCanChooseFilesSel = objc.RegisterName("setCanChooseFiles:")
	setCanChooseDirectoriesSel = objc.RegisterName("setCanChooseDirectories:")
	setAllowedContentTypesSel = objc.RegisterName("setAllowedContentTypes:")
	setAllowsMultipleSelectionSel = objc.RegisterName("setAllowsMultipleSelection:")
	setDirectoryURLSel = objc.RegisterName("setDirectoryURL:")
	setAllowedFileTypesSel = objc.RegisterName("setAllowedFileTypes:")
	runModalSel = objc.RegisterName("runModal")
	defaultManagerSel = objc.RegisterName("defaultManager")
	homeDirectoryForCurrentUserSel = objc.RegisterName("homeDirectoryForCurrentUser")
	fileURLWithPathSel = objc.RegisterName("fileURLWithPath:")
	arrayWithObjectsCountSel = objc.RegisterName("arrayWithObjects:count:")
	URLsSel = objc.RegisterName("URLs")
	setNavigationDelegateSel = objc.RegisterName("setNavigationDelegate:")
	setNameFieldStringValueSel = objc.RegisterName("setNameFieldStringValue:")
	suggestedFilenameSel = objc.RegisterName("suggestedFilename")
	sharedWorkspaceSel = objc.RegisterName("sharedWorkspace")
	activateFileViewerSel = objc.RegisterName("activateFileViewerSelectingURLs:")
	savePanelSel = objc.RegisterName("savePanel")
	panelURLSel = objc.RegisterName("URL")
	preferencesSel = objc.RegisterName("preferences")
	setValueForKeySel = objc.RegisterName("setValue:forKey:")
	numberWithBoolSel = objc.RegisterName("numberWithBool:")
	standardUserDefaultsSel = objc.RegisterName("standardUserDefaults")
	setBoolForKeySel = objc.RegisterName("setBool:forKey:")
	setInspectableSel = objc.RegisterName("setInspectable:")
	addSubviewSel = objc.RegisterName("addSubview:")
	setAutoresizesSubviewsSel = objc.RegisterName("setAutoresizesSubviews:")
	setAutoresizingMaskSel = objc.RegisterName("setAutoresizingMask:")
	setURLSchemeHandlerForURLSchemeSel = objc.RegisterName("setURLSchemeHandler:forURLScheme:")
	schemeRequestSel = objc.RegisterName("request")
	HTTPMethodSel = objc.RegisterName("HTTPMethod")
	allHTTPHeaderFieldsSel = objc.RegisterName("allHTTPHeaderFields")
	didReceiveResponseSel = objc.RegisterName("didReceiveResponse:")
	didReceiveDataSel = objc.RegisterName("didReceiveData:")
	schemeFinishSel = objc.RegisterName("didFinish")
	initWithURLStatusCodeHTTPVersionHeaderFieldsSel = objc.RegisterName("initWithURL:statusCode:HTTPVersion:headerFields:")
	dataWithBytesLengthSel = objc.RegisterName("dataWithBytes:length:")
	dictionaryWithObjectsForKeysCountSel = objc.RegisterName("dictionaryWithObjects:forKeys:count:")
	setMessageTextSel = objc.RegisterName("setMessageText:")
	setInformativeTextSel = objc.RegisterName("setInformativeText:")
	addButtonWithTitleSel = objc.RegisterName("addButtonWithTitle:")
	alertRunModalSel = objc.RegisterName("runModal")
	setAccessoryViewSel = objc.RegisterName("setAccessoryView:")
	nsTextFieldSel = objc.RegisterName("textFieldWithString:")
	stringValueSel = objc.RegisterName("stringValue")
	setInitialFirstResponderSel = objc.RegisterName("setInitialFirstResponder:")
	windowSel = objc.RegisterName("window")
	errorWithDomainSel = objc.RegisterName("errorWithDomain:code:userInfo:")
	didFailWithErrorSel = objc.RegisterName("didFailWithError:")
	arrayInstanceSel = objc.RegisterName("array")
	addObjectSel = objc.RegisterName("addObject:")
	setMessageSel = objc.RegisterName("setMessage:")
	addUserScriptSel = objc.RegisterName("addUserScript:")
	removeAllUserScriptsSel = objc.RegisterName("removeAllUserScripts")
	initWithSourceInjectionTimeForMainFrameOnlySel = objc.RegisterName("initWithSource:injectionTime:forMainFrameOnly:")

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
		panic(err)
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
		panic(err)
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
		panic(err)
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
			{Cmd: objc.RegisterName("download:decideDestinationUsingResponse:suggestedFilename:completionHandler:"), Fn: decideDestination},
			{Cmd: objc.RegisterName("downloadDidFinish:"), Fn: downloadDidFinish},
			{Cmd: objc.RegisterName("download:didFailWithError:"), Fn: downloadDidFail},
		},
	)
	if err != nil {
		panic(err)
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
		panic(err)
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
		panic(err)
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
		methodGo := "GET"
		if m := objc.ID(req).Send(HTTPMethodSel); m != 0 {
			methodGo = goString(m)
		}

		// Get headers (may be nil).
		headersGo := map[string]string{}
		if hdrs := objc.ID(req).Send(allHTTPHeaderFieldsSel); hdrs != 0 {
			headersGo = goMapFromNSDictionary(hdrs)
		}

		// Store task to prevent GC; get numeric ID for the closure.
		taskID := activeSchemeTasks.put(task)

		// Get HTTP body (may be nil for GET).
		var bodyGo []byte
		if httpBody := objc.ID(req).Send(objc.RegisterName("HTTPBody")); httpBody != 0 {
			bodyLen := int(httpBody.Send(objc.RegisterName("length")))
			if bodyLen > 0 {
				bodyGo = make([]byte, bodyLen)
				copy(bodyGo, unsafe.Slice((*byte)(unsafe.Pointer(httpBody.Send(objc.RegisterName("bytes")))), bodyLen))
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
		panic(err)
	}
}

// AppKit work is thread-affine: the NSApplication adopts whichever thread first
// creates it, and all windows/views must be created on that same thread. The Go
// runtime can migrate goroutines between OS threads, so all AppKit calls go
// through a single dedicated host thread running [NSApp run]. Cross-thread
// commands are dispatched via performSelectorOnMainThread:withObject:waitUntilDone:.
var (
	hostOnce  sync.Once
	hostReady chan struct{} // closed once the host loop is running
)

// startAppHost launches the single AppKit host thread if not already running,
// then blocks until it is ready.
func startAppHost() {
	hostOnce.Do(func() {
		hostReady = make(chan struct{})
		go hostLoop()
	})
	<-hostReady
}

// hostLoop runs on one pinned OS thread for the life of the process: it owns
// the NSApplication and pumps its event loop via [NSApp run], which processes
// all events, timers, and the main dispatch queue. Cross-thread commands arrive
// via performSelectorOnMainThread: (which [NSApp run] dispatches from the run
// loop) and are read from the commandChan.
func hostLoop() {
	runtime.LockOSThread()
	app := objc.ID(nsAppClass).Send(sharedApplicationSel)
	if app == 0 {
		panic("darwin: no shared NSApplication")
	}
	app.Send(setActivationPolicySel, activationRegular)
	app.Send(activateIgnoringOtherAppsSel, true)
	// Complete app launch (menu/activation setup) as a nib-less program must.
	app.Send(finishLaunchingSel)
	close(hostReady)
	// Pump the run loop. [NSApp run] processes all events and dispatches
	// performSelectorOnMainThread: calls (which read from commandChan and
	// execute the queued Go functions).
	app.Send(objc.RegisterName("run"))
}

// mainThread runs fn on the AppKit host thread, blocking until it completes.
// Before the host has started (no window exists yet) it is a no-op, which makes
// SetTitle/SetHTML/Navigate/Eval safe to call before Run().
func mainThread(fn func()) {
	select {
	case <-hostReady:
	default:
		return
	}
	commandChan <- fn
	objc.ID(commandHandlerClass).Send(allocSel).Send(
		performSelectorOnMainThreadWithObjectWaitUntilDoneSel,
		objc.RegisterName("runCommand:"),
		0, true)
}

type Platform struct {
	window           objc.ID
	webview          objc.ID
	delegate         objc.ID
	uiDelegate       objc.ID // WKUIDelegate instance; kept alive for the webview.
	downloadDelegate objc.ID // WKNavigationDelegate+WKDownloadDelegate instance.
	// ucc keeps the WKUserContentController alive; the handler instance lives
	// in the package-level scriptHandler (registered class is process-global).
	ucc objc.ID

	// MessageFunc is called with the string body of each JS postMessage to
	// window.webkit.messageHandlers.webviewBridge.
	MessageFunc func(string)
	// BoundFuncs returns the JS-visible func names; the bootstrap script
	// defines window.<name> stubs from it at page start.
	BoundFuncs func() []string
	// OpenPanelFunc overrides the native NSOpenPanel sheet for <input type=file>.
	// When set, WebKit does not show the default panel; the app must call
	// callback with the absolute paths the user chose, or (nil,false) to
	// cancel. callback is async and safe from any goroutine.
	OpenPanelFunc func(params OpenPanelParams, callback func(paths []string, ok bool))
	// DownloadFunc overrides the native NSSavePanel for file downloads.
	// When set, the app must call callback with the absolute save path,
	// or "" to cancel. callback is async and safe from any goroutine.
	DownloadFunc func(suggestedFilename string, callback func(savePath string))

	// Debug enables WebKit Inspector (right-click → Inspect Element) on macOS
	// and dev tools on Windows. Set via Options.Debug.
	Debug bool
	// Incognito makes the webview use a non-persistent (in-memory) website data
	// store: no cookies/cache/localStorage written to disk.
	Incognito bool
	// DataDir sets the persistent website data store directory (cookies, cache,
	// localStorage). Empty = WebKit default. Ignored when Incognito is set.
	DataDir string

	mu     sync.Mutex
	closed bool
	// pendingTitle is set by SetTitle before the window exists and applied in
	// setup(), so a title set before Run() is not lost.
	pendingTitle string
	// pendingHTML is set by SetHTML before the webview exists and loaded in
	// setup(), so HTML set before Run() is not silently dropped.
	pendingHTML string
	// pendingURL is set by Navigate before the webview exists and loaded in
	// setup(), so a navigation set before Run() is not silently dropped.
	pendingURL string
	// runDone is closed by Close() to signal Run() to return.
	runDone chan struct{}
	// schemeHandlers stores registered resource handlers, keyed by scheme
	// name (without "://"). Populated before Run() via InterceptResource,
	// wired to WKWebViewConfiguration in setup().
	schemeHandlers map[string]ResourceHandler
	// userScriptSrcs accumulates JS sources added via Init(). They are
	// injected into WKUserContentController so they run at document start
	// for every page load.
	userScriptSrcs []string

	// pendingMenus stores menus set via SetMenus before Run().
	pendingMenus  []Menu
	hasCustomMenus bool
}

func New() *Platform {
	return &Platform{
		runDone:        make(chan struct{}),
		schemeHandlers: make(map[string]ResourceHandler),
	}
}

// SetMenus replaces the native menu bar. Safe to call before or after Run().
func (p *Platform) SetMenus(menus []Menu) {
	p.pendingMenus = menus
	p.hasCustomMenus = len(menus) > 0
	// If the host thread is already running, apply immediately.
	if p.window != 0 {
		mainThread(func() { p.applyMenus(menus) })
	}
}

// MainThread runs f on the AppKit host thread, blocking until it completes.
func (p *Platform) MainThread(f func()) { mainThread(f) }

func (p *Platform) Run() error {
	startAppHost()
	var setupErr error
	mainThread(func() { setupErr = p.setup() })
	if setupErr != nil {
		return setupErr
	}
	<-p.runDone
	return nil
}

func (p *Platform) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.runDone)
	p.mu.Unlock()

	// Hide the window so a closed platform doesn't linger on screen while the
	// next window in the same process is running. Read window under lock; the
	// command handler reads it again to guard against the window being
	// destroyed between scheduling and execution.
	mainThread(func() {
		p.mu.Lock()
		w := p.window
		p.window = 0
		p.webview = 0
		p.mu.Unlock()
		if w != 0 {
			w.Send(orderOutSel, 0)
		}
	})
	return nil
}

// InterceptResource registers a resource handler for the given URL scheme.
// Must be called before Run(). scheme is the URL scheme without "://"
// (e.g. "app").
func (p *Platform) InterceptResource(scheme string, handler ResourceHandler) {
	p.schemeHandlers[scheme] = handler
}

// signalExit makes Run() return without closing the window. Callable from the
// host thread (windowWillClose:) or any other thread (Close()). Uses a non-
// blocking channel send so it is safe on the host thread where Close()'s
// mainThread orderOut would deadlock. Sets closed=true so a subsequent Close()
// does not try to orderOut: a window that is already being destroyed.
func (p *Platform) signalExit() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.window = 0
	p.webview = 0
	p.mu.Unlock()
	// Do NOT call [NSApp stop:] here — it kills the host thread's run loop,
	// which deadlocks any subsequent mainThread() call (process-global
	// singleton). The run loop lives until the process exits; that's fine.
	select {
	case p.runDone <- struct{}{}:
	default:
	}
}

// setupMainMenu installs a minimal main menu bar with an App menu and an Edit
// menu. Without a nib, a bare AppKit app has no menu, so Cmd-C/Cmd-V key
// equivalents (routed to the first responder via the main menu's Edit items)
// silently do nothing. Follows the reference webview_go implementation
// (webview.h: 2317-2348): each Edit item carries a real key equivalent
// ("x"/"c"/"v"/"a") so the responder chain resolves cut:/copy:/paste:/selectAll:.
// Runs on the host thread from setup().
func setupMainMenu() {
	menu := objc.ID(nsMenuClass).Send(allocSel)
	menu = menu.Send(initSel)

	// Application menu.
	appItem := objc.ID(nsMenuItemClass).Send(allocSel)
	appItem = appItem.Send(initWithTitleSel, nsString(""), 0, nsString(""))
	appMenu := objc.ID(nsMenuClass).Send(allocSel)
	appMenu = appMenu.Send(initWithTitleOnlySel, nsString(""))
	appMenu.Send(autoreleaseSel)
	appItem.Send(setSubmenuSel, appMenu)
	menu.Send(addItemSel, appItem)

	// Edit menu: Cut/Copy/Paste/Select All with Cmd shortcuts.
	editItem := objc.ID(nsMenuItemClass).Send(allocSel)
	editItem = editItem.Send(initWithTitleSel, nsString("Edit"), 0, nsString(""))
	editMenu := objc.ID(nsMenuClass).Send(allocSel)
	editMenu = editMenu.Send(initWithTitleOnlySel, nsString("Edit"))
	editMenu.Send(autoreleaseSel)
	editItem.Send(setSubmenuSel, editMenu)
	menu.Send(addItemSel, editItem)

	for _, e := range []struct {
		title, action, key string
		mods               uintptr
	}{
		{"Undo", "undo:", "z", 1 << 20},                  // Cmd
		{"Redo", "redo:", "z", (1 << 20) | (1 << 17)}, // Cmd+Shift
	} {
		item := objc.ID(nsMenuItemClass).Send(allocSel)
		item = item.Send(initWithTitleSel, nsString(e.title), objc.RegisterName(e.action), nsString(e.key))
		item.Send(setKeyEquivalentModifierMaskSel, e.mods)
		editMenu.Send(addItemSel, item)
	}
	sep := objc.ID(nsMenuItemClass).Send(separatorItemSel)
	editMenu.Send(addItemSel, sep)
	for _, e := range []struct{ title, action, key string }{
		{"Cut", "cut:", "x"},
		{"Copy", "copy:", "c"},
		{"Paste", "paste:", "v"},
	} {
		item := objc.ID(nsMenuItemClass).Send(allocSel)
		item = item.Send(initWithTitleSel, nsString(e.title), objc.RegisterName(e.action), nsString(e.key))
		editMenu.Send(addItemSel, item)
	}
	editMenu.Send(addItemSel, objc.ID(nsMenuItemClass).Send(separatorItemSel))
	selectAll := objc.ID(nsMenuItemClass).Send(allocSel)
	selectAll = selectAll.Send(initWithTitleSel, nsString("Select All"), objc.RegisterName("selectAll:"), nsString("a"))
	editMenu.Send(addItemSel, selectAll)

	app := objc.ID(nsAppClass).Send(sharedApplicationSel)
	app.Send(setMainMenuSel, menu)
}

// applyMenus builds and installs a native NSMenu bar from the given Menu slice.
// Runs on the host thread.
func (p *Platform) applyMenus(menus []Menu) {
	// Clear old callbacks.
	menuCallbackMu.Lock()
	menuCallbacks = map[uintptr]func(){}
	menuCallbackMu.Unlock()

	handler := objc.ID(menuHandlerClass).Send(allocSel).Send(initSel)

	menuBar := objc.ID(nsMenuClass).Send(allocSel).Send(initSel)
	tag := uintptr(1)

	for _, m := range menus {
		topItem := objc.ID(nsMenuItemClass).Send(allocSel)
		topItem = topItem.Send(initWithTitleSel, nsString(m.Label), 0, nsString(""))
		submenu := objc.ID(nsMenuClass).Send(allocSel).Send(initWithTitleOnlySel, nsString(m.Label))
		submenu.Send(autoreleaseSel)
		topItem.Send(setSubmenuSel, submenu)
		menuBar.Send(addItemSel, topItem)

		for _, mi := range m.Items {
			if mi.Separator {
				submenu.Send(addItemSel, objc.ID(nsMenuItemClass).Send(separatorItemSel))
				continue
			}
			item := objc.ID(nsMenuItemClass).Send(allocSel)
			item = item.Send(initWithTitleSel, nsString(mi.Label), menuItemSelectedSel, nsString(""))
			item.Send(setTargetSel, handler)
			item.Send(setTagSel, tag)

			if mi.Shortcut != "" {
				key, mods := parseShortcut(mi.Shortcut)
				if key != "" {
					item.Send(setKeyEquivalentSel, nsString(key))
					item.Send(setKeyEquivalentModifierMaskSel, mods)
				}
			}

			if mi.Action != nil {
				menuCallbackMu.Lock()
				menuCallbacks[tag] = mi.Action
				menuCallbackMu.Unlock()
			}
			tag++

			submenu.Send(addItemSel, item)
		}
	}

	app := objc.ID(nsAppClass).Send(sharedApplicationSel)
	app.Send(setMainMenuSel, menuBar)
}

// parseShortcut parses a shortcut string like "Cmd+Shift+Z" into (key, mods).
func parseShortcut(s string) (string, uintptr) {
	var mods uintptr
	key := ""
	for _, part := range strings.Split(s, "+") {
		switch strings.TrimSpace(part) {
		case "Cmd", "Meta":
			mods |= 1 << 20 // NSEventModifierFlagCommand
		case "Shift":
			mods |= 1 << 17 // NSEventModifierFlagShift
		case "Ctrl", "Control":
			mods |= 1 << 18 // NSEventModifierFlagControl
		case "Alt", "Option":
			mods |= 1 << 19 // NSEventModifierFlagOption
		default:
			key = strings.ToLower(strings.TrimSpace(part))
		}
	}
	return key, mods
}

// setupDataStore returns the WKWebsiteDataStore for the platform: a non-
// persistent (in-memory, incognito) store when Incognito is set, else the
// default persistent store. WKWebsiteDataStore has no public initializer and
// the private custom-directory path is unavailable, so DataDir (a Windows/
// Linux concept) is ignored on darwin. Runs on the host thread from setup().
func (p *Platform) setupDataStore() objc.ID {
	if p.Incognito {
		return objc.ID(wkDataStoreClass).Send(nonPersistentDataStoreSel)
	}
	return objc.ID(wkDataStoreClass).Send(defaultDataStoreSel)
}

// setup creates the NSWindow and WKWebView, then shows them. Runs on the host
// thread (called via mainThread from Run).
func (p *Platform) setup() error {
	// Route script messages of the (process-global) handler class to this
	// platform. Called on the host thread; the didReceiveMessage handler
	// closure reads activePlatform on the same thread, so no lock is needed.
	activePlatform = p

	// Delegate: one per Platform, kept alive as a field.
	delegate := objc.ID(windowDelegateClass).Send(allocSel)
	delegate = delegate.Send(initSel)
	p.delegate = delegate

	w := objc.ID(nsWindowClass).Send(allocSel)
	if w == 0 {
		return errNoWindow
	}
	styleMask := styleTitled | styleClosable | styleResizable
	w = w.Send(initWithContentRectSel,
		rect(0, 0, 800, 600), styleMask, backingBuffered, false)
	p.mu.Lock()
	p.window = w
	p.mu.Unlock()
	w.Send(setDelegateSel, delegate)

	// UCC receives script messages. addScriptMessageHandler:name: does not retain
	// the handler, so keep both the UCC and handler alive on the Platform.
	ucc := objc.ID(wkUCCClass).Send(allocSel)
	if ucc == 0 {
		return errors.New("darwin: failed to alloc WKUserContentController")
	}
	ucc = ucc.Send(initSel)
	p.ucc = ucc
	handler := objc.ID(messageHandlerClass).Send(allocSel)
	handler = handler.Send(initSel)
	scriptHandler = handler
	ucc.Send(addScriptMessageHandlerSel, handler, nsString("webviewBridge"))

	// Inject any user scripts accumulated before Run().
	p.rebuildScriptsLocked()

	config := objc.ID(wkWebViewConfigClass).Send(allocSel)
	config = config.Send(initSel)
	config.Send(setUserContentControllerSel, ucc)
	// Website data store: incognito (non-persistent), custom dir, or default.
	config.Send(setWebsiteDataStoreSel, p.setupDataStore())
	// Register custom URL scheme handlers on the configuration.
	// Must happen before WKWebView is created.
	for scheme, handler := range p.schemeHandlers {
		schemeHandlerInstances[scheme] = handler
		inst := objc.ID(schemeHandlerClass).Send(allocSel)
		inst = inst.Send(initSel)
		config.Send(setURLSchemeHandlerForURLSchemeSel, inst, nsString(scheme))
	}

	// Enable WebKit Inspector (right-click → Inspect Element) when Debug is set.
	if p.Debug {
		prefs := config.Send(preferencesSel)
		yesNum := objc.ID(objc.GetClass("NSNumber")).Send(numberWithBoolSel, true)
		// Private preference that exposes the "Inspect Element" context menu item.
		prefs.Send(setValueForKeySel, yesNum, nsString("developerExtrasEnabled"))
	}
	wv := objc.ID(wkWebViewClass).Send(allocSel)
	if wv == 0 {
		return errNoWebView
	}
	wv = wv.Send(initWithFrameSel, rect(0, 0, 800, 600), config)
	// macOS 13.3+ disables inspection by default; explicitly enable it.
	// https://webkit.org/blog/13936/enabling-the-inspection-of-web-content-in-apps/
	if p.Debug {
		wv.Send(setInspectableSel, true)
	}
	p.mu.Lock()
	p.webview = wv
	p.mu.Unlock()

	// WKUIDelegate handles JS alert/confirm/prompt.
	uiDelegate := objc.ID(uiDelegateClass).Send(allocSel)
	uiDelegate = uiDelegate.Send(initSel)
	p.uiDelegate = uiDelegate
	wv.Send(setUIDelegateSel, uiDelegate)

	// WKNavigationDelegate intercepts downloads; WKDownloadDelegate handles
	// per-download save panel + completion.
	dlDelegate := objc.ID(downloadDelegateClass).Send(allocSel)
	dlDelegate = dlDelegate.Send(initSel)
	p.downloadDelegate = dlDelegate
	wv.Send(setNavigationDelegateSel, dlDelegate)

	// Wrap the webview in a container NSView so the Web Inspector pane can be
	// added as a sibling (matches reference cocoa_webkit.hh set_up_widget).
	widget := objc.ID(nsViewClass).Send(allocSel)
	if widget == 0 {
		return errors.New("darwin: failed to alloc NSView")
	}
	widget = widget.Send(initWithFrameOnlySel, rect(0, 0, 800, 600))
	widget.Send(setAutoresizesSubviewsSel, true)
	wv.Send(setAutoresizingMaskSel, webviewAutoresizingMask)
	widget.Send(addSubviewSel, wv)

	w.Send(setContentViewSel, widget)
	w.Send(centerSel)
	w.Send(makeKeyAndOrderFrontSel, 0)
	// Make webview first responder so it receives keyboard/mouse events.
	w.Send(objc.RegisterName("makeFirstResponder:"), wv)
	// Re-activate app to ensure window gets focus on modern macOS.
	objc.ID(nsAppClass).Send(sharedApplicationSel).Send(activateIgnoringOtherAppsSel, true)
	// Cmd-C/Cmd-V need an Edit menu (key equivalents route via the main menu).
	// Bare AppKit apps without a nib have no menu, so install one once.
	if p.hasCustomMenus {
		p.applyMenus(p.pendingMenus)
	} else {
		setupMainMenu()
	}

	// Apply a title set before Run().
	p.mu.Lock()
	title := p.pendingTitle
	p.pendingTitle = ""
	p.mu.Unlock()
	if title != "" {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, title)
		w.Send(setTitleSel, str)
	}

	// Apply HTML set before Run() (the webview now exists).
	p.mu.Lock()
	html := p.pendingHTML
	p.pendingHTML = ""
	p.mu.Unlock()
	if html != "" {
		html = prependBootstrap(html)
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, html)
		wv.Send(loadHTMLStringSel, str, objc.ID(0))
	}

	// Apply a URL set before Run() (the webview now exists). Tighter priority
	// than pending HTML: a pending HTML page wins, then pending URL (and any
	// WKUserScript still fires for the empty document loadRequest starts with).
	p.mu.Lock()
	url := p.pendingURL
	p.pendingURL = ""
	p.mu.Unlock()
	if url != "" {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, url)
		nsURL := objc.ID(nsURLClass).Send(URLWithStringSel, str)
		req := objc.ID(nsURLRequestClass).Send(requestWithURLSel, nsURL)
		wv.Send(loadRequestSel, req)
	}
	return nil
}

func (p *Platform) SetTitle(title string) error {
	p.mu.Lock()
	if p.window == 0 {
		// No window yet (called before Run): remember the title and apply it
		// once setup() creates the window.
		p.pendingTitle = title
		p.mu.Unlock()
		return nil
	}
	w := p.window
	p.mu.Unlock()
	mainThread(func() {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, title)
		w.Send(setTitleSel, str)
	})
	return nil
}

func (p *Platform) SetSize(width, height int, hint SizeHint) {
}

func (p *Platform) Navigate(url string) error {
	p.mu.Lock()
	wv := p.webview
	if wv == 0 {
		// No webview yet (called before Run): remember the URL and load it
		// once setup() creates the webview.
		p.pendingURL = url
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	mainThread(func() {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, url)
		nsURL := objc.ID(nsURLClass).Send(URLWithStringSel, str)
		req := objc.ID(nsURLRequestClass).Send(requestWithURLSel, nsURL)
		wv.Send(loadRequestSel, req)
	})
	return nil
}

// indexHead returns a byte index in html suitable for inserting a <script> tag
// so that it runs before any user-supplied script. Looks for <head>, </head>,
// <body>, or the first <script>, returning the index AFTER that tag or -1.
func indexHead(html string) int {
	lower := strings.ToLower(html)
	for _, tag := range []string{"<head>", "<head ", "<head\t", "<head\n"} {
		if i := strings.Index(lower, tag); i >= 0 {
			return i + len(tag)
		}
	}
	if i := strings.Index(lower, "</head>"); i >= 0 {
		return i
	}
	if i := strings.Index(lower, "<body"); i >= 0 {
		return i
	}
	if i := strings.Index(lower, "<script"); i >= 0 {
		return i
	}
	return -1
}

// boundFuncNames returns the JS-visible func names from the active platform's
// BoundFuncs. Used by prependBootstrap; activePlatform is set in setup() before
// any SetHTML path runs, so it is always current.
func boundFuncNames() []string {
	if p := activePlatform; p != nil && p.BoundFuncs != nil {
		return p.BoundFuncs()
	}
	return nil
}

// prependBootstrap inserts the bridge bootstrap (webviewBridge + func stubs) as
// an inline <script> so it is available before any user-supplied script runs.
func prependBootstrap(html string) string {
	if js := bootstrapJS(boundFuncNames()); js != "" {
		// HTML parsing closes <script> on </script>, </script>, or </SCRIPT>.
		// Escape </ sequences so the script body is safe inside the tag.
		js = strings.ReplaceAll(js, "</", `<\/`)
		tag := "<script>" + js + "</script>"
		if i := indexHead(html); i >= 0 {
			html = html[:i] + tag + html[i:]
		} else {
			html = tag + html
		}
	}
	return html
}

// looksLikeHTML returns true if body starts with common HTML markers.
func looksLikeHTML(body []byte) bool {
	s := strings.TrimSpace(string(body))
	s = strings.ToLower(s)
	return strings.HasPrefix(s, "<!doctype") || strings.HasPrefix(s, "<html") || strings.HasPrefix(s, "<head")
}

// respondToSchemeTask sends a ResourceResponse to a WKURLSchemeTask. Must be
// called on the host thread.
func respondToSchemeTask(task objc.ID, resp ResourceResponse, reqURL objc.ID) {
	if resp.StatusCode == 0 {
		resp.StatusCode = 200
	}

	// Inject bootstrap JS into HTML responses so Bind works on custom schemes.
	ct := resp.Headers["Content-Type"]
	if ct == "" {
		ct = resp.Headers["content-type"]
	}
	if strings.HasPrefix(ct, "text/html") || (ct == "" && len(resp.Body) > 0 && looksLikeHTML(resp.Body)) {
		resp.Body = []byte(prependBootstrap(string(resp.Body)))
	}

	// Build header NSDictionary.
	var hdrDict objc.ID
	if len(resp.Headers) > 0 {
		hdrDict = nsDictionary(resp.Headers)
	}

	// Create NSHTTPURLResponse.
	httpResp := objc.ID(nsHTTPURLResponseClass).Send(allocSel)
	httpResp = httpResp.Send(initWithURLStatusCodeHTTPVersionHeaderFieldsSel,
		reqURL, uintptr(resp.StatusCode), nsString("HTTP/1.1"), hdrDict)
	if httpResp != 0 {
		task.Send(didReceiveResponseSel, httpResp)
	}

	// Send body data.
	if len(resp.Body) > 0 {
		body := resp.Body
		nsData := objc.ID(nsDataClass).Send(dataWithBytesLengthSel,
			unsafe.Pointer(&body[0]), uintptr(len(body)))
		if nsData != 0 {
			task.Send(didReceiveDataSel, nsData)
		}
	}

	// Finish.
	task.Send(schemeFinishSel)
}

// nsDictionary creates an NSDictionary from a Go map[string]string.
func nsDictionary(m map[string]string) objc.ID {
	if len(m) == 0 {
		return 0
	}
	keys := make([]objc.ID, 0, len(m))
	vals := make([]objc.ID, 0, len(m))
	for k, v := range m {
		keys = append(keys, nsString(k))
		vals = append(vals, nsString(v))
	}
	return objc.ID(nsDictionaryClass).Send(dictionaryWithObjectsForKeysCountSel,
		unsafe.Pointer(&vals[0]), unsafe.Pointer(&keys[0]), uintptr(len(m)))
}

// goMapFromNSDictionary converts an NSDictionary (NSString→NSString) to a Go map.
// Uses objectForKey: with known keys or enumerates via block. Simplified: reads
// allKeys and iterates.
func goMapFromNSDictionary(dict objc.ID) map[string]string {
	out := map[string]string{}
	if dict == 0 {
		return out
	}
	// allKeys returns NSArray of keys.
	allKeysSel := objc.RegisterName("allKeys")
	keys := objc.ID(dict).Send(allKeysSel)
	if keys == 0 {
		return out
	}
	countSel := objc.RegisterName("count")
	objectAtIndexSel := objc.RegisterName("objectAtIndex:")
	n := int(keys.Send(countSel))
	for i := 0; i < n; i++ {
		key := objc.ID(keys).Send(objectAtIndexSel, uintptr(i))
		val := objc.ID(dict).Send(objc.RegisterName("objectForKey:"), key)
		if key != 0 && val != 0 {
			out[goString(key)] = goString(val)
		}
	}
	return out
}

func (p *Platform) SetHTML(html string) error {
	p.mu.Lock()
	wv := p.webview
	if wv == 0 {
		// No webview yet (called before Run): remember the HTML and load it
		// once setup() creates the webview.
		p.pendingHTML = html
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	html = prependBootstrap(html)
	// Use loadHTMLString:baseURL: to avoid data: URL encoding issues.
	mainThread(func() {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, html)
		wv.Send(loadHTMLStringSel, str, objc.ID(0))
	})
	return nil
}

// evalJS runs JS without a completion handler (fire-and-forget), blocking on
// the host thread.
func (p *Platform) evalJS(js string) {
	mainThread(func() { p.evalOnHost(js) })
}

// evalOnHost runs JS on the host thread; must be called from the host thread.
func (p *Platform) evalOnHost(js string) {
	p.mu.Lock()
	wv := p.webview
	p.mu.Unlock()
	if wv == 0 {
		return
	}
	str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, js)
	wv.Send(evaluateJSSel, str, objc.ID(0))
}

// EvalHost queues js to run on the host thread without blocking. Safe from any
// goroutine including host-thread callbacks (e.g. MessageFunc). Uses a
// non-blocking channel send + performSelectorOnMainThread:NO: if called from
// the host thread, the ObjC selector fires after the current callback returns;
// if from another thread, it fires on the next run loop iteration.
func (p *Platform) EvalHost(js string) {
	select {
	case <-hostReady:
	default:
		return
	}
	select {
	case commandChan <- func() { p.evalOnHost(js) }:
		// Command queued; trigger the ObjC selector to read and execute it.
		objc.ID(commandHandlerClass).Send(allocSel).Send(
			performSelectorOnMainThreadWithObjectWaitUntilDoneSel,
			objc.RegisterName("runCommand:"),
			0, false)
	default:
		// Channel full (unlikely) — run directly. Safe when called from the
		// host thread (MessageFunc callback). On other threads this is a race,
		// but channel-full implies 64+ pending evals, which is a bug anyway.
		p.evalOnHost(js)
	}
}

func (p *Platform) Eval(js string) error {
	p.evalJS(js)
	return nil
}

const wkInjectionTimeAtDocumentStart = 0

// Init registers JS to run at document start for every page load.
func (p *Platform) Init(js string) error {
	p.mu.Lock()
	p.userScriptSrcs = append(p.userScriptSrcs, js)
	if p.ucc != 0 {
		p.rebuildScriptsLocked()
	}
	p.mu.Unlock()
	return nil
}

// rebuildScriptsLocked re-injects all user scripts into the UCC.
// Caller must hold p.mu.
func (p *Platform) rebuildScriptsLocked() {
	p.ucc.Send(removeAllUserScriptsSel)
	for _, src := range p.userScriptSrcs {
		addWKUserScript(p.ucc, src)
	}
}

// addWKUserScript adds a WKUserScript to the given WKUserContentController.
func addWKUserScript(ucc objc.ID, src string) {
	s := objc.ID(wkUserScriptClass).Send(allocSel)
	s = s.Send(initWithSourceInjectionTimeForMainFrameOnlySel, nsString(src), wkInjectionTimeAtDocumentStart, true)
	ucc.Send(addUserScriptSel, s)
	s.Send(releaseSel)
}

// boundFuncNames returns the current bound function names from BoundFuncs.
func (p *Platform) boundFuncNames() []string {
	if p.BoundFuncs != nil {
		return p.BoundFuncs()
	}
	return nil
}
