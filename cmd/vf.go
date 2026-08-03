package cmd

import (
	"bytes"
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

var vfLimit int

var verifyCmd = &cobra.Command{
	Use:     "vf [path]",
	GroupID: GroupTools,
	Aliases: []string{"verify"},
	Short:   "Verify file signatures",
	Long: `Download every file under a path and run the strict-AND signature check
(owner-key pin, or TEE signatures for sanitized files). Bytes are verified
in memory and discarded; nothing is written to disk.

Reports tampered or unverifiable files and exits non-zero if any fail.
Files uploaded before the signing rollout are reported as unsigned, not
failed.`,
	Example: `pc vf                # Verify everything
pc vf /Documents     # Verify one folder
pc vf -n 100         # Stop after 100 files`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		path := ""
		if len(args) > 0 {
			path = args[0]
		}
		runVerify(path)
	},
}

func init() {
	rootCmd.AddCommand(verifyCmd)
	verifyCmd.Flags().IntVarP(&vfLimit, "limit", "n", 0, "verify at most N files (0 = all)")
}

type verifyResult struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func runVerify(searchPath string) {
	cmdutil.RequireLogin(ExitWithError)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if !cmdutil.EnsureNamesReadable() {
		return
	}

	resolvedPath := cmdutil.ResolvePath(searchPath)

	treeOpts := map[string]string{
		"source": resolvedPath,
		"depth":  "50",
	}
	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(resolvedPath, "/")
		var paths []string
		if trimmed != "" {
			paths = append(paths, trimmed)
		}
		cmdutil.AddPathTokens(treeOpts, paths, ExitWithError)
	}
	_, tree := cmdutil.ExecuteCommand[api.TreePayload](ctx, "tr", treeOpts, ExitWithError)
	decryptTreeEntries(tree.Entries)

	var files []string
	var collect func(entries []api.TreeEntry, prefix string)
	collect = func(entries []api.TreeEntry, prefix string) {
		for _, e := range entries {
			relPath := e.Name
			if prefix != "" {
				relPath = prefix + "/" + e.Name
			}
			if e.Type == "directory" {
				collect(e.Children, relPath)
				continue
			}
			if resolvedPath == "/" {
				files = append(files, "/"+relPath)
			} else {
				files = append(files, resolvedPath+"/"+relPath)
			}
		}
	}
	collect(tree.Entries, "")

	if vfLimit > 0 && len(files) > vfLimit {
		files = files[:vfLimit]
	}
	if len(files) == 0 {
		output.PrintInfo("No files to verify in " + resolvedPath)
		return
	}

	if !GetQuietOutput() && !GetJSONOutput() {
		fmt.Fprintf(os.Stderr, "Verifying %d file(s)...\n\n", len(files))
	}

	client := api.NewClient()
	var results []verifyResult
	okCount, unsignedCount, failCount := 0, 0, 0

	for _, remotePath := range files {
		if ctx.Err() != nil {
			break
		}

		perFileOpts := map[string]string{}
		if cmdutil.HasE2EEKeys() {
			fileTrimmed := strings.TrimPrefix(remotePath, "/")
			var filePaths []string
			if fileTrimmed != "" {
				filePaths = append(filePaths, fileTrimmed)
				for p := filepath.Dir(fileTrimmed); p != "." && p != ""; p = filepath.Dir(p) {
					filePaths = append(filePaths, p)
				}
			}
			cmdutil.AddPathTokens(perFileOpts, filePaths, ExitWithError)
		}

		res := verifyResult{Path: remotePath, Status: "ok"}
		data, dlResult, err := client.DownloadToMemory(ctx, remotePath, perFileOpts)
		switch {
		case err != nil:
			res.Status = "fail"
			res.Error = "download: " + err.Error()
		case dlResult == nil || !dlResult.E2EE:
			res.Status = "unsigned"
			res.Error = "pre-E2EE file, no signature to verify"
		case dlResult.SignatureEd25519 == "" && dlResult.TEESignatureEd25519 == "":
			res.Status = "unsigned"
			res.Error = "uploaded before the signing rollout"
		default:
			if verr := cmdutil.VerifyDownloadIntegrity(bytes.NewReader(data), dlResult); verr != nil {
				res.Status = "fail"
				res.Error = verr.Error()
			}
		}

		switch res.Status {
		case "ok":
			okCount++
		case "unsigned":
			unsignedCount++
		case "fail":
			failCount++
		}
		results = append(results, res)

		if !GetJSONOutput() {
			switch res.Status {
			case "ok":
				if !GetQuietOutput() {
					fmt.Printf("  %s %s\n", color.GreenString("✓"), remotePath)
				}
			case "unsigned":
				fmt.Printf("  %s %s (%s)\n", color.YellowString("-"), remotePath, res.Error)
			case "fail":
				fmt.Printf("  %s %s: %s\n", color.RedString("✗"), remotePath, res.Error)
			}
		}
	}

	if cmdutil.PrintJSONOrContinue(GetJSONOutput(), map[string]any{
		"results":  results,
		"ok":       okCount,
		"unsigned": unsignedCount,
		"failed":   failCount,
	}) {
		if failCount > 0 {
			os.Exit(1)
		}
		return
	}

	fmt.Println()
	summary := fmt.Sprintf("%d verified", okCount)
	if unsignedCount > 0 {
		summary += fmt.Sprintf(", %d unsigned", unsignedCount)
	}
	if failCount > 0 {
		summary += fmt.Sprintf(", %d FAILED", failCount)
		output.PrintError(summary)
		os.Exit(1)
	}
	output.PrintSuccess(summary)
}
