package main

/*
#include <stdlib.h>

typedef int (*protect_callback)(void *context, int fd);
typedef int (*query_socket_uid_callback)(
	void *context,
	int protocol,
	const char *source,
	const char *target);

static int call_protect(protect_callback callback, void *context, int fd) {
	if (callback == NULL) {
		return 0;
	}
	return callback(context, fd);
}

static int call_query_socket_uid(
	query_socket_uid_callback callback,
	void *context,
	int protocol,
	const char *source,
	const char *target) {
	if (callback == NULL) {
		return -1;
	}
	return callback(context, protocol, source, target);
}
*/
import "C"

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"unsafe"

	core "github.com/Ember-Moth/libclash"
	"github.com/Ember-Moth/libclash/internal/config"
)

const defaultListenAt = "127.0.0.1:0"

type callbackState struct {
	context        unsafe.Pointer
	protect        C.protect_callback
	querySocketUid C.query_socket_uid_callback
}

type setupRequest struct {
	HomeDir            string `json:"home-dir"`
	ConfigPath         string `json:"config-path"`
	ListenAt           string `json:"listen-at"`
	ExternalController string `json:"external-controller"`
	MixedPort          int    `json:"mixed-port"`
}

type androidTunCallback struct{}

var (
	stateMu sync.RWMutex
	state   = struct {
		homeDir  string
		listenAt string
	}{
		listenAt: defaultListenAt,
	}

	callbackMu sync.RWMutex
	callbacks  callbackState

	errorMu   sync.RWMutex
	lastError string
)

func main() {}

func goString(value *C.char) string {
	if value == nil {
		return ""
	}
	return C.GoString(value)
}

func newCString(value string) *C.char {
	return C.CString(value)
}

func setLastError(message string) int {
	errorMu.Lock()
	lastError = message
	errorMu.Unlock()

	if message == "" {
		return 1
	}
	return 0
}

func fail(err error) int {
	if err == nil {
		return setLastError("")
	}
	return setLastError(err.Error())
}

func currentCallbacks() callbackState {
	callbackMu.RLock()
	defer callbackMu.RUnlock()
	return callbacks
}

func currentHomeDir() string {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return state.homeDir
}

func currentListenAt() string {
	stateMu.RLock()
	defer stateMu.RUnlock()
	if state.listenAt == "" {
		return defaultListenAt
	}
	return state.listenAt
}

func updateState(req setupRequest) string {
	stateMu.Lock()
	defer stateMu.Unlock()

	if req.HomeDir != "" {
		state.homeDir = req.HomeDir
	}
	if req.ListenAt != "" {
		state.listenAt = req.ListenAt
	} else if state.listenAt == "" {
		state.listenAt = defaultListenAt
	}

	return state.homeDir
}

func (androidTunCallback) MarkSocket(fd int32) {
	cb := currentCallbacks()
	C.call_protect(cb.protect, cb.context, C.int(fd))
}

func (androidTunCallback) QuerySocketUid(protocol int32, source string, target string) int32 {
	cb := currentCallbacks()
	if cb.querySocketUid == nil {
		return -1
	}

	cSource := C.CString(source)
	defer C.free(unsafe.Pointer(cSource))

	cTarget := C.CString(target)
	defer C.free(unsafe.Pointer(cTarget))

	return int32(C.call_query_socket_uid(
		cb.querySocketUid,
		cb.context,
		C.int(protocol),
		cSource,
		cTarget,
	))
}

func profileDirForPath(path string) (string, func(), error) {
	if path == "" {
		return "", nil, errors.New("config path is empty")
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", nil, err
	}

	if info.IsDir() {
		return path, func() {}, nil
	}

	if filepath.Base(path) == "config.yaml" {
		return filepath.Dir(path), func() {}, nil
	}

	tempRoot := tempRootForPath(path)
	if tempRoot != "" {
		if err := os.MkdirAll(tempRoot, 0755); err != nil {
			return "", nil, err
		}
	}

	tempDir, err := os.MkdirTemp(tempRoot, "libclash-profile-*")
	if err != nil {
		return "", nil, err
	}

	if err := copyConfigFile(path, filepath.Join(tempDir, "config.yaml")); err != nil {
		os.RemoveAll(tempDir)
		return "", nil, err
	}

	return tempDir, func() { os.RemoveAll(tempDir) }, nil
}

func tempRootForPath(path string) string {
	if homeDir := currentHomeDir(); homeDir != "" {
		return filepath.Join(homeDir, "tmp")
	}

	if dir := filepath.Dir(path); dir != "." && dir != "" {
		return filepath.Join(dir, ".libclash-tmp")
	}

	return ""
}

func validateConfigPath(path string) error {
	profileDir, cleanup, err := profileDirForPath(path)
	if err != nil {
		return err
	}
	defer cleanup()

	rawConfig, err := config.UnmarshalAndPatch(profileDir)
	if err != nil {
		return err
	}

	return config.Validate(rawConfig)
}

func copyConfigFile(sourcePath string, targetPath string) error {
	sourceAbs, sourceErr := filepath.Abs(sourcePath)
	targetAbs, targetErr := filepath.Abs(targetPath)
	if sourceErr == nil && targetErr == nil && sourceAbs == targetAbs {
		return nil
	}

	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	return os.WriteFile(targetPath, sourceData, 0644)
}

func setupConfig(setupJSON string) error {
	var req setupRequest
	if setupJSON != "" {
		if err := json.Unmarshal([]byte(setupJSON), &req); err != nil {
			return err
		}
	}

	homeDir := updateState(req)
	if homeDir == "" {
		return errors.New("home directory is not initialized")
	}

	config.SetExternalController(req.ExternalController)
	config.SetMixedPort(req.MixedPort)

	if err := os.MkdirAll(homeDir, 0755); err != nil {
		return err
	}

	if req.ConfigPath != "" {
		configPath := req.ConfigPath
		if info, err := os.Stat(configPath); err == nil && info.IsDir() {
			configPath = filepath.Join(configPath, "config.yaml")
		}
		return config.LoadFromFile(homeDir, configPath)
	}

	if message := core.Load(homeDir); message != "" {
		return errors.New(message)
	}

	return nil
}

//export libclash_set_android_callbacks
func libclash_set_android_callbacks(
	context unsafe.Pointer,
	protect C.protect_callback,
	querySocketUid C.query_socket_uid_callback,
) {
	callbackMu.Lock()
	callbacks = callbackState{
		context:        context,
		protect:        protect,
		querySocketUid: querySocketUid,
	}
	callbackMu.Unlock()
}

//export libclash_init
func libclash_init(homeDir *C.char, androidSdkVersion C.int) C.int {
	home := goString(homeDir)
	if home == "" {
		return C.int(fail(errors.New("home directory is empty")))
	}

	stateMu.Lock()
	state.homeDir = home
	if state.listenAt == "" {
		state.listenAt = defaultListenAt
	}
	stateMu.Unlock()

	core.Init(home, "", "", int32(androidSdkVersion))
	return C.int(setLastError(""))
}

//export libclash_validate_config
func libclash_validate_config(configPath *C.char) *C.char {
	if err := validateConfigPath(goString(configPath)); err != nil {
		return newCString(err.Error())
	}
	return newCString("")
}

//export libclash_setup_config
func libclash_setup_config(setupJSON *C.char) *C.char {
	if err := setupConfig(goString(setupJSON)); err != nil {
		return newCString(err.Error())
	}
	return newCString("")
}

//export libclash_start_listener
func libclash_start_listener() C.int {
	listen := core.StartHttp(currentListenAt())
	if listen == "" {
		return C.int(fail(errors.New("start HTTP listener failed")))
	}

	return C.int(setLastError(""))
}

//export libclash_stop_listener
func libclash_stop_listener() C.int {
	core.StopHttp()
	return C.int(setLastError(""))
}

//export libclash_start_tun
func libclash_start_tun(
	fd C.int,
	mtu C.int,
	stack *C.char,
	addressCSV *C.char,
	dnsCSV *C.char,
) C.int {
	message := core.StartTun(
		int32(fd),
		int32(mtu),
		goString(stack),
		goString(addressCSV),
		"",
		goString(dnsCSV),
		androidTunCallback{},
	)
	if message != "" {
		return C.int(fail(errors.New(message)))
	}

	return C.int(setLastError(""))
}

//export libclash_stop_tun
func libclash_stop_tun() {
	core.StopTun()
	setLastError("")
}

//export libclash_reset
func libclash_reset() {
	config.SetExternalController("")
	config.SetMixedPort(0)
	core.Reset()
	setLastError("")
}

//export libclash_query_traffic_now
func libclash_query_traffic_now() C.longlong {
	return C.longlong(core.QueryTrafficNow())
}

//export libclash_query_traffic_total
func libclash_query_traffic_total() C.longlong {
	return C.longlong(core.QueryTrafficTotal())
}

//export libclash_query_connection_count
func libclash_query_connection_count() C.int {
	return C.int(core.QueryConnectionCount())
}

//export libclash_close_all_connections
func libclash_close_all_connections() {
	core.CloseAllConnections()
	setLastError("")
}

//export libclash_set_mode
func libclash_set_mode(mode *C.char) C.int {
	if core.SetMode(goString(mode)) {
		return C.int(setLastError(""))
	}

	return C.int(fail(errors.New("invalid mode")))
}

//export libclash_query_group_names
func libclash_query_group_names(excludeNotSelectable C.int) *C.char {
	return newCString(core.QueryGroupNames(excludeNotSelectable != 0))
}

//export libclash_query_group
func libclash_query_group(name *C.char, sortMode *C.char) *C.char {
	return newCString(core.QueryGroup(goString(name), goString(sortMode)))
}

//export libclash_query_groups
func libclash_query_groups(sortMode *C.char) *C.char {
	return newCString(core.QueryGroups(goString(sortMode)))
}

//export libclash_patch_selector
func libclash_patch_selector(selector *C.char, name *C.char) C.int {
	if core.PatchSelector(goString(selector), goString(name)) {
		return C.int(setLastError(""))
	}

	return C.int(fail(errors.New("patch selector failed")))
}

//export libclash_health_check
func libclash_health_check(name *C.char) {
	core.HealthCheck(goString(name))
	setLastError("")
}

//export libclash_health_check_all
func libclash_health_check_all() {
	core.HealthCheckAll()
	setLastError("")
}

//export libclash_test_proxy_delay
func libclash_test_proxy_delay(name *C.char, testURL *C.char, timeoutMilliseconds C.int) C.int {
	return C.int(core.TestProxyDelay(goString(name), goString(testURL), int32(timeoutMilliseconds)))
}

//export libclash_force_gc
func libclash_force_gc() {
	debug.FreeOSMemory()
	setLastError("")
}

//export libclash_set_memory_limit
func libclash_set_memory_limit(bytes C.longlong) C.longlong {
	previous := core.SetMemoryLimit(int64(bytes))
	setLastError("")
	return C.longlong(previous)
}

//export libclash_set_gc_percent
func libclash_set_gc_percent(percent C.int) C.int {
	previous := core.SetGCPercent(int32(percent))
	setLastError("")
	return C.int(previous)
}

//export libclash_update_dns
func libclash_update_dns(dnsCSV *C.char) {
	core.NotifyDnsChanged(goString(dnsCSV))
}

//export libclash_last_error
func libclash_last_error() *C.char {
	errorMu.RLock()
	defer errorMu.RUnlock()
	return newCString(lastError)
}

//export libclash_free_string
func libclash_free_string(value *C.char) {
	if value != nil {
		C.free(unsafe.Pointer(value))
	}
}
