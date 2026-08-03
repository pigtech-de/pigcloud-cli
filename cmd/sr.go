package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"
	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/completion"
	"pigcloud/internal/crypto"
	"pigcloud/internal/output"
)

var sharePermission string
var shareForce bool

var srCmd = &cobra.Command{
	Use:     "sr [path]",
	GroupID: GroupSharing,
	Aliases: []string{"share"},
	Short:   "Manage shared files and folders",
	Long: `Manage shared files and folders.

Without arguments, shows shares you've received (inbox).
With a path, shows share recipients for that folder.`,
	Example: `pc sr                            # Show shares you've received
pc sr /Shared                    # List recipients for /Shared
pc sr add alice /Shared          # Share with alice
pc sr rm /Shared alice           # Revoke alice's access
pc sr set /Shared -P secret      # Add password`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			runShareInbox()
		} else {
			runShareList(args[0])
		}
	},
}

var srAddCmd = &cobra.Command{
	Use:               "add <username> <path>",
	Short:             "Share a folder with a user",
	Example:           "pc sr add alice /Documents    # Share with alice",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		runShareAdd(args[1], args[0])
	},
}

var srLsCmd = &cobra.Command{
	Use:   "ls <path>",
	Short: "List share recipients for a folder",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runShareList(args[0])
	},
}

var srRmForce bool

var srRmCmd = &cobra.Command{
	Use:   "rm <path> <username>",
	Short: "Remove a specific share recipient",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		runShareRemove(args[0], args[1])
	},
}

var srInboxCmd = &cobra.Command{
	Use:     "inbox",
	Short:   "List shares you've received from others",
	Example: "pc sr inbox                      # Show folders shared with you",
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runShareInbox()
	},
}

var (
	srSetPermission string
	srSetPassword   string
	srSetExpires    string
	srSetUsername   string
	srRmPassword    bool
	srRmExpires     bool
)

var srSetCmd = &cobra.Command{
	Use:   "set <path>",
	Short: "Update share settings (permission, password, expiry)",
	Long: `Update settings on an existing share.

Update a recipient's permission level:
  sr set /Docs -u bob -p edit

Set or change share password/expiration:
  sr set /Docs --password secret123
  sr set /Docs --expires "2026-12-31"

Remove password/expiration:
  sr set /Docs --remove-password
  sr set /Docs --remove-expiration`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		runShareUpdate(args[0])
	},
}

var srAcceptCmd = &cobra.Command{
	Use:     "accept <path> <owner>",
	Short:   "Accept a pending share",
	Example: "pc sr accept /Documents alice     # Accept a share from alice",
	Args:    cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		runShareAccept(args[0], args[1])
	},
}

var srDeclineCmd = &cobra.Command{
	Use:   "decline <path> <owner>",
	Short: "Decline a received share",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		runShareDecline(args[0], args[1])
	},
}

func init() {
	srAddCmd.Flags().StringVarP(&sharePermission, "permissions", "p", "read", "Permission level: read or edit")
	srAddCmd.Flags().BoolVarP(&shareForce, "force", "f", false, "Skip pending confirmation (activate share immediately)")
	srAddCmd.RegisterFlagCompletionFunc("permissions", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"read\tRead-only access", "edit\tRead and edit access"}, cobra.ShellCompDirectiveNoFileComp
	})
	srSetCmd.Flags().StringVarP(&srSetUsername, "username", "u", "", "recipient username (for permission changes)")
	srSetCmd.Flags().StringVarP(&srSetPermission, "permissions", "p", "", "new permission level: read or edit")
	srSetCmd.Flags().StringVarP(&srSetPassword, "password", "P", "", "set or change share password")
	srSetCmd.Flags().StringVarP(&srSetExpires, "expires", "e", "", "set or change expiration date")
	srSetCmd.Flags().BoolVar(&srRmPassword, "remove-password", false, "remove password protection")
	srSetCmd.Flags().BoolVar(&srRmExpires, "remove-expiration", false, "remove expiration date")
	srSetCmd.RegisterFlagCompletionFunc("permissions", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"read\tRead-only access", "edit\tRead and edit access"}, cobra.ShellCompDirectiveNoFileComp
	})
	srRmCmd.Flags().BoolVarP(&srRmForce, "force", "f", false, "skip confirmation prompt")
	srCmd.AddCommand(srAddCmd, srLsCmd, srRmCmd, srInboxCmd, srAcceptCmd, srDeclineCmd, srSetCmd)
	rootCmd.AddCommand(srCmd)
}

func runShareAdd(targetPath, username string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(targetPath)
	options := map[string]string{
		"source":      resolvedPath,
		"username":    username,
		"permissions": sharePermission,
	}
	if shareForce {
		options["force"] = "1"
	}

	if cmdutil.HasE2EEKeys() {
		sealedKeys, sealedNames := resealKeysAndNamesForRecipient(ctx, resolvedPath, username)
		if sealedKeys != "" {
			options["sealed_keys"] = sealedKeys
		}
		if sealedNames != "" {
			options["sealed_names"] = sealedNames
		}
	}

	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(resolvedPath, "/")
		if trimmed != "" {
			cmdutil.AddPathTokens(options, []string{trimmed}, ExitWithError)
		}
	}

	_, payload := cmdutil.ExecuteCommand[api.SharePayload](ctx, "sr", options, ExitWithError)

	if payload.Status == "revoked" {
		output.PrintSuccess("Revoked sharing of " + output.PrintPath(payload.Path) + " from " + payload.Username)
	} else {
		output.PrintSuccess("Shared " + output.PrintPath(payload.Path) + " with " + payload.Username + " (" + payload.Permission + ")")
	}
}

func resealKeysAndNamesForRecipient(ctx context.Context, folderPath, recipientUsername string) (string, string) {
	client := api.NewClient()

	pubkeyResp, err := client.FetchPublicKey(ctx, recipientUsername)
	if err != nil || !pubkeyResp.Success {
		return "", ""
	}
	var pubkeyPayload api.E2EEPubkeyPayload
	if err := json.Unmarshal(pubkeyResp.Raw, &pubkeyPayload); err != nil {
		return "", ""
	}
	recipientPubKeyBytes, err := base64.StdEncoding.DecodeString(pubkeyPayload.PublicKey)
	if err != nil || len(recipientPubKeyBytes) != 32 {
		return "", ""
	}
	recipientKyberBytes, err := base64.StdEncoding.DecodeString(pubkeyPayload.PublicKeyKyber)
	if err != nil || len(recipientKyberBytes) != crypto.KyberPublicKeySize {
		return "", ""
	}
	var x25519Pub [32]byte
	copy(x25519Pub[:], recipientPubKeyBytes)
	recipientPubKey := &crypto.PublicKeySet{X25519: x25519Pub, Kyber: recipientKyberBytes}

	keysResp, err := client.Execute(ctx, "e2ee_list_keys", map[string]string{
		"source":        folderPath,
		"include_names": "1",
		"include_dirs":  "1",
	})
	if err != nil || !keysResp.Success {
		return "", ""
	}
	var keysPayload api.E2EEListKeysPayload
	if err := json.Unmarshal(keysResp.Raw, &keysPayload); err != nil {
		return "", ""
	}
	if len(keysPayload.Keys) == 0 {
		return "", ""
	}

	_, privKey := cmdutil.GetKeyPair(ExitWithError)

	type sealedKeyEntry struct {
		NodeID    string `json:"node_id"`
		SealedKey string `json:"sealed_key"`
	}
	type sealedNameEntry struct {
		NodeID            string `json:"node_id"`
		SealedDisplayName string `json:"sealed_display_name"`
	}
	var keyEntries []sealedKeyEntry
	var nameEntries []sealedNameEntry

	for _, k := range keysPayload.Keys {
		if k.SealedKey != "" {
			if sealedBytes, err := base64.StdEncoding.DecodeString(k.SealedKey); err == nil {
				if dataKey, err := crypto.UnsealDataKey(sealedBytes, privKey); err == nil {
					if reSealed, err := crypto.SealDataKey(dataKey, recipientPubKey); err == nil {
						keyEntries = append(keyEntries, sealedKeyEntry{
							NodeID:    k.NodeID,
							SealedKey: base64.StdEncoding.EncodeToString(reSealed),
						})
					}
				}
			}
		}
		if k.E2EEDisplayName != "" {
			if sealedNameBytes, err := base64.StdEncoding.DecodeString(k.E2EEDisplayName); err == nil {
				if plaintext, err := crypto.UnsealDisplayName(sealedNameBytes, privKey); err == nil {
					if reSealed, err := crypto.SealDisplayName(plaintext, recipientPubKey); err == nil {
						nameEntries = append(nameEntries, sealedNameEntry{
							NodeID:            k.NodeID,
							SealedDisplayName: base64.StdEncoding.EncodeToString(reSealed),
						})
					}
				}
			}
		}
	}

	var keysJSON, namesJSON string
	if len(keyEntries) > 0 {
		if data, err := json.Marshal(keyEntries); err == nil {
			keysJSON = string(data)
		}
	}
	if len(nameEntries) > 0 {
		if data, err := json.Marshal(nameEntries); err == nil {
			namesJSON = string(data)
		}
	}
	return keysJSON, namesJSON
}

func runShareList(targetPath string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(targetPath)
	srListOpts := map[string]string{
		"source": resolvedPath,
		"mode":   "list",
	}
	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(resolvedPath, "/")
		if trimmed != "" {
			cmdutil.AddPathTokens(srListOpts, []string{trimmed}, ExitWithError)
		}
	}
	resp, payload := cmdutil.ExecuteCommand[api.ShareListPayload](ctx, "sr", srListOpts, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}

	fmt.Printf("%s\n\n", output.PrintPath(payload.Path))

	if len(payload.Recipients) == 0 {
		output.PrintInfo("Not shared with anyone")
		return
	}

	table := output.Table([]string{"Username", "Permission", "Status"})
	for _, r := range payload.Recipients {
		status := "active"
		if r.Expired {
			status = "expired"
		}
		table.Append([]string{r.Username, r.Permission, status})
	}
	table.Render()

	if payload.HasPassword {
		fmt.Println("\n  Password protected: yes")
	}
	if payload.ExpiresAt != nil {
		fmt.Printf("  Expires: %s\n", *payload.ExpiresAt)
	}
}

func runShareRemove(targetPath, username string) {
	cmdutil.RequireLogin(ExitWithError)

	if !GetJSONOutput() && !GetQuietOutput() {
		if !cmdutil.ConfirmAction("Remove "+username+" from "+cmdutil.ResolvePath(targetPath)+"?", srRmForce) {
			output.PrintInfo("Cancelled")
			return
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(targetPath)
	srRmOpts := map[string]string{
		"source":   resolvedPath,
		"username": username,
		"mode":     "remove",
	}
	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(resolvedPath, "/")
		if trimmed != "" {
			cmdutil.AddPathTokens(srRmOpts, []string{trimmed}, ExitWithError)
		}
	}
	_, payload := cmdutil.ExecuteCommand[api.SharePayload](ctx, "sr", srRmOpts, ExitWithError)

	output.PrintSuccess("Removed " + payload.Username + " from " + output.PrintPath(payload.Path))
}

func runShareInbox() {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, payload := cmdutil.ExecuteCommand[api.ShareInboxPayload](ctx, "sr", map[string]string{
		"mode": "inbox",
	}, ExitWithError)

	for i := range payload.Shares {
		s := &payload.Shares[i]
		if s.E2EEDisplayName != "" {
			s.Name = cmdutil.DecryptE2EEName(s.E2EEDisplayName)
		}
	}

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}

	if len(payload.Shares) == 0 {
		output.PrintInfo("No shared folders received")
		return
	}

	table := output.Table([]string{"Owner", "Folder", "Permission"})
	for _, s := range payload.Shares {
		name := s.Name
		if name == "" {
			name = s.Path
		}
		table.Append([]string{s.Owner, name, s.Permission})
	}
	table.Render()
	fmt.Printf("\n%d shared folders\n", len(payload.Shares))
}

func runShareUpdate(targetPath string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(targetPath)
	options := map[string]string{
		"source": resolvedPath,
		"mode":   "update",
	}
	if srSetUsername != "" {
		options["username"] = srSetUsername
	}
	if srSetPermission != "" {
		options["permissions"] = srSetPermission
	}
	if srSetPassword != "" {
		options["password"] = srSetPassword
	}
	if srSetExpires != "" {
		options["expires"] = srSetExpires
	}
	if srRmPassword {
		options["remove-password"] = "true"
	}
	if srRmExpires {
		options["remove-expiration"] = "true"
	}

	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(resolvedPath, "/")
		if trimmed != "" {
			cmdutil.AddPathTokens(options, []string{trimmed}, ExitWithError)
		}
	}

	_, payload := cmdutil.ExecuteCommand[api.ShareUpdatePayload](ctx, "sr", options, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	output.PrintSuccess("Updated share for " + output.PrintPath(payload.Path))
	for _, change := range payload.Changes {
		fmt.Println("  - " + change)
	}
}

func runShareAccept(targetPath, owner string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	_, payload := cmdutil.ExecuteCommand[api.SharePayload](ctx, "sr", map[string]string{
		"source":   targetPath,
		"username": owner,
		"mode":     "accept",
	}, ExitWithError)

	output.PrintSuccess("Accepted share from " + payload.Username + " for " + output.PrintPath(payload.Path))
}

func runShareDecline(targetPath, owner string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	_, payload := cmdutil.ExecuteCommand[api.SharePayload](ctx, "sr", map[string]string{
		"source":   targetPath,
		"username": owner,
		"mode":     "decline",
	}, ExitWithError)

	output.PrintSuccess("Declined share from " + payload.Username + " for " + output.PrintPath(payload.Path))
}
