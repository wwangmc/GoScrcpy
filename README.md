# GoScrcpy

用 **Go + Wails v2** 重写 QtScrcpy 的桌面核心功能，同时支持 Android 和安装了 [`mks_tweak`](https://github.com/wwangmc/mks_tweak) 的越狱 iOS 手机。两类设备在同一列表中显示，共用投屏窗口、输入和快捷控制，不需要切换平台页面。

不依赖 Qt、QtScrcpyCore、FFmpeg 或外部 scrcpy 桌面进程。Android 服务端固定为官方 scrcpy **3.3.4**，已嵌入程序；iOS 使用手机上已有的 `mks_tweak`，USB 转发在 Go 内完成，不需要安装或配置 iproxy。

![独立投屏窗口与右侧工具栏（浏览器诊断模式）](docs/window-preview.png)

## 已实现

- 自动检测设备，每 3 秒刷新：ADB 设备与 Apple USB 设备合并到同一列表，区分就绪、未授权、离线和手机服务不可用状态。
- 独立的手机连接窗口，支持自动刷新、选择连接和双击设备打开投屏。
- 每台设备使用独立的 Wails 投屏窗口，显示 H.264 或 JPEG，并自动选择对应解码器；可同时打开多台手机。
- 手机画面铺满窗口，首次显示和横竖屏切换时按手机比例适配窗口。浮动侧栏默认隐藏，鼠标移到窗口最右端 6 px 内显示，移开约 350 ms 后自动收起；鼠标停留在按钮或设置面板上可连续操作，手机拖动操作不会触发侧栏。按钮与设置覆盖在画面上，显示和收起都不增加窗口宽度。支持画面参数、快捷按键、发送文字，以及文件管理；Android 还支持查 IP、获取当前应用包名和跳转商店。粘贴使用电脑剪贴板，iOS 按设备能力禁用不支持的操作。
- 鼠标点击、长按、拖动、滚轮、右键（Android 返回，iOS 返回桌面）；键盘输入、方向键、Enter、Backspace、Escape。
- 按设备能力启用主页、最近任务、电源、返回和音量等快捷按钮；没有对应接口的操作会禁用。
- 共用「发送文字」：Android 通过剪贴板粘贴，iOS 使用 MKS `/input`，支持 Unicode。需要先聚焦手机输入框，兼容性取决于手机系统、插件版本和输入框。
- 分辨率、最大帧率、码率设置按连接能力启用；图片尺寸变化后更新画布与输入坐标。MKS 保留原始分辨率、使用与原 ios_control 相同的 JPEG 质量 0.1，最高 60 FPS，不支持的分辨率/码率选项会禁用。
- 停止、断线和退出时关闭本程序的会话、USB 连接、ADB 转发、服务进程及临时文件，并在正常停止时释放未抬起的触摸。

可以同时连接和列出多台手机，每个投屏窗口只操作对应设备。关闭或停止一个投屏窗口不会影响其他手机；关闭连接窗口后已打开的投屏窗口可继续使用。同一连接窗口会避免重复打开相同设备，关闭对应窗口后可再次打开。各窗口的 WebView2 数据目录独立，避免多进程争用。音频、录屏、APK 安装、游戏按键映射、群控、无线配对等功能未实现。已有 ADB 网络连接也可检测；MKS 连接始终只走 USB。

## 运行

Windows 构建产物：`build/bin/GoScrcpy.exe`。

1. 用 USB 连接手机并允许手机上的授权。Android 开启 USB 调试；越狱 iOS 需安装并运行 `mks_tweak`，手机服务端口为 `8081`。
2. Android 需要 ADB（产物目录已附带）。iOS 需要 Apple USB 驱动和正在运行的 Apple Mobile Device Service；macOS/Linux 使用本机 usbmuxd。仅使用 iOS 时不要求安装 ADB。
3. 运行 GoScrcpy，在连接窗口中双击手机，或选择后点击「连接选中手机」。程序自动打开该手机的独立投屏窗口，继续选择其他设备可同时投屏。
4. 点击画面即可操作；鼠标移到窗口最右端显示工具栏，点击「设置」悬浮展开面板。鼠标移开后侧栏自动隐藏，Escape 也可收起；在文字框输入或操作下拉框时会保持可见，离开窗口时收起。画面没有额外的上下状态栏，状态提示短暂浮现后消失，诊断日志通过工具栏打开。修改画面参数后先停止、再重新连接即可生效；发送文字前先聚焦手机输入框。点击「文件」会打开该手机的独立文件管理窗口，也可以直接在连接窗口右键点击设备后选择「文件管理」。

需要 Windows 10/11 和 Microsoft Edge WebView2 Runtime，Android 视频需要其支持 WebCodecs H.264 解码。Android 设备最低为 Android 5.0，剪贴板粘贴通常需要 Android 7.0+。部分厂商机型还需要开启「USB 调试（安全设置）」才能注入输入事件。iOS 版本支持范围取决于已安装的 `mks_tweak`；当前真机验证为 iOS 15.6。

`mks_tweak` 当前没有标准返回键和音量 HTTP 接口，这些按钮对该设备禁用。支持点击、单点拖动、滚轮转换为滑动、Home、双击 Home 打开任务视图、电源、文字、Enter、Tab、Backspace 和左右光标。现有插件源码将 move 传入 touchDown，拖动已在这台 iOS 15.6 手机上验证，其他插件/系统版本仍需确认实际效果。

ADB 查找顺序：`ADB_PATH` → 可执行程序旁的 `adb.exe` / `tools/adb.exe` → 当前工作目录 `tools/adb.exe` → PATH → `ANDROID_HOME` / `ANDROID_SDK_ROOT` 下的 platform-tools。

## 文件管理与双向传输

连接窗口仅保留设备列表；右键设备可选「连接手机」或「文件管理」，双击仍直接打开投屏。自动刷新不再改变刷新按钮的外观。

程序会记住上次选中的手机，并分别记住每台手机上次成功打开的文件范围和目录，重新打开窗口或重启后恢复。上次目录不可访问时回到默认位置。配置保存到用户配置目录中的 GoScrcpy 文件夹；可通过 `GOSCRCPY_CONFIG_DIR` 指定配置目录。

在连接窗口右键点击设备后选择「文件管理」，或在投屏右侧工具栏点击「文件」。文件管理使用独立窗口，不改变投屏尺寸；关闭文件窗口不会停止投屏。也可以用 `GoScrcpy.exe --files <设备序列号>` 直接打开。

- iOS 移植 PocketFiles 的目录浏览、上传、下载、新建文件夹、重命名、传输进度和取消功能，支持 AFC2 根目录、AFC 媒体目录及应用共享文档。AFC2 不可用时，首次打开自动尝试媒体目录；文件服务不依赖 MKS 投屏服务。
- Android 使用相同界面，默认浏览 `/sdcard`，提供 Download、DCIM、Pictures、Documents 快捷入口。目录权限沿用手机授予 ADB 的权限，不能读取的目录会显示错误并保留原目录。
- 双击目录进入；可在路径框直接输入目录。选择电脑文件上传到手机当前目录；选择手机普通文件后点击「下载到电脑」，通过系统对话框选择保存位置。支持拖入多个电脑文件按顺序上传，暂不复制整个文件夹、不自动安装 APK、不提供删除功能。
- 可直接从资源管理器或桌面拖入文件：拖到文件管理窗口上传至当前目录；拖到连接窗口或投屏画面则发送到 Android 的 `/sdcard/Download` 或 iOS 媒体根目录。拖入时显示目标提示，批量上传显示总字节进度和当前文件序号，可取消剩余传输；已经完成的文件会保留。链接、文件夹及同名冲突会明确提示，不会触发浏览器下载或跳转。
- 支持当前目录筛选、按名称或大小排序，以及 F2 重命名、F5 刷新、Ctrl+L 定位路径框、Backspace 返回上级。
- 内容在 Go 后端按块流式传输，不经过前端或云端。显示字节进度，支持取消。上传先写随机临时文件，校验大小后再发布；下载写入电脑临时文件，同步完成后发布。拒绝覆盖已有目标，失败/取消会清理本次临时文件；设备完全断开时可能无法清理手机上的临时文件。
- 共享应用是否允许访问文档由 iOS 决定。越狱侧载应用可能被系统拒绝，可改用 AFC2 根目录定位其文件。

验证：Android Pixel 2 双向传输 2,520,000 字节、iPhone 双向传输 2,640,000 字节，逐字节一致；中文及特殊字符文件名、同名拒绝、重命名、取消清理通过。iOS 根目录、媒体目录和可用共享应用文档均经 USB 真机验证。

## USB 与端口

- 手机上的服务仍为 `8081`，电脑端**不监听或占用 8081**，也不复用已有 iproxy 进程的端口。
- Go 直接向 USBMux 发出带设备 ID 的 Connect 请求，相当于内置 iproxy。设备先按 USB 连接类型和 UDID 查找，各手机的通道相互独立，相同手机端口不会冲突，也不访问手机的局域网 IP。
- Windows 使用已有 Apple USB 服务的 `127.0.0.1:27015`，macOS/Linux 使用 `/var/run/usbmuxd`。这些是系统 USB 服务地址，不是本程序创建的固定转发监听端口。
- 本程序的 WebSocket 桥接使用 `127.0.0.1:0`，由系统随机分配空闲端口；USBMux 客户端 TCP 源端口也由系统分配。多个窗口或其他映射工具可以并存。
- 使用与原 ios_control 相同的 `/screenshot?quality=0.1`，204 表示保留上一帧，复用 USB HTTP 连接且每次只发一个截图请求；首次遇到 204 时单独取原始截图初始化窗口，不重置其他客户端的 dirty frame 状态。
- 控制使用独立 USB WebSocket，移动事件间隔 50ms，避免逐次等待 HTTP。旧插件没有控制 WebSocket 时回退 HTTP。性能对比见 [PERFORMANCE.md](docs/PERFORMANCE.md)。

## 开发与构建

使用 Go 1.23+。前端是原生 JavaScript 和 CSS，无 npm 构建依赖。Windows 脚本将 Go 依赖、工具和编译缓存保存在项目 `.cache/` 中。

```powershell
# 在 GoScrcpy 目录运行
.\scripts\dev.ps1
.\scripts\build.ps1

# 也可以将当前电脑的 ADB 及 DLL 复制到产物目录
.\scripts\build.ps1 -Portable

# 单元测试、协议测试、静态检查（前端测试使用 Node 22+）
.\scripts\test.ps1

# 真机集成测试：会在指定设备上启动投屏并按主页键
.\scripts\test.ps1 -DeviceSerial '你的设备序列号'

# 同时执行 Android 与 iOS 的 USB 真机集成测试
.\scripts\test.ps1 -DeviceSerial 'Android序列号' -IOSDeviceSerial 'iOS的UDID'

# 原生桌面自检：在 Wails / WebView2 中绘制有效画面、发送主页键、停止并退出
# 不依赖系统截图工具，不需要鼠标键盘操作
.\scripts\smoke.ps1 -DeviceSerial 'Android序列号或iOS的UDID'
```

也可使用全局安装的 Wails：`wails dev` / `wails build`。只运行 `go run .` 不会进入 Wails 生产构建，优先使用上述脚本。

浏览器诊断模式使用同一套 Go 后端和前端：

```powershell
# 控制台构建，便于读取打印出的本机 URL
go build -tags production -o bin/GoScrcpy-console.exe .
.\bin\GoScrcpy-console.exe --web
```

打开终端输出的带随机 token 的地址。HTTP / WebSocket 仅监听 `127.0.0.1`，API 和视频通道均需要随机 token；此模式不支持远程网络访问。关闭浏览器会结束当前视频会话，Ctrl+C 关闭诊断服务。

## 结构

```text
main.go                  Wails 入口与嵌入资源
app.go                   设备 / 会话绑定、本机 WebSocket 桥接
windows.go               独立投屏进程、窗口去重与退出跟踪
tools.go                 Android IP、前台包名和商店
files.go / files_web.go  统一文件管理、系统文件对话框、进度和取消
internal/phone/         PocketFiles 移植：iOS AFC2 / AFC / 共享应用文档
internal/afc/           AFC 协议适配与上游 MIT 许可证
internal/androidfiles/  Android 目录操作及 ADB sync 流式双向传输
session.go               统一投屏会话接口
internal/device/         共用设备描述与能力
internal/adb/            ADB 查找、命令超时、设备解析
internal/scrcpy/         服务启动、双通道连接、H.264 分包、输入包校验
internal/usbmux/         Apple USB 设备枚举与按设备隔离的端口连接
internal/mks/            MKS 截图与独立 WebSocket 输入通道
internal/process/        Windows 子进程隐藏控制台
frontend/                设备列表、WebCodecs 渲染、鼠标和键盘输入
assets/                  固定版本 Android 服务端和原始许可证
scripts/                 开发、构建与验证脚本
```

视频链路：Android MediaCodec → ADB forward → Go 读取 scrcpy 包 → 本机 WebSocket → WebCodecs → canvas。控制走同一个 WebSocket 的反方向，由 Go 校验后转发至独立的 scrcpy control socket。每次启动使用独立 SCID 和动态端口，不调用 `adb kill-server`，也不结束其他应用的投屏会话。

iOS 视频链路：mks_tweak `/screenshot?quality=0.1` → USBMux 设备端口 8081 → 同一个本机 WebSocket → ImageBitmap → 同一个 canvas。共用输入消息由 Go 转成独立 USB WebSocket 的 touch/inputText/backspace/moveCursor 消息，或 Home/任务/电源等 HTTP API。只使用投屏和输入 API，未修改 `mks_tweak` 源码或手机服务配置。

## 验证记录

2026-10-22，按用户要求恢复覆盖式布局并加入自动隐藏：默认隐藏工具栏，实测鼠标到达右端可反复唤出，移开自动收起，设置展开前后窗口和投屏区域尺寸不变。此前向右扩展窗口的侧栏方案已撤回。

2026-10-22，侧栏遮挡修复：按钮栏移到手机画面外侧，普通窗口展开/收起设置时增加/恢复对应宽度。浏览器实测收起和展开时画面与侧栏均无重叠，按手机比例检查了完整画面和外侧按钮栏；Go 测试及 Pixel 2 原生解码自检通过。

2026-10-20，窗口拆分版本：通过 Android 与 iOS USB 集成测试、独立窗口进程生命周期测试、前端协议测试和 Go 静态检查；Pixel 2 与 iPhone 原生 WebView2 自检均成功绘制至少 5 帧。从连接页面分别打开了 Pixel 2 和 iPhone 的原生投屏窗口，关闭 Pixel 2 窗口后 iPhone 仍持续显示画面。连接窗口、设置面板收起/展开和 Android 前台包名查询已通过浏览器诊断模式检查。

2026-10-20，Pixel 2 / Android 10：已验证 USB 检测与授权、1080×1920 视频配置和关键帧、前端画面解码显示、点击打开设置、拖动列表、滚轮滚动、主页和键盘 Escape 返回。Go 集成测试额外验证了 720×1280 投屏、WebSocket 视频、控制包传输、重复启动、断线及主动停止后的 ADB 转发回收。

原生 Wails 桌面自检亦已通过：实际 WebView2 的 VideoDecoder 输出并绘制了 5 帧 1080×1920 画面，随后停止会话、回收 ADB 转发并退出。其他系统和手机仍需实际验证。

iPhone11,8 / iOS 15.6：已通过 USB 读取 MKS 设备信息并在原生 Wails 绘制 828×1792 图像；真机点击打开设置、拖动滚动列表及 Home 已验证。统一 WebSocket 集成测试连续执行两次，验证图像、输入、断开、主动停止与重新连接。测试还模拟两台手机使用相同服务端口、电脑 8081 被占用、屏蔽 Wi-Fi 设备、独立 WebSocket 文字与触摸、坐标缩放、异常后的抬手、HTTP 控制回退及 204 无变化响应。新版截图采集在这台手机的滑动短测中达到约 50 FPS，旧 MJPEG 路径约 7.6 FPS，详细条件和测量范围见性能文档。

## 上游

- [Wails v2 文档](https://v2.wails.io/docs/introduction/)
- [scrcpy 3.3.4 协议与开发文档](https://github.com/Genymobile/scrcpy/blob/v3.3.4/doc/develop.md)
- [scrcpy 3.3.4 官方服务端](https://github.com/Genymobile/scrcpy/releases/tag/v3.3.4)
- [控制协议序列化](https://github.com/Genymobile/scrcpy/blob/v3.3.4/app/src/control_msg.c)

嵌入服务端 SHA-256：`8588238c9a5a00aa542906b6ec7e6d5541d9ffb9b5d0f6e1bc0e365e2303079e`。Android 服务端使用 Apache-2.0 许可证，原文见 `assets/scrcpy-LICENSE`。原 QtScrcpy 源码保留在同级 `../QtScrcpy/`，本工程未复制其 C++ 核心。
