//go:build windows

package tray

import (
	"fyne.io/systray"

	"github.com/hyperdesign/hyper-stack/internal/brand"
)

// Run shows the tray icon and blocks until Quit is chosen or stop() is called.
func Run(open, stopAll func(), onQuit func()) {
	systray.Run(func() {
		systray.SetIcon(brand.ICO())
		systray.SetTitle("Hyper Stack")
		systray.SetTooltip("Hyper Stack: local Laravel environments")
		mOpen := systray.AddMenuItem("Open dashboard", "Open the Hyper Stack dashboard")
		mStop := systray.AddMenuItem("Stop all services", "Stop nginx, PHP and MySQL")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit tray", "Close the dashboard and this icon (services keep running)")
		systray.SetOnTapped(open) // left-click on the icon opens the dashboard
		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					open()
				case <-mStop.ClickedCh:
					stopAll()
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}, onQuit)
}

// Stop removes the icon and makes Run return.
func Stop() { systray.Quit() }

// Icon is the tray icon as an .ico file's bytes.
func Icon() []byte { return brand.ICO() }
