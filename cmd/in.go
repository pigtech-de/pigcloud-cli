package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/completion"
	"pigcloud/internal/output"
)

var inCmd = &cobra.Command{
	Use:     "in [path]",
	GroupID: GroupTools,
	Aliases: []string{"info"},
	Short:   "Show file or directory info",
	Long: `Display detailed information about a file or directory.

Shows size, dates, type, sharing status, and recipients for shared folders.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		targetPath := ""
		if len(args) > 0 {
			targetPath = args[0]
		}
		runInfo(targetPath)
	},
}

func init() {
	rootCmd.AddCommand(inCmd)
}

func runInfo(targetPath string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(targetPath)
	options := map[string]string{"source": resolvedPath}
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
	resp, payload := cmdutil.ExecuteCommand[api.InfoPayload](ctx, "in", options, ExitWithError)
	details := payload.Details

	if details.E2EEDisplayName != "" {
		details.Name = cmdutil.DecryptE2EEName(details.E2EEDisplayName)
	}

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), details) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}

	fmt.Println()
	displayPath := details.Path
	if details.Name != "" {
		displayPath = details.Name
	}
	fmt.Printf("  %s  %s\n", color.HiWhiteString("Name:"), output.PrintPath(displayPath))
	fmt.Printf("  %s  %s\n", color.HiWhiteString("Type:"), details.Type)

	if details.Size != nil {
		fmt.Printf("  %s  %s\n", color.HiWhiteString("Size:"), output.FormatSize(details.Size))
	}
	if details.Entries != nil {
		fmt.Printf("  %s  %d items\n", color.HiWhiteString("Items:"), *details.Entries)
	}
	if details.Extension != "" {
		fmt.Printf("  %s  .%s\n", color.HiWhiteString("Extension:"), details.Extension)
	}

	fmt.Printf("  %s  %s\n", color.HiWhiteString("Modified:"), output.FormatTime(details.Modified))
	fmt.Printf("  %s  %s\n", color.HiWhiteString("Created:"), output.FormatTime(details.Created))

	fmt.Printf("  %s  %s\n", color.HiWhiteString("Owner:"), details.Owner)
	if details.Favorited {
		fmt.Printf("  %s  %s\n", color.HiWhiteString("Favorited:"), color.YellowString("Yes"))
	} else {
		fmt.Printf("  %s  No\n", color.HiWhiteString("Favorited:"))
	}
	if details.Hidden {
		fmt.Printf("  %s  %s\n", color.HiWhiteString("Hidden:"), color.HiBlackString("Yes"))
	} else {
		fmt.Printf("  %s  No\n", color.HiWhiteString("Hidden:"))
	}

	if details.Shared {
		if details.Direct {
			perm := "read"
			if details.Permission != nil {
				perm = *details.Permission
			}
			fmt.Printf("  %s  %s (%s)\n", color.HiWhiteString("Sharing:"), color.YellowString("Directly shared"), perm)
			if len(details.Recipients) > 0 {
				fmt.Printf("  %s\n", color.HiWhiteString("Shared with:"))
				for _, r := range details.Recipients {
					fmt.Printf("    - %s (%s)\n", r.Username, r.Permission)
				}
			}
		} else {
			fmt.Printf("  %s  %s\n", color.HiWhiteString("Sharing:"), color.HiBlackString("Inherited from parent"))
		}
	} else {
		fmt.Printf("  %s  Not shared\n", color.HiWhiteString("Sharing:"))
	}

	if details.PlaintextSize != nil {
		fmt.Printf("  %s  %s\n", color.HiWhiteString("Plaintext size:"), output.FormatSize(details.PlaintextSize))
	}

	fmt.Println()
}
