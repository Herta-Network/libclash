package dns

import (
	"context"
	"net/netip"
	"sync/atomic"

	"github.com/metacubex/mihomo/component/resolver"
	D "github.com/miekg/dns"
)

type resolverState struct{ resolver.Resolver }

// dynamicResolver keeps the object used by TUN, listeners and outbounds stable
// while network changes replace its resolver (and its cache) atomically.
type dynamicResolver struct{ state atomic.Pointer[resolverState] }

func (r *dynamicResolver) replace(next resolver.Resolver) {
	old := r.state.Swap(&resolverState{next})
	if old != nil {
		old.ResetConnection()
	}
}

func (r *dynamicResolver) current() resolver.Resolver { return r.state.Load().Resolver }

func (r *dynamicResolver) LookupIP(ctx context.Context, host string) ([]netip.Addr, error) {
	return r.current().LookupIP(ctx, host)
}

func (r *dynamicResolver) LookupIPv4(ctx context.Context, host string) ([]netip.Addr, error) {
	return r.current().LookupIPv4(ctx, host)
}

func (r *dynamicResolver) LookupIPv6(ctx context.Context, host string) ([]netip.Addr, error) {
	return r.current().LookupIPv6(ctx, host)
}

func (r *dynamicResolver) ResolveECH(ctx context.Context, host string) ([]byte, error) {
	return r.current().ResolveECH(ctx, host)
}

func (r *dynamicResolver) ExchangeContext(ctx context.Context, msg *D.Msg) (*D.Msg, error) {
	return r.current().ExchangeContext(ctx, msg)
}

func (r *dynamicResolver) Invalid() bool    { return r.current().Invalid() }
func (r *dynamicResolver) ClearCache()      { r.current().ClearCache() }
func (r *dynamicResolver) ResetConnection() { r.current().ResetConnection() }

var _ resolver.Resolver = (*dynamicResolver)(nil)
