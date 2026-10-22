# iOS USB 采集性能

2026-10-20，同一台 iPhone11,8 / iOS 15.6，使用已有 mks_tweak，经同一 USBMux 连接，在主屏幕左右滑动时分别短测约 4 秒。以下是电脑收到图像的速度，**不等同于最终窗口显示帧率，也不是所有机型的保证值**。

| 路径 | 收图 FPS | 每帧平均大小 | 收图/请求耗时中位数 |
| --- | ---: | ---: | ---: |
| 旧版 `/screenshotStream?quality=0.55&fps=30` | 7.61 | 115.0 KiB | 126.3 ms |
| `/screenshotStream?quality=0.1&fps=30` | 7.87 | 48.8 KiB | 122.2 ms |
| 原 `ios_control` 的 `/screenshot?quality=0.1`，25ms 请求间隔 | 38.84 | 49.8 KiB | 20.5 ms |
| 新版 Go MKS 会话，截图路径，限制 30 FPS | 29.63 | 62.3 KiB | 33.7 ms（含帧率等待） |
| 新版 Go MKS 会话，截图路径，限制 60 FPS | 50.28 | 62.3 KiB | 19.8 ms |

原 `ios_control/src/components/DeviceScreen.vue` 使用 `/screenshot?quality=0.1`，画面未变化时处理 204，并用独立 WebSocket 发送触摸，移动事件间隔为 50ms。新版采用同样的采集与输入路径，截图请求复用 USB HTTP 连接，每次只保留一个在途请求，前端仅保留最新待解码图像。

MJPEG 生产者和 `/screenshot` 使用不同的截图实现。在这台已安装插件的手机上，仅降低 MJPEG 质量无法解决帧率瓶颈，因此新版不再优先选择流接口。没有修改手机插件。

静止画面返回 204 时不重复传送或绘制图像，FPS 降低属于正常情况；不能用静止桌面上的 FPS 数字判断滑动时的流畅度。首帧若因其他客户端已取走 dirty frame 而收到 204，会单独请求 `/screenshot_original`，保证新窗口有初始画面，不重置其他客户端的检测状态。

触摸、文字、删除和左右光标通过独立 USB WebSocket 发送，避免每次移动都等待 HTTP 响应；Home、任务、电源和 Enter/Tab 使用对应 HTTP API。旧插件无法建立控制 WebSocket 时回退 HTTP。正常结束会先发送抬手，再关闭连接。
