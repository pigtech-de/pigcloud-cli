package cmdutil

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"

	"golang.org/x/term"

	"pigcloud/internal/api"
	"pigcloud/internal/config"
	"pigcloud/internal/output"
)

func RequireLogin(exitFn func()) {
	if !config.IsLoggedIn() {
		output.PrintError("Not logged in. Run 'pigcloud login' first.")
		exitFn()
	}
}

func IsExistingDirectory(ctx context.Context, remotePath string) bool {
	if remotePath == "" || remotePath == "/" {
		return true
	}
	options := map[string]string{"source": remotePath}
	if HasE2EEKeys() {
		trimmed := strings.TrimPrefix(remotePath, "/")
		var paths []string
		if trimmed != "" {
			paths = append(paths, trimmed)
			if parent := path.Dir(trimmed); parent != "." && parent != "" {
				paths = append(paths, parent)
			}
		}
		AddPathTokens(options, paths, func() {})
	}
	client := api.NewClient()
	resp, err := client.Execute(ctx, "in", options)
	if err != nil || resp == nil || !resp.Success {
		return false
	}
	var payload struct {
		Details struct {
			Type string `json:"type"`
		} `json:"details"`
	}
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		return false
	}
	return payload.Details.Type == "directory"
}

func ResolvePath(inputPath string) string {
	inputPath = stripMSYSConversion(inputPath)
	if inputPath == "" {
		return config.GetCwd()
	}
	if inputPath == "/" {
		return "/"
	}
	if inputPath[0] == '/' {
		return path.Clean(inputPath)
	}
	return path.Clean(path.Join(config.GetCwd(), inputPath))
}

func stripMSYSConversion(p string) string {
	if runtime.GOOS != "windows" || len(p) < 3 {
		return p
	}
	if !(p[1] == ':' && (p[2] == '/' || p[2] == '\\')) {
		return p
	}
	normalized := strings.ReplaceAll(p, "\\", "/")

	exePath := os.Getenv("EXEPATH")
	if exePath != "" {
		root := strings.ReplaceAll(exePath, "\\", "/")
		root = strings.TrimSuffix(root, "/bin")
		root = strings.TrimSuffix(root, "/usr/bin")
		root = strings.TrimSuffix(root, "/")
		if strings.HasPrefix(normalized, root+"/") {
			return "/" + normalized[len(root)+1:]
		}
		if normalized == root {
			return "/"
		}
	}

	for _, marker := range []string{"/Git/", "/msys64/", "/msys32/"} {
		if idx := strings.Index(normalized, marker); idx >= 0 {
			return "/" + normalized[idx+len(marker):]
		}
	}

	return p
}

func ExecuteCommand[T any](ctx context.Context, cmd string, options map[string]string, exitFn func()) (*api.Response, T) {
	var zero T

	client := api.NewClient()
	resp, err := client.Execute(ctx, cmd, options)
	if err != nil {
		output.PrintError("Request failed: " + err.Error())
		exitFn()
		return nil, zero
	}
	if !resp.Success {
		output.PrintError(resp.Message)
		exitFn()
		return nil, zero
	}

	var payload T
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		output.PrintError("Failed to parse response: " + err.Error())
		exitFn()
		return nil, zero
	}

	return resp, payload
}

func PrintJSONOrContinue(jsonEnabled bool, v any) bool {
	if !jsonEnabled {
		return false
	}
	jsonOut, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		output.PrintError("Failed to format JSON: " + err.Error())
		return true
	}
	fmt.Println(string(jsonOut))
	return true
}

func RenderServerDisplay(resp *api.Response) bool {
	if resp == nil || len(resp.Raw) == 0 {
		return false
	}
	var probe struct {
		Display json.RawMessage `json:"_display"`
	}
	if err := json.Unmarshal(resp.Raw, &probe); err != nil || len(probe.Display) == 0 {
		return false
	}
	var blocks []output.DisplayBlock
	if err := json.Unmarshal(probe.Display, &blocks); err != nil || len(blocks) == 0 {
		return false
	}
	if output.HasNameRefs(blocks) && !EnsureNamesReadable() {
		return true
	}
	output.RenderDisplay(os.Stdout, blocks, DecryptE2EEName)
	return true
}

func ConfirmAction(prompt string, force bool) bool {
	if force {
		return true
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return true
	}
	fmt.Printf("%s [y/N] ", prompt)
	var response string
	fmt.Scanln(&response)
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}
