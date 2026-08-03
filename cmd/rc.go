package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/output"

	"github.com/spf13/cobra"
)

var rcLimit string

var rcCmd = &cobra.Command{
	Use:     "rc",
	GroupID: GroupTools,
	Aliases: []string{"recents"},
	Short:   "List recently accessed files",
	Long:    `Show files and folders you've recently opened or accessed.`,
	Example: `pc rc              # List recent items
pc rc -n 10        # Show last 10 recent items`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runRecentList()
	},
}

func init() {
	rcCmd.Flags().StringVarP(&rcLimit, "limit", "n", "", "maximum number of items to show (default 50)")
	rootCmd.AddCommand(rcCmd)
}

func runRecentList() {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	options := map[string]string{}
	if rcLimit != "" {
		options["limit"] = rcLimit
	}

	resp, payload := cmdutil.ExecuteCommand[api.RecentListPayload](ctx, "rc", options, ExitWithError)

	for i := range payload.Recents {
		item := &payload.Recents[i]
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

	if len(payload.Recents) == 0 {
		output.PrintInfo("No recent items")
		return
	}

	table := output.Table([]string{"Name", "Type", "Accessed"})
	for _, item := range payload.Recents {
		table.Append([]string{output.FormatType(item.Type, item.Name), item.Type, item.AccessedAt})
	}
	table.Render()

	fmt.Printf("\n%d recent item(s)\n", len(payload.Recents))
}
