package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Ember-Moth/libclash/internal/config"
	C "github.com/metacubex/mihomo/constant"
)

func TestSetupConfigPreservesPreviousFileOnValidationFailure(t *testing.T) {
	home := t.TempDir()
	oldHome := C.Path.HomeDir()
	C.SetHomeDir(home)
	stateMu.Lock()
	oldState := state
	stateMu.Unlock()
	t.Cleanup(func() {
		C.SetHomeDir(oldHome)
		stateMu.Lock()
		state = oldState
		stateMu.Unlock()
		config.SetExternalController("")
		config.SetMixedPort(0)
	})
	path := filepath.Join(home, "config.yaml")
	const good = "proxies: [{name: test, type: direct}]\nrules: [\"MATCH,DIRECT\"]\n"
	if err := os.WriteFile(path, []byte(good), 0600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"proxies: [", "proxies: [{name: bad, type: nonexistent}]"} {
		source := filepath.Join(t.TempDir(), "replacement.yaml")
		if err := os.WriteFile(source, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		req, err := json.Marshal(setupRequest{HomeDir: home, ConfigPath: source})
		if err != nil {
			t.Fatal(err)
		}
		if err := setupConfig(string(req)); err == nil {
			t.Fatal("accepted invalid configuration")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != good {
			t.Fatalf("lost old configuration: %q, %v", got, err)
		}
	}
}
