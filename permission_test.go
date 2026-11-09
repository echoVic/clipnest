package main

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/permissionflow"
)

// TestOpenPermissionGuidesAccessibilityInTheBackground keeps the real Guide out
// of the test: it would open System Settings. The stub blocks until released,
// so OpenPermission returning at all shows the guide runs off the main thread
// instead of waiting for the user.
func TestOpenPermissionGuidesAccessibilityInTheBackground(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Accessibility is macOS only")
	}
	calls := make(chan permissionflow.GuideOptions, 1)
	release := make(chan struct{})
	guided := make(chan struct{})
	original := guideAccessibility
	guideAccessibility = func(ctx context.Context, options permissionflow.GuideOptions) error {
		defer close(guided)
		calls <- options
		<-release
		return nil
	}
	t.Cleanup(func() {
		close(release)
		<-guided
		guideAccessibility = original
	})

	if err := (&Desktop{}).OpenPermission(); err != nil {
		t.Fatalf("OpenPermission returned %v, want nil", err)
	}
	select {
	case options := <-calls:
		if options.Name != "Clipnest" {
			t.Fatalf("GuideOptions.Name = %q, want %q", options.Name, "Clipnest")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OpenPermission didn't call Guide in the background")
	}
}
