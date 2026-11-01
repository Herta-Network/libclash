package dns

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/component/resolver"
	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
	coreDNS "github.com/metacubex/mihomo/dns"
	D "github.com/miekg/dns"
)

func localDNSServer(t *testing.T, ip string, counter *atomic.Int32) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	server := &D.Server{
		PacketConn:        conn,
		NotifyStartedFunc: func() { close(ready) },
		Handler: D.HandlerFunc(func(w D.ResponseWriter, req *D.Msg) {
			counter.Add(1)
			msg := new(D.Msg)
			msg.SetReply(req)
			if len(req.Question) > 0 && req.Question[0].Qtype == D.TypeA {
				msg.Answer = []D.RR{&D.A{Hdr: D.RR_Header{Name: req.Question[0].Name, Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 300}, A: net.ParseIP(ip)}}
			}
			w.WriteMsg(msg)
		}),
	}
	go server.ActivateAndServe()
	<-ready
	t.Cleanup(func() { server.Shutdown() })
	return conn.LocalAddr().String()
}

func answer(r resolver.Resolver, host string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg := new(D.Msg)
	msg.SetQuestion(host, D.TypeA)
	res, err := r.ExchangeContext(ctx, msg)
	if err != nil {
		return "", err
	}
	if len(res.Answer) != 1 {
		return "", fmt.Errorf("unexpected answer: %v", res)
	}
	return res.Answer[0].(*D.A).A.String(), nil
}

func TestHostDNSUpdatesAllResolutionPaths(t *testing.T) {
	var firstQueries, secondQueries atomic.Int32
	first := localDNSServer(t, "10.0.0.1", &firstQueries)
	second := localDNSServer(t, "10.0.0.2", &secondQueries)
	oldHome := C.Path.HomeDir()
	C.SetHomeDir(t.TempDir())
	t.Cleanup(func() { C.SetHomeDir(oldHome) })
	t.Cleanup(func() {
		cfg, _ := config.Parse(nil)
		ApplyConfig(cfg)
		if mainResolver.Invalid() || proxyResolver.Invalid() || directResolver.Invalid() {
			t.Error("reset retained the active DNS resolvers")
		}
		NotifyChanged("")
	})
	if err := NotifyChanged(first); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse([]byte(`
dns:
  enable: true
  enhanced-mode: redir-host
  use-hosts: false
  use-system-hosts: false
  nameserver: [system://]
  default-nameserver: [127.0.0.1]
  proxy-server-nameserver: [system://]
  direct-nameserver: [system://]
  nameserver-policy:
    '+.policy.test': system://
rules: ["MATCH,DIRECT"]
`))
	if err != nil {
		t.Fatal(err)
	}
	cfg.DNS.NameServerPolicy = append(cfg.DNS.NameServerPolicy, coreDNS.Policy{
		Domain:      "fixed.test",
		NameServers: []coreDNS.NameServer{{Net: "udp", Addr: first}},
	})
	ApplyConfig(cfg)
	paths := []resolver.Resolver{resolver.SystemResolver, resolver.DefaultResolver, resolver.ProxyServerHostResolver, resolver.DirectHostResolver}
	service := resolver.DefaultService
	mapper := resolver.DefaultHostMapper
	for _, update := range []struct{ addr, want string }{{first, "10.0.0.1"}, {second, "10.0.0.2"}} {
		if err := NotifyChanged(update.addr); err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			for _, host := range []string{"switch.test.", "switch.policy.test."} {
				got, err := answer(path, host)
				if err != nil || got != update.want {
					t.Fatalf("%s: got %q, %v; want %s", host, got, err, update.want)
				}
			}
		}
		msg := new(D.Msg)
		msg.SetQuestion("switch.test.", D.TypeA)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		res, err := service.ServeMsg(ctx, msg)
		cancel()
		if err != nil || len(res.Answer) != 1 || res.Answer[0].(*D.A).A.String() != update.want {
			t.Fatalf("TUN service did not update: %v, %v", res, err)
		}
		if got, err := answer(paths[1], "fixed.test."); err != nil || got != "10.0.0.1" {
			t.Fatalf("explicit DNS changed: %q, %v", got, err)
		}
		if resolver.DefaultHostMapper != mapper {
			t.Fatal("network change replaced the host/fake-IP mapper")
		}
	}
	if resolver.DefaultService != service {
		t.Fatal("network change replaced the live DNS service")
	}
	beforeFlush := secondQueries.Load()
	resolver.DefaultResolver.ClearCache()
	for _, path := range paths[1:] {
		if _, err := answer(path, "switch.test."); err != nil {
			t.Fatal(err)
		}
	}
	if secondQueries.Load() < beforeFlush+3 {
		t.Fatal("cache flush did not include proxy and direct DNS")
	}
	if err := NotifyChanged("not-an-ip"); err == nil {
		t.Fatal("accepted invalid DNS")
	}
	if got, err := answer(paths[0], "switch.test."); err != nil || got != "10.0.0.2" {
		t.Fatalf("invalid update changed DNS: %q %v", got, err)
	}

	// Exercise live queries during network changes under the race detector.
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				got, err := answer(paths[j%len(paths)], "concurrent.test.")
				if err != nil || (got != "10.0.0.1" && got != "10.0.0.2") {
					t.Errorf("concurrent lookup: %q, %v", got, err)
					return
				}
			}
		}()
	}
	for i := 0; i < 10; i++ {
		addr := first
		if i%2 == 1 {
			addr = second
		}
		if err := NotifyChanged(addr); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	if err := NotifyChanged(""); err != nil {
		t.Fatal(err)
	}
	if systemResolver.current() != nativeSystem {
		t.Fatal("empty DNS list did not restore native resolver")
	}
}

func TestParseServers(t *testing.T) {
	servers, err := parseServers(" 1.1.1.1, ,2606:4700:4700::1111,[::1]:5353,127.0.0.1:5353 ")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1.1.1.1:53", "[2606:4700:4700::1111]:53", "[::1]:5353", "127.0.0.1:5353"}
	if len(servers) != len(want) {
		t.Fatalf("got %v", servers)
	}
	for i, server := range servers {
		if server.Addr != want[i] {
			t.Fatalf("got %q, want %q", server.Addr, want[i])
		}
	}
	for _, invalid := range []string{"example.com", "127.0.0.1:0", "127.0.0.1:65536", "1.1.1.1,invalid"} {
		if _, err := parseServers(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}

func TestReplaceSystemRetainsPolicyAndParameters(t *testing.T) {
	mu.Lock()
	defer mu.Unlock()
	old := hostServers
	defer func() { hostServers = old }()
	hostServers = []coreDNS.NameServer{{Net: "udp", Addr: "1.1.1.1:53"}}
	original := []coreDNS.Policy{{Domain: "example.com", NameServers: []coreDNS.NameServer{{Net: "system", Params: map[string]string{"h3": "true"}}}}}
	replaced := replacePolicies(original)
	if original[0].NameServers[0].Net != "system" {
		t.Fatal("modified saved DNS configuration")
	}
	if got := replaced[0].NameServers[0]; got.Addr != "1.1.1.1:53" || got.Params["h3"] != "true" {
		t.Fatalf("lost nameserver settings: %v", got)
	}
}
