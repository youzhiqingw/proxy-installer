package main

import (
	"context"
	"embed"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/windows/icon.ico
var trayIcon []byte

func main() {
	// Create an instance of the app structure
	app := NewApp()
	root, _ := proxyDataRoot()
	dirs, _ := ensureProxyDirs()
	if dirs == nil {
		dirs = map[string]string{"root": root, "webview": filepath.Join(root, "webview")}
	}
	systray.Register(func() { configureTray(app) }, nil)

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "Proxy Installer",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		OnShutdown: func(ctx context.Context) {
			systray.Quit()
		},
		HideWindowOnClose: true,
		Windows: &windows.Options{
			WebviewUserDataPath: dirs["webview"],
			Theme:               windows.SystemDefault,
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

func configureTray(app *App) {
	systray.SetIcon(trayIcon)
	systray.SetTitle("Proxy Installer")
	systray.SetTooltip("Proxy Installer 正在后台运行")

	openItem := systray.AddMenuItem("打开主界面", "显示 Proxy Installer 主窗口")
	statusItem := systray.AddMenuItem(trayStatusTitle(), "当前后台状态")
	statusItem.Disable()
	dataItem := systray.AddMenuItem("打开数据目录", "打开本机数据留档目录")
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("退出 Proxy Installer", "关闭后台运行的 Proxy Installer")

	go func() {
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-openItem.ClickedCh:
				app.showMainWindow()
			case <-dataItem.ClickedCh:
				openProxyDataDir()
			case <-quitItem.ClickedCh:
				systray.Quit()
				app.quitFromTray()
				return
			case <-ticker.C:
				statusItem.SetTitle(trayStatusTitle())
				systray.SetTooltip(fmt.Sprintf("Proxy Installer 后台运行中，已保存 %d 台 VPS", savedProfileCount()))
			}
		}
	}()
}

func trayStatusTitle() string {
	return fmt.Sprintf("状态：后台运行 / 已保存 %d 台 VPS", savedProfileCount())
}

func openProxyDataDir() {
	dirs, err := ensureProxyDirs()
	if err != nil {
		return
	}
	_ = exec.Command("explorer.exe", dirs["root"]).Start()
}
