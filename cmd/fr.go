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

var frCmd = &cobra.Command{
	Use:     "fr",
	GroupID: GroupSharing,
	Aliases: []string{"friend"},
	Short:   "Manage friends",
	Example: `  pc fr                    # List your friends
  pc fr add alice           # Send a friend request
  pc fr accept alice        # Accept a friend request
  pc fr decline alice       # Decline a friend request
  pc fr rm alice            # Remove a friend
  pc fr pending             # List pending friend requests`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runFriendList()
	},
}

var frAddCmd = &cobra.Command{
	Use:   "add <username>",
	Short: "Send a friend request",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runFriendAdd(args[0])
	},
}

var frAcceptCmd = &cobra.Command{
	Use:   "accept <username>",
	Short: "Accept a friend request",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runFriendRespond(args[0], "accept")
	},
}

var frDeclineCmd = &cobra.Command{
	Use:   "decline <username>",
	Short: "Decline a friend request",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runFriendRespond(args[0], "decline")
	},
}

var frRmCmd = &cobra.Command{
	Use:   "rm <username>",
	Short: "Remove a friend",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runFriendRemove(args[0])
	},
}

var frPendingCmd = &cobra.Command{
	Use:   "pending",
	Short: "List pending friend requests",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runFriendPending()
	},
}

var frListCmd = &cobra.Command{
	Use:   "ls",
	Short: "List your friends",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runFriendList()
	},
}

func init() {
	frCmd.AddCommand(frListCmd, frAddCmd, frAcceptCmd, frDeclineCmd, frRmCmd, frPendingCmd)
	rootCmd.AddCommand(frCmd)
}

func runFriendList() {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, payload := cmdutil.ExecuteCommand[api.FriendListPayload](ctx, "fr", map[string]string{
		"mode": "list",
	}, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}

	if len(payload.Friends) == 0 {
		output.PrintInfo("No friends yet")
		return
	}

	table := output.Table([]string{"Username", "Friends since"})
	for _, f := range payload.Friends {
		table.Append([]string{f.Username, output.FormatTime(&f.CreatedAt)})
	}
	table.Render()
}

func runFriendAdd(username string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, _ := cmdutil.ExecuteCommand[api.FriendActionPayload](ctx, "fr", map[string]string{
		"mode":     "add",
		"username": username,
	}, ExitWithError)

	output.PrintSuccess(resp.Message)
}

func runFriendRespond(username, action string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, _ := cmdutil.ExecuteCommand[api.FriendActionPayload](ctx, "fr", map[string]string{
		"mode":     action,
		"username": username,
	}, ExitWithError)

	output.PrintSuccess(resp.Message)
}

func runFriendRemove(username string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, _ := cmdutil.ExecuteCommand[api.FriendActionPayload](ctx, "fr", map[string]string{
		"mode":     "remove",
		"username": username,
	}, ExitWithError)

	output.PrintSuccess(resp.Message)
}

func runFriendPending() {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, payload := cmdutil.ExecuteCommand[api.FriendPendingPayload](ctx, "fr", map[string]string{
		"mode": "pending",
	}, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}

	if len(payload.Pending) == 0 {
		output.PrintInfo("No pending friend requests")
		return
	}

	table := output.Table([]string{"Username", "Sent"})
	for _, p := range payload.Pending {
		table.Append([]string{p.Username, p.CreatedAt})
	}
	table.Render()
	fmt.Printf("\n%d pending request(s)\n", len(payload.Pending))
}
