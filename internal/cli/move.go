package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/hyperdesign/hyper-stack/internal/app"
)

// moveCmd is "hyper move": relocate the whole install to another folder or drive.
func moveCmd(run func(func(context.Context, *app.App, []string) error) func(*cobra.Command, []string) error) *cobra.Command {
	c := &cobra.Command{
		Use:   "move <new folder>",
		Short: "Move the whole install (and your www projects) to another folder or drive",
		Long: `Moves everything Hyper Stack keeps in its install folder (PHP, MySQL, your databases,
certificates, and the www folder with all projects) to a new folder, for example another drive.

It copies first, checks that every file matches, switches over, and starts the projects
that were running from the new place. Only then is the old folder deleted. If anything
goes wrong it switches back and the old install is untouched. No administrator prompt.`,
		Args: cobra.ExactArgs(1),
	}
	c.Flags().Bool("keep-old", false, "do not delete the old folder afterwards")
	c.RunE = run(func(ctx context.Context, a *app.App, args []string) error {
		keep, _ := c.Flags().GetBool("keep-old")
		return a.MoveInstall(ctx, args[0], keep)
	})
	return c
}
