package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/output"
)

var nodeIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

var restoreCmd = &cobra.Command{
	Use:     "rs <path-or-node-id>",
	GroupID: GroupFiles,
	Aliases: []string{"restore"},
	Short:   "Restore an item from the recycling bin",
	Long: `Restore a file or directory from the recycling bin to its original location.

Accepts either a path ('/photo.jpg') or a 32-char hex node ID from 'pc tb ls'.
Paths are resolved against the recycling bin; if multiple deleted items share
the same name, you'll be asked which to restore.`,
	Example: `pc rs /report.pdf                 # Restore by path (prompts on collision)
pc rs abc123def456...           # Restore by node ID from 'pc tb' output`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runRestore(args[0])
	},
}

func init() {
	rootCmd.AddCommand(restoreCmd)
}

func runRestore(arg string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	nodeID := arg
	if !nodeIDPattern.MatchString(arg) {
		nodeID = resolveTrashPath(ctx, arg)
		if nodeID == "" {
			ExitWithError()
			return
		}
	}

	options := map[string]string{"node_id": nodeID}
	_, payload := cmdutil.ExecuteCommand[api.RestorePayload](ctx, "rs", options, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	output.PrintSuccess("Item restored successfully")
}

func resolveTrashPath(ctx context.Context, input string) string {
	_, payload := cmdutil.ExecuteCommand[api.TrashListPayload](ctx, "tb", map[string]string{}, ExitWithError)
	if len(payload.Items) == 0 {
		output.PrintError("Recycling bin is empty")
		return ""
	}

	target := strings.TrimPrefix(input, "/")
	target = path.Base(target)
	if target == "" || target == "." || target == "/" {
		output.PrintError("Path cannot be empty")
		return ""
	}

	type candidate struct {
		name      string
		nodeID    string
		itemType  string
		deletedAt string
	}
	var matches []candidate
	for i := range payload.Items {
		item := &payload.Items[i]
		name := item.Name
		if item.E2EEDisplayName != "" {
			name = cmdutil.DecryptE2EEName(item.E2EEDisplayName)
		}
		if name == target {
			matches = append(matches, candidate{
				name: name, nodeID: item.NodeID,
				itemType: item.Type, deletedAt: item.DeletedAt,
			})
		}
	}

	if len(matches) == 0 {
		output.PrintError("No item named " + target + " in recycling bin")
		return ""
	}
	if len(matches) == 1 {
		return matches[0].nodeID
	}

	fmt.Println("Multiple trashed items match " + target + ":")
	for i, m := range matches {
		fmt.Printf("  %d) [%s] %s — deleted %s (id %s)\n",
			i+1, m.itemType, m.name, m.deletedAt, m.nodeID[:8]+"…")
	}
	fmt.Print("Pick one (number, or empty to cancel): ")
	var choice string
	fmt.Scanln(&choice)
	choice = strings.TrimSpace(choice)
	if choice == "" {
		output.PrintInfo("Cancelled")
		return ""
	}
	idx := 0
	if _, err := fmt.Sscanf(choice, "%d", &idx); err != nil || idx < 1 || idx > len(matches) {
		output.PrintError("Invalid selection")
		return ""
	}
	return matches[idx-1].nodeID
}
