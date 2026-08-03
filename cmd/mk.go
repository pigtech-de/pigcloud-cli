package cmd

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/completion"
	"pigcloud/internal/output"
)

var mkParents bool

var mkCmd = &cobra.Command{
	Use:     "mk <path>",
	GroupID: GroupFiles,
	Aliases: []string{"mkdir"},
	Short:   "Create a new directory",
	Long: `Create a new directory in your cloud storage.

By default, the parent directory must already exist.
Use -p to create parent directories as needed.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		runMk(args[0])
	},
}

func init() {
	rootCmd.AddCommand(mkCmd)
	mkCmd.Flags().BoolVarP(&mkParents, "parents", "p", false, "create parent directories as needed")
}

func runMk(targetPath string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedPath := cmdutil.ResolvePath(targetPath)
	options := map[string]string{"source": resolvedPath}
	if mkParents {
		options["parents"] = "true"
		fullPath, _ := cmdutil.ResolveAndBaseName(resolvedPath)
		segments := strings.Split(strings.Trim(fullPath, "/"), "/")
		var nonEmpty []string
		for _, s := range segments {
			s = strings.TrimSpace(s)
			if s != "" && s != "." && s != ".." {
				nonEmpty = append(nonEmpty, s)
			}
		}
		cmdutil.AddE2eeNameFieldsForMkParents(options, nonEmpty, ExitWithError)
	} else {
		fullPath, baseName := cmdutil.ResolveAndBaseName(resolvedPath)
		cmdutil.AddE2eeNameFields(options, baseName, fullPath, ExitWithError)
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

	_, payload := cmdutil.ExecuteCommand[api.CdPayload](ctx, "mk", options, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	if !GetQuietOutput() {
		output.PrintSuccess("Created " + output.PrintPath(resolvedPath))
	}
}
