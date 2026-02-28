# PigCloud CLI

A command-line interface for managing your PigCloud storage.

## Installation

### Download Binary

Download the appropriate binary for your platform from the [releases page](https://github.com/pigtech-de/pigcloud-cli/releases).

#### Linux / macOS

```bash
# Download (replace with actual URL for your platform)
curl -sSL https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-1.4.0-linux-amd64.tar.gz -o pigcloud.tar.gz
tar -xzf pigcloud.tar.gz
sudo mv pigcloud pc /usr/local/bin/
```

#### Windows

Download the `.zip` from the releases page, extract, and add the directory to your PATH.

### Build from Source

Requires Go 1.21 or later.

```bash
cd cli
go mod download
go build -o pigcloud .
```

## Quick Start

```bash
# Login with your API key
pigcloud login

# List files
pigcloud ls

# Upload a file
pigcloud ul document.pdf /Documents/

# Download a file
pigcloud dl /Documents/document.pdf ./

# Start interactive shell
pigcloud shell
```

## Commands

Use `pc` as a shorthand for `pigcloud`. All commands have two-letter aliases.

### Authentication

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `li` | `login` | Authenticate with your API key |  |
| `lo` | `logout` | Remove stored credentials |  |
| `wh` | `whoami` | Show current user |  |

### Navigation

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `cd` | — | Change working directory |  |
| `fd` | `find` | Find files by name | ` [-n] [-t]` |
| `fv` | `favorite` | Manage favorites |  |
| `fv add` |  | Add a path to favorites |  |
| `fv list` |  | List all favorites |  |
| `fv rm` |  | Remove a path from favorites |  |
| `in` | `info` | Show file or directory info |  |
| `ls` | `list` | List files and directories | ` [-l] [-r] [-S] [-t]` |
| `op` | `open` | Open a file or folder in the browser |  |
| `tr` | `tree` | Display directory tree | ` [-d] [-D]` |
| `wd` | `pwd` | Print working directory |  |

### File Operations

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `cp` | `copy` | Copy a file or directory | ` [-d]` |
| `ct` | `cat` | Display file content | ` [--head] [-n] [-t]` |
| `dl` | `download` | Download a file or folder from cloud storage | ` [-x]` |
| `et` | `empty` | Empty the recycling bin | ` [-f]` |
| `mk` | `mkdir` | Create a new directory | ` [-p]` |
| `mv` | `move` | Move or rename a file/directory | ` [-d]` |
| `rm` | `remove` | Delete a file or directory | ` [-d] [-f] [-p]` |
| `rs` | `restore` | Restore an item from the recycling bin |  |
| `tb` | `trash` | List recycling bin contents |  |
| `ul` | `upload` | Upload a file or directory to cloud storage |  |
| `vh` | `versions` | View and manage file version history |  |
| `vh restore` |  | Restore a file to a specific version |  |
| `vh rm` |  | Delete a specific version |  |

### Sharing

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `pl` | `link` | Create and manage public links | ` [-e] [--max-downloads] [-P]` |
| `pl get` |  | Show public link details |  |
| `pl rm` |  | Revoke a public link |  |
| `pl set` |  | Update public link settings | ` [-e] [--max-downloads] [-P] [--remove-expiration] [--remove-max-downloads] [--remove-password]` |
| `sr` | `share` | Manage shared folders | ` [-p]` |
| `sr decline` |  | Decline a received share |  |
| `sr inbox` |  | List shares you've received from others |  |
| `sr ls` |  | List share recipients for a folder |  |
| `sr rm` |  | Remove a specific share recipient |  |

### Info & Tools

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `ac` | `activity` | View activity log and notifications | ` [-n] [-m] [-u]` |
| `cf` | `config` | Manage CLI configuration |  |
| `cf get` |  | Get a configuration value |  |
| `cf list` | `ls` | Show all configuration values |  |
| `cf set` |  | Set a configuration value |  |
| `cm` | `completion` | Generate shell completion scripts |  |
| `du` | `usage` | Show storage breakdown by file type | ` [-c] [-n]` |
| `hl` | `help` | Show help for commands | ` [-v]` |
| `sh` | `shell` | Start an interactive shell |  |
| `st` | `stats` | Show storage statistics |  |
| `vr` | `version` | Show version information |  |

## Command Details


### `li` (login) — Authenticate with your API key

Authenticate with your PigCloud API key.

You can find your API key in the PigCloud web interface under Settings > API Keys.
The API key will be stored securely in your local configuration file.

If no API key is provided as an argument, you will be prompted to enter it.


```bash
pc li                             # Authenticate with your API key
```


### `lo` (logout) — Remove stored credentials

Remove stored API key and configuration from your system.

This will delete your local configuration file and require you to login again.


### `wh` (whoami) — Show current user

Display information about the currently authenticated user.


### `cd` — Change working directory

Change the current working directory in your cloud storage.

The working directory is persisted across CLI sessions.
Use '..' to go up one directory, or '/' to go to root.


### `fd` (find) — Find files by name

Search for files and directories matching a pattern.

The pattern supports wildcards:
  * matches any characters
  ? matches a single character


```bash
pc fd "*.pdf"                     # Search for PDF files
pc fd "report*" /docs              # Search in /docs folder
pc fd -t d "project"               # Search for directories only
```


### `in` (info) — Show file or directory info

Display detailed information about a file or directory.

Shows size, dates, type, sharing status, and recipients for shared folders.


### `ls` (list) — List files and directories

List files and directories in your cloud storage.

If no path is specified, lists the current working directory.
Use 'pc cd' to change the working directory.

Flags:
  -l    Show detailed information (size in bytes, full timestamps)
  -r    List directories recursively
  -S    Sort by file size (largest first)
  -t    Sort by modification time (newest first)


```bash
pc ls -l -S                      # List with details, sorted by size
```


### `op` (open) — Open a file or folder in the browser

Open a file or folder in your default web browser.

If no path is specified, opens the current working directory.


```bash
pc op                             # Open current directory
pc op /                           # Open root directory
pc op /photos                     # Open the photos folder
```


### `tr` (tree) — Display directory tree

Display a tree view of directories and files.

If no path is specified, shows the tree from the current working directory.


### `wd` (pwd) — Print working directory

Display the current working directory in your cloud storage.


### `ct` (cat) — Display file content

Display the content of a text file from your cloud storage.

Only text files up to 1MB can be displayed. Binary files are not supported.

Flags:
  -n, --lines N   Show only the first N lines
  --head N        Show only the first N lines (same as -n)
  --tail N        Show only the last N lines


### `dl` (download) — Download a file or folder from cloud storage

Download a file or folder from your cloud storage.

If local-path is not specified, downloads to the current directory.
Folders are downloaded as ZIP archives.

Use --extract (-x) to download a folder's contents as individual files,
preserving the directory structure locally instead of creating a ZIP.


```bash
pc dl /Documents/report.pdf ./       # Download a file
pc dl /Documents ./docs -x              # Download folder contents individually
```


### `et` (empty) — Empty the recycling bin

Permanently delete all items in the recycling bin (.Trash folder).


### `mk` (mkdir) — Create a new directory

Create a new directory in your cloud storage.

By default, the parent directory must already exist.
Use -p to create parent directories as needed.


### `mv` (move) — Move or rename a file/directory

Move a file or directory to a new location, or rename it.

If target is a directory, the source will be moved into it.
If target doesn't exist, source will be renamed to target.

Flags:
  -d, --dry   Show what would be moved without actually moving


### `rm` (remove) — Delete a file or directory

Delete a file or directory from your cloud storage.

By default, items are moved to the recycling bin (.Trash folder) and can be restored later.
Items in the recycling bin are automatically deleted after 30 days.

Use --permanent to bypass the recycling bin and delete immediately.

Flags:
  -p, --permanent   Permanently delete, bypassing the recycling bin
  -d, --dry         Show what would be deleted without actually deleting
  -f, --force       Skip confirmation prompt


### `rs` (restore) — Restore an item from the recycling bin

Restore a file or directory from the recycling bin (.Trash folder) to its original location.

The item will be moved back to where it was before deletion. If the original
location no longer exists or contains an item with the same name, the restore
will fail.


```bash
pc rs /.Trash/document.txt       # Restore from recycling bin
```


### `tb` (trash) — List recycling bin contents

Show all items currently in the recycling bin with their original paths, sizes, and deletion dates.

Use 'rs' to restore items or 'et' to empty the bin.


```bash
pc tb                             # List trash contents
pc rs /.Trash/document.txt        # Restore an item
pc et                             # Empty the bin
```


### `ul` (upload) — Upload a file or directory to cloud storage

Upload a local file or directory to your cloud storage.

If remote-path is not specified, uploads to the current working directory.
If remote-path is a directory, the file keeps its original name.
If a directory is given, all files are uploaded recursively.


```bash
pc ul report.pdf /Documents/     # Upload a file
pc ul ./my-folder /Backups/      # Upload a directory recursively
```


### `vh` (versions) — View and manage file version history

View, restore, or delete file version history.

Subcommands:
  vh <file>                       List versions (default)
  vh restore <file> <version-id>  Restore a specific version
  vh rm <version-id>              Delete a specific version


```bash
pc vh /report.pdf                # List versions
pc vh restore /report.pdf 42     # Restore version #42
pc vh rm 42                      # Delete version #42
```


### `pl` (link) — Create and manage public links

Create shareable public links for files and directories.

When called with a path, creates a public link (or shows existing one).
Use subcommands for more control:

  pl <path>                  Create a public link
  pl get <path>              Show existing link details
  pl set <path>              Update link settings
  pl rm <path>               Revoke a public link


```bash
pc pl /report.pdf                         # Create public link
pc pl /report.pdf --password secret123    # Create with password
pc pl /report.pdf --expires "2026-03-01"  # Create with expiration
pc pl get /report.pdf                     # Show link details
pc pl set /report.pdf --max-downloads 50  # Set download limit
pc pl rm /report.pdf                      # Revoke link
```


### `sr` (share) — Manage shared folders

Share folders with other PigCloud users.

When called with <path> and <username>, toggles sharing.
Use subcommands for more control:

  sr ls <path>                  List share recipients
  sr rm <path> <username>       Remove a specific recipient
  sr inbox                      List shares you've received
  sr decline <path> <owner>     Decline a received share


```bash
pc sr /Shared team-member        # Share a folder
```


#### `sr inbox` — List shares you've received from others


```bash
pc sr inbox                      # Show folders shared with you
```


### `ac` (activity) — View activity log and notifications

View your recent activity and notifications.


```bash
pc ac                             # Show recent activity
pc ac -u                          # Show only unread notifications
pc ac -n 5                        # Show last 5 events
pc ac -m all                      # Mark all as read
pc ac -m 42                       # Mark a specific event as read
```


### `cf` (config) — Manage CLI configuration

View and modify CLI configuration settings.

Subcommands:
  list (ls)  Show all configuration values
  get        Get a specific configuration value
  set        Set a configuration value

Valid configuration keys:
  api_key   - Your API key for authentication
  endpoint  - The PigCloud API endpoint URL
  cwd       - Current working directory in cloud storage


#### `cf get` — Get a configuration value

Get a specific configuration value.

Valid keys: api_key, endpoint, cwd


#### `cf set` — Set a configuration value

Set a configuration value.

Valid keys: api_key, endpoint, cwd


```bash
pc cf set cwd /Documents          # Set a configuration value
pc cf set endpoint https://custom.example.com/api
```


### `cm` (completion) — Generate shell completion scripts

Generate shell completion scripts for PigCloud CLI.

To load completions:

Bash:
  $ source <(pigcloud completion bash)
  # To load completions for each session, add to your bashrc:
  $ echo 'source <(pigcloud completion bash)' >> ~/.bashrc

Zsh:
  $ source <(pigcloud completion zsh)
  # To load completions for each session, add to your zshrc:
  $ echo 'source <(pigcloud completion zsh)' >> ~/.zshrc

Fish:
  $ pigcloud completion fish | source
  # To load completions for each session:
  $ pigcloud completion fish > ~/.config/fish/completions/pigcloud.fish

PowerShell:
  PS> pigcloud completion powershell | Out-String | Invoke-Expression
  # To load completions for each session, add to your profile:
  PS> pigcloud completion powershell >> $PROFILE


### `du` (usage) — Show storage breakdown by file type

Analyze storage usage broken down by file type.

Shows how much space each category (images, documents, videos, etc.) uses.


```bash
pc du                             # Show storage breakdown
pc du -c                          # Show largest files for cleanup
pc du -c -n 20                    # Show top 20 largest files
```


### `hl` (help) — Show help for commands

Display help information about PigCloud CLI commands.


### `sh` (shell) — Start an interactive shell

Start an interactive shell for running multiple commands.

The shell shows your current working directory in the prompt.
Type 'exit' or 'quit' to leave the shell.

Example session:
  / > ls
  / > cd Documents
  /Documents > ct readme.txt
  /Documents > exit


### `st` (stats) — Show storage statistics

Display storage usage statistics for your account.


### `vr` (version) — Show version information

Display the version, commit hash, and build date of the CLI.


## Configuration

Configuration is stored in:
- Linux/macOS: `~/.config/pigcloud/config.json`
- Windows: `%APPDATA%\pigcloud\config.json`

| Key | Description |
|-----|-------------|
| `api_key` | Your API key for authentication |
| `endpoint` | The PigCloud API endpoint URL |
| `cwd` | Current working directory in cloud storage |

## Global Flags

| Flag | Description |
|------|-------------|
| `--json` | Output in JSON format for scripting |
| `-q, --quiet` | Suppress non-essential output |
| `--config` | Use a specific config file |

## License

Copyright PigTech. All rights reserved.
