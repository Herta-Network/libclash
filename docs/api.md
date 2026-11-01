# API 文档

本文档描述 `gomobile bind` 生成的 Android API。默认 Java 包名为：

```text
com.github.embermoth.libclash
```

主入口类：

```text
Libclash
```

## 生成类

```text
com.github.embermoth.libclash.Libclash
com.github.embermoth.libclash.ContentCallback
com.github.embermoth.libclash.FetchCallback
com.github.embermoth.libclash.TunCallback
com.github.embermoth.libclash.LogcatCallback
```

## 回调接口

### ContentCallback

```java
int open(String url)
```

用于打开 `content://` 配置源。

- 参数 `url`：content URI 字符串
- 返回值：可读 fd；负数表示失败

### FetchCallback

```java
void report(String statusJSON)
```

接收 `fetchAndValid` 的进度 JSON。

### TunCallback

```java
void markSocket(int fd)
int querySocketUid(int protocol, String source, String target)
```

用于 Android VPN/TUN 相关系统能力。

- `markSocket`：通常调用 `VpnService.protect(fd)`
- `querySocketUid`：查询连接归属 UID；不支持时返回 `-1`
- `protocol`：系统协议号，通常为 TCP 或 UDP
- `source`、`target`：地址字符串，例如 `192.0.2.10:12345`

### LogcatCallback

```java
boolean received(String jsonPayload)
```

接收日志 JSON。

- 返回 `true`：继续订阅
- 返回 `false`：停止订阅

## 生命周期 API

### version

```java
String version()
```

返回 `init` 传入的 `gitVersion`，未初始化时为 `unknown`。

### init

```java
void init(String home, String versionName, String gitVersion, int sdkVersion)
```

初始化 mihomo 和 Android delegate。

- `home`：核心工作目录
- `versionName`：App 版本名
- `gitVersion`：核心版本标识
- `sdkVersion`：Android SDK level

调用后会自动执行一次 `reset`。

### reset

```java
void reset()
```

加载空配置、重置流量统计、关闭所有连接并触发 GC。

### forceGC

```java
void forceGC()
```

异步请求 Go runtime 执行 GC。

### setMemoryLimit

```java
long setMemoryLimit(long bytes)
```

设置 Go runtime 软内存上限，返回旧值。传入负数只查询当前值、不修改；
传入 `Long.MAX_VALUE` 可取消该软上限。

### setGCPercent

```java
int setGCPercent(int percent)
```

设置 Go runtime GC 目标百分比，返回旧值。移动端内存敏感进程可使用较低值，例如
`50`。

## Android 上下文 API

### setContentCallback

```java
void setContentCallback(ContentCallback callback)
```

设置 `content://` 打开回调。传入 `null` 会恢复为默认失败实现。

### notifyDnsChanged

```java
void notifyDnsChanged(String dnsList)
```

通知宿主 DNS 变化，在 Android、iOS 和 OpenHarmony 上生效。
`dnsList` 使用英文逗号分隔，支持 IPv4、IPv6 和带端口的 IP 地址：

```text
8.8.8.8,1.1.1.1
```

更新系统解析器及配置中的 `system://`（包括 policy、proxy-server 和 direct DNS），
同时清理旧 DNS 缓存/连接。显式配置的其他 DNS、代理连接和 fake-IP 映射保留。
空列表恢复核心原有的系统 DNS 发现行为；非法地址记录日志并保留原设置。

### notifyInstalledAppsChanged

```java
void notifyInstalledAppsChanged(String uidList)
```

通知 UID 到包名的映射。格式：

```text
10001:com.example.app,10002:com.example.other
```

### notifyTimeZoneChanged

```java
void notifyTimeZoneChanged(String name, int offset)
```

通知时区变化。

- `name`：时区名称
- `offset`：UTC offset 秒数

## 配置 API

### fetchAndValid

```java
String fetchAndValid(String path, String url, boolean force, FetchCallback callback)
```

下载配置和 provider，并验证配置。

- `path`：profile 目录
- `url`：配置地址，支持 `http`、`https`、`content`
- `force`：是否强制重新下载 `config.yaml`
- `callback`：进度回调，可为 `null`
- 返回值：空字符串表示成功，非空字符串为错误信息

进度 JSON：

```json
{
  "action": "FetchConfiguration",
  "args": ["example.com"],
  "progress": -1,
  "max": -1
}
```

`action` 可能值：

```text
FetchConfiguration
FetchProviders
Verifying
```

### load

```java
String load(String path)
```

加载 profile 目录中的 `config.yaml`。

- 返回空字符串：成功
- 返回非空字符串：错误信息

### queryConfiguration

```java
String queryConfiguration()
```

返回 UI 配置 JSON。目前实现返回空对象：

```json
{}
```

## Override API

slot 使用整数：

```text
0 = Persist
1 = Session
```

### readOverride

```java
String readOverride(int slot)
```

读取 override JSON。

### writeOverride

```java
void writeOverride(int slot, String content)
```

写入 override JSON，失败时记录日志。为兼容已有调用方保留此方法；
需要判断写入是否成功时使用 `writeOverrideChecked`。

### writeOverrideChecked

```java
String writeOverrideChecked(int slot, String content)
```

要求 `content` 为 JSON 对象。返回空字符串表示成功，非空字符串为非法 slot、
JSON 或文件 I/O 错误。持久化写入使用原子替换，失败时保留旧文件。

### clearOverride

```java
void clearOverride(int slot)
```

清除 override。

## TUN 与 HTTP API

### startTun

```java
String startTun(
    int fd,
    int mtu,
    String stack,
    String gateway,
    String portal,
    String dns,
    TunCallback callback
)
```

启动 TUN listener。

- `fd`：Android VPN TUN fd
- `mtu`：宿主 TUN 接口实际配置的 MTU；非正数使用兼容默认值 `9000`
- `stack`：mihomo TUN stack，例如 `system`
- `gateway`：网关 CIDR，支持逗号分隔 IPv4/IPv6
- `portal`：保留参数，当前核心逻辑未使用
- `dns`：DNS 劫持地址，逗号分隔
- `callback`：Android VPN 回调
- 返回空字符串：成功
- 返回非空字符串：错误信息

### stopTun

```java
void stopTun()
```

停止当前 TUN listener。

### startHttp

```java
String startHttp(String listenAt)
```

启动本地 HTTP listener。

- 返回监听地址：成功
- 返回空字符串：失败

示例：

```text
127.0.0.1:0
```

### stopHttp

```java
void stopHttp()
```

停止 HTTP listener。

## Tunnel 查询 API

### queryTunnelState

```java
String queryTunnelState()
```

返回 tunnel 状态 JSON：

```json
{
  "mode": "rule"
}
```

### queryTrafficNow

```java
long queryTrafficNow()
```

返回当前速率的 packed traffic。

### queryTrafficTotal

```java
long queryTrafficTotal()
```

返回累计流量的 packed traffic。

packed traffic 格式：

```text
高 32 bit = upload
低 32 bit = download
```

每个 32 bit 值：

```text
bit 31..30 = 单位类型
bit 29..0  = 数值
```

单位类型：

```text
0 = Bytes
1 = KiB * 100
2 = MiB * 100
3 = GiB * 100
```

## Proxy API

### queryGroupNames

```java
String queryGroupNames(boolean excludeNotSelectable)
```

返回代理组名称 JSON 数组：

```json
["GLOBAL", "Proxy", "Auto"]
```

### queryGroup

```java
String queryGroup(String name, String sortMode)
```

查询代理组。

`sortMode`：

```text
Default
Title
Delay
```

返回 JSON：

```json
{
  "type": "Selector",
  "now": "ProxyA",
  "proxies": [
    {
      "name": "ProxyA",
      "title": "ProxyA",
      "subtitle": "Vmess",
      "type": "Vmess",
      "delay": 123
    }
  ]
}
```

如果找不到代理组，返回空字符串。

### patchSelector

```java
boolean patchSelector(String selector, String name)
```

切换 selector 当前节点。

### healthCheck

```java
void healthCheck(String name)
```

对指定代理组执行健康检查。

### healthCheckAll

```java
void healthCheckAll()
```

对所有代理组启动健康检查。

## Provider API

### queryProviders

```java
String queryProviders()
```

返回 provider JSON 数组：

```json
[
  {
    "name": "provider-a",
    "vehicleType": "HTTP",
    "type": "Proxy",
    "updatedAt": 1710000000000
  }
]
```

### updateProvider

```java
String updateProvider(String providerType, String name)
```

更新 provider。

`providerType`：

```text
Proxy
Rule
```

返回空字符串表示成功，非空字符串为错误信息。

## 日志 API

### subscribeLogcat

```java
long subscribeLogcat(LogcatCallback callback)
```

订阅日志。返回订阅 ID。`callback` 为 `null` 时返回 `0`。

日志 JSON：

```json
{
  "level": "info",
  "message": "Init core",
  "time": 1710000000000
}
```

### unsubscribeLogcat

```java
void unsubscribeLogcat(long id)
```

取消日志订阅。

## 其他 API

### suspendCore

```java
void suspendCore(boolean suspended)
```

记录 Android 侧暂停状态。当前内部实现不会停止 tunnel 处理。
