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
)

var (
	treeDepth    int
	treeDirsOnly bool
	treeAll      bool
)

var treeCmd = &cobra.Command{
	Use:     "tr [path]",
	GroupID: GroupNav,
	Aliases: []string{"tree"},
	Short:   "Display directory tree",
	Long: `Display a tree view of directories and files.

If no path is specified, shows the tree from the current working directory.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		targetPath := ""
		if len(args) > 0 {
			targetPath = args[0]
		}
		runTree(targetPath)
	},
}

func init() {
	rootCmd.AddCommand(treeCmd)
	treeCmd.Flags().IntVarP(&treeDepth, "depth", "L", 3, "maximum display depth")
	treeCmd.Flags().BoolVarP(&treeDirsOnly, "dirs", "d", false, "show directories only")
	treeCmd.Flags().BoolVarP(&treeAll, "all", "a", false, "show hidden files")
}

func runTree(targetPath string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(targetPath)
	options := map[string]string{
		"source": resolvedPath,
		"depth":  fmt.Sprintf("%d", treeDepth),
	}
	if treeDirsOnly {
		options["dirs"] = "true"
	}
	if treeAll {
		options["all"] = "true"
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

	resp, payload := cmdutil.ExecuteCommand[api.TreePayload](ctx, "tr", options, ExitWithError)

	decryptTreeEntries(payload.Entries)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}
	if cmdutil.RenderServerDisplay(resp) {
		return
	}
	fmt.Println(color.CyanString(payload.Path))
	printTreeEntries(payload.Entries, "")

	dirs, files := countTreeItems(payload.Entries)
	fmt.Printf("\n%d directories, %d files\n", dirs, files)
}

func decryptTreeEntries(entries []api.TreeEntry) {
	for i := range entries {
		if entries[i].E2EEDisplayName != "" {
			entries[i].Name = cmdutil.DecryptE2EEName(entries[i].E2EEDisplayName)
		}
		if len(entries[i].Children) > 0 {
			decryptTreeEntries(entries[i].Children)
		}
	}
}

func printTreeEntries(entries []api.TreeEntry, prefix string) {
	for i, entry := range entries {
		isLast := i == len(entries)-1

		connector := "├── "
		if isLast {
			connector = "└── "
		}

		name := entry.Name
		if entry.Type == "directory" {
			name = color.BlueString(name) + "/"
		}

		fmt.Printf("%s%s%s\n", prefix, connector, name)

		if len(entry.Children) > 0 {
			childPrefix := prefix
			if isLast {
				childPrefix += "    "
			} else {
				childPrefix += "│   "
			}
			printTreeEntries(entry.Children, childPrefix)
		}
	}
}

func countTreeItems(entries []api.TreeEntry) (dirs, files int) {
	for _, entry := range entries {
		if entry.Type == "directory" {
			dirs++
		} else {
			files++
		}
		if len(entry.Children) > 0 {
			childDirs, childFiles := countTreeItems(entry.Children)
			dirs += childDirs
			files += childFiles
		}
	}
	return
}
