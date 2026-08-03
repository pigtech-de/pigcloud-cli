package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/completion"
	"pigcloud/internal/crypto"
	"pigcloud/internal/output"
)

var (
	chLimit    string
	chBeforeID string
	chFile     string
)

var chCmd = &cobra.Command{
	Use:     "ch [username] [message]",
	GroupID: GroupSharing,
	Aliases: []string{"chat"},
	Short:   "Send and receive E2EE chat messages",
	Long: `Encrypted chat with other PigCloud users (requires friendship).

When called with no arguments, lists conversations.
With a username, shows recent messages.
With a username and message, sends a message.

Use subcommands for more control:

  ch                           List conversations
  ch <username>                Show messages
  ch <username> "hello"        Send a message (shorthand)
  ch send <user> "message"     Send (explicit, supports stdin pipe)
  ch send <user> -f /file      Share a file in chat
  ch rm <message-id>           Delete own message
  ch read <username>           Mark conversation as read
  ch unread                    Show unread counts`,
	Example: `pc ch                            # List conversations
pc ch alice                      # Show messages with alice
pc ch alice "hello!"             # Send message to alice
echo "hi" | pc ch send alice     # Pipe message
pc ch send alice -f /Photos      # Share a file`,
	Args: cobra.RangeArgs(0, 2),
	Run: func(cmd *cobra.Command, args []string) {
		switch len(args) {
		case 0:
			runChatList()
		case 1:
			runChatHistory(args[0])
		case 2:
			runChatSendMessage(args[0], args[1])
		}
	},
}

var chSendCmd = &cobra.Command{
	Use:               "send <username> [message]",
	Short:             "Send a chat message or share a file",
	Args:              cobra.RangeArgs(1, 2),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		username := args[0]
		if chFile != "" {
			runChatShareFile(username, chFile)
			return
		}
		message := ""
		if len(args) > 1 {
			message = args[1]
		}
		if message == "" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				output.PrintError("Failed to read stdin: " + err.Error())
				ExitWithError()
			}
			message = string(data)
		}
		if message == "" {
			output.PrintError("No message provided")
			ExitWithError()
		}
		runChatSendMessage(username, message)
	},
}

var chRmCmd = &cobra.Command{
	Use:   "rm <message-id>",
	Short: "Delete a sent message",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runChatDelete(args[0])
	},
}

var chReadCmd = &cobra.Command{
	Use:   "read <username>",
	Short: "Mark conversation as read",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runChatMarkRead(args[0])
	},
}

var chUnreadCmd = &cobra.Command{
	Use:   "unread",
	Short: "Show unread message counts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runChatUnread()
	},
}

func init() {
	chCmd.Flags().StringVarP(&chLimit, "limit", "n", "", "number of messages to show (default 50)")
	chCmd.Flags().StringVar(&chBeforeID, "before-id", "", "show messages before this message ID")

	chSendCmd.Flags().StringVarP(&chFile, "file", "f", "", "share a cloud file in chat")

	chListCmd := &cobra.Command{
		Use:   "ls",
		Short: "List conversations",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runChatList()
		},
	}
	chCmd.AddCommand(chListCmd, chSendCmd, chRmCmd, chReadCmd, chUnreadCmd)
	rootCmd.AddCommand(chCmd)
}

func runChatList() {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, payload := cmdutil.ExecuteCommand[api.ChatListPayload](ctx, "ch", map[string]string{
		"mode": "list",
	}, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}

	if len(payload.Conversations) == 0 {
		output.PrintInfo("No conversations")
		return
	}

	table := output.Table([]string{"User", "Unread", "Last", "Time"})
	for _, c := range payload.Conversations {
		unread := ""
		if c.Unread > 0 {
			unread = color.YellowString("%d", c.Unread)
		} else {
			unread = "0"
		}
		table.Append([]string{c.Username, unread, c.LastDirection, output.FormatTime(&c.LastAt)})
	}
	table.Render()
}

func runChatHistory(username string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	options := map[string]string{
		"mode":     "history",
		"username": username,
	}
	if chLimit != "" {
		options["limit"] = chLimit
	}
	if chBeforeID != "" {
		options["before-id"] = chBeforeID
	}

	_, payload := cmdutil.ExecuteCommand[api.ChatHistoryPayload](ctx, "ch", options, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	if len(payload.Messages) == 0 {
		output.PrintInfo("No messages with " + username)
		return
	}

	var privKey *crypto.PrivateKeySet
	needsKeys := false
	for _, msg := range payload.Messages {
		if !msg.Deleted && msg.EncryptedBody != "" {
			needsKeys = true
			break
		}
	}
	if needsKeys {
		_, privKey = cmdutil.GetKeyPair(ExitWithError)
	}

	for _, msg := range payload.Messages {
		timestamp := output.FormatTime(&msg.CreatedAt)
		isMine := msg.SenderID == payload.UserID

		var sender string
		if isMine {
			sender = color.CyanString("you")
		} else {
			sender = color.GreenString(username)
		}

		if msg.Deleted {
			fmt.Printf("  %s %s  %s\n", color.HiBlackString("#%d", msg.ID), sender, color.HiBlackString("[deleted]"))
			continue
		}

		plaintext := decryptChatMessage(msg, privKey, payload.UserID)

		readMark := ""
		if isMine && msg.ReadAt != nil {
			readMark = color.HiBlackString(" ✓")
		}

		shareTag := ""
		if msg.ShareID != nil {
			status := ""
			if msg.ShareStatus != nil {
				status = *msg.ShareStatus
			}
			shareTag = color.MagentaString(" [file:%s]", status)
		}

		fmt.Printf("  %s %s  %s%s%s\n    %s\n",
			color.HiBlackString("#%d", msg.ID),
			sender,
			color.HiBlackString(timestamp),
			readMark,
			shareTag,
			plaintext,
		)
	}

	client := api.NewClient()
	client.Execute(ctx, "ch", map[string]string{"mode": "mark-read", "username": username})
}

func runChatSendMessage(username, message string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	options := encryptChatMessage(ctx, username, message)
	options["mode"] = "send"
	options["username"] = username

	_, payload := cmdutil.ExecuteCommand[api.ChatSendPayload](ctx, "ch", options, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	output.PrintSuccess("Message sent to " + username)
}

func runChatShareFile(username, filePath string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(filePath)

	options := encryptChatMessage(ctx, username, resolvedPath)
	options["mode"] = "send-file"
	options["username"] = username
	options["source"] = resolvedPath

	if cmdutil.HasE2EEKeys() {
		sealedKeys, _ := resealKeysAndNamesForRecipient(ctx, resolvedPath, username)
		if sealedKeys != "" {
			options["sealed_keys"] = sealedKeys
		}
	}

	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(resolvedPath, "/")
		if trimmed != "" {
			cmdutil.AddPathTokens(options, []string{trimmed}, ExitWithError)
		}
	}

	_, payload := cmdutil.ExecuteCommand[api.ChatSendPayload](ctx, "ch", options, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	output.PrintSuccess("Shared " + output.PrintPath(resolvedPath) + " with " + username)
}

func runChatDelete(messageIDStr string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cmdutil.ExecuteCommand[api.ChatDeletePayload](ctx, "ch", map[string]string{
		"mode":       "delete",
		"message_id": messageIDStr,
	}, ExitWithError)

	output.PrintSuccess("Message #" + messageIDStr + " deleted")
}

func runChatMarkRead(username string) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	_, payload := cmdutil.ExecuteCommand[api.ChatMarkReadPayload](ctx, "ch", map[string]string{
		"mode":     "mark-read",
		"username": username,
	}, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	if payload.Marked > 0 {
		output.PrintSuccess(fmt.Sprintf("Marked %d messages from %s as read", payload.Marked, username))
	} else {
		output.PrintInfo("No unread messages from " + username)
	}
}

func runChatUnread() {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resp, payload := cmdutil.ExecuteCommand[api.ChatUnreadPayload](ctx, "ch", map[string]string{
		"mode": "unread",
	}, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}

	if len(payload.Unread) == 0 {
		output.PrintInfo("No unread messages")
		return
	}

	table := output.Table([]string{"User", "Unread"})
	for _, u := range payload.Unread {
		table.Append([]string{u.Username, strconv.Itoa(u.Count)})
	}
	table.Render()
	fmt.Printf("\n%d total unread\n", payload.Total)
}

func encryptChatMessage(ctx context.Context, recipientUsername, plaintext string) map[string]string {
	pubKey, _ := cmdutil.GetKeyPair(ExitWithError)

	client := api.NewClient()
	pubkeyResp, err := client.FetchPublicKey(ctx, recipientUsername)
	if err != nil || !pubkeyResp.Success {
		output.PrintError("Failed to fetch recipient's public key. They may not have E2EE set up.")
		ExitWithError()
	}
	var pubkeyPayload api.E2EEPubkeyPayload
	if err := json.Unmarshal(pubkeyResp.Raw, &pubkeyPayload); err != nil {
		output.PrintError("Failed to parse public key response")
		ExitWithError()
	}
	recipientPubKeyBytes, err := base64.StdEncoding.DecodeString(pubkeyPayload.PublicKey)
	if err != nil || len(recipientPubKeyBytes) != 32 {
		output.PrintError("Invalid recipient public key")
		ExitWithError()
	}
	recipientKyberBytes, err := base64.StdEncoding.DecodeString(pubkeyPayload.PublicKeyKyber)
	if err != nil || len(recipientKyberBytes) != crypto.KyberPublicKeySize {
		output.PrintError("Invalid recipient kyber public key")
		ExitWithError()
	}
	var x25519Pub [32]byte
	copy(x25519Pub[:], recipientPubKeyBytes)
	recipientPubKey := &crypto.PublicKeySet{X25519: x25519Pub, Kyber: recipientKyberBytes}

	dataKey, err := crypto.GenerateDataKey()
	if err != nil {
		output.PrintError("Failed to generate data key: " + err.Error())
		ExitWithError()
	}

	ciphertext, nonce, err := crypto.EncryptMessage([]byte(plaintext), dataKey)
	if err != nil {
		output.PrintError("Failed to encrypt message: " + err.Error())
		ExitWithError()
	}

	senderSealed, err := crypto.SealDataKey(dataKey, pubKey)
	if err != nil {
		output.PrintError("Failed to seal data key for sender: " + err.Error())
		ExitWithError()
	}

	recipientSealed, err := crypto.SealDataKey(dataKey, recipientPubKey)
	if err != nil {
		output.PrintError("Failed to seal data key for recipient: " + err.Error())
		ExitWithError()
	}

	return map[string]string{
		"encrypted_body":       base64.StdEncoding.EncodeToString(ciphertext),
		"body_nonce":           base64.StdEncoding.EncodeToString(nonce),
		"sender_sealed_key":    base64.StdEncoding.EncodeToString(senderSealed),
		"recipient_sealed_key": base64.StdEncoding.EncodeToString(recipientSealed),
	}
}

func decryptChatMessage(msg api.ChatMessage, priv *crypto.PrivateKeySet, currentUserID int) string {
	if msg.EncryptedBody == "" {
		return "[empty]"
	}

	sealedKeyB64 := msg.SenderSealedKey
	if msg.SenderID != currentUserID {
		sealedKeyB64 = msg.RecipientSealedKey
	}

	sealedKeyBytes, err := base64.StdEncoding.DecodeString(sealedKeyB64)
	if err != nil {
		return "[decryption failed: invalid key]"
	}

	dataKey, err := crypto.UnsealDataKey(sealedKeyBytes, priv)
	if err != nil {
		return "[decryption failed: unseal error]"
	}

	ciphertext, err := base64.StdEncoding.DecodeString(msg.EncryptedBody)
	if err != nil {
		return "[decryption failed: invalid body]"
	}

	nonce, err := base64.StdEncoding.DecodeString(msg.BodyNonce)
	if err != nil || len(nonce) != 24 {
		return "[decryption failed: invalid nonce]"
	}

	plaintext, err := crypto.DecryptMessage(ciphertext, nonce, dataKey)
	if err != nil {
		return "[decryption failed]"
	}

	return string(plaintext)
}
