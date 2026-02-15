# PigCloud CLI

A command-line interface for managing your PigCloud storage.

## Installation

### Download Binary

Download the appropriate binary for your platform from the [releases page](https://pigtech.de/cli/).

#### Linux / macOS

```bash
# Download (replace URL with actual download link)
curl -sSL https://pigtech.de/cli/pigcloud-linux-amd64 -o pigcloud
chmod +x pigcloud
sudo mv pigcloud /usr/local/bin/

# Optional: Also install the 'pc' shorthand
sudo ln -s /usr/local/bin/pigcloud /usr/local/bin/pc
```

#### Windows

Download `pigcloud-windows-amd64.exe` and add it to your PATH, or run it directly.

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

| Command | Alias    | Description                    |
|---------|----------|--------------------------------|
| `li`    | `login`  | Authenticate with your API key |
| `lo`    | `logout` | Remove stored credentials      |
| `wh`    | `whoami` | Show current user info         |

### Navigation

| Command | Alias            | Description                 | Flags                                                           |
|---------|------------------|-----------------------------|-----------------------------------------------------------------|
| `ls`    | `list`           | List files and directories  | `-l` long, `-r` recursive, `-S` sort by size, `-t` sort by time |
| `cd`    | -                | Change working directory    |                                                                 |
| `wd`    | `pwd`            | Print working directory     |                                                                 |
| `tr`    | `tree`           | Display directory tree      | `-d` depth, `-D` dirs only                                      |
| `fd`    | `find`, `search` | Find files by name pattern  | `-t` type (f/d), `-n` limit                                     |
| `in`    | `info`           | Show file or directory info |                                                                 |
| `op`    | `open`           | Open file/folder in browser |                                                                 |

### File Operations

| Command | Alias         | Description                     | Flags                          |
|---------|---------------|---------------------------------|--------------------------------|
| `ul`    | `upload`      | Upload a file                   |                                |
| `dl`    | `download`    | Download a file or folder       |                                |
| `ct`    | `cat`         | Display file content            | `-n` lines, `--head`, `--tail` |
| `mk`    | `mkdir`       | Create a new directory          | `-p` create parents            |
| `mv`    | `move`        | Move or rename a file/directory | `-d` dry run                   |
| `rm`    | `remove`      | Delete a file or directory      | `-p` permanent, `-d` dry run   |
| `et`    | `empty-trash` | Empty the recycling bin         |                                |
| `rs`    | `restore`     | Restore from recycling bin      |                                |

### Sharing

| Command                     | Alias   | Description              | Flags                        |
|-----------------------------|---------|--------------------------|------------------------------|
| `sr <path> <user>`          | `share` | Share a folder with user | `-p` permissions (read/edit) |
| `sr ls <path>`              |         | List share recipients    |                              |
| `sr rm <path> <user>`       |         | Remove a share recipient |                              |
| `sr inbox`                  |         | List received shares     |                              |
| `sr decline <path> <owner>` |         | Decline a received share |                              |

### Info & Tools

| Command | Alias            | Description                          | Flags                                      |
|---------|------------------|--------------------------------------|--------------------------------------------|
| `st`    | `stat`, `status` | Show storage statistics              |                                            |
| `du`    | `usage`          | Show storage breakdown by file type  | `-c` cleanup suggestions, `-n` file limit  |
| `ac`    | `activity`       | View activity log and notifications  | `-n` limit, `-u` unread, `-m` mark-read    |
| `cf`    | `config`         | View and modify CLI configuration    |                                            |
| `sh`    | `shell`          | Start an interactive shell           |                                            |
| `vr`    | `version`        | Show version information             |                                            |
| `hl`    | `help`           | Show help                            |                                            |
| `cp`    | `completion`     | Generate shell completions           |                                            |

## Examples

```bash
# Navigate
pc cd /Documents
pc ls
pc ls -l -S                     # Detailed list, sorted by size

# File operations
pc ul report.pdf                # Upload to current directory
pc dl report.pdf ./             # Download to current directory
pc mv report.pdf archive/       # Move to archive folder
pc mv -d old.txt new.txt        # Preview move (dry run)
pc rm old-file.txt              # Delete (moves to trash)
pc rm -p old-file.txt           # Permanently delete
pc rm -d important.txt          # Preview deletion (dry run)
pc rs /.Trash/file.txt          # Restore from recycling bin
pc et                           # Empty the recycling bin
pc ct readme.txt -n 20          # Show first 20 lines
pc op /Documents                # Open folder in browser

# Search and explore
pc fd "*.pdf" /Documents        # Find PDF files
pc tr -d 2                      # Tree with depth 2

# Directory management
pc mk -p /projects/new/folder   # Create nested directories

# Sharing
pc sr /Projects teammate -p edit  # Share with edit permissions
pc sr ls /Projects                # List share recipients
pc sr rm /Projects teammate       # Remove a recipient
pc sr inbox                       # List received shares
pc sr decline /Shared owner       # Decline a received share
pc in /Projects                   # View sharing info

# Storage info
pc st                           # Storage usage overview
pc du                           # Breakdown by file type
pc du -c                        # Cleanup suggestions

# Activity log
pc ac                           # Recent activity
pc ac -u                        # Unread notifications only
pc ac -m all                    # Mark all as read

# Configuration
pc cf list                      # Show all config
pc cf set cwd /Documents        # Change default directory

# Interactive shell
pc shell
/ > ls
/ > cd Documents
/Documents > exit

# Get help
pc hl
pc ls --help
```

## Configuration

Configuration is stored in:
- Linux/macOS: `~/.config/pigcloud/config.json`
- Windows: `%APPDATA%\pigcloud\config.json`

### Config Commands

```bash
# View all configuration
pc cf list

# Get a specific value
pc cf get endpoint

# Set a value
pc cf set cwd /Documents
pc cf set endpoint https://custom.example.com/api
```

### Config Keys

| Key        | Description                                |
|------------|--------------------------------------------|
| `api_key`  | Your API key for authentication            |
| `endpoint` | The PigCloud API endpoint URL              |
| `cwd`      | Current working directory in cloud storage |

## Shell Completion

Tab completion for remote paths is built-in. Enable shell completion:

```bash
# Bash
source <(pigcloud completion bash)

# Zsh
source <(pigcloud completion zsh)

# Fish
pigcloud completion fish | source

# PowerShell
pigcloud completion powershell | Out-String | Invoke-Expression
```

## Interactive Shell

Start an interactive shell for running multiple commands:

```bash
pc shell
```

Features:
- Prompt shows current working directory
- Commands don't exit on errors
- Type `exit` or `quit` to leave

## Global Flags

| Flag          | Description                         |
|---------------|-------------------------------------|
| `--json`      | Output in JSON format for scripting |
| `-q, --quiet` | Suppress non-essential output       |
| `--config`    | Use a specific config file          |

## Building

```bash
# Build for current platform
make build

# Build for all platforms
make build-all

# Build with version info
make build-all VERSION=1.2.0

# Run tests
make test
```

## License

Copyright PigTech. All rights reserved.
