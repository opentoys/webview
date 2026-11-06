//go:build linux

package linux

import (
	"strings"
	"testing"
)

func TestShowDevToolsRequiresDebugAndRunningWindow(t *testing.T) {
	p := &gtk{}
	if err := p.ShowDevTools(); err == nil || !strings.Contains(err.Error(), "Debug") {
		t.Fatalf("disabled: %v", err)
	}
	p.Debug = true
	if err := p.ShowDevTools(); err == nil || !strings.Contains(err.Error(), "running window") {
		t.Fatalf("not started: %v", err)
	}
}
