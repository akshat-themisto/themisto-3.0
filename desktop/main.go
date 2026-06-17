package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

const themistoWindowClassName = "ThemistoDesktopWindow"
const themistoAppUserModelID = "Themisto.Desktop.Employee"
const themistoSingleInstanceID = "E87B4F2A-7C3D-4A1E-B9F5-2D6E8A9C0B1F"

// isBackgroundLaunch checks whether --background was passed on the command line.
// Used by the HKCU Run key to start the desktop app hidden at login.
func isBackgroundLaunch() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--background" || arg == "-background" {
			return true
		}
	}
	return false
}

func main() {
	setCurrentProcessAppUserModelID(themistoAppUserModelID)

	app := NewApp()

	err := wails.Run(&options.App{
		Title:             "Themisto",
		Width:             1280,
		Height:            800,
		StartHidden:       isBackgroundLaunch(),
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 26, G: 29, B: 33, A: 1},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnShutdown:       app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               themistoSingleInstanceID,
			OnSecondInstanceLaunch: app.onSecondInstance,
		},
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windows.Dark,
			WindowClassName:      themistoWindowClassName,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
