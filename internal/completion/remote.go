package completion

import (
	"context"
	"encoding/json"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/config"
	"pigcloud/internal/filetypes"
)

var (
	cache      = make(map[string]cacheEntry)
	cacheMutex sync.Mutex
	cacheTTL   = 10 * time.Second
)

type cacheEntry struct {
	entries   []api.ListEntry
	timestamp time.Time
}

func RemotePathCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if !config.IsLoggedIn() {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var dirPath, prefix string
	if toComplete == "" {
		dirPath = config.GetCwd()
		prefix = ""
	} else if toComplete == "/" {
		dirPath = "/"
		prefix = ""
	} else if strings.HasSuffix(toComplete, "/") {
		dirPath = toComplete
		prefix = ""
	} else {
		dirPath = path.Dir(toComplete)
		prefix = path.Base(toComplete)
	}

	if dirPath != "/" && !strings.HasPrefix(dirPath, "/") {
		dirPath = path.Join(config.GetCwd(), dirPath)
	}

	dirPath = path.Clean(dirPath)
	if dirPath == "." {
		dirPath = config.GetCwd()
	}

	entries := getDirectoryListing(dirPath)
	if entries == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var completions []string
	for _, entry := range entries {
		name := entry.Name
		if name == "" || name == "(encrypted)" {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			continue
		}

		var completionPath string
		if strings.HasPrefix(toComplete, "/") {
			completionPath = path.Join(dirPath, name)
		} else if toComplete == "" {
			completionPath = name
		} else if strings.Contains(toComplete, "/") {
			completionPath = path.Join(path.Dir(toComplete), name)
		} else {
			completionPath = name
		}

		if entry.Type == "directory" {
			completionPath += "/"
			completions = append(completions, completionPath+"\tdir")
		} else {
			ext := strings.TrimPrefix(filepath.Ext(name), ".")
			cat := filetypes.TypeOf(strings.ToLower(ext))
			if cat == "other" {
				completions = append(completions, completionPath)
			} else {
				completions = append(completions, completionPath+"\t"+cat)
			}
		}
	}

	return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

func getDirectoryListing(dirPath string) []api.ListEntry {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	if entry, ok := cache[dirPath]; ok {
		if time.Since(entry.timestamp) < cacheTTL {
			return entry.entries
		}
	}

	options := map[string]string{"source": dirPath}

	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(dirPath, "/")
		var paths []string
		if trimmed != "" {
			paths = append(paths, trimmed)
			if parent := filepath.Dir(trimmed); parent != "." && parent != "" {
				paths = append(paths, parent)
			}
		}
		noExit := func() {}
		tokensJSON := cmdutil.ComputePathTokensForPaths(paths, noExit)
		if tokensJSON != "" {
			options["path_tokens"] = tokensJSON
		}
	}

	client := api.NewClient()
	resp, err := client.Execute(context.Background(), "ls", options)
	if err != nil || !resp.Success {
		return nil
	}

	var payload api.ListPayload
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		return nil
	}

	for i := range payload.Entries {
		entry := &payload.Entries[i]
		if entry.E2EEDisplayName != "" {
			entry.Name = cmdutil.DecryptE2EEName(entry.E2EEDisplayName)
		}
	}

	cache[dirPath] = cacheEntry{
		entries:   payload.Entries,
		timestamp: time.Now(),
	}

	return payload.Entries
}
