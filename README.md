# PigCloud CLI

A command-line interface for managing your PigCloud storage.

PigCloud provides **end-to-end encryption** — files are encrypted on your device
before upload and decrypted locally after download. The server never sees
plaintext content, file names, or data keys. A familiar, Unix-style CLI lets you
upload, download, share, and organise files straight from your terminal.
Two-letter aliases, an interactive shell with tab-completion, and `--json`
output make common workflows fast and scriptable.

The CLI also serves as a unique **decryption endpoint** — commands like `gr`
(grep) and `df` (diff) can search and compare encrypted file contents
client-side, something the server and web UI cannot do.

## Installation

### Download Binary

Download the appropriate binary for your platform from the [releases page](https://github.com/pigtech-de/pigcloud-cli/releases).

Each release contains two binaries: `pigcloud` (full name) and `pc` (shorthand alias).

#### Linux / macOS

```bash
curl -sSL https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-1.9.0-linux-amd64.tar.gz -o pigcloud.tar.gz
tar -xzf pigcloud.tar.gz
sudo install -m 755 pigcloud pc /usr/local/bin/
```

#### Windows

```powershell
# Download and extract
Invoke-WebRequest -Uri "https://github.com/pigtech-de/pigcloud-cli/releases/latest/download/pigcloud-1.9.0-windows-amd64.zip" -OutFile pigcloud.zip
Expand-Archive pigcloud.zip -DestinationPath "$env:LOCALAPPDATA\pigcloud"

# Add to PATH (current user, persistent)
$path = [Environment]::GetEnvironmentVariable("Path", "User")
if ($path -notlike "*$env:LOCALAPPDATA\pigcloud*") {
    [Environment]::SetEnvironmentVariable("Path", "$path;$env:LOCALAPPDATA\pigcloud", "User")
}
```

Restart your terminal after adding to PATH.

### Build from Source

Requires Go 1.25 or later.

```bash
cd cli
make build
```

## Quick Start

```bash
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

# Search inside encrypted files
pc gr -r "TODO" /Documents

# Diff between file versions
pc df /report.md 3 5

# Start an interactive shell session
pc shell
```

### Authentication

Authentication is API-key based. Run `pc login` and paste the key from
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
| File names | X25519 `crypto_box_seal` | Each name is individually sealed. The server stores and returns opaque blobs; the CLI decrypts client-side. |
| Path resolution | BLAKE2b-256 path tokens | Client computes deterministic tokens for O(1) lookups without revealing directory structure. |
| Key pair | X25519 (Curve25519) | Generated at account setup. Private key encrypted with a password-derived key (Argon2id). |
| File sharing | Re-seal per recipient | Data key is unsealed with sender's private key, then re-sealed with recipient's public key. |

E2EE keys are set up on first login and cached locally. The private key remains
encrypted at rest — it is only decrypted in memory when needed. Use `pc uk` to
unlock keys for a session and `pc lk` to lock them again.

## Paths

Commands that take paths distinguish between **remote** (cloud) and **local** (OS) paths:

- **Remote paths** are Unix-style and absolute (`/Documents/report.pdf`) or relative
  to the current cloud working directory (`report.pdf`). Use `pc wd` to see it,
  `pc cd` to change it.
- **Local paths** are your OS file system paths (`./downloads/`, `C:\Users\...`).
- In `ul` and `dl`, the first argument is always the **source** and the second
  is the **destination**. `ul` takes local then remote; `dl` takes remote then local.
- Use `-` as the local path for `ul` to read from stdin (remote path required).

## Commands

Use `pc` as a shorthand for `pigcloud`. All commands have two-letter aliases.

### Authentication

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `li` | `login` | Authenticate with your API key |  |
| `lk` | `lock` | Lock encryption keys |  |
| `lo` | `logout` | Remove stored credentials |  |
| `uk` | `unlock` | Unlock encryption keys for this session | ` [-t]` |
| `wh` | `whoami` | Show current user info |  |

### Navigation

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `cd` | — | Change working directory |  |
| `fd` | `find` | Find files by name | ` [-a] [-F] [-i] [-n] [-E] [-t]` |
| `ls` | `list` | List files and directories | ` [-a] [-n] [-l] [-o] [-r] [-S] [-t]` |
| `tr` | `tree` | Display directory tree | ` [-a] [-d] [-D]` |
| `wd` | `pwd` | Print working directory |  |

### File Operations

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `cp` | `copy` | Copy a file or directory | ` [-d]` |
| `ct` | `cat` | Display file content | ` [--head] [-n] [-t]` |
| `df` | `diff` | Diff a file between versions |  |
| `dl` | `download` | Download a file or folder from cloud storage | ` [-x] [--overwrite] [--skip-existing] [-z]` |
| `fv` | `favorite` | Manage favorites |  |
| `fv add` |  | Add a path to favorites |  |
| `fv ls` |  | List all favorites |  |
| `fv rm` |  | Remove a path from favorites |  |
| `gr` | `grep` | Search inside encrypted files | ` [-l] [-F] [-i] [-m] [-r] [-E]` |
| `hd` | `hide` | Hide or unhide files and folders |  |
| `hd add` |  | Hide a file or folder |  |
| `hd ls` |  | List all hidden items |  |
| `hd rm` |  | Unhide a file or folder |  |
| `mk` | `mkdir` | Create a new directory | ` [-p]` |
| `mn` | `mount` | Mount cloud storage as a local drive |  |
| `mn clean` |  | Remove rejected (unsyncable) files from mount |  |
| `mn files` |  | Show per-file sync status | ` [--issues] [--tr] [--tree]` |
| `mn mv` |  | Move the sync folder to a different location | ` [-d] [-f]` |
| `mn pin` |  | Pin a file or folder for offline access | ` [--list] [--remove]` |
| `mn start` |  | Start the mount daemon | ` [--cache-size] [--poll-interval] [--read-only] [--virtual]` |
| `mn status` |  | Show mount status and cache statistics |  |
| `mn stop` | `unmount` | Stop the mount daemon and unmount |  |
| `mv` | `move` | Move or rename a file/directory | ` [-d]` |
| `rm` | `remove` | Delete a file or directory | ` [-d] [-f] [-p]` |
| `rs` | `restore` | Restore an item from the recycling bin |  |
| `tb` | `trash` | Manage recycling bin | ` [-S] [-t]` |
| `tb empty` |  | Permanently delete all items in the recycling bin | ` [-f]` |
| `tb ls` |  | List recycling bin contents |  |
| `tc` | `touch` | Create a new text file | ` [-c]` |
| `ul` | `upload` | Upload a file or directory to cloud storage | ` [-f] [-j] [--skip-existing]` |
| `vh` | `versions` | View and manage file version history |  |
| `vh dl` |  | Download a specific version |  |
| `vh prune` |  | Delete all but the last N versions | ` [-k]` |
| `vh rm` |  | Delete a specific version | ` [-f]` |
| `vh rs` |  | Restore a file to a specific version |  |

### Sharing

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `ch` | `chat` | Send and receive E2EE chat messages | ` [--before] [-n]` |
| `ch ls` |  | List conversations |  |
| `ch read` |  | Mark conversation as read |  |
| `ch rm` |  | Delete a sent message |  |
| `ch send` |  | Send a chat message or share a file | ` [-f]` |
| `ch unread` |  | Show unread message counts |  |
| `fr` | `friend` | Manage friends |  |
| `fr accept` |  | Accept a friend request |  |
| `fr add` |  | Send a friend request |  |
| `fr decline` |  | Decline a friend request |  |
| `fr ls` |  | List your friends |  |
| `fr pending` |  | List pending friend requests |  |
| `fr rm` |  | Remove a friend |  |
| `pl` | `link` | Create and manage public links |  |
| `pl add` |  | Create a public link | ` [-e] [--max-downloads] [-P]` |
| `pl ls` |  | List all public links |  |
| `pl rm` |  | Revoke a public link | ` [-f]` |
| `pl set` |  | Update public link settings | ` [-e] [--max-downloads] [-P] [--remove-expiration] [--remove-max-downloads] [--remove-password]` |
| `sr` | `share` | Manage shared files and folders |  |
| `sr accept` |  | Accept a pending share |  |
| `sr add` |  | Share a folder with a user | ` [-f] [-p]` |
| `sr decline` |  | Decline a received share |  |
| `sr inbox` |  | List shares you've received from others |  |
| `sr ls` |  | List share recipients for a folder |  |
| `sr rm` |  | Remove a specific share recipient | ` [-f]` |
| `sr set` |  | Update share settings (permission, password, expiry) | ` [-e] [-P] [-p] [--remove-expiration] [--remove-password] [-u]` |

### Info & Tools

| Command | Alias | Description | Flags |
|---------|-------|-------------|-------|
| `ac` | `activity` | View activity log and notifications | ` [-n] [-m] [-o] [-u]` |
| `cf` | `config` | Manage CLI configuration |  |
| `cf get` |  | Get a configuration value |  |
| `cf ls` | `list` | Show all configuration values |  |
| `cf set` |  | Set a configuration value |  |
| `cm` | `completion` | Generate shell completion scripts |  |
| `du` | `usage` | Show storage breakdown by file type |  |
| `hl` | `help` | Show help for commands | ` [-v]` |
| `in` | `info` | Show file or directory info |  |
| `op` | `open` | Open a file or folder in the browser |  |
| `rc` | `recents` | List recently accessed files | ` [-l]` |
| `sh` | `shell` | Start an interactive shell |  |
| `ss` | `sessions` | Manage active sessions and devices |  |
| `ss devices` |  | List trusted devices |  |
| `ss forget` |  | Remove a trusted device |  |
| `ss ls` |  | List active sessions and devices |  |
| `ss revoke` |  | Revoke an active session |  |
| `ss revoke-all` |  | Revoke all other sessions |  |
| `st` | `stats` | Show storage statistics |  |
| `vr` | `version` | Show version information |  |
| `xp` | `export` | Export all personal data | ` [-o]` |

## Command Details


### `li` (login) — Authenticate with your API key

Authenticate with your PigCloud API key.

You can find your API key in the PigCloud web interface under Settings > API Keys.
The API key will be stored securely in your local configuration file.

You will be prompted to enter your API key securely (input is hidden).


```bash
pc li                             # Authenticate with your API key
```


### `lk` (lock) — Lock encryption keys

Stop the background key agent and clear decrypted keys from memory.

After locking, commands that access encrypted files will prompt for your
encryption password again (or require 'pc uk' to unlock).


```bash
pc lk    # Lock encryption keys
```


### `lo` (logout) — Remove stored credentials

Remove stored API key and configuration from your system.

This will delete your local configuration file and require you to login again.


### `uk` (unlock) — Unlock encryption keys for this session

Unlock your encryption keys so subsequent commands don't prompt for a password.

Starts a background agent that holds your decrypted keys in memory.
The agent automatically expires after the TTL (default: 1 hour).

Use 'pc lk' to lock (stop the agent) manually.


```bash
pc uk              # Unlock for 1 hour (default)
pc uk -t 8h          # Unlock for 8 hours
pc uk -t 30m         # Unlock for 30 minutes
```


### `wh` (whoami) — Show current user info

Display information about the currently authenticated user.


### `cd` — Change working directory

Change the current working directory in your cloud storage.

The working directory is persisted across CLI sessions.
Use '..' to go up one directory, or '/' to go to root.


### `fd` (find) — Find files by name

Search for files and directories matching a pattern.

By default the pattern uses glob matching:
  * matches any characters
  ? matches a single character

Use -E for regex matching or -F for literal substring matching.


```bash
pc fd "*.pdf"                     # Glob: find PDF files
pc fd "report*" /docs              # Glob: search in /docs
pc fd -t d "project"               # Glob: directories only
pc fd -E "report_\d{4}" /docs      # Regex: report_2024 etc.
pc fd -Fi "readme" /               # Fixed: case-insensitive substring
```


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


### `df` (diff) — Diff a file between versions

Show differences between two versions of a file, or between a version
and the current file.

Only works with text files. Downloads and decrypts both versions locally,
then displays a unified diff.


```bash
pc df /report.md 3 5     # Diff version 3 vs version 5
pc df /report.md 3       # Diff version 3 vs current
```


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


### `fv` (favorite) — Manage favorites

Manage your favorites list.

Without arguments, lists all favorites.
With a path argument, toggles the favorite status.


```bash
pc fv                  # List favorites
pc fv /Documents         # Toggle favorite
pc fv add /Documents     # Add to favorites
pc fv rm /Documents      # Remove from favorites
```


### `gr` (grep) — Search inside encrypted files

Search for a pattern inside encrypted file contents.

Downloads each text file, decrypts client-side, and searches for matches.
Only text files are searched (binary files are skipped).

By default the pattern is a regular expression. Use -F for literal
substring matching or -E to be explicit about regex mode.

This is slower than local grep because each file must be downloaded and
decrypted. Use a specific path to narrow the search scope.


```bash
pc gr "TODO" /Documents       # Regex: search in /Documents
pc gr -r "func\s+\w+" /src    # Regex: recursive function search
pc gr -F "fmt.Println" /src   # Fixed: literal substring match
pc gr -l "API_KEY" /configs   # Show matching file names only
pc gr -i "error" /logs        # Case-insensitive search
```


### `hd` (hide) — Hide or unhide files and folders

Manage hidden files and folders.

Without arguments, lists all hidden items.
With a path argument, toggles the hidden status.


```bash
pc hd                  # List hidden items
pc hd /Private           # Toggle hidden
pc hd add /Private       # Hide
pc hd rm /Private        # Unhide
```


### `mk` (mkdir) — Create a new directory

Create a new directory in your cloud storage.

By default, the parent directory must already exist.
Use -p to create parent directories as needed.


### `mn` (mount) — Mount cloud storage as a local drive

Mount your PigCloud storage as a local filesystem.

Run 'pc mn' to check mount status. Use 'pc mn start' to mount.

Sync mode (default): files are downloaded to a local folder and kept in sync
bidirectionally. The folder is mapped as a drive letter (Windows) or symlink
(Linux/macOS). Reads and writes are instant — no network latency.

Virtual mode (--virtual): FUSE/WinFsp network-backed mount where files are
fetched on demand. Lower disk usage but higher latency.

Requires unlocked encryption keys (run 'pc uk' first).


```bash
pc mn                        # Show mount status
pc mn start /Photos P:       # Sync /Photos to P: drive (fast, local files)
pc mn start /Photos P: --virtual  # Virtual mount (network-backed)
pc mn start                  # Sync root at default location
pc mn stop                   # Unmount and stop sync
pc mn files --tree           # Show synced files as a tree
pc mn files --issues         # Show files with sync problems
```


#### `mn mv` — Move the sync folder to a different location

Move the local sync folder to a new directory. The mount daemon will be
stopped during the move and restarted afterward.

Use -f to skip the confirmation prompt.


```bash
pc mn mv -d D:\PigCloud
pc mn mv -d /mnt/data/pigcloud -f
```


### `mv` (move) — Move or rename a file/directory

Move a file or directory to a new location, or rename it.

If target is a directory, the source will be moved into it.
If target doesn't exist, source will be renamed to target.

Flags:
  -d, --dry   Show what would be moved without actually moving


### `rm` (remove) — Delete a file or directory

Delete a file or directory from your cloud storage.

By default, items are moved to the recycling bin and can be restored later.
Items in the recycling bin are automatically deleted after 30 days.

Use --permanent to bypass the recycling bin and delete immediately.

Flags:
  -p, --permanent   Permanently delete, bypassing the recycling bin
  -d, --dry         Show what would be deleted without actually deleting
  -f, --force       Skip confirmation prompt


### `rs` (restore) — Restore an item from the recycling bin

Restore a file or directory from the recycling bin to its original location.

Accepts either a path ('/photo.jpg') or a 32-char hex node ID from 'pc tb ls'.
Paths are resolved against the recycling bin; if multiple deleted items share
the same name, you'll be asked which to restore.


```bash
pc rs /report.pdf                 # Restore by path (prompts on collision)
pc rs abc123def456...           # Restore by node ID from 'pc tb' output
```


### `tb` (trash) — Manage recycling bin

Show all items in the recycling bin.

Use 'rs <node-id>' to restore items or 'tb empty' to empty the bin.


```bash
pc tb                    # List trash contents
pc rs <node-id>          # Restore an item by node ID
pc tb empty              # Empty the bin
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

Use '-' as the local path to read from stdin. Remote path is required
when uploading from stdin.


```bash
pc ul report.pdf /Documents/     # Upload a file
pc ul ./my-folder /Backups/      # Upload a directory recursively
echo "hello" | pc ul - /hello.txt  # Upload from stdin
```


### `vh` (versions) — View and manage file version history

View, restore, or delete file version history.

Subcommands:
  vh <file>                    List versions (default)
  vh rs <file> <version-id>    Restore a specific version
  vh rm <version-id>           Delete a specific version


```bash
pc vh /report.pdf             # List versions
pc vh rs /report.pdf 42       # Restore version #42
pc vh rm 42                   # Delete version #42
```


#### `vh dl` — Download a specific version


```bash
pc vh dl /report.pdf 42 ./        # Download version #42
pc vh dl /report.pdf 42 old.pdf  # Download to specific file
```


#### `vh prune` — Delete all but the last N versions


```bash
pc vh prune /report.pdf --keep 3    # Keep last 3 versions
pc vh prune /report.pdf --keep 0    # Delete all versions
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

Manage public links for files and directories.

With a path argument, shows existing link details (or "no link" if none exists).
Use subcommands to create, update, or revoke links.


```bash
pc pl /report.pdf                         # Show link details
pc pl add /report.pdf                     # Create public link
pc pl add /report.pdf -P secret123        # Create with password
pc pl set /report.pdf --max-downloads 50  # Set download limit
pc pl rm /report.pdf                      # Revoke link
```


### `sr` (share) — Manage shared files and folders

Manage shared files and folders.

Without arguments, shows shares you've received (inbox).
With a path, shows share recipients for that folder.


```bash
pc sr                            # Show shares you've received
pc sr /Shared                    # List recipients for /Shared
pc sr add alice /Shared          # Share with alice
pc sr rm /Shared alice           # Revoke alice's access
pc sr set /Shared -P secret      # Add password
```


#### `sr accept` — Accept a pending share


```bash
pc sr accept /Documents alice     # Accept a share from alice
```


#### `sr add` — Share a folder with a user


```bash
pc sr add alice /Documents    # Share with alice
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
pc du    # Show storage breakdown
```


### `hl` (help) — Show help for commands

Display help information about PigCloud CLI commands.


### `in` (info) — Show file or directory info

Display detailed information about a file or directory.

Shows size, dates, type, sharing status, and recipients for shared folders.


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


### `ss` (sessions) — Manage active sessions and devices

View and manage active login sessions and trusted devices.

Without arguments, lists all sessions and trusted devices.
Use subcommands to revoke sessions or forget devices.


```bash
pc ss                          # List sessions and devices
pc ss revoke <session-id>      # Revoke a session
pc ss revoke-all               # Revoke all other sessions
pc ss devices                  # List trusted devices only
pc ss forget <device-id>       # Remove a trusted device
```


### `st` (stats) — Show storage statistics

Display storage usage statistics for your account.


### `vr` (version) — Show version information

Display the version, commit hash, and build date of the CLI.


### `xp` (export) — Export all personal data

Download all personal data associated with your account as a JSON file.

Includes account info, file metadata, shares, activity log, and more.
File contents are NOT included (encrypted at rest and too large).


```bash
pc xp                    # Export to pigcloud-export-YYYY-MM-DD.json
pc xp -o backup.json     # Export to a specific file
```


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
| `--no-color` | Disable colored output |
| `--config` | Use a custom config file path |

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `Authentication required` | Run `pc login` to set your API key. Keys can be generated in the PigCloud web UI under Settings > API Keys. |
| `Connection refused` / timeouts | Check your network and that the endpoint in `pc cf get endpoint` is reachable. |
| Upload fails for large files | Files up to your plan's storage limit are supported. Check remaining space with `pc st`. |
| `File not found` on a path you can see in the web UI | Your CLI working directory may differ. Run `pc wd` to check and `pc cd /` to reset. |
| Destructive command prompts | Most destructive commands (`rm`, `vh rm`, `pl rm`, `tb empty`) prompt for confirmation. Use `-f` to skip, or `-d` (dry run) to preview. |

## License

Copyright (c) PigTech. All rights reserved.
See [LICENSE](LICENSE) for details.
