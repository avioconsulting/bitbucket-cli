package cleanup

import (
	"github.com/spf13/cobra"

	"github.com/avivsinai/bitbucket-cli/pkg/cmdutil"
)

// NewCmdCleanup wires cleanup subcommands.
func NewCmdCleanup(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Discover cleanup candidates for Bitbucket Cloud",
		Long: `Read-only discovery commands for finding stale, empty, or otherwise
likely-to-delete repositories and projects in Bitbucket Cloud.

These commands do not delete anything. Use the output to target the explicit
repo delete or project delete commands safely.`,
	}

	cmd.AddCommand(newReposCmd(f))
	cmd.AddCommand(newProjectsCmd(f))

	return cmd
}
