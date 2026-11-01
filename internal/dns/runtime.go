// Package dns applies host-supplied DNS on every platform. mihomo's native
// UpdateSystemDNS API is Android-only, so libclash uses its public resolver APIs
// and replaces system:// entries when constructing each resolver generation.
package dns

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"

	"github.com/metacubex/mihomo/adapter/inbound"
	"github.com/metacubex/mihomo/component/resolver"
	"github.com/metacubex/mihomo/config"
	D "github.com/metacubex/mihomo/dns"
	"github.com/metacubex/mihomo/hub"
)

var (
	mu             sync.Mutex
	hostServers    []D.NameServer
	active         *D.Config
	nativeSystem   resolver.Resolver
	systemResolver dynamicResolver
	mainResolver   dynamicResolver
	proxyResolver  dynamicResolver
	directResolver dynamicResolver
)

func init() {
	nativeSystem = resolver.SystemResolver
	systemResolver.replace(nativeSystem)
	resolver.SystemResolver = &systemResolver
}

// ApplyConfig serializes configuration changes with host DNS notifications.
// Callers must still serialize core lifecycle operations with each other.
func ApplyConfig(cfg *config.Config) {
	mu.Lock()
	defer mu.Unlock()

	hub.ApplyConfig(cfg)
	active = nil
	c := cfg.DNS
	if !c.Enable {
		empty := D.NewResolver(D.Config{})
		mainResolver.replace(empty)
		proxyResolver.replace(empty.Resolver)
		directResolver.replace(empty.Resolver)
		return
	}
	initialResolver := resolver.DefaultResolver
	active = &D.Config{
		Main:                 c.NameServer,
		Fallback:             c.Fallback,
		Default:              c.DefaultNameserver,
		ProxyServer:          c.ProxyServerNameserver,
		DirectServer:         c.DirectNameServer,
		Policy:               c.NameServerPolicy,
		ProxyServerPolicy:    c.ProxyServerPolicy,
		DirectFollowPolicy:   c.DirectFollowPolicy,
		IPv6:                 c.IPv6 && cfg.General.IPv6,
		IPv6Timeout:          c.IPv6Timeout,
		FallbackIPFilter:     c.FallbackIPFilter,
		FallbackDomainFilter: c.FallbackDomainFilter,
		FallbackLazyQuery:    c.FallbackLazyQuery,
		CacheAlgorithm:       c.CacheAlgorithm,
		CacheMaxSize:         c.CacheMaxSize,
	}
	rebuildResolvers()
	initialResolver.ResetConnection()
	resolver.DefaultResolver = &mainResolver
	resolver.ProxyServerHostResolver = &proxyResolver
	resolver.DirectHostResolver = &directResolver

	// Retain the enhancer installed by mihomo, including its fake-IP mapping.
	service := D.NewService(&mainResolver, resolver.DefaultHostMapper.(*D.ResolverEnhancer))
	resolver.DefaultService = service
	lc := inbound.NewListenConfig()
	lc.SetRouteMark(c.ListenRoutingMark)
	D.ReCreateServer(c.Listen, lc, service)
}

// NotifyChanged accepts IP addresses, optionally with ports. An invalid list
// leaves the previous resolvers intact; an empty list restores native discovery.
func NotifyChanged(list string) error {
	servers, err := parseServers(list)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	hostServers = servers
	if len(servers) == 0 {
		nativeSystem.ClearCache()
		systemResolver.replace(nativeSystem)
	} else {
		systemResolver.replace(D.NewResolver(D.Config{Main: servers, IPv6: true}).Resolver)
	}
	if active != nil {
		rebuildResolvers()
	}
	return nil
}

func parseServers(list string) ([]D.NameServer, error) {
	var servers []D.NameServer
	for _, addr := range strings.Split(list, ",") {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		if ip, err := netip.ParseAddr(addr); err == nil {
			addr = net.JoinHostPort(ip.String(), "53")
		} else if endpoint, err := netip.ParseAddrPort(addr); err != nil || endpoint.Port() == 0 {
			return nil, fmt.Errorf("invalid DNS server %q", addr)
		}
		servers = append(servers, D.NameServer{Net: "udp", Addr: addr})
	}
	return servers, nil
}

// mu must be held. Existing services keep the same dynamicResolver pointers.
func rebuildResolvers() {
	c := *active
	c.Main = replaceSystem(c.Main)
	c.Fallback = replaceSystem(c.Fallback)
	c.Default = replaceSystem(c.Default)
	c.ProxyServer = replaceSystem(c.ProxyServer)
	c.DirectServer = replaceSystem(c.DirectServer)
	c.Policy = replacePolicies(c.Policy)
	c.ProxyServerPolicy = replacePolicies(c.ProxyServerPolicy)
	r := D.NewResolver(c)
	// Keep the composite resolver: its ClearCache/ResetConnection also cover
	// the proxy and direct resolvers, as mihomo's controller expects.
	mainResolver.replace(r)
	if r.ProxyResolver.Invalid() {
		proxyResolver.replace(r.ProxyResolver)
	} else {
		proxyResolver.replace(r.Resolver)
	}
	if r.DirectResolver.Invalid() {
		directResolver.replace(r.DirectResolver)
	} else {
		directResolver.replace(r.Resolver)
	}
}

func replaceSystem(servers []D.NameServer) []D.NameServer {
	if len(hostServers) == 0 {
		return servers
	}
	result := make([]D.NameServer, 0, len(servers))
	for _, server := range servers {
		if server.Net != "system" {
			result = append(result, server)
			continue
		}
		for _, host := range hostServers {
			next := server // retain routing and wrapper parameters
			next.Net, next.Addr = host.Net, host.Addr
			result = append(result, next)
		}
	}
	return result
}

func replacePolicies(policies []D.Policy) []D.Policy {
	result := append([]D.Policy(nil), policies...)
	for i := range result {
		result[i].NameServers = replaceSystem(result[i].NameServers)
	}
	return result
}
