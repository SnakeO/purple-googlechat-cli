# gchat

A command-line tool for personal Google Chat with local caching and semantic search. Read messages, send messages, search your history, and stream real-time events from your @gmail.com account -- no Google Workspace required.

gchat uses the reverse-engineered Dynamite protocol (the same internal protocol the Google Chat web client uses) to connect to personal Google Chat accounts that have no official API access.

## How it works

Google Chat has no public API for consumer @gmail.com accounts -- the official Chat API is restricted to paid Google Workspace organizations. gchat bypasses this by speaking the same internal protobuf protocol that your browser uses when you visit chat.google.com.

Authentication works by extracting cookies directly from your Chrome browser's encrypted cookie store. gchat reads the cookie database, decrypts values using the OS keychain, and uses them to authenticate API requests with an XSRF token.

All data fetched from the API is cached locally in SQLite. Messages are automatically embedded using the [nomic-embed-text v1.5](https://huggingface.co/nomic-ai/nomic-embed-text-v1.5) model (downloaded on first use, ~138MB) for semantic vector search via [sqlite-vec](https://github.com/asg017/sqlite-vec).

## Install

### Build from source

Requires Go 1.21+ and protoc (for regenerating proto files only).

```bash
git clone https://github.com/SnakeO/purple-googlechat-cli.git
cd purple-googlechat-cli
make build
```

CGO is required for SQLite, FTS5, and sqlite-vec.

### Download binary

macOS Apple Silicon (M1/M2/M3/M4):

```bash
curl -L https://github.com/SnakeO/purple-googlechat-cli/releases/latest/download/gchat-darwin-arm64 -o gchat
chmod +x gchat
```

## Quick start

```bash
# Authenticate by extracting cookies from Chrome
gchat auth login --browser

# Load and cache the last 30 days of data (downloads embedding model on first run)
gchat load 720h

# Search your messages semantically
gchat search "meeting tomorrow"

# List your conversations
gchat conversations

# Read messages from a conversation
gchat messages dm:k8pXsYAAAAE

# Send a message
gchat send dm:k8pXsYAAAAE "hello from the terminal"

# Stream real-time incoming messages
gchat watch
```

## Authentication

gchat authenticates by reading cookies from your Chrome browser. You must be logged into chat.google.com in Chrome for this to work.

### Browser cookie extraction (recommended)

```bash
# Auto-detect Chrome profile
gchat auth login --browser

# Specify a Chrome profile
gchat auth login --browser --profile "Profile 3"
```

This reads cookies from Chrome's encrypted SQLite database, decrypts them using the Chrome Safe Storage key from macOS Keychain, and bootstraps an XSRF token from Google's mole/world endpoint.

**Requirements:**
- Chrome must be installed
- You must be logged into chat.google.com in Chrome
- macOS (reads Keychain for decryption key)

### Manual cookie paste

If browser extraction doesn't work, you can paste cookies manually:

```bash
gchat auth login --cookies
```

Then paste cookies from Chrome DevTools (Application > Cookies > chat.google.com):
```
COMPASS=dynamite-ui=...
SSID=...
SID=...
OSID=...
HSID=...
```

Press Enter on a blank line when done.

### Check auth status

```bash
gchat auth status
gchat auth logout
```

Credentials are stored in `~/.config/gchat/credentials.json` with 0600 permissions.

## Commands

### Load and cache data

```bash
gchat load 168h                     # Load last 7 days
gchat load 720h                     # Load last 30 days
gchat load 8760h                    # Load last year
gchat load 2024-01-15               # Load since a specific date
gchat load 2024-01-15T00:00:00Z     # Load since RFC3339 datetime
```

Fetches all conversations, messages, and member info since the given time. Caches everything in SQLite and automatically generates vector embeddings for semantic search. Shows progress bars during loading.

### Search

```bash
gchat search "deadline tomorrow"                 # Semantic (vector) search
gchat search "meeting" --keyword                 # FTS5 keyword search
gchat search "project update" -n 10              # Limit results
gchat search "budget" --since 720h               # Only last 30 days
gchat search "hello" --since 2025-01-01          # Since a specific date
gchat search "query" --json                      # JSON output
```

Semantic search uses nomic-embed-text v1.5 embeddings + sqlite-vec for vector similarity. Results are ranked by relevance. Keyword search uses SQLite FTS5 for exact text matching.

### List conversations

```bash
gchat conversations          # all conversations
gchat conversations -n 5     # first 5
gchat convos                 # alias
gchat ls                     # alias
gchat ls --json              # JSON output
```

### Read messages

```bash
gchat messages dm:ID                  # all messages
gchat messages dm:ID -n 10            # last 10 messages
gchat messages dm:ID --since 24h      # last 24 hours
gchat messages space:ID --since 168h  # last week from a Space
gchat messages dm:ID --json           # JSON output
```

Conversation IDs use the format `dm:<id>` for direct messages and `space:<id>` for Spaces. Get IDs from `gchat conversations`.

### Send messages

```bash
gchat send dm:ID "hello world"
gchat send space:ID "message to the space"
```

### List DM contacts

```bash
gchat dms            # all DM contacts with resolved names
gchat dms -n 5       # first 5
gchat dms --json     # JSON output
```

### Recent activity

```bash
gchat recent                  # last 24 hours (default)
gchat recent --since 1h       # last hour
gchat recent --since 168h     # last week
gchat recent -n 20            # limit to 20 messages
```

### Real-time streaming

```bash
gchat watch          # stream incoming messages to stdout
gchat watch --json   # JSON format for piping
```

### Cache management

```bash
gchat cache stats              # show row counts, DB size, model status
gchat cache clear              # wipe cached data (keeps model)
gchat cache clear --models     # also delete downloaded embedding model
```

### Identity

```bash
gchat whoami         # show your Gaia ID
gchat whoami --json  # JSON output
```

## JSON output

All commands support `--json` for structured output, suitable for piping to `jq` or other tools:

```bash
gchat conversations --json | jq '.[].ID'
gchat messages dm:ID --json | jq '.[] | "\(.Sender): \(.Text)"'
gchat search "query" --json | jq '.[] | "\(.SenderName): \(.Text)"'
gchat dms --json | jq '.[] | "\(.name) (\(.dm_id))"'
```

## File locations

| File | Path |
|------|------|
| Credentials | `~/.config/gchat/credentials.json` |
| Cache database | `~/.config/gchat/cache.db` |
| Embedding model | `~/.config/gchat/models/nomic-ai_nomic-embed-text-v1.5/` |

## Architecture

```
cmd/gchat/          CLI entrypoint and commands (cobra)
internal/
  auth/             Cookie extraction, XSRF bootstrap
  transport/        Authenticated HTTP client, content-type routing
  api/              Typed wrappers for /api/* endpoints
  channel/          Webchannel long-poll streaming
  cache/            SQLite cache (conversations, messages, users, memberships)
  embed/            Embedding model download + inference (Hugot + nomic-embed-text)
  proto/            Generated Go protobuf from googlechat.proto
  pblite/           Pblite codec (Google's JSON-array protobuf encoding)
  model/            Normalized app types (Conversation, Message, User)
  config/           Config directory and credential storage
```

The protobuf definitions in `proto/googlechat.proto` are from the [purple-googlechat](https://github.com/EionRobb/purple-googlechat) project, which reverse-engineered Google Chat's internal protocol. The proto file is explicitly licensed for any use.

## Limitations

- **macOS only** for `--browser` auth (reads Chrome Keychain). Manual `--cookies` works on any OS.
- **Chrome only** for automatic cookie extraction. Firefox/Safari support is planned.
- **Cookies expire.** If you get auth errors, re-run `gchat auth login --browser` to refresh.
- **Unofficial protocol.** Google can change the internal API at any time without notice.
- **First run downloads ~138MB** embedding model from HuggingFace.
- **No end-to-end encryption.** Messages are transmitted using Google's standard transport security.

## Development

```bash
make build          # build binary
make test           # run tests
make vet            # run go vet
make proto          # regenerate protobuf (requires protoc + protoc-gen-go)
make all            # vet + test + build
```

Tests cover the pblite codec, auth flows, transport layer, API request construction, model normalization, channel parser, cache CRUD, time parsing, and embedding model checks.

## Credits

- Protocol reverse engineering by [Eion Robb](https://github.com/EionRobb/purple-googlechat) (purple-googlechat Pidgin plugin)
- Embedding model: [nomic-embed-text v1.5](https://huggingface.co/nomic-ai/nomic-embed-text-v1.5) by Nomic AI (Apache 2.0)
- Vector search: [sqlite-vec](https://github.com/asg017/sqlite-vec) by Alex Garcia
- Embedding inference: [Hugot](https://github.com/knights-analytics/hugot) by Knights Analytics

## License

MIT
