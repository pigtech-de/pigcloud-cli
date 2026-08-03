package cmd

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/completion"
	"pigcloud/internal/output"
	"strings"

	"github.com/spf13/cobra"
)

var cpDry bool

var cpCmd = &cobra.Command{
	Use:               "cp <source> <target>",
	GroupID:           GroupFiles,
	Aliases:           []string{"copy"},
	Short:             "Copy a file or directory",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		runCopy(args[0], args[1])
	},
}

func init() {
	cpCmd.Flags().BoolVarP(&cpDry, "dry-run", "d", false, "Preview what would be copied without making changes")
	rootCmd.AddCommand(cpCmd)
}

func runCopy(source, target string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolvedSource := cmdutil.ResolvePath(source)
	resolvedTarget := cmdutil.ResolvePath(target)

	targetTrailingSlash := strings.HasSuffix(target, "/")
	effectiveTarget := resolvedTarget
	if cmdutil.IsExistingDirectory(ctx, resolvedTarget) || targetTrailingSlash {
		sourceBase := filepath.Base(resolvedSource)
		if resolvedTarget == "/" {
			effectiveTarget = "/" + sourceBase
		} else {
			effectiveTarget = strings.TrimRight(resolvedTarget, "/") + "/" + sourceBase
		}
	}

	options := map[string]string{
		"source": resolvedSource,
		"target": effectiveTarget,
	}
	if cpDry {
		options["dry-run"] = "true"
	}

	fullPath, baseName := cmdutil.ResolveAndBaseName(effectiveTarget)
	cmdutil.AddE2eeNameFields(options, baseName, fullPath, ExitWithError)

	if cmdutil.HasE2EEKeys() {
		var paths []string
		srcTrimmed := strings.TrimPrefix(resolvedSource, "/")
		tgtTrimmed := strings.TrimPrefix(effectiveTarget, "/")
		if srcTrimmed != "" {
			paths = append(paths, srcTrimmed)
			if parent := filepath.Dir(srcTrimmed); parent != "." && parent != "" {
				paths = append(paths, parent)
			}
		}
		if tgtTrimmed != "" {
			paths = append(paths, tgtTrimmed)
			if parent := filepath.Dir(tgtTrimmed); parent != "." && parent != "" {
				paths = append(paths, parent)
			}
		}
		cmdutil.AddPathTokens(options, paths, ExitWithError)
	}

	_, payload := cmdutil.ExecuteCommand[api.CopyPayload](ctx, "cp", options, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	if payload.Dry {
		output.PrintInfo("[dry-run] Would copy " + output.PrintPath(payload.Path) + " to " + output.PrintPath(payload.Target))
		return
	}

	if !GetQuietOutput() {
		output.PrintSuccess("Copied " + output.PrintPath(payload.Path) + " to " + output.PrintPath(payload.Target))
	}
}
