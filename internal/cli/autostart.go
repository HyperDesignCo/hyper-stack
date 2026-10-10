package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/hyperdesign/hyper-stack/internal/app"
)

// autostartCmd is "hyper autostart": what starts by itself when you sign in to Windows.
func autostartCmd(run func(func(context.Context, *app.App, []string) error) func(*cobra.Command, []string) error) *cobra.Command {
	root := &cobra.Command{
		Use:   "autostart",
		Short: "Choose what starts by itself when you sign in to Windows",
		Long: `Shows what starts automatically. Two separate switches:

  hyper autostart on | off       start Hyper Stack (tray icon and dashboard) when you sign in
  hyper autostart add [folder]   start this project whenever Hyper Stack starts at sign-in
  hyper autostart remove [folder]

The folder may be a path, a project name from www, or empty for the current folder.
Projects with first-time setup commands you have not reviewed yet are never started automatically.`,
		Args: cobra.NoArgs,
		RunE: run(func(_ context.Context, a *app.App, _ []string) error {
			a.AutostartStatus()
			return nil
		}),
	}
	root.AddCommand(&cobra.Command{
		Use: "on", Short: "Start Hyper Stack when you sign in to Windows", Args: cobra.NoArgs,
		RunE: run(func(_ context.Context, a *app.App, _ []string) error {
			if err := a.SetLoginAutostart(true); err != nil {
				return err
			}
			a.AutostartStatus()
			return nil
		}),
	})
	root.AddCommand(&cobra.Command{
		Use: "off", Short: "Do not start Hyper Stack at sign-in", Args: cobra.NoArgs,
		RunE: run(func(_ context.Context, a *app.App, _ []string) error {
			if err := a.SetLoginAutostart(false); err != nil {
				return err
			}
			a.AutostartStatus()
			return nil
		}),
	})
	root.AddCommand(&cobra.Command{
		Use: "add [folder]", Short: "Start this project automatically", Args: cobra.MaximumNArgs(1),
		RunE: run(func(_ context.Context, a *app.App, args []string) error {
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			if err := a.SetProjectAutostart(arg, true); err != nil {
				return err
			}
			a.AutostartStatus()
			return nil
		}),
	})
	root.AddCommand(&cobra.Command{
		Use: "remove [folder]", Short: "Stop starting this project automatically", Args: cobra.MaximumNArgs(1),
		RunE: run(func(_ context.Context, a *app.App, args []string) error {
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			if err := a.SetProjectAutostart(arg, false); err != nil {
				return err
			}
			a.AutostartStatus()
			return nil
		}),
	})
	return root
}
