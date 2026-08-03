package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"pigcloud/internal/agent"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/config"
	"pigcloud/internal/mount"
	"pigcloud/internal/output"
)

var logoutCmd = &cobra.Command{
	Use:     "lo",
	GroupID: GroupAuth,
	Aliases: []string{"logout"},
	Short:   "Remove stored credentials",
	Long: `Remove stored API key and configuration from your system.

This will delete your local configuration file and require you to login again.`,
	Run: func(cmd *cobra.Command, args []string) {
		runLogout()
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}

func runLogout() {
	if !config.IsLoggedIn() {
		output.PrintInfo("You are not logged in.")
		return
	}

	if mounts := mount.ListMounts(); len(mounts) > 0 {
		output.PrintInfo("Stopping mounts...")
		for _, m := range mounts {
			stopEntry(m)
		}
	}

	agent.Shutdown()
	cmdutil.ClearCachedKey()

	if err := config.Clear(); err != nil {
		output.PrintError("Failed to clear configuration: " + err.Error())
		os.Exit(1)
	}

	output.PrintSuccess("Logged out successfully. Your credentials have been removed.")
}
