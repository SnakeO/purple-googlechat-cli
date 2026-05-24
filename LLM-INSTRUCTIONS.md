# gchat CLI -- LLM Instructions

You are helping a user interact with their personal Google Chat (@gmail.com) from the command line using the `gchat` CLI tool. This document contains everything you need to assist them.

## What gchat does

gchat is a command-line tool that connects to personal Google Chat accounts using Google's internal (reverse-engineered) Dynamite protocol. It does NOT use the official Google Chat API (which requires a paid Workspace account). Instead, it authenticates using browser cookies and speaks the same protobuf protocol the chat.google.com web client uses.

## Available commands

```
gchat auth login --browser              # Extract cookies from Chrome (macOS)
gchat auth login --browser --profile "Profile 3"  # Specify Chrome profile
gchat auth login --cookies              # Manually paste cookies
gchat auth status                       # Show auth method
gchat auth logout                       # Clear credentials

gchat whoami                            # Show authenticated user's Gaia ID
gchat conversations                     # List all conversations (DMs + Spaces)
gchat conversations -n 10               # Limit to 10
gchat convos                            # Alias
gchat ls                                # Alias

gchat messages dm:<id>                  # Read all messages from a DM
gchat messages space:<id>               # Read messages from a Space
gchat messages dm:<id> -n 20            # Last 20 messages
gchat messages dm:<id> --since 24h      # Messages from last 24 hours

gchat send dm:<id> "message text"       # Send a message to a DM
gchat send space:<id> "message text"    # Send a message to a Space

gchat dms                               # List all DM contacts with names
gchat dms -n 5                          # First 5 contacts

gchat recent                            # Recent messages across all conversations
gchat recent --since 1h -n 10           # Last hour, max 10

gchat watch                             # Stream real-time incoming messages

# --- Cache, Search, and Bulk Loading ---

gchat load 168h                         # Load + cache last 7 days (all entities)
gchat load 720h                         # Load last 30 days
gchat load 2024-01-15                   # Load since specific date
gchat load 2024-01-15T00:00:00Z         # Load since RFC3339 datetime
gchat load --sync                       # Incremental: load since last import

gchat search "deadline tomorrow"        # Semantic vector search (uses embeddings)
gchat search "hello" --keyword          # FTS5 keyword search (exact text match)
gchat search "budget" -n 10             # Limit results
gchat search "update" --since 720h      # Only search last 30 days
gchat search "query" --since 2025-01-01 # Since specific date
gchat search "query" --json             # JSON output

gchat mentions                          # Messages where you were @mentioned
gchat mentions -n 10                    # Limit results
gchat mentions --since 720h             # Only last 30 days

gchat cache stats                       # Show row counts, DB size, model status
gchat cache clear                       # Wipe cached data (keeps model)
gchat cache clear --models              # Also delete downloaded embedding model

# --- Attachments ---

gchat messages dm:<id> -n 10            # Shows 📎 icon with filename and token
gchat download <token>                  # Download attachment to current directory
gchat download <token> -o ~/Downloads   # Download to specific directory

# All commands support --json for structured output
gchat conversations --json
gchat messages dm:<id> --json | jq '.'
gchat search "query" --json | jq '.'
```

## Conversation ID format

- DMs use `dm:<id>` (e.g., `dm:k8pXsYAAAAE`)
- Spaces use `space:<id>` (e.g., `space:AAQAzTFgX1Q`)
- Get IDs from `gchat conversations`
- Bare IDs without prefix are treated as Space IDs

## Authentication

gchat requires browser cookies from a logged-in Google Chat session. The `--browser` flag reads cookies directly from Chrome's encrypted cookie database on macOS.

### How --browser works (macOS + Chrome)

1. Finds Chrome profiles that have `chat.google.com` cookies
2. Opens the SQLite cookie database at `~/Library/Application Support/Google/Chrome/<Profile>/Cookies`
3. Retrieves the encryption key from macOS Keychain: `security find-generic-password -w -s "Chrome Safe Storage" -a "Chrome"`
4. Derives an AES-128-CBC key using PBKDF2 (salt: "saltysalt", iterations: 1003)
5. Decrypts v10-prefixed cookie values (16-byte IV of spaces, strips 32-byte header from decrypted output)
6. Bootstraps an XSRF token by fetching `chat.google.com/mole/world` and extracting `SMqcke` from `window.WIZ_global_data`
7. Stores everything in `~/.config/gchat/credentials.json` (0600 permissions)

### If --browser doesn't work

Common issues:
- **"No Chrome profiles found"**: User isn't logged into chat.google.com in Chrome
- **"XSRF bootstrap failed"**: Cookies are stale. User needs to visit chat.google.com in Chrome first, then retry
- **Permission denied on Keychain**: User needs to allow Terminal/the app to access Chrome Safe Storage in System Preferences > Privacy & Security

### Manual cookie extraction (--cookies)

If `--browser` fails, users can paste cookies manually:

1. Open `chat.google.com` in any browser
2. Open DevTools (F12) > Application > Cookies > chat.google.com
3. Copy these cookie values:
   - `COMPASS` (from `chat.google.com`, path `/`)
   - `SSID` (from `.google.com`)
   - `SID` (from `.google.com`)
   - `OSID` (from `chat.google.com`)
   - `HSID` (from `.google.com`)
4. Run `gchat auth login --cookies` and paste them as `NAME=VALUE` lines

### Extracting cookies from other browsers and OSes

#### Chrome on Linux

Cookie database: `~/.config/google-chrome/<Profile>/Cookies` (SQLite)

Encryption key retrieval depends on the desktop environment:
- **GNOME/Keyring**: `secret-tool search Title 'Chromium Safe Storage'`
- **KDE/KWallet**: `kwallet-query kdewallet -r 'Chrome Safe Storage'`
- **Headless/no keyring**: Chrome falls back to the hardcoded key `peanuts`

Key derivation: PBKDF2-SHA1, salt `saltysalt`, **1 iteration** (not 1003 like macOS), 16-byte key. Decryption: AES-128-CBC with 16-byte space IV. Cookie values prefixed with `v11` (GNOME Keyring) or `v10` (other).

#### Chrome on Windows

Cookie database: `%LOCALAPPDATA%\Google\Chrome\User Data\<Profile>\Network\Cookies` (SQLite)

Encryption: Two-layer encryption.
1. Read `os_crypt.encrypted_key` from `%LOCALAPPDATA%\Google\Chrome\User Data\Local State` (JSON file). It's base64-encoded with a `DPAPI` prefix.
2. Strip the 5-byte `DPAPI` prefix, then decrypt with Windows DPAPI (`CryptUnprotectData`). This yields a 32-byte AES key.
3. Cookie values prefixed with `v10` are decrypted with AES-256-GCM. The nonce is bytes 3-15, ciphertext is bytes 15 to end-16, and the auth tag is the last 16 bytes.
4. **v20 cookies** (Chrome 127+, 2024): Use App-Bound Encryption requiring SYSTEM-level DPAPI decryption via a COM elevation service. Significantly harder to extract.

#### Firefox (all platforms)

Cookie database (unencrypted):
- macOS: `~/Library/Application Support/Firefox/Profiles/<profile>/cookies.sqlite`
- Linux: `~/.mozilla/firefox/<profile>/cookies.sqlite`
- Windows: `%APPDATA%\Mozilla\Firefox\Profiles\<profile>\cookies.sqlite`

Firefox does NOT encrypt cookie values. Direct SQL query:
```sql
SELECT name, value FROM moz_cookies
WHERE host IN ('.google.com', 'chat.google.com')
AND name IN ('SID','HSID','SSID','OSID','COMPASS','SAPISID');
```

Note: Firefox locks the database while running. Either close Firefox or copy the file first.

#### Brave Browser (all platforms)

Same encryption as Chrome but different paths:
- macOS: `~/Library/Application Support/BraveSoftware/Brave-Browser/<Profile>/Cookies`
- Linux: `~/.config/BraveSoftware/Brave-Browser/<Profile>/Cookies`
- Windows: `%LOCALAPPDATA%\BraveSoftware\Brave-Browser\User Data\<Profile>\Cookies`

Keychain entry on macOS: `security find-generic-password -w -s "Brave Safe Storage" -a "Brave"`

Note: Some Brave versions do not encrypt cookie values, making them directly readable from SQLite.

#### Microsoft Edge (all platforms)

Same as Chrome but different paths:
- macOS: `~/Library/Application Support/Microsoft Edge/<Profile>/Cookies`
- Linux: `~/.config/microsoft-edge/<Profile>/Cookies`
- Windows: `%LOCALAPPDATA%\Microsoft\Edge\User Data\<Profile>\Cookies`

Keychain entry on macOS: `security find-generic-password -w -s "Microsoft Edge Safe Storage" -a "Microsoft Edge"`

#### Safari (macOS only)

Cookie file: `~/Library/Cookies/Cookies.binarycookies`

Safari uses Apple's binary cookie format, not SQLite. Parsing requires a dedicated reader. Safari's cookies are not encrypted but the file is in a proprietary binary format. Libraries like Python's `browser_cookie3` or Go's `kooky` can parse it.

Note: Full Disk Access may be required in System Preferences > Privacy & Security > Full Disk Access to read Safari's cookie file.

## Troubleshooting

### "XSRF bootstrap failed"
The cookies are stale or invalid. Solution:
1. Open `chat.google.com` in Chrome and make sure you're logged in
2. Re-run `gchat auth login --browser`

### "API returned status 401"
Same as above -- cookies expired. Re-authenticate with `--browser`.

### "No Chrome profiles found with chat.google.com cookies"
The user hasn't visited chat.google.com in Chrome recently. Have them:
1. Open Chrome
2. Go to chat.google.com and sign in
3. Retry `gchat auth login --browser`

### Multiple Chrome profiles
If the user has multiple Chrome profiles, `--browser` will prompt them to select one. They can also specify directly:
```bash
gchat auth login --browser --profile "Profile 3"
```

### Messages show Gaia IDs instead of names
Sender names come from the message's `creator` field. Some older messages or system messages don't include names. The Gaia ID (a numeric string like `112380969115626336378`) is the user's Google Account ID.

### "no events" or empty message list
- The conversation may have no recent messages. Try `--since` with a longer duration or omit it to fetch all history.
- Some very old conversations have been archived server-side and return no events.

## Local cache and search

gchat caches all fetched data in a local SQLite database at `~/.config/gchat/cache.db`. Every API response automatically upserts into the cache.

### How caching works
- All commands that fetch data (conversations, messages, dms, recent, watch) automatically write results to the cache
- Messages are automatically embedded using nomic-embed-text v1.5 (768-dim vectors)
- The embedding model (~138MB) is downloaded from HuggingFace on first use to `~/.config/gchat/models/`
- Embeddings run locally using Hugot's pure Go backend -- no API keys, no internet needed after model download

### Cached entities
- **conversations**: ID, name, type (DM/Space), last message preview
- **messages**: conversation ID, message ID, sender, text, timestamp, deleted flag
- **users**: Gaia ID, name, email
- **memberships**: which users are in which conversations
- **vec_messages**: 768-dim float32 embeddings for semantic search (sqlite-vec)

### Search types
- **Semantic search** (default): Embeds the query, finds similar messages by vector distance. Good for finding related content even with different wording.
- **Keyword search** (`--keyword`): FTS5 full-text search. Exact text matching. No embedding model needed.

### --since format (used by load, search, messages, recent)
Accepts three formats:
- Go duration: `168h`, `720h`, `8760h`
- Date: `2024-01-15` (midnight UTC)
- RFC3339: `2024-01-15T10:30:00Z`

### Typical workflow for a new user
```bash
gchat auth login --browser                # authenticate
gchat load 720h                           # cache last 30 days (takes ~1 min)
gchat search "project update"             # semantic search
gchat search "exact phrase" --keyword     # keyword search
gchat mentions                            # see who @mentioned you
gchat conversations                       # browse conversations
gchat messages dm:ID -n 20               # read recent messages
gchat download "token..."                 # download an attachment shown with 📎
```

### Subsequent sessions (incremental sync)
```bash
gchat load --sync                         # fetch only new data since last import
gchat search "new topic"                  # search includes fresh data
gchat mentions --since 168h               # recent mentions
```

### Downloading attachments
Messages with file attachments display a 📎 icon with the filename and a download token:
```
📎 document.pdf (token: AOo0EE...)
```
Copy the token and run:
```bash
gchat download "AOo0EE..." -o ~/Downloads
```
The file downloads via Google's FIFE image CDN at original resolution. Requires fresh cookies (auto-refreshed from Chrome if `--browser` profile is saved).

## Protocol details (for advanced LLM use)

gchat uses the Google Chat Dynamite protocol:

- **API base**: `https://chat.google.com/api/`
- **Wire format**: Protobuf (binary) for requests, pblite (JSON array) for responses
- **Auth**: Cookies + `X-Framework-XSRF-Token` header
- **Streaming**: Webchannel long-poll at `/webchannel/events_encoded`

Key API endpoints:
- `/api/get_self_user_status` -- authenticated user identity
- `/api/paginated_world` -- conversation list
- `/api/catch_up_group` -- message history for one conversation
- `/api/catch_up_user` -- recent events across all conversations
- `/api/create_topic` -- send a message
- `/api/list_members` -- conversation members
- `/api/get_members` -- detailed member info (name, email)

Pblite format: protobuf messages encoded as JSON arrays where array index = field number. Google prefixes responses with `)]}'\n` (XSS protection) and wraps pblite data in `[["dfe.method.name", [actual_data]]]`.

## File locations

| File | Path |
|------|------|
| Credentials | `~/.config/gchat/credentials.json` (0600 perms) |
| Cache database | `~/.config/gchat/cache.db` (SQLite + sqlite-vec) |
| Embedding model | `~/.config/gchat/models/nomic-ai_nomic-embed-text-v1.5/` (~138MB) |

None of these should be committed to git or shared.

## Building from source

```bash
git clone https://github.com/SnakeO/purple-googlechat-cli.git
cd purple-googlechat-cli
make build
```

CGO is required for `go-sqlite3`, FTS5, and `sqlite-vec`. The binary is architecture-specific (arm64 or amd64) but can be copied between Macs of the same architecture.
