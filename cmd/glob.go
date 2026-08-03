package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"path"
	"strings"

	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/output"
)

type globMatch struct {
	ID   string
	Name string
	Path string
	Type string
}

func isGlobArg(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func expandRemoteGlob(ctx context.Context, pattern string) []globMatch {
	resolved := cmdutil.ResolvePath(pattern)
	parent := path.Dir(resolved)
	base := path.Base(resolved)
	if isGlobArg(parent) {
		output.PrintError("Glob patterns are only supported in the last path segment: " + pattern)
		ExitWithError()
	}
	if !cmdutil.EnsureNamesReadable() {
		ExitWithError()
	}
	matcher, err := cmdutil.CompileMatcher(base, cmdutil.MatchGlob, false)
	if err != nil {
		output.PrintError("Invalid pattern: " + err.Error())
		ExitWithError()
	}

	options := map[string]string{"source": parent, "all": "true", "limit": "1000"}
	if cmdutil.HasE2EEKeys() {
		trimmed := strings.TrimPrefix(parent, "/")
		var paths []string
		if trimmed != "" {
			paths = append(paths, trimmed)
			if pp := path.Dir(trimmed); pp != "." && pp != "" && pp != "/" {
				paths = append(paths, pp)
			}
		}
		cmdutil.AddPathTokens(options, paths, ExitWithError)
	}
	_, payload := cmdutil.ExecuteCommand[api.ListPayload](ctx, "ls", options, ExitWithError)

	var matches []globMatch
	for i := range payload.Entries {
		entry := &payload.Entries[i]
		name := entry.Name
		if entry.E2EEDisplayName != "" {
			name = cmdutil.DecryptE2EEName(entry.E2EEDisplayName)
		}
		if name == "" || name == "(encrypted)" || !matcher.MatchString(name) {
			continue
		}
		matches = append(matches, globMatch{ID: entry.ID, Name: name, Path: path.Join(parent, name), Type: entry.Type})
	}
	return matches
}

func expandPathArgs(ctx context.Context, args []string) []string {
	var out []string
	for _, arg := range args {
		if !isGlobArg(arg) {
			out = append(out, arg)
			continue
		}
		matches := expandRemoteGlob(ctx, arg)
		if len(matches) == 0 {
			output.PrintWarning("No matches for pattern: " + arg)
			continue
		}
		for _, m := range matches {
			out = append(out, m.Path)
		}
	}
	return out
}

func forEachExpandedPath(args []string, fn func(string)) {
	cmdutil.RequireLogin(ExitWithError)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	for _, p := range expandPathArgs(ctx, args) {
		fn(p)
	}
}

func globMatchIDsJSON(matches []globMatch) string {
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m.ID
	}
	encoded, _ := json.Marshal(ids)
	return string(encoded)
}
