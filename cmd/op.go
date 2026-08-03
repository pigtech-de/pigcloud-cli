package cmd

import (
	"fmt"
	"net/url"
	"os/exec"
	"path"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"pigcloud/internal/completion"
	"pigcloud/internal/config"
	"pigcloud/internal/output"
)

var opCmd = &cobra.Command{
	Use:     "op [path]",
	GroupID: GroupTools,
	Aliases: []string{"open"},
	Short:   "Open a file or folder in the browser",
	Long: `Open a file or folder in your default web browser.

If no path is specified, opens the current working directory.`,
	Example: `pc op                             # Open current directory
pc op /                           # Open root directory
pc op /photos                     # Open the photos folder`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.RemotePathCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		targetPath := ""
		if len(args) > 0 {
			targetPath = args[0]
		}
		runOpen(targetPath)
	},
}

func init() {
	rootCmd.AddCommand(opCmd)
}

func runOpen(targetPath string) {
	if !config.IsLoggedIn() {
		output.PrintError("Not logged in. Run 'pigcloud login' first.")
		ExitWithError()
	}

	if targetPath == "" {
		targetPath = config.GetCwd()
	} else if targetPath[0] != '/' {
		targetPath = path.Join(config.GetCwd(), targetPath)
	}

	endpoint := config.GetEndpoint()
	webURL := buildWebURL(endpoint, targetPath)

	if !GetQuietOutput() {
		output.PrintInfo("Opening " + output.PrintPath(targetPath))
	}

	if err := openBrowser(webURL); err != nil {
		output.PrintError("Failed to open browser: " + err.Error())
		ExitWithError()
	}
}

func buildWebURL(endpoint, targetPath string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "https://pigtech.de/cloud/#" + strings.TrimPrefix(targetPath, "/")
	}

	basePath := strings.TrimSuffix(parsed.Path, "actions.php")
	basePath = strings.TrimSuffix(basePath, "/")

	parsed.Path = basePath + "/"
	parsed.RawQuery = ""
	parsed.Fragment = strings.TrimPrefix(targetPath, "/")

	return parsed.String()
}

func openBrowser(u string) error {
	parsed, err := url.Parse(u)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return fmt.Errorf("refusing to open non-HTTP(S) URL: %s", u)
	}

	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}
