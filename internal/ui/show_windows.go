//go:build windows

package ui

import (
	"github.com/hyperdesign/hyper-stack/internal/config"
	"github.com/hyperdesign/hyper-stack/internal/gui"
)

// showWindow brings the running window to the front.
func showWindow(config.Paths) bool { return gui.Show() }
