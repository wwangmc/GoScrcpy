# Android curl

`curl` 由项目使用者提供，用于手机缺少 curl 时经 ADB 自动推送。

- ELF 64-bit，AArch64 / ARM64-v8a，动态加载器 `/system/bin/linker64`。
- 大小：3,557,552 字节。
- SHA-256：`8df5d2828dd6bc3779c34fffe5bdd820fe9ff61577cc19f03b48cbf20eec6456`。
- 通过 `go:embed` 打包进程序；编译和运行均不依赖项目外层的 `curl` 文件。
- 仅在 Android 缺少系统 curl 时使用，推送后先校验、再检查 `--version`，不兼容时给出错误。
