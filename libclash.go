// Package libclash exposes a gomobile-friendly API for the Android native core.
package libclash

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sync/semaphore"

	"github.com/Ember-Moth/libclash/internal/app"
	"github.com/Ember-Moth/libclash/internal/config"
	"github.com/Ember-Moth/libclash/internal/delegate"
	"github.com/Ember-Moth/libclash/internal/proxy"
	"github.com/Ember-Moth/libclash/internal/tun"
	coretunnel "github.com/Ember-Moth/libclash/internal/tunnel"

	"github.com/metacubex/mihomo/log"
)

// ContentCallback lets Android open content:// profile sources and return a raw fd.
type ContentCallback interface {
	Open(url string) int32
}

// FetchCallback receives JSON progress payloads from FetchAndValid.
type FetchCallback interface {
	Report(statusJSON string)
}

// TunCallback lets the core delegate Android-specific socket operations.
type TunCallback interface {
	MarkSocket(fd int32)
	QuerySocketUid(protocol int32, source string, target string) int32
}

// LogcatCallback receives JSON log payloads. Return false to unsubscribe.
type LogcatCallback interface {
	Received(jsonPayload string) bool
}

var (
	coreVersion = "unknown"

	tunLock sync.Mutex
	active  *remoteTun

	logSeq  atomic.Int64
	logLock sync.Mutex
	logSubs = map[int64]chan struct{}{}
)

type remoteTun struct {
	closer   interface{ Close() error }
	callback TunCallback

	closed bool
	limit  *semaphore.Weighted
}

type proxyGroupWire struct {
	Name    string       `json:"Name"`
	Type    string       `json:"Type"`
	Now     string       `json:"Now"`
	TestURL string       `json:"TestUrl"`
	Proxies []*proxyWire `json:"Proxies"`
}

type proxyWire struct {
	Name  string `json:"Name"`
	Type  string `json:"Type"`
	Now   string `json:"Now"`
	Delay *int   `json:"Delay"`
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// marshalJSON encodes obj for one of the query APIs. A failure here would mean
// an unmarshalable wire type, which is a programming error rather than
// something a caller can act on, but this runs at an FFI boundary where a
// panic would take the whole host process down, so it is logged and reported
// as an empty payload instead.
func marshalJSON(obj any) string {
	encoded, err := json.Marshal(obj)
	if err != nil {
		log.Errorln("[APP] marshal JSON failed: %s", err.Error())
		return ""
	}
	if len(encoded) == 0 {
		return ""
	}
	// json.Marshal hands back a buffer that nothing else references and that
	// is never written to again, so it can back the string directly rather
	// than being copied into a new one.
	return unsafe.String(unsafe.SliceData(encoded), len(encoded))
}

func downScaleTraffic(value uint64) uint64 {
	if value > 1042*1024*1024 {
		return ((value * 100 / 1024 / 1024 / 1024) & 0x3fffffff) | (3 << 30)
	}
	if value > 1024*1024 {
		return ((value * 100 / 1024 / 1024) & 0x3fffffff) | (2 << 30)
	}
	if value > 1024 {
		return ((value * 100 / 1024) & 0x3fffffff) | (1 << 30)
	}
	return value & 0x3fffffff
}

func packTraffic(upload, download int64) int64 {
	up := downScaleTraffic(uint64(upload))
	down := downScaleTraffic(uint64(download))
	return int64(up<<32 | down)
}

// Version returns the native core build/version string supplied to Init.
func Version() string {
	return coreVersion
}

// Init initializes mihomo state and Android-specific delegates.
func Init(home string, versionName string, gitVersion string, sdkVersion int32) {
	if gitVersion != "" {
		coreVersion = gitVersion
	}

	delegate.Init(home, versionName, gitVersion, int(sdkVersion))
	Reset()
}

// Reset applies an empty config and clears runtime state.
func Reset() {
	config.LoadDefault()
	coretunnel.ResetStatistic()
	coretunnel.CloseAllConnections()

	runtime.GC()
	debug.FreeOSMemory()
}

// ForceGC asks Go to release memory back to the runtime/OS.
func ForceGC() {
	go func() {
		log.Infoln("[APP] request force GC")

		runtime.GC()
		debug.FreeOSMemory()
	}()
}

// SetMemoryLimit sets Go runtime's soft memory limit and returns the previous value.
func SetMemoryLimit(bytes int64) int64 {
	return debug.SetMemoryLimit(bytes)
}

// SetGCPercent sets Go runtime's GC target percentage and returns the previous value.
func SetGCPercent(percent int32) int32 {
	return int32(debug.SetGCPercent(int(percent)))
}

// SetContentCallback installs the Android content:// opener.
func SetContentCallback(callback ContentCallback) {
	if callback == nil {
		app.ApplyContentContext(nil)
		return
	}

	app.ApplyContentContext(func(url string) (int, error) {
		fd := callback.Open(url)
		if fd < 0 {
			return -1, fmt.Errorf("open content failed: %s", url)
		}
		return int(fd), nil
	})
}

// NotifyDnsChanged updates the system DNS list used by mihomo.
func NotifyDnsChanged(dnsList string) {
	app.NotifyDnsChanged(dnsList)
}

// NotifyInstalledAppsChanged updates "uid:package" mappings joined by comma.
func NotifyInstalledAppsChanged(uidList string) {
	app.NotifyInstallAppsChanged(uidList)
}

// NotifyTimeZoneChanged updates the timezone visible to the core.
func NotifyTimeZoneChanged(name string, offset int32) {
	app.NotifyTimeZoneChanged(name, int(offset))
}

// QueryConfiguration returns UI configuration JSON.
func QueryConfiguration() string {
	return marshalJSON(&struct{}{})
}

// FetchAndValid fetches a profile and providers, reports progress JSON, and returns an error string.
func FetchAndValid(path string, url string, force bool, callback FetchCallback) string {
	report := func(string) {}
	if callback != nil {
		report = callback.Report
	}

	defer runtime.GC()

	return errorString(config.FetchAndValid(path, url, force, report))
}

// Load loads a prepared profile directory and returns an error string.
func Load(path string) string {
	err := config.Load(path)

	runtime.GC()

	return errorString(err)
}

// ReadOverride returns the JSON override content for a slot.
func ReadOverride(slot int32) string {
	return config.ReadOverride(config.OverrideSlot(slot))
}

// WriteOverride stores JSON override content for a slot.
func WriteOverride(slot int32, content string) {
	if message := WriteOverrideChecked(slot, content); message != "" {
		log.Warnln("Write override: %s", message)
	}
}

// WriteOverrideChecked stores an override and reports validation or I/O errors.
func WriteOverrideChecked(slot int32, content string) string {
	return errorString(config.WriteOverride(config.OverrideSlot(slot), content))
}

// ClearOverride removes override content for a slot.
func ClearOverride(slot int32) {
	if err := config.ClearOverride(config.OverrideSlot(slot)); err != nil {
		log.Warnln("Clear override: %s", err)
	}
}

// StartHttp starts the local HTTP listener and returns its bound address, or "" on failure.
func StartHttp(listenAt string) string {
	listen, err := proxy.Start(listenAt)
	if err != nil {
		log.Warnln("Start HTTP listener: %s", err.Error())
		return ""
	}
	return listen
}

// StopHttp stops the local HTTP listener.
func StopHttp() {
	proxy.Stop()
}

func (t *remoteTun) markSocket(fd int) {
	_ = t.limit.Acquire(context.Background(), 1)
	defer t.limit.Release(1)

	if t.closed {
		return
	}

	t.callback.MarkSocket(int32(fd))
}

func (t *remoteTun) querySocketUid(protocol int, source, target string) int {
	_ = t.limit.Acquire(context.Background(), 1)
	defer t.limit.Release(1)

	if t.closed {
		return -1
	}

	return int(t.callback.QuerySocketUid(int32(protocol), source, target))
}

func (t *remoteTun) close() {
	_ = t.limit.Acquire(context.Background(), 4)
	defer t.limit.Release(4)

	t.closed = true

	if t.closer != nil {
		_ = t.closer.Close()
	}

	app.ApplyTunContext(nil, nil)
}

// StartTun attaches a platform VPN fd to mihomo and returns an error string.
// Pass the MTU the platform configured on the interface; a non-positive value
// falls back to tun.DefaultMTU. sing-tun sizes its per-packet buffers from the
// MTU, so an oversized value costs memory on every buffered packet.
func StartTun(fd int32, mtu int32, stack string, gateway string, portal string, dns string, callback TunCallback) string {
	if callback == nil {
		return "tun callback is nil"
	}

	tunLock.Lock()
	defer tunLock.Unlock()

	if active != nil {
		active.close()
		active = nil
	}

	remote := &remoteTun{
		callback: callback,
		closed:   false,
		limit:    semaphore.NewWeighted(4),
	}

	app.ApplyTunContext(remote.markSocket, remote.querySocketUid)

	closer, err := tun.Start(int(fd), int(mtu), stack, gateway, portal, dns)
	if err != nil {
		remote.close()
		return err.Error()
	}

	remote.closer = closer
	active = remote

	return ""
}

// StopTun closes the active TUN listener.
func StopTun() {
	tunLock.Lock()
	defer tunLock.Unlock()

	if active != nil {
		active.close()
		active = nil
	}
}

// QueryTunnelState returns tunnel state JSON.
func QueryTunnelState() string {
	return marshalJSON(&struct {
		Mode string `json:"mode"`
	}{coretunnel.QueryMode()})
}

// SetMode changes the running tunnel outbound mode.
func SetMode(mode string) bool {
	return coretunnel.SetMode(strings.ToLower(strings.TrimSpace(mode)))
}

// QueryTrafficNow returns packed upload/download traffic.
func QueryTrafficNow() int64 {
	up, down := coretunnel.Now()
	return packTraffic(up, down)
}

// QueryTrafficTotal returns packed upload/download traffic.
func QueryTrafficTotal() int64 {
	up, down := coretunnel.Total()
	return packTraffic(up, down)
}

// QueryConnectionCount returns the active connection count.
func QueryConnectionCount() int32 {
	return int32(coretunnel.QueryConnectionCount())
}

// CloseAllConnections closes all active tracked connections.
func CloseAllConnections() {
	coretunnel.CloseAllConnections()
}

// QueryGroupNames returns JSON proxy group names.
func QueryGroupNames(excludeNotSelectable bool) string {
	return marshalJSON(coretunnel.QueryProxyGroupNames(excludeNotSelectable))
}

// QueryGroup returns a proxy group JSON payload. sortMode accepts Default, Title, or Delay.
func QueryGroup(name string, sortMode string) string {
	mode := coretunnel.Default

	switch sortMode {
	case "Title":
		mode = coretunnel.Title
	case "Delay":
		mode = coretunnel.Delay
	}

	response := coretunnel.QueryProxyGroup(name, mode, app.SubtitlePattern())
	if response == nil {
		return ""
	}

	return marshalJSON(response)
}

// QueryGroups returns all visible proxy groups in the UI wire shape.
func QueryGroups(sortMode string) string {
	mode := coretunnel.Default

	switch sortMode {
	case "Title":
		mode = coretunnel.Title
	case "Delay":
		mode = coretunnel.Delay
	}

	names := coretunnel.QueryProxyGroupNames(false)
	groups := make([]*proxyGroupWire, 0, len(names))
	subtitlePattern := app.SubtitlePattern()

	for _, name := range names {
		group := coretunnel.QueryProxyGroup(name, mode, subtitlePattern)
		if group == nil || len(group.Proxies) == 0 {
			continue
		}

		proxies := make([]*proxyWire, 0, len(group.Proxies))
		for _, proxy := range group.Proxies {
			if proxy == nil || strings.TrimSpace(proxy.Name) == "" {
				continue
			}

			proxyType := proxy.Type
			if proxyType == "" {
				proxyType = proxy.Subtitle
			}

			var delay *int
			if proxy.Delay > 0 && proxy.Delay < 65535 {
				value := proxy.Delay
				delay = &value
			}

			proxies = append(proxies, &proxyWire{
				Name:  proxy.Name,
				Type:  proxyType,
				Now:   "",
				Delay: delay,
			})
		}

		if len(proxies) == 0 {
			continue
		}

		groups = append(groups, &proxyGroupWire{
			Name:    name,
			Type:    group.Type,
			Now:     group.Now,
			TestURL: "",
			Proxies: proxies,
		})
	}

	return marshalJSON(groups)
}

// HealthCheck runs a health check for a group.
func HealthCheck(name string) {
	coretunnel.HealthCheck(name)
}

// HealthCheckAll starts health checks for all groups.
func HealthCheckAll() {
	coretunnel.HealthCheckAll()
}

// PatchSelector updates a selector group choice.
func PatchSelector(selector string, name string) bool {
	return coretunnel.PatchSelector(selector, name)
}

// TestProxyDelay runs URLTest for one proxy and returns the delay in milliseconds.
func TestProxyDelay(name string, testURL string, timeoutMilliseconds int32) int32 {
	delay, err := coretunnel.TestProxyDelay(name, testURL, int(timeoutMilliseconds))
	if err != nil {
		log.Warnln("Test proxy delay `%s`: %s", name, err.Error())
		return -1
	}

	return int32(delay)
}

// QueryProviders returns provider list JSON.
func QueryProviders() string {
	return marshalJSON(coretunnel.QueryProviders())
}

// UpdateProvider updates a provider and returns an error string.
func UpdateProvider(providerType string, name string) string {
	return errorString(coretunnel.UpdateProvider(providerType, name))
}

// SuspendCore records the Android suspended/running state.
func SuspendCore(suspended bool) {
	coretunnel.Suspend(suspended)
}

type logMessage struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Time    int64  `json:"time"`
}

func deliverLog(callback LogcatCallback, payload string) (keep bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Warnln("Logcat callback panic: %v", r)
			keep = false
		}
	}()

	return callback.Received(payload)
}

// SubscribeLogcat starts delivering JSON log messages and returns a subscription id.
func SubscribeLogcat(callback LogcatCallback) int64 {
	if callback == nil {
		return 0
	}

	id := logSeq.Add(1)
	stop := make(chan struct{})

	logLock.Lock()
	logSubs[id] = stop
	logLock.Unlock()

	go func() {
		sub := log.Subscribe()
		defer log.UnSubscribe(sub)
		defer func() {
			logLock.Lock()
			delete(logSubs, id)
			logLock.Unlock()
		}()

		for {
			select {
			case <-stop:
				return
			case msg, ok := <-sub:
				if !ok {
					return
				}

				if msg.LogLevel < log.Level() && !strings.HasPrefix(msg.Payload, "[APP]") {
					continue
				}

				payload := marshalJSON(&logMessage{
					Level:   msg.LogLevel.String(),
					Message: msg.Payload,
					Time:    time.Now().UnixNano() / 1000 / 1000,
				})

				if !deliverLog(callback, payload) {
					return
				}
			}
		}
	}()

	log.Infoln("[APP] Logcat level: %s", log.Level().String())

	return id
}

// UnsubscribeLogcat stops a log subscription.
func UnsubscribeLogcat(id int64) {
	logLock.Lock()
	stop, ok := logSubs[id]
	if ok {
		delete(logSubs, id)
		close(stop)
	}
	logLock.Unlock()
}
