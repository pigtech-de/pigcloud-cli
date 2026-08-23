package cmd

import (
	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/completion"
	"pigcloud/internal/e2ee"
	"pigcloud/internal/output"

	"github.com/spf13/cobra"
)

var fvCmd = &cobra.Command{
	Use:     "fv [path]",
	GroupID: GroupFiles,
	Aliases: []string{"favorite"},
	Short:   "Manage favorites",
	Long: `Manage your favorites list.

Without arguments, lists all favorites.
With a path argument, toggles the favorite status.`,
	Example: `pc fv                  # List favorites
pc fv /Documents         # Toggle favorite
pc fv add "*.jpg"        # Add all matches to favorites
pc fv rm /Documents      # Remove from favorites`,
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			runFavoriteList()
			return
		}
		cmdutil.ForEachExpandedPath(args, runFavoriteToggle, ExitWithError)
	},
}

var fvListCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all favorites",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runFavoriteList()
	},
}

var fvAddCmd = &cobra.Command{
	Use:               "add <path>...",
	Short:             "Add paths to favorites",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		cmdutil.ForEachExpandedPath(args, runFavoriteAdd, ExitWithError)
	},
}

var fvRmCmd = &cobra.Command{
	Use:               "rm <path>...",
	Short:             "Remove paths from favorites",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		cmdutil.ForEachExpandedPath(args, runFavoriteRemove, ExitWithError)
	},
}

func init() {
	fvCmd.AddCommand(fvListCmd, fvAddCmd, fvRmCmd)
	rootCmd.AddCommand(fvCmd)
}

func runFavoriteToggle(path string) {
	ctx, cancel := cmdutil.StartAuthed(ExitWithError)
	defer cancel()
	resolvedPath := cmdutil.ResolvePath(path)
	options := map[string]string{"source": resolvedPath, "mode": "toggle"}
	e2ee.AddPathTokensFor(options, resolvedPath, e2ee.SelfOnly, ExitWithError)
	_, payload := cmdutil.ExecuteCommand[api.FavoritePayload](ctx, "fv", options, ExitWithError)
	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if payload.Action == "added" {
		output.PrintSuccess("Added " + output.PrintPath(payload.Path) + " to favorites")
	} else {
		output.PrintSuccess("Removed " + output.PrintPath(payload.Path) + " from favorites")
	}
}

func runFavoriteList() {
	ctx, cancel := cmdutil.StartAuthed(ExitWithError)
	defer cancel()
	options := map[string]string{"mode": "list"}
	resp, payload := cmdutil.ExecuteCommand[api.FavoriteListPayload](ctx, "fv", options, ExitWithError)

	for i := range payload.Favorites {
		fav := &payload.Favorites[i]
		if fav.E2EEDisplayName != "" {
			fav.Name = e2ee.DecryptE2EEName(fav.E2EEDisplayName)
		}
	}

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}
	if len(payload.Favorites) == 0 {
		output.PrintInfo("No favorites")
		return
	}
	table := output.Table([]string{"Name", "Type"})
	for _, fav := range payload.Favorites {
		table.Append([]string{output.FormatType(fav.Type, fav.Name), fav.Type})
	}
	table.Render()
}

func runFavoriteAdd(path string) {
	ctx, cancel := cmdutil.StartAuthed(ExitWithError)
	defer cancel()
	resolvedPath := cmdutil.ResolvePath(path)
	options := map[string]string{"source": resolvedPath, "mode": "add"}
	e2ee.AddPathTokensFor(options, resolvedPath, e2ee.SelfOnly, ExitWithError)
	_, payload := cmdutil.ExecuteCommand[api.FavoritePayload](ctx, "fv", options, ExitWithError)
	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	output.PrintSuccess("Added " + output.PrintPath(payload.Path) + " to favorites")
}

func runFavoriteRemove(path string) {
	ctx, cancel := cmdutil.StartAuthed(ExitWithError)
	defer cancel()
	resolvedPath := cmdutil.ResolvePath(path)
	options := map[string]string{"source": resolvedPath, "mode": "remove"}
	e2ee.AddPathTokensFor(options, resolvedPath, e2ee.SelfOnly, ExitWithError)
	_, payload := cmdutil.ExecuteCommand[api.FavoritePayload](ctx, "fv", options, ExitWithError)
	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	output.PrintSuccess("Removed " + output.PrintPath(payload.Path) + " from favorites")
}
