package config

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	C "github.com/metacubex/mihomo/constant"
)

const testProfile = "proxies:\n  - name: test\n    type: direct\nrules:\n  - MATCH,DIRECT\n"

func profileHome(t *testing.T) string {
	t.Helper()
	oldHome := C.Path.HomeDir()
	home := t.TempDir()
	C.SetHomeDir(home)
	t.Cleanup(func() { C.SetHomeDir(oldHome) })
	return home
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("read %s: got %q, err %v; want %q", path, got, err, want)
	}
}

func TestFetchAndValidPreservesProfileOnFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		truncated bool
	}{
		{"http error", http.StatusServiceUnavailable, testProfile, false},
		{"invalid yaml", http.StatusOK, "proxies: [", false},
		{"invalid proxy", http.StatusOK, "proxies: [{name: bad, type: nonexistent}]", false},
		{"interrupted download", http.StatusOK, testProfile, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := profileHome(t)
			path := filepath.Join(home, "config.yaml")
			if err := os.WriteFile(path, []byte(testProfile), 0600); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.truncated {
					w.Header().Set("Content-Length", fmt.Sprint(len(tc.body)+100))
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			if err := FetchAndValid(home, server.URL, true, nil); err == nil {
				t.Fatal("expected fetch/validation failure")
			}
			assertFile(t, path, testProfile)
			entries, err := filepath.Glob(filepath.Join(home, ".*"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary files remain: %v (%v)", entries, err)
			}
		})
	}
}

func TestFetchAndValidCommitsValidatedProfile(t *testing.T) {
	home := profileHome(t)
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, testProfile)
	}))
	defer server.Close()
	if err := FetchAndValid(home, server.URL, true, nil); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, testProfile)
	// A cached profile must not need a reachable subscription server.
	if err := FetchAndValid(home, "invalid://cached", false, nil); err != nil {
		t.Fatal(err)
	}
}

func TestFetchAndValidReportsProviderFailure(t *testing.T) {
	home := profileHome(t)
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte(testProfile), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/provider" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		fmt.Fprintf(w, "proxy-providers:\n  remote:\n    type: http\n    url: http://%s/provider\n    path: remote.yaml\n", r.Host)
	}))
	defer server.Close()
	err := FetchAndValid(home, server.URL, true, nil)
	if err == nil || !strings.Contains(err.Error(), `provider "remote"`) || !strings.Contains(err.Error(), "502") {
		t.Fatalf("expected provider HTTP failure, got %v", err)
	}
	assertFile(t, path, testProfile)
}

func TestFetchPreservesProviderOnHTTPError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider.yaml")
	if err := os.WriteFile(path, []byte("last good provider"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "not a provider")
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	if err := fetch(u, path); err == nil {
		t.Fatal("expected HTTP failure")
	}
	assertFile(t, path, "last good provider")
}

type failingReader struct{}

func (failingReader) Read(p []byte) (int, error) {
	return copy(p, "partial"), errors.New("read failed")
}

func TestAtomicWritePreservesDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "override.json")
	if err := os.WriteFile(path, []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomically(path, failingReader{}); err == nil {
		t.Fatal("expected read failure")
	}
	assertFile(t, path, "previous")
}
