package main

import (
	"fmt"
	"os"
	"strings"
	"text/template"

	"pigcloud/cmd"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type cmdInfo struct {
	Name        string
	Aliases     string
	Description string
	Flags       string
	Long        string
	Examples    string
	Subcommands []cmdInfo
}

type groupData struct {
	Title    string
	Commands []cmdInfo
}

type readmeData struct {
	Version string
	Groups  []groupData
	Tree    string
}

func flagHints(c *cobra.Command) string {
	var hints []string
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		if f.Shorthand != "" {
			hints = append(hints, "[-"+f.Shorthand+"]")
		} else {
			hints = append(hints, "[--"+f.Name+"]")
		}
	})
	return strings.Join(hints, " ")
}

func visibleSubs(c *cobra.Command) []*cobra.Command {
	var subs []*cobra.Command
	for _, sub := range c.Commands() {
		if !sub.Hidden && sub.IsAvailableCommand() {
			subs = append(subs, sub)
		}
	}
	return subs
}

func buildCommandsTree(root *cobra.Command, groupOrder []string, groupTitles map[string]string) string {
	grouped := map[string][]*cobra.Command{}
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || !c.IsAvailableCommand() {
			continue
		}
		gid := c.GroupID
		if gid == "" {
			gid = "tools"
		}
		grouped[gid] = append(grouped[gid], c)
	}

	var b strings.Builder
	b.WriteString("COMMANDS\n")

	for gi, gid := range groupOrder {
		cmds := grouped[gid]
		if len(cmds) == 0 {
			continue
		}
		isLastGroup := gi == len(groupOrder)-1
		groupConnector := "├── "
		groupPrefix := "│   "
		if isLastGroup {
			groupConnector = "└── "
			groupPrefix = "    "
		}
		fmt.Fprintf(&b, "\n%s%s\n", groupConnector, groupTitles[gid])

		for ci, c := range cmds {
			isLastCmd := ci == len(cmds)-1
			subs := visibleSubs(c)
			hasSubs := len(subs) > 0
			cmdConnector := "├── "
			cmdChildPrefix := "│   "
			if isLastCmd {
				cmdConnector = "└── "
				cmdChildPrefix = "    "
			}

			label := c.Name()
			if len(c.Aliases) > 0 {
				label = c.Name() + ", " + strings.Join(c.Aliases, ", ")
			}
			desc := c.Short
			if h := flagHints(c); h != "" {
				desc += " " + h
			}
			fmt.Fprintf(&b, "%s%s%-16s %s\n", groupPrefix, cmdConnector, label, desc)

			for si, sub := range subs {
				isLastSub := si == len(subs)-1
				subConnector := "├── "
				if isLastSub {
					subConnector = "└── "
				}
				subDesc := sub.Short
				if h := flagHints(sub); h != "" {
					subDesc += " " + h
				}
				fmt.Fprintf(&b, "%s%s%s%-12s %s\n", groupPrefix, cmdChildPrefix, subConnector, sub.Name(), subDesc)
			}
			if hasSubs && !isLastCmd {
				fmt.Fprintf(&b, "%s│\n", groupPrefix)
			}
		}
	}

	b.WriteString("\nGLOBAL FLAGS\n")
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		label := "--" + f.Name
		if f.Shorthand != "" {
			label = "-" + f.Shorthand + ", --" + f.Name
		}
		usage := f.Usage
		if len(usage) > 0 {
			usage = strings.ToUpper(usage[:1]) + usage[1:]
		}
		fmt.Fprintf(&b, "    %-15s%s\n", label, usage)
	})
	fmt.Fprintf(&b, "    %-15s%s\n", "--version", "Show version")
	return b.String()
}

func collectFlags(cmd *cobra.Command) string {
	var flags []string
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		if f.Shorthand != "" {
			flags = append(flags, fmt.Sprintf("-%s", f.Shorthand))
		} else {
			flags = append(flags, fmt.Sprintf("--%s", f.Name))
		}
	})
	if len(flags) == 0 {
		return ""
	}
	return " [" + strings.Join(flags, "] [") + "]"
}

func dedentExamples(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimLeft(line, " \t")
	}
	return strings.Join(lines, "\n")
}

func getAliases(c *cobra.Command) string {
	aliases := c.Aliases
	if len(aliases) == 0 {
		return ""
	}
	return strings.Join(aliases, ", ")
}

func main() {
	root := cmd.GetRootCmd()
	version := "latest"
	if len(os.Args) > 1 {
		version = os.Args[1]
	}

	groupOrder := []string{"auth", "nav", "files", "sharing", "tools"}
	groupTitles := map[string]string{
		"auth":    "Authentication",
		"nav":     "Navigation",
		"files":   "File Operations",
		"sharing": "Sharing",
		"tools":   "Info & Tools",
	}

	groupCmds := map[string][]cmdInfo{}
	for _, gid := range groupOrder {
		groupCmds[gid] = []cmdInfo{}
	}

	for _, c := range root.Commands() {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}
		gid := c.GroupID
		if gid == "" {
			continue
		}

		info := cmdInfo{
			Name:        c.Name(),
			Aliases:     getAliases(c),
			Description: c.Short,
			Flags:       collectFlags(c),
			Long:        strings.TrimSpace(c.Long),
			Examples:    dedentExamples(c.Example),
		}

		for _, sub := range c.Commands() {
			if !sub.IsAvailableCommand() {
				continue
			}
			subInfo := cmdInfo{
				Name:        sub.Name(),
				Aliases:     getAliases(sub),
				Description: sub.Short,
				Flags:       collectFlags(sub),
				Long:        strings.TrimSpace(sub.Long),
				Examples:    dedentExamples(sub.Example),
			}
			info.Subcommands = append(info.Subcommands, subInfo)
		}

		groupCmds[gid] = append(groupCmds[gid], info)
	}

	var groups []groupData
	for _, gid := range groupOrder {
		groups = append(groups, groupData{
			Title:    groupTitles[gid],
			Commands: groupCmds[gid],
		})
	}

	data := readmeData{
		Version: version,
		Groups:  groups,
		Tree:    buildCommandsTree(root, groupOrder, groupTitles),
	}

	tmpl := template.Must(template.New("readme").Parse(readmeTmpl))
	if err := tmpl.Execute(os.Stdout, data); err != nil {
		fmt.Fprintf(os.Stderr, "template error: %v\n", err)
		os.Exit(1)
	}
}

const readmeTmpl = `# PigCloud CLI

A command-line interface for managing your PigCloud storage.

PigCloud provides **end-to-end encryption** — files are encrypted on your device
before upload and decrypted locally after download. The server never sees
plaintext content, file names, or data keys. A familiar, Unix-style CLI lets you
upload, download, share, and organise files straight from your terminal.
Two-letter aliases, an interactive shell with tab-completion, and ` + "`--json`" + `
output make common workflows fast and scriptable.

The CLI also serves as a unique **decryption endpoint** — commands like ` + "`gr`" + `
(grep) and ` + "`di`" + ` (diff) can search and compare encrypted file contents
client-side, something the server and web UI cannot do.

## Installation

### Quick install

Linux and macOS:

` + "```bash" + `
curl -fsSL https://pigtech.de/cli/install.sh | sh
` + "```" + `

Windows (PowerShell):

` + "```powershell" + `
irm https://pigtech.de/cli/install.ps1 | iex
` + "```" + `

The script downloads the right build for your platform, verifies its SHA-256, and installs ` + "`pc`" + ` and ` + "`pigcloud`" + `.

### Download Binary

Download the appropriate binary for your platform from the [releases page](https://github.com/pigtech-de/pigcloud-cli/releases).

Each release contains two binaries: ` + "`pigcloud`" + ` (full name) and ` + "`pc`" + ` (shorthand alias).

#### Linux / macOS

` + "```bash" + `
curl -sSL https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-{{.Version}}-linux-amd64.tar.gz -o pigcloud.tar.gz
tar -xzf pigcloud.tar.gz
sudo install -m 755 pigcloud pc /usr/local/bin/
` + "```" + `

#### Windows

` + "```powershell" + `
# Download and extract
Invoke-WebRequest -Uri "https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-{{.Version}}-windows-amd64.zip" -OutFile pigcloud.zip
Expand-Archive pigcloud.zip -DestinationPath "$env:LOCALAPPDATA\pigcloud"

# Add to PATH (current user, persistent)
$path = [Environment]::GetEnvironmentVariable("Path", "User")
if ($path -notlike "*$env:LOCALAPPDATA\pigcloud*") {
    [Environment]::SetEnvironmentVariable("Path", "$path;$env:LOCALAPPDATA\pigcloud", "User")
}
` + "```" + `

Restart your terminal after adding to PATH.

### Build from Source

Requires Go 1.25 or later.

` + "```bash" + `
cd cli
make build
` + "```" + `

## Quick Start

` + "```bash" + `
# Authenticate — paste your API key when prompted
# (find it in the PigCloud web UI under Settings > API Keys)
pc login

# List files in the current directory
pc ls

# Upload a file
pc ul document.pdf /Documents/

# Upload from stdin
echo "Hello world" | pc ul - /hello.txt

# Download a file
pc dl /Documents/document.pdf ./

# Search file contents (fast — sealed index by default)
pc gr "TODO"

# Diff between file versions
pc di /report.md 3 5

# Start an interactive shell session
pc shell
` + "```" + `

### Authentication

Authentication is API-key based. Run ` + "`pc login`" + ` and paste the key from
your PigCloud account settings. The key is stored locally in your config file
and sent as a header with every request over HTTPS. No OAuth flow or browser
redirect needed.

- Each account has one active API key. Generating a new key revokes the previous one.
- The key secret is hashed with Argon2id on the server — it cannot be recovered, only regenerated.
- On Linux/macOS the config file is written with mode ` + "`0600`" + ` (owner-only). On Windows,
  standard user-directory ACLs apply.
- Treat your config file like an SSH private key: don't share it or commit it to version control.

## Security Model

PigCloud uses **end-to-end encryption (E2EE)** — the CLI encrypts files locally
before upload and decrypts them locally after download. The server stores only
ciphertext and never has access to plaintext content or data keys.

### End-to-End Encryption

| Layer | Algorithm | Details |
|-------|-----------|---------|
| File content | XChaCha20-Poly1305 (libsodium) | Streamed in 1 MB chunks. Per-file random data key, nonce incremented per chunk. SHA-256 integrity check on decrypt. |
| Data key sealing | X25519 ` + "`crypto_box_seal`" + ` | Each file's data key is sealed to the user's public key. Only the user's private key can unseal it. |
| File names | X25519 ` + "`crypto_box_seal`" + ` | Each name is individually sealed. The server stores and returns opaque blobs; the CLI decrypts client-side. |
| Path resolution | BLAKE2b-256 path tokens | Client computes deterministic tokens for O(1) lookups without revealing directory structure. |
| Key pair | X25519 (Curve25519) | Generated at account setup. Private key encrypted with a password-derived key (Argon2id). |
| File sharing | Re-seal per recipient | Data key is unsealed with sender's private key, then re-sealed with recipient's public key. |

E2EE keys are set up on first login and cached locally. The private key remains
encrypted at rest — it is only decrypted in memory when needed. Use ` + "`pc uk`" + ` to
unlock keys for a session and ` + "`pc lk`" + ` to lock them again.

## Paths

Commands that take paths distinguish between **remote** (cloud) and **local** (OS) paths:

- **Remote paths** are Unix-style and absolute (` + "`/Documents/report.pdf`" + `) or relative
  to the current cloud working directory (` + "`report.pdf`" + `). Use ` + "`pc wd`" + ` to see it,
  ` + "`pc cd`" + ` to change it.
- **Local paths** are your OS file system paths (` + "`./downloads/`" + `, ` + "`C:\\Users\\...`" + `).
- In ` + "`ul`" + ` and ` + "`dl`" + `, the first argument is always the **source** and the second
  is the **destination**. ` + "`ul`" + ` takes local then remote; ` + "`dl`" + ` takes remote then local.
- Use ` + "`-`" + ` as the local path for ` + "`ul`" + ` to read from stdin (remote path required).

## Commands

Use ` + "`pc`" + ` as a shorthand for ` + "`pigcloud`" + `. All commands have two-letter aliases.
The same tree is shown by ` + "`pc hl`" + ` at runtime.

` + "```text" + `
{{.Tree}}` + "```" + `

Run ` + "`pc hl <command>`" + ` for detailed help on any command (flags, examples, related commands).

## Command Details
{{range .Groups}}
{{- range .Commands}}{{$parent := .Name}}
{{- if or .Long .Examples}}

### ` + "`" + `{{.Name}}` + "`" + `{{if .Aliases}} ({{.Aliases}}){{end}} — {{.Description}}
{{if .Long}}
{{.Long}}
{{end}}
{{- if .Examples}}

` + "```bash" + `
{{.Examples}}
` + "```" + `
{{end}}
{{- end}}
{{- range .Subcommands}}
{{- if or .Long .Examples}}

#### ` + "`" + `{{$parent}} {{.Name}}` + "`" + ` — {{.Description}}
{{if .Long}}
{{.Long}}
{{end}}
{{- if .Examples}}

` + "```bash" + `
{{.Examples}}
` + "```" + `
{{end}}
{{- end}}
{{- end}}
{{- end}}
{{- end}}

## Configuration

Configuration is stored in:
- **Linux / macOS:** ` + "`~/.config/pigcloud/config.json`" + `
- **Windows:** ` + "`%APPDATA%\\pigcloud\\config.json`" + `

` + "```json" + `
{
  "api_key": "pc_live_abc123...",
  "endpoint": "https://pigtech.de/cloud/actions.php",
  "cwd": "/Documents",
  "e2ee_public_key": "base64...",
  "e2ee_private_key": "base64...(encrypted)"
}
` + "```" + `

| Key | Description |
|-----|-------------|
| ` + "`api_key`" + ` | Your API key for authentication |
| ` + "`endpoint`" + ` | The PigCloud API endpoint URL |
| ` + "`cwd`" + ` | Current working directory in cloud storage |
| ` + "`e2ee_*`" + ` | End-to-end encryption keys (fetched automatically on first use) |

Manage config from the CLI:

` + "```bash" + `
pc cf list                        # Show all values
pc cf get cwd                     # Get a single value
pc cf set cwd /Documents          # Set a value
` + "```" + `

## Troubleshooting

| Problem | Solution |
|---------|----------|
| ` + "`Authentication required`" + ` | Run ` + "`pc login`" + ` to set your API key. Keys can be generated in the PigCloud web UI under Settings > API Keys. |
| ` + "`Connection refused`" + ` / timeouts | Check your network and that the endpoint in ` + "`pc cf get endpoint`" + ` is reachable. |
| Upload fails for large files | Files up to your plan's storage limit are supported. Check remaining space with ` + "`pc st`" + `. |
| ` + "`File not found`" + ` on a path you can see in the web UI | Your CLI working directory may differ. Run ` + "`pc wd`" + ` to check and ` + "`pc cd /`" + ` to reset. |
| Destructive command prompts | Most destructive commands (` + "`rm`" + `, ` + "`vh rm`" + `, ` + "`pl rm`" + `, ` + "`tb empty`" + `) prompt for confirmation. Use ` + "`-f`" + ` to skip, or ` + "`-d`" + ` (dry run) to preview. |

## License

Copyright (c) PigTech. All rights reserved.
See [LICENSE](LICENSE) for details.
`
