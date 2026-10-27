package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"phonescreen/internal/scrcpy"
)

//go:embed frontend/index.html frontend/style.css frontend/app.js frontend/files.js frontend/drop.js frontend/shell.js frontend/ios-settings.js frontend/tools.js frontend/protocol.js frontend/app-icon.png
var frontend embed.FS

//go:embed assets/scrcpy-server-v3.3.4
var server []byte

func main() {
	shellSerial := flag.String("shell", "", "在独立命令行窗口连接指定手机")
	shellPlatform := flag.String("shell-platform", "", "命令行设备平台：android 或 ios")
	fileSerial := flag.String("files", "", "在独立窗口管理指定手机的文件")
	web := flag.Bool("web", false, "使用本机浏览器界面（诊断用途）")
	smokeSerial := flag.String("smoke-test", "", "在指定设备上执行 Wails 原生投屏自检，完成后退出")
	mirrorSerial := flag.String("mirror", "", "在独立窗口中连接指定设备")
	maxSize := flag.Int("max-size", 1920, "最大分辨率")
	maxFPS := flag.Int("max-fps", 60, "最大帧率")
	bitRate := flag.Int("bit-rate", 8000000, "视频码率")
	videoMode := flag.String("video-mode", "auto", "投屏方式：auto、jpeg、h264")
	flag.Parse()
	if *shellSerial != "" {
		if err := runNativeShell(*shellSerial, *shellPlatform); err != nil {
			log.Print(err)
		}
		return
	}
	app, err := NewApp(server)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		app.close()
		// WebView2 releases its profile after Wails has returned. This directory
		// was created solely for this App, so it is safe to remove in full.
		for attempt := 0; attempt < 10; attempt++ {
			if os.RemoveAll(app.tempDir) == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	app.smokeSerial = *smokeSerial
	app.mirrorSerial = *mirrorSerial
	app.fileSerial = *fileSerial
	app.launchSettings = scrcpy.Options{MaxSize: *maxSize, MaxFPS: *maxFPS, BitRate: *bitRate, VideoMode: *videoMode}
	files, _ := fs.Sub(frontend, "frontend")
	if *web {
		app.enableWeb(files)
		fmt.Println(app.webURL())
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt)
		<-interrupt
		return
	}
	title, width, height, minWidth, minHeight := "GoScrcpy · 连接手机", 400, 650, 340, 480
	if *mirrorSerial != "" || *smokeSerial != "" {
		title, width, height, minWidth, minHeight = "GoScrcpy · "+*mirrorSerial, 480, 880, 240, 320
	}
	if *fileSerial != "" {
		title, width, height, minWidth, minHeight = "GoScrcpy · 文件管理", 1060, 740, 640, 480
	}
	err = wails.Run(&options.App{
		Title: title, Width: width, Height: height, MinWidth: minWidth, MinHeight: minHeight,
		BackgroundColour: &options.RGBA{R: 13, G: 17, B: 25, A: 255},
		AssetServer:      &assetserver.Options{Assets: files},
		// Keep external drag events enabled: DisableWebViewDrop would also prevent
		// Windows from delivering the native file objects needed for uploads.
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},
		Windows:     &windows.Options{WebviewUserDataPath: filepath.Join(app.tempDir, "webview"), Theme: windows.Dark},
		OnStartup:   app.startup, OnShutdown: app.shutdown,
		Bind: []interface{}{app},
	})
	if err != nil {
		log.Print(err)
	}
}
