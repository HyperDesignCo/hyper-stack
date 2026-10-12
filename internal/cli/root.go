// Package cli defines the cobra commands. Logic lives in package app.
package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hyperdesign/hyper-stack/internal/app"
	"github.com/hyperdesign/hyper-stack/internal/config"
	"github.com/hyperdesign/hyper-stack/internal/elevate"
	"github.com/hyperdesign/hyper-stack/internal/ui"
)

// Set at build time:
//
//	-ldflags "-X github.com/hyperdesign/hyper-stack/internal/cli.Version=1.2.3 -X ...Commit=abc1234 -X ...Date=2026-10-09"
var (
	Version = "0.0.1-dev"
	Commit  = "none"
	Date    = "unknown"
)

// Execute runs the root command.
func Execute() error {
	root := &cobra.Command{
		Use:           "hyper",
		Short:         "Hyper Stack: local Laravel environments for Hyper Design",
		Version:       fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().Bool("verbose", false, "print detailed output")

	run := func(f func(ctx context.Context, a *app.App, args []string) error) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			v, _ := cmd.Flags().GetBool("verbose")
			a, err := app.New(os.Stdout, v)
			if err != nil {
				return err
			}
			defer a.Close()
			return f(cmd.Context(), a, args)
		}
	}
	cwd := func() string { d, _ := os.Getwd(); return d }

	install := &cobra.Command{
		Use:   "install",
		Short: "Install the local stack (PHP, nginx, MySQL, phpMyAdmin, Composer, Node) into a folder you choose",
		Long: `Installs everything into ONE folder you choose (like C:\laragon), with a "www"
folder inside it for all your projects. Every folder in www becomes <folder>.test.`,
		Args: cobra.NoArgs,
	}
	install.Flags().String("dir", "", "install folder (asked interactively when omitted)")
	install.Flags().Int("http-port", 0, "web port if 80 is taken (for example by Laragon)")
	install.Flags().Int("https-port", 0, "HTTPS port if 443 is taken")
	install.Flags().Int("mysql-port", 0, "MySQL port if 3306 is taken")
	install.RunE = run(func(ctx context.Context, a *app.App, _ []string) error {
		dir, _ := install.Flags().GetString("dir")
		if dir == "" && !a.P.Configured {
			var err error
			if dir, err = askInstallDir(); err != nil {
				return err
			}
		}
		hp, _ := install.Flags().GetInt("http-port")
		sp, _ := install.Flags().GetInt("https-port")
		mp, _ := install.Flags().GetInt("mysql-port")
		return a.InstallTo(ctx, dir, app.Ports{HTTP: hp, HTTPS: sp, MySQL: mp})
	})
	root.AddCommand(install)

	up := &cobra.Command{
		Use:   "up",
		Short: "Start the project in the current folder (or every project in www with --all)",
		Args:  cobra.NoArgs,
	}
	up.Flags().Bool("all", false, "start every project in the www folder")
	up.RunE = run(func(ctx context.Context, a *app.App, _ []string) error {
		if all, _ := up.Flags().GetBool("all"); all {
			return a.UpAll(ctx)
		}
		return a.Up(ctx, cwd())
	})
	root.AddCommand(up)

	root.AddCommand(autostartCmd(run))
	root.AddCommand(phpCmd(run))
	root.AddCommand(moveCmd(run))

	root.AddCommand(&cobra.Command{
		Use: "www", Short: "Open the www folder where your projects live", Args: cobra.NoArgs,
		RunE: run(func(ctx context.Context, a *app.App, _ []string) error { return a.OpenWWW() }),
	})
	down := &cobra.Command{
		Use: "down", Short: "Stop the project's workers (--all stops shared services too)", Args: cobra.NoArgs,
		RunE: run(func(ctx context.Context, a *app.App, _ []string) error { return nil }),
	}
	down.Flags().Bool("all", false, "also stop nginx, PHP and MySQL")
	down.RunE = func(cmd *cobra.Command, _ []string) error {
		v, _ := cmd.Flags().GetBool("verbose")
		all, _ := cmd.Flags().GetBool("all")
		a, err := app.New(os.Stdout, v)
		if err != nil {
			return err
		}
		defer a.Close()
		dir := cwd()
		if all {
			dir = ""
		}
		return a.Down(cmd.Context(), dir, all)
	}
	root.AddCommand(down)
	root.AddCommand(&cobra.Command{
		Use: "status", Short: "Show services and registered projects", Args: cobra.NoArgs,
		RunE: run(func(ctx context.Context, a *app.App, _ []string) error { return a.Status(ctx) }),
	})
	doc := &cobra.Command{
		Use: "doctor", Short: "Check the environment for problems", Args: cobra.NoArgs,
	}
	doc.Flags().Bool("fix", false, "apply safe fixes (CA trust, hosts permission and entries)")
	doc.RunE = func(cmd *cobra.Command, _ []string) error {
		v, _ := cmd.Flags().GetBool("verbose")
		fix, _ := cmd.Flags().GetBool("fix")
		a, err := app.New(os.Stdout, v)
		if err != nil {
			return err
		}
		defer a.Close()
		return a.Doctor(cmd.Context(), fix)
	}
	root.AddCommand(doc)
	root.AddCommand(&cobra.Command{
		Use: "db:open", Short: "Open phpMyAdmin on the project's database", Args: cobra.NoArgs,
		RunE: run(func(ctx context.Context, a *app.App, _ []string) error { return a.DBOpen(ctx, cwd()) }),
	})
	root.AddCommand(&cobra.Command{
		Use: "db:info", Short: "Show the MySQL host, port, user and password (for plain PHP projects)", Args: cobra.NoArgs,
		RunE: run(func(ctx context.Context, a *app.App, _ []string) error { return a.DBInfo() }),
	})
	dbImport := &cobra.Command{
		Use: "db:import <file.sql[.gz]>", Short: "Import a SQL dump into the project's database", Args: cobra.ExactArgs(1),
	}
	dbImport.Flags().Bool("fresh", false, "empty the database first (its old contents are saved to a backup file)")
	dbImport.RunE = run(func(ctx context.Context, a *app.App, args []string) error {
		fresh, _ := dbImport.Flags().GetBool("fresh")
		return a.DBImport(ctx, cwd(), args[0], fresh)
	})
	root.AddCommand(dbImport)
	root.AddCommand(&cobra.Command{
		Use: "config set <http_port|https_port|mysql_port> <port>", Short: "Change a port (for machines where 80/443/3306 are taken)",
		Args: cobra.ExactArgs(3),
		RunE: run(func(ctx context.Context, a *app.App, args []string) error {
			if args[0] != "set" {
				return fmt.Errorf("usage: hyper config set <key> <port>")
			}
			return a.SetConfig(args[1], args[2])
		}),
	})
	un := &cobra.Command{Use: "uninstall", Short: "Remove certificate trust and hosts changes (--purge also deletes all data)", Args: cobra.NoArgs}
	un.Flags().Bool("purge", false, "also delete the data directory and the stored MySQL password")
	un.RunE = func(cmd *cobra.Command, _ []string) error {
		v, _ := cmd.Flags().GetBool("verbose")
		purge, _ := cmd.Flags().GetBool("purge")
		a, err := app.New(os.Stdout, v)
		if err != nil {
			return err
		}
		defer a.Close()
		return a.Uninstall(cmd.Context(), purge)
	}
	root.AddCommand(un)

	uiCmd := &cobra.Command{Use: "ui", Short: "Open the Hyper Stack window and tray icon (no PowerShell needed)", Args: cobra.NoArgs}
	uiCmd.Flags().Bool("serve", false, "run the dashboard in this process (used internally)")
	uiCmd.Flags().Bool("no-browser", false, "do not open the browser")
	uiCmd.Flags().Bool("startup", false, "started at Windows sign-in: also start the autostart projects")
	_ = uiCmd.Flags().MarkHidden("startup")
	uiCmd.RunE = func(cmd *cobra.Command, _ []string) error {
		serve, _ := cmd.Flags().GetBool("serve")
		noBrowser, _ := cmd.Flags().GetBool("no-browser")
		p, err := config.Default()
		if err != nil {
			return err
		}
		if serve {
			startup, _ := cmd.Flags().GetBool("startup")
			return ui.Serve(p, !noBrowser, startup)
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		startup, _ := cmd.Flags().GetBool("startup")
		u, err := ui.Launch(p, exe, !noBrowser, startup)
		if err != nil {
			return err
		}
		fmt.Println("Dashboard running. If the browser did not open, use the Hyper Stack tray icon.")
		_ = u
		return nil
	}
	root.AddCommand(uiCmd)

	root.AddCommand(selfInstallCmd())

	// Hidden: runs in the elevated copy launched by install/doctor --fix.
	root.AddCommand(&cobra.Command{
		Use: "_elevated", Hidden: true, Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return elevate.Do(args[0], args[1:]) },
	})
	return root.Execute()
}
