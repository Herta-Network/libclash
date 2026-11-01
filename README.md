# libclash

`libclash` 是一个独立的 Go 项目，用于构建基于 mihomo 的移动端原生核心。

它把 Go 代码和 mihomo 依赖从客户端工程中拆出来，通过 Go Modules 管理
[Herta-Network/mihomo](https://github.com/Herta-Network/mihomo) fork。当前提供以下产物的构建脚本：

- `gomobile bind` 生成的 Android AAR
- `go build -buildmode=c-shared` 生成的 Android `libclash.so`
- `go build -buildmode=c-archive` 生成的 iOS `libclash.a` / `libclash.xcframework`
- 使用 OpenHarmony SDK 生成的 `libclash.so`

Avalonia/.NET Android 客户端优先使用 C shared 产物，通过 P/Invoke 直接加载 `libclash.so`，不需要项目自维护的 C/C++ JNI 胶水代码。

## 产物

构建后生成：

```text
dist/android/libclash.aar
dist/cshared/android/<abi>/libclash.so
dist/cshared/ios/libclash.xcframework
dist/cshared/ios/iphoneos-arm64/libclash.a
dist/cshared/ios/iphonesimulator/libclash.a
dist/cshared/ohos/<ohos-target>/libclash.so
```

AAR 内包含：

- gomobile 生成的 Java API
- `go.Seq` 运行时类
- 多 ABI native library：`armeabi-v7a`、`arm64-v8a`、`x86_64`

默认 Java 包名：

```text
com.hertanetwork.libclash
```

主入口类：

```text
com.hertanetwork.libclash.Libclash
```

## 目录结构

```text
libclash.go                 gomobile 可绑定 API
cmd/main.go                 C shared 导出 API
internal/app                Android 回调上下文、应用状态
internal/config             配置下载、校验、覆盖、预处理
internal/delegate           mihomo 初始化、socket hook、进程解析
internal/dns                跨平台宿主 DNS、网络切换与解析器更新
internal/proxy              本地 HTTP listener
internal/tun                Android TUN listener
internal/tunnel             代理组、provider、流量、连接、暂停状态
scripts/build-android.sh    AAR 构建脚本
scripts/build-cshared-android.sh
                            Android C shared 构建脚本
scripts/build-carchive-ios.sh
                            iOS C archive/XCFramework 构建脚本
scripts/build-cshared-ohos.sh
                            OpenHarmony C shared 构建脚本
.github/workflows/build.yml  测试和各平台 native artifact 构建
docs/usage.md               构建与 Android 接入说明
docs/api.md                 API 与 JSON 结构说明
```

## 快速构建

需要 Go（版本见 go.mod）、gomobile、gobind、Android SDK 和 Android NDK。
从项目根目录运行：

```bash
./scripts/build-android.sh
```

脚本读取以下环境变量：

- `ANDROID_NDK_HOME` 或 `NDK_HOME`：NDK 路径，必需
- `ANDROID_API_LEVEL`：Android API level，默认 `21`
- `ANDROID_NDK_HOST_TAG`：NDK 主机目录，默认自动识别 macOS/Linux
- `JAVA_PKG`：生成 Java 包名前缀，默认 `com.hertanetwork`
- `AAR_NAME`：输出 AAR 文件名，默认 `libclash.aar`
- `GOMOBILE_TARGET`：gomobile target，默认 `android/arm,android/arm64,android/amd64`
- `GO_TAGS`：Go build tags，默认 `foss,cmfa,with_low_memory,no_tailscale,no_fake_tcp`
- `STRIP_SO`：是否 strip AAR 内的 `.so`，默认 `1`；调试符号构建可设为 `0`
- `STRIP_TOOL`：自定义 strip 工具路径，默认使用 NDK toolchain 的 `llvm-strip`

默认构建偏向移动端低内存场景：启用 `with_low_memory`，禁用 Tailscale
outbound 和 fake-tcp，并且不包含 `with_gvisor`。这意味着默认产物只保证
`system` TUN 栈；如需 gVisor/mixed 栈，可以显式传入：

```bash
GO_TAGS=foss,with_gvisor,cmfa ./scripts/build-cshared-android.sh
```

NDK 路径示例，请替换为本机实际路径：

```bash
NDK_HOME=/opt/android-sdk/ndk/27.1.12297006 ./scripts/build-android.sh
```

## 构建 C shared

Avalonia Android 项目使用这个产物：

```bash
ANDROID_NDK_HOME=/opt/android-sdk/ndk/29.0.14206865 \
INSTALL_DIR=/path/to/app/NativeLibraries \
./scripts/build-cshared-android.sh
```

脚本默认构建以下 ABI，不包含 i386/x86：

```text
armeabi-v7a
arm64-v8a
x86_64
```

构建完成后会执行 `llvm-strip --strip-unneeded`。输出位置：

```text
dist/cshared/android/armeabi-v7a/libclash.so
dist/cshared/android/arm64-v8a/libclash.so
dist/cshared/android/x86_64/libclash.so
dist/cshared/android/include/libclash.h
```

如果设置 `INSTALL_DIR`，脚本会把各 ABI 的 `libclash.so` 复制到对应目录。

## 构建 iOS C archive

iOS 构建需要 macOS + 完整 Xcode，也可由 GitHub Actions 生成：

```text
.github/workflows/build.yml
```

在 macOS 环境手动构建：

```bash
./scripts/build-carchive-ios.sh
```

输出位置：

```text
dist/cshared/ios/iphoneos-arm64/libclash.a
dist/cshared/ios/iphonesimulator/libclash.a
dist/cshared/ios/include/libclash.h
dist/cshared/ios/libclash.xcframework
```

GitHub artifact 名称：

```text
libclash-ios
```

## 文档

- [使用文档](docs/usage.md)
- [API 文档](docs/api.md)

## 依赖策略

mihomo fork 保留上游模块名和 import 路径，`go.mod` 使用远程 `replace`
固定到 fork 的提交 `9f121787bb563fe655d2789cbf6dbcc7ae19a0c9`：

```go
replace github.com/metacubex/mihomo => github.com/Herta-Network/mihomo v1.19.34-0.20261031193628-9f121787bb56
```

更新依赖时先解析 fork 的指定提交，再把得到的完整 pseudo-version 写入替换项：

```bash
go mod download -json github.com/Herta-Network/mihomo@<commit>
go mod edit -replace=github.com/metacubex/mihomo=github.com/Herta-Network/mihomo@<pseudo-version>
go mod tidy
```

不要在发布产物中使用浮动分支或本地 `replace`，这样才能保证 native core 可复现。
作为其他 Go 项目的依赖使用时，调用方也需要配置此替换项；Go 不继承依赖模块的 `replace`。

## 验证

```bash
go test -race -tags 'foss,with_low_memory,no_tailscale,no_fake_tcp' ./...
go vet -tags 'foss,with_low_memory,no_tailscale,no_fake_tcp' ./...
```

测试覆盖失败下载保留旧配置、HTTP/provider 错误、override 文件释放、节点切换失败
保留连接，以及宿主 DNS 切换。平台构建和真机 VPN 行为仍需分别验证。
