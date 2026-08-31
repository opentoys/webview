//go:build linux

package linux

import "sync"

// --- instance + dispatch registries ----------------------------------------

var (
	regMu     sync.Mutex
	registry  = map[uintptr]*gtk{}
	engineSeq uintptr

	dispatchMu  sync.Mutex
	dispatchMap = map[uintptr]func(){}
	dispatchSeq uintptr
)

func registerPlatform(p *gtk) uintptr {
	regMu.Lock()
	engineSeq++
	id := engineSeq
	registry[id] = p
	regMu.Unlock()
	return id
}

func unregisterPlatform(id uintptr) {
	regMu.Lock()
	delete(registry, id)
	regMu.Unlock()
}

func lookupPlatform(id uintptr) *gtk {
	regMu.Lock()
	defer regMu.Unlock()
	return registry[id]
}

func dispatchMain(f func()) {
	dispatchMu.Lock()
	dispatchSeq++
	id := dispatchSeq
	dispatchMap[id] = f
	dispatchMu.Unlock()
	gIdleAddFull(gPriorityHighIdle, dispatchSourceFn, id, 0)
}
