# MCP Server Setup Guide

mneme ships a Model Context Protocol server (`mneme-mcp`) exposing 24 tools
and 3 resources over stdio (default) or streamable HTTP. This guide builds
it, wires it into your MCP client (Claude Code, Codex, Cursor, or any
JSON-RPC client), and verifies the connection.

---

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `DATABASE_URL` | `postgres://localhost:5432/mneme?sslmode=disable` | PostgreSQL connection string (see [db-setup.md](db-setup.md)) |
| `MCP_MODE` | `stdio` | Transport: `stdio` or `http` |
| `MCP_PORT` | `8081` | Listen port when `MCP_MODE=http` (serves at `http://localhost:<port>/mcp`) |
| `LOG_LEVEL` | `info` | Log verbosity (`debug` / `info` / `warn` / `error`) |

> **stdio note:** in stdio mode the server speaks JSON-RPC on
> stdin/stdout — the log stream goes to **stderr**-safe JSON on stdout is
> reserved for protocol frames, so clients capture stdout only. Prefer
> `http` mode when you want human-readable logs.

## Building

```bash
git clone https://github.com/phanijapps/mneme.git
cd mneme
go build ./cmd/mneme-mcp    # → ./mneme-mcp
```

## Using with Claude Code

Add mneme as a tool provider in `.mcp.json` (project scope) or
`~/.claude/settings.json` (user scope):

```json
{
  "mcpServers": {
    "mneme": {
      "command": "/absolute/path/to/mneme-mcp",
      "env": {
        "DATABASE_URL": "postgres://postgres:postgres@localhost:5432/mneme?sslmode=disable",
        "MCP_MODE": "stdio",
        "LOG_LEVEL": "info"
      }
    }
  }
}
```

For HTTP transport instead:

```json
{
  "mcpServers": {
    "mneme": {
      "url": "http://localhost:8081/mcp"
    }
  }
}
```

Then restart Claude Code and run `/mcp` — the `mneme` server should appear
with its 24 tools.

## Using with Codex

Add mneme to your Codex MCP config (`~/.codex/config.toml`):

```toml
[mcp.mneme]
command = "/absolute/path/to/mneme-mcp"
env = { DATABASE_URL = "postgres://postgres:postgres@localhost:5432/mneme?sslmode=disable", MCP_MODE = "stdio" }
```

For HTTP mode, point Codex at the endpoint instead:

```toml
[mcp.mneme]
url = "http://localhost:8081/mcp"
```

## Using with Cursor

Create `.cursor/mcp.json` in your project (or add it via
*Settings → MCP → Add Server*):

```json
{
  "mcpServers": {
    "mneme": {
      "command": "/absolute/path/to/mneme-mcp",
      "env": {
        "DATABASE_URL": "postgres://postgres:postgres@localhost:5432/mneme?sslmode=disable",
        "MCP_MODE": "stdio"
      }
    }
  }
}
```

Cursor discovers stdio servers automatically; the mneme tools appear in the
agent tool list after restart.

## Using with any MCP client (generic JSON-RPC)

**stdio** — launch the binary and frame MCP JSON-RPC 2.0 messages over its
stdin/stdout:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"manual","version":"0"}}}' | ./mneme-mcp
```

**HTTP** — run the server in streamable mode and POST JSON-RPC to the
endpoint:

```bash
MCP_MODE=http MCP_PORT=8081 ./mneme-mcp &

curl -s http://localhost:8081/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

## Available tools (24)

| Group | Tools |
|-------|-------|
| **Memory** | `save_memory`, `get_memory`, `list_memories`, `update_memory`, `delete_memory`, `link_memories` |
| **Recall** | `recall`, `recall_async` |
| **Session** | `start_session`, `get_session`, `activate_memory`, `deactivate_memory`, `end_session` |
| **Space** | `create_space`, `list_spaces`, `get_space`, `promote_memory`, `review_proposals`, `approve_proposal`, `reject_proposal`, `sync_space` |
| **Lifecycle** | `consolidate`, `decay`, `memory_stats` |

Tool input schemas are published at runtime — `tools/list` returns each
tool's full JSON schema; they mirror the REST DTOs in
[api-contracts.md](../api-contracts.md).

## Available resources (3)

| URI | Returns |
|-----|---------|
| `memory://spaces/{space_id}` | A shared memory space, its members, and access policy |
| `memory://sessions/{session_id}` | An agent session with context-window usage |
| `memory://stats` | Global memory statistics (counts by type, decay, storage) |

## Testing the connection

1. **stdio smoke test** — a well-formed `initialize` must return the
   server's capabilities:

   ```bash
   echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}' \
     | ./mneme-mcp | head -1
   ```

   You should get a result containing `"serverInfo"` for mneme.

2. **List the tools** — replace `initialize` with a `tools/list` call (after
   sending the required `initialize` handshake) and confirm 24 entries.

3. **Exercise one end-to-end** (database required):

   ```bash
   # tools/call save_memory with a minimal payload
   # then recall it:
   #   tools/call recall {"query": "<the content you just saved>", ...}
   ```

4. **HTTP mode health** — with `MCP_MODE=http`, any MCP client (or the
   `curl` above) reaching `/mcp` and answering `tools/list` proves the
   transport; `memory://stats` proves the database wiring.
