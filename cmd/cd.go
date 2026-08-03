package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/completion"
	"pigcloud/internal/config"
	"pigcloud/internal/output"
)

var cdCmd = &cobra.Command{
	Use:     "cd <path>",
	GroupID: GroupNav,
	Short:   "Change working directory",
	Long: `Change the current working directory in your cloud storage.

The working directory is persisted across CLI sessions.
Use '..' to go up one directory, or '/' to go to root.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		runCd(args[0])
	},
}

func init() {
	rootCmd.AddCommand(cdCmd)
}

func runCd(targetPath string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(targetPath)

	options := map[string]string{
		"source": resolvedPath,
	}
	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(resolvedPath, "/")
		var paths []string
		if trimmed != "" {
			paths = append(paths, trimmed)
			if parent := filepath.Dir(trimmed); parent != "." && parent != "" {
				paths = append(paths, parent)
			}
		}
		cmdutil.AddPathTokens(options, paths, ExitWithError)
	}

	client := api.NewClient()
	resp, err := client.Execute(ctx, "cd", options)
	if err != nil {
		output.PrintError("Request failed: " + err.Error())
		ExitWithError()
	}
	if !resp.Success {
		output.PrintError(resp.Message)
		ExitWithError()
	}

	var payload api.CdPayload
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		output.PrintError("Failed to parse response: " + err.Error())
		ExitWithError()
	}

	if err := config.SetCwd(payload.Path); err != nil {
		output.PrintError("Failed to save working directory: " + err.Error())
		ExitWithError()
	}

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	if !GetQuietOutput() {
		output.PrintSuccess("Changed to " + output.PrintPath(payload.Path))
	}
}
