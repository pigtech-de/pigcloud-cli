package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/completion"
	"pigcloud/internal/output"
	"strings"

	"github.com/spf13/cobra"
)

var hdCmd = &cobra.Command{
	Use:     "hd [path]",
	GroupID: GroupFiles,
	Aliases: []string{"hide"},
	Short:   "Hide or unhide files and folders",
	Long: `Manage hidden files and folders.

Without arguments, lists all hidden items.
With a path argument, toggles the hidden status.`,
	Example: `pc hd                  # List hidden items
pc hd /Private           # Toggle hidden
pc hd add "*.bak"        # Hide all matches
pc hd rm /Private        # Unhide`,
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			runHideList()
			return
		}
		forEachExpandedPath(args, runHideToggle)
	},
}

var hdAddCmd = &cobra.Command{
	Use:               "add <path>...",
	Short:             "Hide files or folders",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		forEachExpandedPath(args, runHide)
	},
}

var hdRmCmd = &cobra.Command{
	Use:               "rm <path>...",
	Short:             "Unhide files or folders",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		forEachExpandedPath(args, runUnhide)
	},
}

var hdListCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all hidden items",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runHideList()
	},
}

func init() {
	hdCmd.AddCommand(hdAddCmd, hdRmCmd, hdListCmd)
	rootCmd.AddCommand(hdCmd)
}

func runHideToggle(path string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	resolvedPath := cmdutil.ResolvePath(path)
	options := map[string]string{
		"source": resolvedPath,
		"mode":   "toggle",
	}
	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(resolvedPath, "/")
		if trimmed != "" {
			cmdutil.AddPathTokens(options, []string{trimmed}, ExitWithError)
		}
	}
	_, payload := cmdutil.ExecuteCommand[api.HidePayload](ctx, "hd", options, ExitWithError)
	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if payload.Hidden {
		output.PrintSuccess("Hidden " + output.PrintPath(payload.Path))
	} else {
		output.PrintSuccess("Unhidden " + output.PrintPath(payload.Path))
	}
}

func runHide(path string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	resolvedPath := cmdutil.ResolvePath(path)
	options := map[string]string{
		"source": resolvedPath,
		"mode":   "hide",
	}
	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(resolvedPath, "/")
		if trimmed != "" {
			cmdutil.AddPathTokens(options, []string{trimmed}, ExitWithError)
		}
	}
	_, payload := cmdutil.ExecuteCommand[api.HidePayload](ctx, "hd", options, ExitWithError)
	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	output.PrintSuccess("Hidden " + output.PrintPath(payload.Path))
}

func runUnhide(path string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	resolvedPath := cmdutil.ResolvePath(path)
	options := map[string]string{
		"source": resolvedPath,
		"mode":   "unhide",
	}
	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(resolvedPath, "/")
		if trimmed != "" {
			cmdutil.AddPathTokens(options, []string{trimmed}, ExitWithError)
		}
	}
	_, payload := cmdutil.ExecuteCommand[api.HidePayload](ctx, "hd", options, ExitWithError)
	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	output.PrintSuccess("Unhidden " + output.PrintPath(payload.Path))
}

func runHideList() {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	resp, payload := cmdutil.ExecuteCommand[api.HideListPayload](ctx, "hd", map[string]string{
		"mode": "list",
	}, ExitWithError)

	for i := range payload.Items {
		item := &payload.Items[i]
		if item.E2EEDisplayName != "" {
			item.Name = cmdutil.DecryptE2EEName(item.E2EEDisplayName)
		}
	}

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}
	if len(payload.Items) == 0 {
		output.PrintInfo("No hidden items")
		return
	}
	table := output.Table([]string{"Name", "Type"})
	for _, item := range payload.Items {
		table.Append([]string{output.FormatType(item.Type, item.Name), item.Type})
	}
	table.Render()
	fmt.Printf("\n%d hidden item(s)\n", len(payload.Items))
}
