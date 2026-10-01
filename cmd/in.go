package cmd

import (
	"fmt"

	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/completion"
	"pigcloud/internal/e2ee"
	"pigcloud/internal/output"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
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
	ctx, cancel := cmdutil.StartAuthed(ExitWithError)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(targetPath)
	options := map[string]string{"source": resolvedPath}
	if e2ee.HasE2EEKeys() {
		e2ee.AddPathTokensFor(options, resolvedPath, e2ee.SelfAndParent, ExitWithError)
		addChildScope(ctx, options, resolvedPath)
	}
	resp, payload := cmdutil.ExecuteCommand[api.InfoPayload](ctx, "in", options, ExitWithError)
	details := payload.Details

	if details.E2EEDisplayName != "" {
		details.Name = e2ee.DecryptE2EEName(details.E2EEDisplayName)
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
