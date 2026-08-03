package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/output"
)

var ssCmd = &cobra.Command{
	Use:     "ss",
	GroupID: GroupTools,
	Aliases: []string{"sessions"},
	Short:   "Manage active sessions and devices",
	Long: `View and manage active login sessions and trusted devices.

Without arguments, lists all sessions and trusted devices.
Use subcommands to revoke sessions or forget devices.`,
	Example: `pc ss                          # List sessions and devices
pc ss revoke <session-id>      # Revoke a session
pc ss revoke-all               # Revoke all other sessions
pc ss devices                  # List trusted devices only
pc ss forget <device-id>       # Remove a trusted device`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runSessionList()
	},
}

var ssListCmd = &cobra.Command{
	Use:   "ls",
	Short: "List active sessions and devices",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runSessionList()
	},
}

var ssRevokeCmd = &cobra.Command{
	Use:   "revoke <session-id>",
	Short: "Revoke an active session",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runSessionRevoke(args[0])
	},
}

var ssRevokeAllCmd = &cobra.Command{
	Use:   "revoke-all",
	Short: "Revoke all other sessions",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runSessionRevokeAll()
	},
}

var ssDevicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List trusted devices",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runSessionDevices()
	},
}

var ssForgetCmd = &cobra.Command{
	Use:   "forget <device-id>",
	Short: "Remove a trusted device",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runSessionForget(args[0])
	},
}

func init() {
	ssCmd.AddCommand(ssListCmd, ssRevokeCmd, ssRevokeAllCmd, ssDevicesCmd, ssForgetCmd)
	rootCmd.AddCommand(ssCmd)
}

func runSessionList() {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, payload := cmdutil.ExecuteCommand[api.SessionsPayload](ctx, "ss", map[string]string{
		"mode": "list",
	}, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}

	if len(payload.Sessions) > 0 {
		fmt.Println(color.HiWhiteString("Sessions"))
		fmt.Println()
		table := output.Table([]string{"ID", "User Agent", "Location", "Last Active"})
		for _, s := range payload.Sessions {
			id := s.SessionID
			if len(id) > 12 {
				id = id[:12] + "..."
			}
			if s.Label != "" {
				id = s.Label
			}
			ua := truncate(s.UserAgent, 40)
			table.Append([]string{id, ua, s.Location, s.LastActive})
		}
		table.Render()
		fmt.Printf("\n%d session(s)\n", len(payload.Sessions))
	} else {
		output.PrintInfo("No active sessions")
	}

	if len(payload.Devices) > 0 {
		fmt.Println()
		fmt.Println(color.HiWhiteString("Trusted Devices"))
		fmt.Println()
		table := output.Table([]string{"ID", "User Agent", "Location", "Last Used"})
		for _, d := range payload.Devices {
			ua := truncate(d.UserAgent, 40)
			table.Append([]string{fmt.Sprintf("%d", d.ID), ua, d.Location, d.LastUsedAt})
		}
		table.Render()
		fmt.Printf("\n%d device(s)\n", len(payload.Devices))
	}
}

func runSessionRevoke(sessionID string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, _ := cmdutil.ExecuteCommand[any](ctx, "ss", map[string]string{
		"mode":       "revoke",
		"session-id": sessionID,
	}, ExitWithError)

	output.PrintSuccess(resp.Message)
}

func runSessionRevokeAll() {
	cmdutil.RequireLogin(ExitWithError)

	if !cmdutil.ConfirmAction("Revoke all other sessions?", false) {
		output.PrintInfo("Cancelled")
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, _ := cmdutil.ExecuteCommand[any](ctx, "ss", map[string]string{
		"mode": "revoke-all",
	}, ExitWithError)

	output.PrintSuccess(resp.Message)
}

func runSessionDevices() {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, payload := cmdutil.ExecuteCommand[api.SessionsPayload](ctx, "ss", map[string]string{
		"mode": "devices",
	}, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}

	if len(payload.Devices) == 0 {
		output.PrintInfo("No trusted devices")
		return
	}

	table := output.Table([]string{"ID", "User Agent", "Location", "Last Used"})
	for _, d := range payload.Devices {
		ua := truncate(d.UserAgent, 40)
		table.Append([]string{fmt.Sprintf("%d", d.ID), ua, d.Location, d.LastUsedAt})
	}
	table.Render()
	fmt.Printf("\n%d device(s)\n", len(payload.Devices))
}

func runSessionForget(deviceID string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, _ := cmdutil.ExecuteCommand[any](ctx, "ss", map[string]string{
		"mode":      "forget",
		"device-id": deviceID,
	}, ExitWithError)

	output.PrintSuccess(resp.Message)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
