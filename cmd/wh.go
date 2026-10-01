package cmd

import (
	"fmt"
	"strings"

	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:     "wh",
	GroupID: GroupAuth,
	Aliases: []string{"whoami"},
	Short:   "Show current user info",
	Long:    `Display information about the currently authenticated user.`,
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runWhoami()
	},
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}

func runWhoami() {
	ctx, cancel := cmdutil.StartAuthed(ExitWithError)
	defer cancel()

	resp, payload := cmdutil.ExecuteCommand[api.WhoamiPayload](ctx, "wh", map[string]string{}, ExitWithError)
	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}

	fmt.Printf("%s %s\n", color.CyanString("User:"), payload.Username)
	if payload.Email != "" {
		fmt.Printf("%s %s\n", color.CyanString("Email:"), payload.Email)
	}
	if payload.Tier != "" {
		fmt.Printf("%s %s\n", color.CyanString("Tier:"), payload.Tier)
	}
	if payload.StorageLimit != "" {
		fmt.Printf("%s %s / %s\n", color.CyanString("Storage:"), payload.StorageUsed, payload.StorageLimit)
	}

	var twoFA []string
	if payload.Fido2Enabled {
		twoFA = append(twoFA, "Passkey")
	}
	if payload.TotpEnabled {
		twoFA = append(twoFA, "TOTP")
	}
	if payload.TwoFactorEnabled {
		twoFA = append(twoFA, "Email")
	}
	label := "Off"
	if len(twoFA) > 0 {
		label = strings.Join(twoFA, ", ")
	}
	fmt.Printf("%s %s\n", color.CyanString("2FA:"), label)

	if payload.MemberSince != "" {
		fmt.Printf("%s %s\n", color.CyanString("Since:"), payload.MemberSince)
	}
}
