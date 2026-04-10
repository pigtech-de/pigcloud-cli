# PigCloud CLI

A command-line interface for managing your PigCloud storage.

PigCloud provides **end-to-end encryption** — files are encrypted on your device
before upload and decrypted locally after download. The server never sees
plaintext. File names are also encrypted at rest with per-user keys. A familiar,
Unix-style CLI lets you upload, download, share, and organise files straight from
your terminal. Two-letter aliases and an interactive shell with tab-completion
keep common workflows fast, and `--json` output makes scripting easy.

## Installation

### Download Binary

Download the appropriate binary for your platform from the [releases page](https://github.com/pigtech-de/pigcloud-cli/releases).

Each release contains two binaries: `pigcloud` (full name) and `pc` (shorthand alias).

#### Linux / macOS

```bash
curl -sSL https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-1.7.0-linux-amd64.tar.gz -o pigcloud.tar.gz
tar -xzf pigcloud.tar.gz
sudo install -m 755 pigcloud pc /usr/local/bin/
```

#### Windows

```powershell
# Download and extract
Invoke-WebRequest -Uri "https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-1.7.0-windows-amd64.zip" -OutFile pigcloud.zip
Expand-Archive pigcloud.zip -DestinationPath "$env:LOCALAPPDATA\pigcloud"

# Add to PATH (current user, persistent)
$path = [Environment]::GetEnvironmentVariable("Path", "User")
if ($path -notlike "*$env:LOCALAPPDATA\pigcloud*") {
    [Environment]::SetEnvironmentVariable("Path", "$path;$env:LOCALAPPDATA\pigcloud", "User")
}
```

Restart your terminal after adding to PATH.

### Build from Source

Requires Go 1.24 or later.

```bash
cd cli
go mod download
go build -o pigcloud .
```

## Quick Start

```bash
# Authenticate — paste your API key when prompted
# (find it in the PigCloud web UI under Settings > API Keys)
pigcloud login

# List files in the current directory
pigcloud ls

# Upload a file
pigcloud ul document.pdf /Documents/

# Download a file
pigcloud dl /Documents/document.pdf ./

# Start an interactive shell session
pigcloud shell
```

### Authentication

Authentication is API-key based. Run `pigcloud login` and paste the key from
your PigCloud account settings. The key is stored locally in your config file
and sent as a header with every request over HTTPS. No OAuth flow or browser
redirect needed.

- Each account has one active API key. Generating a new key revokes the previous one.
- The key secret is hashed with Argon2id on the server — it cannot be recovered, only regenerated.
- On Linux/macOS the config file is written with mode `0600` (owner-only). On Windows,
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
| Data key sealing | X25519 `crypto_box_seal` | Each file's data key is sealed to the user's public key. Only the user's private key can unseal it. |
| Key pair | X25519 (Curve25519) | Generated at account setup. Private key encrypted with a password-derived key (Argon2id). |
| File sharing | Re-seal per recipient | Data key is unsealed with sender's private key, then re-sealed with recipient's public key. |

E2EE keys are set up on first login and cached locally. The private key remains
encrypted at rest — it is only decrypted in memory when needed.

### Server-Side Layers

| Layer | Algorithm | Details |
|-------|-----------|---------|
| File name encryption | AES-256-GCM (libsodium) | Non-deterministic — fresh random nonce per name. Names are never stored in plaintext. |
| Path lookup | HMAC-SHA-256 | Canonical paths are hashed with a per-user key for O(1) lookups without revealing structure. |
| Key storage | HashiCorp Vault KV | Per-user name-encryption and HMAC keys stored in Vault — not derived from the API key. |

## Paths

Commands that take paths distinguish between **remote** (cloud) and **local** (OS) paths:

- **Remote paths** are Unix-style and absolute (`/Documents/report.pdf`) or relative
  to the current cloud working directory (`report.pdf`). Use `pc wd` to see it,
  `pc cd` to change it.
- **Local paths** are your OS file system paths (`./downloads/`, `C:\Users\...`).
- In `ul` and `dl`, the first argument is always the **source** and the second
  is the **destination**. `ul` takes local then remote; `dl` takes remote then local.

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
| `fd` | `find` | Find files by name | ` [-a] [-n] [-t]` |
| `fv` | `favorite` | Manage favorites |  |
| `fv add` |  | Add a path to favorites |  |
| `fv list` |  | List all favorites |  |
| `fv rm` |  | Remove a path from favorites |  |
| `in` | `info` | Show file or directory info |  |
| `ls` | `list` | List files and directories | ` [-a] [-n] [-l] [-o] [-r] [-S] [-t]` |
| `op` | `open` | Open a file or folder in the browser |  |
| `rc` | `recents` | List recently accessed files | ` [-l]` |
| `tr` | `tree` | Display directory tree | ` [-a] [-d] [-D]` |
| `wd` | `pwd` | Print working directory |  |

### File Operations

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `cp` | `copy` | Copy a file or directory | ` [-d]` |
| `ct` | `cat` | Display file content | ` [--head] [-n] [-t]` |
| `dl` | `download` | Download a file or folder from cloud storage | ` [-x] [--overwrite] [--skip-existing] [-z]` |
| `et` | `empty` | Empty the recycling bin | ` [-f]` |
| `hd` | `hide` | Hide or unhide files and folders |  |
| `hd list` |  | List all hidden items |  |
| `hd off` |  | Unhide a file or folder |  |
| `hd on` |  | Hide a file or folder |  |
| `mk` | `mkdir` | Create a new directory | ` [-p]` |
| `mv` | `move` | Move or rename a file/directory | ` [-d]` |
| `rm` | `remove` | Delete a file or directory | ` [-d] [-f] [-p]` |
| `rs` | `restore` | Restore an item from the recycling bin |  |
| `sy` | `sync` | Synchronize a local directory with cloud storage | ` [--delete] [-n] [-j] [--pull] [--push] [-v]` |
| `tb` | `trash` | List recycling bin contents | ` [-S] [-t]` |
| `tc` | `touch` | Create a new text file | ` [-c]` |
| `ul` | `upload` | Upload a file or directory to cloud storage | ` [-j] [--skip-existing]` |
| `vh` | `versions` | View and manage file version history |  |
| `vh restore` |  | Restore a file to a specific version |  |
| `vh rm` |  | Delete a specific version |  |

### Sharing

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `ch` | `chat` | Send and receive E2EE chat messages | ` [--before] [-n]` |
| `ch read` |  | Mark conversation as read |  |
| `ch rm` |  | Delete a sent message |  |
| `ch send` |  | Send a chat message or share a file | ` [-f]` |
| `ch unread` |  | Show unread message counts |  |
| `fr` | `friend` | Manage friends |  |
| `fr accept` |  | Accept a friend request |  |
| `fr add` |  | Send a friend request |  |
| `fr decline` |  | Decline a friend request |  |
| `fr pending` |  | List pending friend requests |  |
| `fr rm` |  | Remove a friend |  |
| `pl` | `link` | Create and manage public links | ` [-e] [--max-downloads] [-P]` |
| `pl get` |  | Show public link details |  |
| `pl rm` |  | Revoke a public link |  |
| `pl set` |  | Update public link settings | ` [-e] [--max-downloads] [-P] [--remove-expiration] [--remove-max-downloads] [--remove-password]` |
| `sr` | `share` | Manage shared files and folders | ` [-f] [-p]` |
| `sr accept` |  | Accept a pending share |  |
| `sr decline` |  | Decline a received share |  |
| `sr inbox` |  | List shares you've received from others |  |
| `sr ls` |  | List share recipients for a folder |  |
| `sr rm` |  | Remove a specific share recipient |  |
| `sr set` |  | Update share settings (permission, password, expiry) | ` [-e] [-P] [-p] [--remove-expiration] [--remove-password] [-u]` |

### Info & Tools

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `ac` | `activity` | View activity log and notifications | ` [-n] [-m] [-o] [-u]` |
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

You will be prompted to enter your API key securely (input is hidden).


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


### `rc` (recents) — List recently accessed files

Show files and folders you've recently opened or accessed.


```bash
pc rc              # List recent items
pc rc -l 10        # Show last 10 recent items
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

Use --extract (-x) to download a folder's contents as individual files,
preserving the directory structure locally instead of creating a ZIP.

Use --zip (-z) to download a folder's contents into a local ZIP archive.
Files are decrypted before being added to the archive.


```bash
pc dl /Documents/report.pdf ./       # Download a file
pc dl /Documents ./docs -x              # Download folder contents individually
pc dl /Documents ./backup.zip --zip     # Download folder as ZIP archive
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


### `sy` (sync) — Synchronize a local directory with cloud storage

Synchronize files between a local directory and your cloud storage.

By default, performs bidirectional sync: uploads new/changed local files
and downloads new/changed remote files. Use --push or --pull to restrict
the direction.

Files are compared by size and modification time. A file is considered
changed when its size differs from the other side.


```bash
pc sy ./project /Backups/project         # Bidirectional sync
pc sy ./photos /Photos --push            # Upload only (local → cloud)
pc sy ./docs /Documents --pull           # Download only (cloud → local)
pc sy . /Work --delete --dry-run         # Preview sync with deletions
```


### `tb` (trash) — List recycling bin contents

Show all items currently in the recycling bin with their original paths, sizes, and deletion dates.

Use 'rs' to restore items or 'et' to empty the bin.


```bash
pc tb                             # List trash contents
pc rs /.Trash/document.txt        # Restore an item
pc et                             # Empty the bin
```


### `tc` (touch) — Create a new text file

Create a new text file in your cloud storage.

Content can be provided via --content flag or piped from stdin.
The file is encrypted client-side before upload (E2EE).

If no extension is given, .txt is appended automatically.
Only text file types are allowed.


```bash
pc tc notes.txt /Documents --content "Hello world"
echo "piped content" | pc tc log.txt /Logs
pc tc readme.md --content "# Title"
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


### `ch` (chat) — Send and receive E2EE chat messages

Encrypted chat with other PigCloud users (requires friendship).

When called with no arguments, lists conversations.
With a username, shows recent messages.
With a username and message, sends a message.

Use subcommands for more control:

  ch                           List conversations
  ch <username>                Show messages
  ch <username> "hello"        Send a message (shorthand)
  ch send <user> "message"     Send (explicit, supports stdin pipe)
  ch send <user> -f /file      Share a file in chat
  ch rm <message-id>           Delete own message
  ch read <username>           Mark conversation as read
  ch unread                    Show unread counts


```bash
pc ch                            # List conversations
pc ch alice                      # Show messages with alice
pc ch alice "hello!"             # Send message to alice
echo "hi" | pc ch send alice     # Pipe message
pc ch send alice -f /Photos      # Share a file
```


### `fr` (friend) — Manage friends


```bash
pc fr                    # List your friends
pc fr add alice           # Send a friend request
pc fr accept alice        # Accept a friend request
pc fr decline alice       # Decline a friend request
pc fr rm alice            # Remove a friend
pc fr pending             # List pending friend requests
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


### `sr` (share) — Manage shared files and folders

Share files and folders with other PigCloud users.

When called with <path> and <username>, toggles sharing.
Use subcommands for more control:

  sr ls <path>                  List share recipients
  sr rm <path> <username>       Remove a specific recipient
  sr set <path>                 Update share settings
  sr inbox                      List shares you've received
  sr accept <path> <owner>      Accept a pending share
  sr decline <path> <owner>     Decline a received share


```bash
pc sr /Shared team-member        # Share a folder
```


#### `sr accept` — Accept a pending share


```bash
pc sr accept /Documents alice     # Accept a share from alice
```


#### `sr inbox` — List shares you've received from others


```bash
pc sr inbox                      # Show folders shared with you
```


#### `sr set` — Update share settings (permission, password, expiry)

Update settings on an existing share.

Update a recipient's permission level:
  sr set /Docs -u bob -p edit

Set or change share password/expiration:
  sr set /Docs --password secret123
  sr set /Docs --expires "2026-12-31"

Remove password/expiration:
  sr set /Docs --remove-password
  sr set /Docs --remove-expiration


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
  api_key       - Your API key for authentication
  endpoint      - The PigCloud API endpoint URL
  cwd           - Current working directory in cloud storage
  default_json  - Always output JSON (true/false)
  default_quiet - Suppress non-essential output (true/false)
  no_color      - Disable colored output (true/false)


#### `cf get` — Get a configuration value

Get a specific configuration value.

Valid keys: api_key, endpoint, cwd, default_json, default_quiet, no_color


#### `cf set` — Set a configuration value

Set a configuration value.

Valid keys: api_key, endpoint, cwd, default_json, default_quiet, no_color


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
Press Ctrl+C to cancel the current line, Ctrl+D to exit.

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
- **Linux / macOS:** `~/.config/pigcloud/config.json`
- **Windows:** `%APPDATA%\pigcloud\config.json`

```json
{
  "api_key": "pc_live_abc123...",
  "endpoint": "https://pigtech.de/cloud/actions.php",
  "cwd": "/Documents",
  "e2ee_public_key": "base64...",
  "e2ee_private_key": "base64...(encrypted)"
}
```

| Key | Description |
|-----|-------------|
| `api_key` | Your API key for authentication |
| `endpoint` | The PigCloud API endpoint URL |
| `cwd` | Current working directory in cloud storage |
| `e2ee_*` | End-to-end encryption keys (fetched automatically on first use) |

Manage config from the CLI:

```bash
pc cf list                        # Show all values
pc cf get cwd                     # Get a single value
pc cf set cwd /Documents          # Set a value
```

## Global Flags

| Flag | Description |
|------|-------------|
| `--json` | Output in JSON format for scripting |
| `-q, --quiet` | Suppress non-essential output |
| `--config` | Use a specific config file |

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `Authentication required` | Run `pc login` to set your API key. Keys can be generated in the PigCloud web UI under Settings > API Keys. |
| `Connection refused` / timeouts | Check your network and that the endpoint in `pc cf get endpoint` is reachable. |
| Upload fails for large files | Files up to your plan's storage limit are supported. Check remaining space with `pc st`. |
| `File not found` on a path you can see in the web UI | Your CLI working directory may differ. Run `pc wd` to check and `pc cd /` to reset. |
| Destructive command anxiety | Use `-d` (dry run) on `rm` and `mv` to preview changes before committing. |

## License

Copyright (c) PigTech. All rights reserved.
See [LICENSE](LICENSE) for details.
