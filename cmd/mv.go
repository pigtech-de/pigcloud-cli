package cmd

import (
	"context"
	"fmt"
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

var mvDry bool

var mvCmd = &cobra.Command{
	Use:     "mv <source> <target>",
	GroupID: GroupFiles,
	Aliases: []string{"move"},
	Short:   "Move or rename a file/directory",
	Long: `Move files or directories to a new location, or rename one.

If target is a directory, sources are moved into it.
If target doesn't exist, a single source is renamed to target.

Accepts multiple sources and glob patterns; the last argument is the target,
which must be an existing directory when moving more than one source.

Flags:
  -d, --dry-run   Show what would be moved without actually moving`,
	Example: `pc mv /draft.txt /final.txt        # Rename
pc mv a.txt b.txt /Archive/         # Move several into a directory
pc mv "*.log" /logs/                # Move by glob`,
	Args:              cobra.MinimumNArgs(2),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		runMvMulti(args)
	},
}

func runMvMulti(args []string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	target := args[len(args)-1]
	sources := expandPathArgs(ctx, args[:len(args)-1])
	if len(sources) == 0 {
		output.PrintError("Nothing to move")
		ExitWithError()
	}
	if len(sources) > 1 {
		resolvedTarget := cmdutil.ResolvePath(target)
		if !cmdutil.IsExistingDirectory(ctx, resolvedTarget) {
			output.PrintError("Target must be an existing directory when moving multiple sources: " + target)
			ExitWithError()
		}
	}
	for _, source := range sources {
		runMv(source, target)
	}
}

func init() {
	mvCmd.Flags().BoolVarP(&mvDry, "dry-run", "d", false, "Show what would be moved without actually moving")
	rootCmd.AddCommand(mvCmd)
}

func runMv(source, target string) {
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
	if mvDry {
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

	_, payload := cmdutil.ExecuteCommand[api.MovePayload](ctx, "mv", options, ExitWithError)

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), payload) {
		return
	}

	if payload.Dry {
		output.PrintInfo("[dry-run] Would move " + output.PrintPath(payload.Source) + " to " + output.PrintPath(payload.Target))
		return
	}

	if payload.Noop {
		output.PrintInfo("Source and target are the same, nothing to do")
		return
	}

	cmdutil.PropagateSubtreeNamesAtPath(ctx, effectiveTarget, ExitWithError)

	if !GetQuietOutput() {
		if payload.Count > 0 {
			output.PrintSuccess(fmt.Sprintf("Moved %d items matching %s to %s", payload.Count, payload.Pattern, output.PrintPath(payload.Target)))
		} else {
			output.PrintSuccess("Moved " + output.PrintPath(payload.Source) + " to " + output.PrintPath(payload.Target))
		}
	}
}
