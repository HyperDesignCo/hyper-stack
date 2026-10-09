package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hyperdesign/hyper-stack/internal/config"
	"github.com/hyperdesign/hyper-stack/internal/selfinstall"
	"github.com/hyperdesign/hyper-stack/internal/ui"
)

func selfInstallCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "self-install",
		Short: "Install hyper.exe for this user: PATH, Start menu shortcut, optional start at login",
		Long: `Copies hyper.exe to %LOCALAPPDATA%\HyperStack\app, adds that folder to your user PATH,
and creates a Start menu shortcut for the dashboard. No administrator rights needed.
Open a new terminal afterwards so the new PATH is picked up.`,
		Args: cobra.NoArgs,
	}
	c.Flags().Bool("autostart", false, "start the dashboard tray icon when you log in")
	c.Flags().Bool("no-shortcut", false, "skip the Start menu shortcut")
	c.Flags().Bool("remove", false, "undo: remove PATH entry, shortcut and autostart (keeps all data)")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		auto, _ := cmd.Flags().GetBool("autostart")
		noLnk, _ := cmd.Flags().GetBool("no-shortcut")
		remove, _ := cmd.Flags().GetBool("remove")
		p, err := config.Default()
		if err != nil {
			return err
		}
		if err := p.Ensure(); err != nil {
			return err
		}
		if remove {
			if err := selfinstall.Remove(filepath.Join(p.Root, "app")); err != nil {
				return err
			}
			fmt.Println("Removed hyper from your PATH, the Start menu and login autostart. Your projects and data are untouched.")
			return nil
		}
		cur, _ := os.Executable()
		dir := filepath.Join(p.Root, "app")

		// A running dashboard holds the old exe open: stop it, install, bring it back.
		wasRunning := ui.Stop(p, cur)
		res, err := selfinstall.Install(dir, selfinstall.Options{Shortcut: !noLnk, Autostart: auto})
		if err != nil {
			return err
		}
		if res.Copied {
			fmt.Println("Installed", res.ExePath)
		} else {
			fmt.Println("Already running from", res.ExePath)
		}
		if res.PathAdded {
			fmt.Println("Added to your PATH. Open a NEW terminal to use the hyper command.")
		} else {
			fmt.Println("PATH already contains it.")
		}
		if res.ShortcutPath != "" {
			fmt.Println("Start menu shortcut:", res.ShortcutPath)
		}
		if res.Autostart {
			fmt.Println("The dashboard will start when you log in.")
		}
		if wasRunning {
			if _, err := ui.Launch(p, res.ExePath, false); err != nil {
				return err
			}
		}
		fmt.Println("Next: run  hyper install   (or open Hyper Stack from the Start menu)")
		return nil
	}
	return c
}
