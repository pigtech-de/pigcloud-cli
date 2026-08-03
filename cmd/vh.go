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

var vhCmd = &cobra.Command{
	Use:     "vh <file>",
	GroupID: GroupFiles,
	Aliases: []string{"versions"},
	Short:   "View and manage file version history",
	Long: `View, restore, or delete file version history.

Subcommands:
  vh <file>                    List versions (default)
  vh rs <file> <version-id>    Restore a specific version
  vh rm <version-id>           Delete a specific version`,
	Example: `pc vh /report.pdf             # List versions
pc vh rs /report.pdf 42       # Restore version #42
pc vh rm 42                   # Delete version #42`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		runVersionList(args[0])
	},
}

var vhRestoreCmd = &cobra.Command{
	Use:   "rs <file> <version-id>",
	Short: "Restore a file to a specific version",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		runVersionRestore(args[0], args[1])
	},
}

var vhDlCmd = &cobra.Command{
	Use:   "dl <file> <version-id> [local-path]",
	Short: "Download a specific version",
	Example: `pc vh dl /report.pdf 42 ./        # Download version #42
pc vh dl /report.pdf 42 old.pdf  # Download to specific file`,
	Args: cobra.RangeArgs(2, 3),
	Run: func(cmd *cobra.Command, args []string) {
		localPath := "."
		if len(args) > 2 {
			localPath = args[2]
		}
		runVersionDownload(args[0], args[1], localPath)
	},
}

var vhPruneKeep int

var vhPruneCmd = &cobra.Command{
	Use:   "prune <file> --keep <N>",
	Short: "Delete all but the last N versions",
	Example: `pc vh prune /report.pdf --keep 3    # Keep last 3 versions
pc vh prune /report.pdf --keep 0    # Delete all versions`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runVersionPrune(args[0])
	},
}

var vhRmForce bool

var vhRmCmd = &cobra.Command{
	Use:   "rm <version-id>",
	Short: "Delete a specific version",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runVersionDelete(args[0])
	},
}

func init() {
	vhPruneCmd.Flags().IntVarP(&vhPruneKeep, "keep", "k", -1, "number of versions to keep")
	vhPruneCmd.MarkFlagRequired("keep")
	vhRmCmd.Flags().BoolVarP(&vhRmForce, "force", "f", false, "skip confirmation prompt")
	vhCmd.AddCommand(vhDlCmd, vhPruneCmd, vhRestoreCmd, vhRmCmd)
	rootCmd.AddCommand(vhCmd)
}

func runVersionList(filePath string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(filePath)
	vhListOpts := map[string]string{
		"source": resolvedPath,
		"mode":   "list",
	}
	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(resolvedPath, "/")
		var paths []string
		if trimmed != "" {
			paths = append(paths, trimmed)
			if parent := filepath.Dir(trimmed); parent != "." && parent != "" {
				paths = append(paths, parent)
			}
		}
		cmdutil.AddPathTokens(vhListOpts, paths, ExitWithError)
	}
	resp, payload := cmdutil.ExecuteCommand[api.VersionListPayload](ctx, "vh", vhListOpts, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}

	if len(payload.Versions) == 0 {
		output.PrintInfo("No version history for " + output.PrintPath(payload.Path))
		return
	}

	fmt.Printf("%s\n\n", output.PrintPath(payload.Path))

	table := output.Table([]string{"ID", "Version", "Size", "Created"})
	for _, v := range payload.Versions {
		created := output.FormatTime(&v.CreatedAt)
		table.Append([]string{
			fmt.Sprintf("%d", v.ID),
			color.CyanString("v%d", v.VersionNumber),
			output.FormatSize(&v.Size),
			created,
		})
	}
	table.Render()
	fmt.Printf("\n%d version(s)\n", len(payload.Versions))
}

func runVersionRestore(filePath, versionID string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(filePath)
	vhRestoreOpts := map[string]string{
		"source":     resolvedPath,
		"mode":       "restore",
		"version-id": versionID,
	}
	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(resolvedPath, "/")
		var paths []string
		if trimmed != "" {
			paths = append(paths, trimmed)
			if parent := filepath.Dir(trimmed); parent != "." && parent != "" {
				paths = append(paths, parent)
			}
		}
		cmdutil.AddPathTokens(vhRestoreOpts, paths, ExitWithError)
	}
	_, payload := cmdutil.ExecuteCommand[api.VersionActionPayload](ctx, "vh", vhRestoreOpts, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	if !GetQuietOutput() {
		output.PrintSuccess(fmt.Sprintf("Restored %s to v%d", output.PrintPath(payload.Path), payload.VersionNumber))
	}
}

func runVersionDelete(versionID string) {
	cmdutil.RequireLogin(ExitWithError)

	if !GetJSONOutput() && !GetQuietOutput() {
		if !cmdutil.ConfirmAction("Permanently delete version "+versionID+"?", vhRmForce) {
			output.PrintInfo("Cancelled")
			return
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	_, payload := cmdutil.ExecuteCommand[api.VersionActionPayload](ctx, "vh", map[string]string{
		"mode":       "delete",
		"version-id": versionID,
	}, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	if !GetQuietOutput() {
		output.PrintSuccess(fmt.Sprintf("Deleted version v%d", payload.VersionNumber))
	}
}

func runVersionDownload(filePath, versionID, localPath string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(filePath)
	options := map[string]string{
		"source":     resolvedPath,
		"mode":       "download",
		"version-id": versionID,
	}
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

	fileName := filepath.Base(resolvedPath)
	if fileName == "" || fileName == "/" {
		fileName = "download"
	}
	ext := filepath.Ext(fileName)
	base := strings.TrimSuffix(fileName, ext)
	versionedName := fmt.Sprintf("%s_v%s%s", base, versionID, ext)

	stat, err := os.Stat(localPath)
	if err == nil && stat.IsDir() {
		localPath = filepath.Join(localPath, versionedName)
	} else if localPath == "." {
		localPath = versionedName
	}

	bar := output.NewProgressBar(-1, "Downloading "+versionedName)

	client := api.NewClient()
	dlResult, err := client.DownloadCommand(ctx, "vh", options, localPath, func(received, total int64) {
		if total > 0 {
			bar.ChangeMax64(total)
		}
		bar.Set64(received)
	})

	bar.Finish()

	if err != nil {
		output.PrintError("Download failed: " + err.Error())
		os.Remove(localPath)
		ExitWithError()
	}

	if dlResult != nil && dlResult.E2EE {
		decryptDownloadedFile(localPath, dlResult)
	}

	finalStat, _ := os.Stat(localPath)
	var size int64
	if finalStat != nil {
		size = finalStat.Size()
	}

	if !GetQuietOutput() {
		output.PrintSuccess(fmt.Sprintf("Downloaded v%s to %s", versionID, localPath))
		output.PrintInfo("Size: " + output.FormatSize(&size))
	}
}

func runVersionPrune(filePath string) {
	cmdutil.RequireLogin(ExitWithError)

	if vhPruneKeep < 0 {
		output.PrintError("--keep flag is required")
		ExitWithError()
	}

	resolvedPath := cmdutil.ResolvePath(filePath)

	if !GetJSONOutput() && !GetQuietOutput() {
		prompt := fmt.Sprintf("Delete all but the last %d version(s) of %s?", vhPruneKeep, resolvedPath)
		if !cmdutil.ConfirmAction(prompt, false) {
			output.PrintInfo("Cancelled")
			return
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	options := map[string]string{
		"source": resolvedPath,
		"mode":   "prune",
		"keep":   fmt.Sprintf("%d", vhPruneKeep),
	}
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

	_, payload := cmdutil.ExecuteCommand[api.VersionPrunePayload](ctx, "vh", options, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	if payload.Pruned == 0 {
		output.PrintInfo("Nothing to prune")
	} else {
		output.PrintSuccess(fmt.Sprintf("Pruned %d version(s), keeping %d", payload.Pruned, payload.Kept))
	}
}
