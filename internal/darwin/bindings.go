//go:build darwin

package darwin

// Objective-C runtime loading and cached classes/selectors.

import (
	"errors"
	"fmt"
	"sync"

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

// WKNavigationResponsePolicy values.
const (
	wkNavigationResponsePolicyAllow    int64 = 1
	wkNavigationResponsePolicyDownload int64 = 2
)

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
	setContentSizeSel                                     objc.SEL
	setContentMinSizeSel                                  objc.SEL
	setContentMaxSizeSel                                  objc.SEL
	setStyleMaskSel                                       objc.SEL
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
	responseSel                                           objc.SEL
	valueForHTTPHeaderFieldSel                            objc.SEL
	canShowMIMETypeSel                                    objc.SEL
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

// probeOnce / probeErr: lazy framework loading. Converted from init() so that
// Probe() can return an error instead of panicking, allowing New() to fall back
// to another backend when Cocoa/WebKit are unavailable.
var (
	probeOnce sync.Once
	probeErr  error
)

func probe() {
	probeOnce.Do(func() {
		// AppKit and WebKit are not linked into a CGO_ENABLED=0 binary, so
		// load them explicitly before looking up their classes.
		for _, fw := range []string{
			"/System/Library/Frameworks/Cocoa.framework/Cocoa",
			"/System/Library/Frameworks/WebKit.framework/WebKit",
		} {
			if _, err := purego.Dlopen(fw, purego.RTLD_GLOBAL|purego.RTLD_LAZY); err != nil {
				probeErr = fmt.Errorf("webview: failed to load %s: %w", fw, err)
				return
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
		setContentSizeSel = objc.RegisterName("setContentSize:")
		setContentMinSizeSel = objc.RegisterName("setContentMinSize:")
		setContentMaxSizeSel = objc.RegisterName("setContentMaxSize:")
		setStyleMaskSel = objc.RegisterName("setStyleMask:")
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
		responseSel = objc.RegisterName("response")
		valueForHTTPHeaderFieldSel = objc.RegisterName("valueForHTTPHeaderField:")
		canShowMIMETypeSel = objc.RegisterName("canShowMIMEType")
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

		registerDelegateClasses()
	})
}

// AppKit work is thread-affine: the NSApplication adopts whichever thread first
// creates it, and all windows/views must be created on that same thread. The Go
// runtime can migrate goroutines between OS threads, so all AppKit calls go
// through a single dedicated host thread running [NSApp run]. Cross-thread
// commands are dispatched via performSelectorOnMainThread:withObject:waitUntilDone:.
