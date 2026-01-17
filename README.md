# PigCloud CLI

A command-line interface for managing your PigCloud storage.

## Installation

### Download Binary

Download the latest release for your platform:

| Platform | Download |
|----------|----------|
| Windows (64-bit) | [pigcloud-windows-amd64.exe](https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-windows-amd64.exe) |
| Linux (64-bit) | [pigcloud-linux-amd64](https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-linux-amd64) |
| Linux (ARM64) | [pigcloud-linux-arm64](https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-linux-arm64) |
| macOS (Intel) | [pigcloud-darwin-amd64](https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-darwin-amd64) |
| macOS (Apple Silicon) | [pigcloud-darwin-arm64](https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-darwin-arm64) |

### Linux / macOS

```bash
# Download (example for Linux 64-bit)
curl -sSL https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-linux-amd64 -o pigcloud
chmod +x pigcloud
sudo mv pigcloud /usr/local/bin/

# Optional: Create 'pc' shorthand
sudo ln -s /usr/local/bin/pigcloud /usr/local/bin/pc
```

### Windows

Download `pigcloud-windows-amd64.exe`, rename it to `pigcloud.exe` (or `pc.exe`), and add it to your PATH.

### Build from Source

Requires Go 1.21 or later.

```bash
git clone https://github.com/pigtech-de/pigcloud-cli.git
cd pigcloud-cli
go build -o pigcloud .
```

## Quick Start

```bash
# Login with your API key
pigcloud login

# Or provide the key directly
pigcloud login YOUR_API_KEY

# List files
pigcloud ls

# Upload a file
pigcloud ul document.pdf /Documents/

# Download a file
pigcloud dl /Documents/document.pdf ./
```

## Commands

Use `pc` as a shorthand for `pigcloud`. Commands also have aliases.

| Command | Alias | Description |
|---------|-------|-------------|
| `login` | - | Authenticate with your API key |
| `logout` | - | Remove stored credentials |
| `ls` | `list` | List files and directories |
| `cd` | - | Change working directory |
| `in` | `info` | Show file or directory info |
| `ul` | `upload` | Upload a file |
| `dl` | `download` | Download a file or folder |
| `mv` | `move` | Move or rename a file/directory |
| `rm` | `remove` | Delete a file or directory |
| `sr` | `share` | Share a folder with another user |
| `hl` | `help` | Show help |
| `completion` | - | Generate shell completions |

## Examples

```bash
# Navigate
pc cd /Documents
pc ls

# File operations
pc ul report.pdf              # Upload to current directory
pc dl report.pdf ./           # Download to current directory
pc mv report.pdf archive/     # Move to archive folder
pc rm old-file.txt -y         # Delete with confirmation skip

# Sharing
pc sr /Projects teammate --permissions edit
pc in /Projects               # View sharing info

# Get help
pc hl
pc upload --help
```

## Configuration

Configuration is stored in:
- **Linux/macOS:** `~/.config/pigcloud/config.json`
- **Windows:** `%APPDATA%\pigcloud\config.json`

## Shell Completion

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

## License

Copyright PigTech. All rights reserved.
