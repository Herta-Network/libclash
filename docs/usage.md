# 使用文档

本文说明如何构建 `libclash.aar`，以及如何在 Android 工程中接入 gomobile 生成的 API。

## 环境要求

需要安装：

- Go
- gomobile 和 gobind
- Android SDK
- Android NDK
- JDK

在项目根目录安装与 `go.mod` 中 `golang.org/x/mobile` 相同版本的两个工具：

```bash
version="$(go list -m -f '{{.Version}}' golang.org/x/mobile)"
go install "golang.org/x/mobile/cmd/gomobile@$version"
go install "golang.org/x/mobile/cmd/gobind@$version"
gomobile init
```

## 构建 AAR

```bash
./scripts/build-android.sh
```

如果只设置了 `NDK_HOME`，脚本也能识别：

```bash
NDK_HOME=/opt/android-sdk/ndk/27.1.12297006 ./scripts/build-android.sh
```

输出：

```text
dist/android/libclash.aar
```

默认构建 ABI：

```text
armeabi-v7a
arm64-v8a
x86_64
```

默认不构建 `x86` / `android/386`。

自定义 Java 包名前缀：

```bash
JAVA_PKG=com.example ./scripts/build-android.sh
```

自定义输出文件名：

```bash
AAR_NAME=libclash-debug.aar ./scripts/build-android.sh
```

默认情况下，脚本会在 `gomobile bind` 完成后解包 AAR，并使用 NDK 的 `llvm-strip --strip-unneeded` 对 `jni/**/*.so` 执行 strip，然后重新打包 AAR。

如需保留调试符号：

```bash
STRIP_SO=0 ./scripts/build-android.sh
```

如需指定 strip 工具：

```bash
STRIP_TOOL=/path/to/llvm-strip ./scripts/build-android.sh
```

## Android 接入

把 `dist/android/libclash.aar` 作为 Android dependency 引入。常见做法是放入 app module 的 `libs/` 目录：

```text
app/libs/libclash.aar
```

Gradle 示例：

```kotlin
dependencies {
    implementation(files("libs/libclash.aar"))
}
```

如果使用 flatDir：

```kotlin
repositories {
    flatDir {
        dirs("libs")
    }
}

dependencies {
    implementation(name = "libclash", ext = "aar")
}
```

## 初始化流程

Android 侧应在使用核心前调用 `Libclash.init`：

```kotlin
import com.github.embermoth.libclash.Libclash

val home = filesDir.resolve("clash").apply { mkdirs() }.absolutePath
val versionName = packageManager.getPackageInfo(packageName, 0).versionName ?: "unknown"
val gitVersion = "custom-build"
val sdkVersion = android.os.Build.VERSION.SDK_INT

Libclash.init(home, versionName, gitVersion, sdkVersion)
```

`home` 是 mihomo 的工作目录。配置覆盖文件、运行时状态等会以该目录为基础。
`init`、`load`、`reset`、启动和停止等生命周期操作应由宿主串行调用。

## content:// 支持

如果配置来源可能是 `content://`，需要设置 `ContentCallback`：

```kotlin
import com.github.embermoth.libclash.ContentCallback
import com.github.embermoth.libclash.Libclash

Libclash.setContentCallback(object : ContentCallback {
    override fun open(url: String): Int {
        val uri = android.net.Uri.parse(url)
        return contentResolver.openFileDescriptor(uri, "r")?.detachFd() ?: -1
    }
})
```

返回负数表示打开失败。

## 下载和加载配置

`fetchAndValid` 会下载 `config.yaml` 和 provider，并对配置做解析校验。

```kotlin
val profileDir = filesDir.resolve("profiles/default").apply { mkdirs() }

val error = Libclash.fetchAndValid(
    profileDir.absolutePath,
    "https://example.com/config.yaml",
    true
) { statusJson ->
    // statusJson 见 docs/api.md
}

if (error.isNotEmpty()) {
    throw IllegalStateException(error)
}

val loadError = Libclash.load(profileDir.absolutePath)
if (loadError.isNotEmpty()) {
    throw IllegalStateException(loadError)
}
```

注意：`fetchAndValid` 和 `load` 是同步调用，Android 侧应放到 IO 线程或协程 `Dispatchers.IO` 中执行。
下载及校验成功后才会替换 `config.yaml`；HTTP 错误、中断下载和配置校验失败都会保留旧配置。
同一 profile 的下载/加载应串行执行。

## TUN 接入

Android VPN 服务创建 TUN fd 后，调用 `startTun`：

```kotlin
import com.github.embermoth.libclash.TunCallback

val err = Libclash.startTun(
    fd,
    1500, // 必须与 VpnService.Builder.setMtu(...) 使用的值一致
    "system",
    "172.19.0.1/30",
    "172.19.0.2",
    "172.19.0.2",
    object : TunCallback {
        override fun markSocket(fd: Int) {
            vpnService.protect(fd)
        }

        override fun querySocketUid(protocol: Int, source: String, target: String): Int {
            return -1
        }
    }
)

if (err.isNotEmpty()) {
    throw IllegalStateException(err)
}
```

停止：

```kotlin
Libclash.stopTun()
```

网络变化时传入宿主获取到的 DNS，建议在首次加载配置前也通知一次：

```kotlin
Libclash.notifyDnsChanged("1.1.1.1,2606:4700:4700::1111")
```

Android、iOS 和 OpenHarmony 都使用这套更新逻辑；C ABI 对应 `libclash_update_dns`。
它会更新系统解析器及配置中的 `system://`，清理旧 DNS 缓存和连接，同时保留代理连接、
显式配置的 DNS 和 fake-IP 映射。空字符串恢复核心原有的系统 DNS 发现行为。

## HTTP listener

```kotlin
val address = Libclash.startHttp("127.0.0.1:0")
if (address.isEmpty()) {
    // 启动失败
}

Libclash.stopHttp()
```

## 日志订阅

```kotlin
import com.github.embermoth.libclash.LogcatCallback

val id = Libclash.subscribeLogcat(object : LogcatCallback {
    override fun received(jsonPayload: String): Boolean {
        // 返回 false 会自动取消订阅
        return true
    }
})

Libclash.unsubscribeLogcat(id)
```

## 错误约定

为了保持 gomobile API 简单，错误以字符串返回：

- 返回 `""`：成功
- 返回非空字符串：失败原因

适用方法：

- `fetchAndValid`
- `load`
- `startTun`
- `updateProvider`
- `writeOverrideChecked`

`startHttp` 比较特殊：

- 返回非空地址：成功
- 返回 `""`：失败

## 与旧 JNI 方案的区别

旧链路：

```text
Kotlin external fun -> C/C++ JNI bridge -> Go c-shared ABI -> mihomo
```

当前链路：

```text
Kotlin/Java -> gomobile 生成 Java API -> gomobile JNI/runtime -> Go package -> mihomo
```

项目不再维护 C/C++ JNI 胶水代码。
