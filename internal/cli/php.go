package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hyperdesign/hyper-stack/internal/app"
)

// phpCmd is "hyper php": which PHP a project runs on, and switching it.
func phpCmd(run func(func(context.Context, *app.App, []string) error) func(*cobra.Command, []string) error) *cobra.Command {
	return &cobra.Command{
		Use:   "php [version] [folder]",
		Short: "Show or change the PHP version of a project (7.4, 8.0, 8.1, 8.2, 8.3, 8.4)",
		Long: `Without a version it shows what the project runs on. With one it saves the version in the
project's hyper.json and restarts the project on it. Handy when upgrading an old project step by step:

  hyper php 7.4        run an old project (Laravel 5.8 to 8) as it is
  hyper php 8.1        after upgrading to Laravel 9 or 10
  hyper php 8.3        Laravel 11 and 12

The folder is optional and defaults to the current one.`,
		Args: cobra.MaximumNArgs(2),
		RunE: run(func(ctx context.Context, a *app.App, args []string) error {
			folder := ""
			if len(args) == 2 {
				folder = args[1]
			}
			if len(args) == 0 {
				info, err := a.ProjectPHP("")
				if err != nil {
					return err
				}
				a.Say("%s runs on PHP %s (%s)", info.Project, info.Version, info.Source)
				a.Say("Available: %s", strings.Join(info.Available, ", "))
				return nil
			}
			return a.SetProjectPHP(ctx, folder, args[0])
		}),
	}
}
