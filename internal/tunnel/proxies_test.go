package tunnel

import (
	"io"
	"testing"

	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
	// ParseRawConfig links its temporary settings hook from executor.
	_ "github.com/metacubex/mihomo/hub/executor"
	"github.com/metacubex/mihomo/tunnel"
	"github.com/metacubex/mihomo/tunnel/statistic"
)

type selectorConnection struct {
	statistic.Tracker
	closed bool
}

func (*selectorConnection) ID() string      { return "libclash-selector-test" }
func (*selectorConnection) Chains() C.Chain { return C.Chain{"select"} }
func (c *selectorConnection) Close() error  { c.closed = true; return nil }

func TestPatchSelectorFailurePreservesConnections(t *testing.T) {
	cfg, err := config.Parse([]byte(`
proxies:
  - {name: a, type: direct}
  - {name: b, type: direct}
proxy-groups:
  - {name: select, type: select, proxies: [a, b]}
rules: ["MATCH,DIRECT"]
`))
	if err != nil {
		t.Fatal(err)
	}
	oldProxies, oldProviders := tunnel.Proxies(), tunnel.Providers()
	tunnel.UpdateProxies(cfg.Proxies, cfg.Providers)
	t.Cleanup(func() {
		tunnel.UpdateProxies(oldProxies, oldProviders)
		for _, p := range cfg.Providers {
			if c, ok := p.(io.Closer); ok {
				c.Close()
			}
		}
	})
	conn := &selectorConnection{}
	statistic.DefaultManager.Join(conn)
	defer statistic.DefaultManager.Leave(conn)
	if PatchSelector("select", "missing") {
		t.Fatal("reported success for a missing proxy")
	}
	if conn.closed {
		t.Fatal("failed selection closed an existing connection")
	}
	if got := QueryProxyGroup("select", Default, nil).Now; got != "a" {
		t.Fatalf("selection changed to %q", got)
	}
	if !PatchSelector("select", "b") {
		t.Fatal("valid selection failed")
	}
	if !conn.closed {
		t.Fatal("successful selection did not close the old connection")
	}
	if got := QueryProxyGroup("select", Default, nil).Now; got != "b" {
		t.Fatalf("selection is %q", got)
	}
}
