//go:build windows

package main

import (
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// testWinVKeys points the switch at keys of its own under HKCU, which the
// test removes, so that it never changes Explorer's real settings.
func testWinVKeys(t *testing.T) winVKeys {
	t.Helper()
	base := `Software\ClipnestTest-` + strconv.Itoa(os.Getpid())
	k := winVKeys{advanced: base + `\Advanced`, clipnest: base + `\Clipnest`}
	t.Cleanup(func() {
		for _, path := range []string{k.advanced, k.clipnest, base} {
			if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil && !errors.Is(err, registry.ErrNotExist) {
				t.Error(err)
			}
		}
	})
	key, _, err := registry.CreateKey(registry.CURRENT_USER, k.advanced, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	key.Close()
	return k
}

func readDisabledHotkeys(t *testing.T, k winVKeys) (string, error) {
	t.Helper()
	key, err := registry.OpenKey(registry.CURRENT_USER, k.advanced, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	list, _, err := key.GetStringValue("DisabledHotkeys")
	return list, err
}

func TestWinVRegistryKeepsOtherLettersAndCleansUp(t *testing.T) {
	k := testWinVKeys(t)
	restarts := 0
	s := k.system(func() error { restarts++; return nil })
	key, _, err := registry.CreateKey(registry.CURRENT_USER, k.advanced, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := key.SetStringValue("DisabledHotkeys", "QE"); err != nil {
		t.Fatal(err)
	}
	key.Close()
	if err := turnOnWinV(s); err != nil {
		t.Fatal(err)
	}
	if list, err := readDisabledHotkeys(t, k); list != "QEV" || err != nil || restarts != 1 {
		t.Fatal(list, err, restarts)
	}
	if state, err := s.state(); state != winVAdded || err != nil {
		t.Fatal(state, err)
	}
	if err := turnOffWinV(s); err != nil {
		t.Fatal(err)
	}
	if list, err := readDisabledHotkeys(t, k); list != "QE" || err != nil || restarts != 2 {
		t.Fatal(list, err, restarts)
	}
	// Turning off leaves nothing of Clipnest's behind.
	if _, err := registry.OpenKey(registry.CURRENT_USER, k.clipnest, registry.QUERY_VALUE); !errors.Is(err, registry.ErrNotExist) {
		t.Fatal("state key left behind:", err)
	}
}

func TestWinVRegistryDeletesTheValueItCreated(t *testing.T) {
	k := testWinVKeys(t)
	s := k.system(func() error { return nil })
	if err := turnOnWinV(s); err != nil {
		t.Fatal(err)
	}
	if list, err := readDisabledHotkeys(t, k); list != "V" || err != nil {
		t.Fatal(list, err)
	}
	if err := turnOffWinV(s); err != nil {
		t.Fatal(err)
	}
	if _, err := readDisabledHotkeys(t, k); !errors.Is(err, registry.ErrNotExist) {
		t.Fatal("DisabledHotkeys left behind:", err)
	}
}

// Explorer inherits the environment of the process that restarts it, and
// every app started from the shell inherits Explorer's, so the restarter
// gets the user's own rather than Clipnest's.
func TestExplorerRestarterHasTheUsersEnvironment(t *testing.T) {
	t.Setenv("CLIPNEST_TEST_ONLY", "1")
	cmd, err := explorerRestarter()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != exe || !reflect.DeepEqual(cmd.Args[1:], []string{"--" + restartExplorerFlag}) {
		t.Fatal(cmd.Path, cmd.Args)
	}
	env := map[string]string{}
	for _, kv := range cmd.Env {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			env[strings.ToUpper(k)] = v
		}
	}
	if _, ok := env["CLIPNEST_TEST_ONLY"]; ok {
		t.Fatal("the restarter inherits Clipnest's environment")
	}
	for _, k := range []string{"SYSTEMROOT", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "PATH", "TEMP", "USERNAME", "USERDOMAIN"} {
		if env[k] == "" {
			t.Errorf("%s missing", k)
		}
	}
	// What differs from this process's environment, which came from Explorer.
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" && env[strings.ToUpper(k)] != v {
			t.Logf("differs: %s=%q, restarter %q", k, v, env[strings.ToUpper(k)])
		}
	}
}

func TestWinVRegistryLeavesAValueOfAnotherTypeAlone(t *testing.T) {
	k := testWinVKeys(t)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, k.advanced, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	if err := key.SetDWordValue("DisabledHotkeys", 7); err != nil {
		t.Fatal(err)
	}
	if err := turnOnWinV(k.system(func() error { t.Fatal("restarted Explorer"); return nil })); err == nil {
		t.Fatal("turned on over a value it cannot read")
	}
	if v, _, err := key.GetIntegerValue("DisabledHotkeys"); v != 7 || err != nil {
		t.Fatal(v, err)
	}
	if state, err := k.state(); state != winVOff || err != nil {
		t.Fatal(state, err)
	}
}
