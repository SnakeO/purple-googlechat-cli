# gchat

A command-line tool for personal Google Chat. Read messages, send messages, list conversations, and stream real-time events from your @gmail.com account -- no Google Workspace required.

gchat uses the reverse-engineered Dynamite protocol (the same internal protocol the Google Chat web client uses) to connect to personal Google Chat accounts that have no official API access.

## How it works

Google Chat has no public API for consumer @gmail.com accounts -- the official Chat API is restricted to paid Google Workspace organizations. gchat bypasses this by speaking the same internal protobuf protocol that your browser uses when you visit chat.google.com.

Authentication works by extracting cookies directly from your Chrome browser's encrypted cookie store. gchat reads the cookie database, decrypts values using the OS keychain, and uses them to authenticate API requests with an XSRF token.

## Install

### Build from source

Requires Go 1.21+ and protoc (for regenerating proto files only).

```bash
git clone https://github.com/jacobchapa/gchat.git
cd gchat
CGO_ENABLED=1 go build -o bin/gchat ./cmd/gchat
```

CGO is required for SQLite support (reading Chrome's cookie database).

### Homebrew (not yet available)

```bash
# Coming soon
brew install gchat
```

## Quick start

```bash
# Authenticate by extracting cookies from Chrome
gchat auth login --browser

# See who you are
gchat whoami

# List your conversations
gchat conversations

# List all DM contacts with names
gchat dms

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

This reads cookies from Chrome's encrypted SQLite database (`~/Library/Application Support/Google/Chrome/<Profile>/Cookies`), decrypts them using the Chrome Safe Storage key from macOS Keychain, and bootstraps an XSRF token from Google's mole/world endpoint.

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
gchat dms --json | jq '.[] | "\(.name) (\(.dm_id))"'
```

## Architecture

gchat is built in Go with no external runtime dependencies (single static binary once compiled).

```
cmd/gchat/          CLI entrypoint and commands (cobra)
internal/
  auth/             Cookie extraction, XSRF bootstrap, OAuth (unused)
  transport/        Authenticated HTTP client, content-type routing
  api/              Typed wrappers for /api/* endpoints
  channel/          Webchannel long-poll streaming
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
- **No end-to-end encryption.** Messages are transmitted using Google's standard transport security.

## Development

```bash
make build          # build binary
make test           # run tests
make vet            # run go vet
make proto          # regenerate protobuf (requires protoc + protoc-gen-go)
make all            # vet + test + build
```

79 tests across 7 packages covering the pblite codec, auth flows, transport layer, API request construction, model normalization, and channel parser.

## Credits

Protocol reverse engineering by [Eion Robb](https://github.com/EionRobb/purple-googlechat) (purple-googlechat Pidgin plugin).

## License

MIT
