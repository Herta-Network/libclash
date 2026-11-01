package app

import (
	"github.com/Ember-Moth/libclash/internal/dns"
	"github.com/metacubex/mihomo/log"
)

func NotifyDnsChanged(dnsList string) {
	if err := dns.NotifyChanged(dnsList); err != nil {
		log.Warnln("Update system DNS: %s", err)
	}
}
