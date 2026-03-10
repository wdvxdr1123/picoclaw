# Tools Configuration

PicoClaw's tools configuration is located in the `tools` field of `config.json`.

## Directory Structure

```json
{
  "tools": {
    "web": { ... },
    "mcp": { ... },
    "exec": { ... },
    "cron": { ... },
    "skills": { ... }
  }
}
```

## Web Tools

Web tools are used for web search and fetching.

### Search Provider Selection

| Config            | Type   | Default | Description                                       |
| ----------------- | ------ | ------- | ------------------------------------------------- |
| `search_provider` | string | `auto`  | Search backend for `web_search`: `auto`, `openai` |

When `search_provider` is `auto`, PicoClaw enables `web_search` only when `openai_search` is fully configured.

### OpenAI-Compatible Search

This backend is for providers that expose search through an OpenAI-compatible `POST /chat/completions` endpoint, such as:

```bash
curl --location --request POST 'http://jeniya.cn/v1/chat/completions'
```

| Config             | Type   | Default | Description |
| ------------------ | ------ | ------- | ----------- |
| `enabled`          | bool   | false   | Enable OpenAI-compatible search backend |
| `api_key`          | string | -       | Bearer token for the endpoint |
| `base_url`         | string | -       | Full `chat/completions` URL, or an API base URL ending in `/v1` |
| `model`            | string | -       | Search model name, for example `grok-4-fast` |

PicoClaw currently sends fixed defaults for this backend:

- `max_tokens: 8192`
- `temperature: 1.0`
- timeout: `30s`
- built-in system prompt

Example:

```json
{
  "tools": {
    "web": {
      "search_provider": "openai",
      "openai_search": {
        "enabled": true,
        "api_key": "xxxx",
        "base_url": "http://jeniya.cn/v1/chat/completions",
        "model": "grok-4-fast"
      }
    }
  }
}
```

`web_search` uses only the configured `openai_search` backend. `web_fetch` now always performs a direct HTTP fetch plus local extraction.

## Exec Tool

The exec tool is used to execute shell commands.

| Config                 | Type  | Default | Description                                |
| ---------------------- | ----- | ------- | ------------------------------------------ |
| `enable_deny_patterns` | bool  | true    | Enable default dangerous command blocking  |
| `custom_deny_patterns` | array | []      | Custom deny patterns (regular expressions) |

### Functionality

- **`enable_deny_patterns`**: Set to `false` to completely disable the default dangerous command blocking patterns
- **`custom_deny_patterns`**: Add custom deny regex patterns; commands matching these will be blocked

### Default Blocked Command Patterns

By default, PicoClaw blocks the following dangerous commands:

- Delete commands: `rm -rf`, `del /f/q`, `rmdir /s`
- Disk operations: `format`, `mkfs`, `diskpart`, `dd if=`, writing to `/dev/sd*`
- System operations: `shutdown`, `reboot`, `poweroff`
- Command substitution: `$()`, `${}`, backticks
- Pipe to shell: `| sh`, `| bash`
- Privilege escalation: `sudo`, `chmod`, `chown`
- Process control: `pkill`, `killall`, `kill -9`
- Remote operations: `curl | sh`, `wget | sh`, `ssh`
- Package management: `apt`, `yum`, `dnf`, `npm install -g`, `pip install --user`
- Containers: `docker run`, `docker exec`
- Git: `git push`, `git force`
- Other: `eval`, `source *.sh`

### Configuration Example

```json
{
  "tools": {
    "exec": {
      "enable_deny_patterns": true,
      "custom_deny_patterns": ["\\brm\\s+-r\\b", "\\bkillall\\s+python"]
    }
  }
}
```

## Cron Tool

The cron tool is used for scheduling periodic tasks.

| Config                 | Type | Default | Description                                    |
| ---------------------- | ---- | ------- | ---------------------------------------------- |
| `exec_timeout_minutes` | int  | 5       | Execution timeout in minutes, 0 means no limit |

## MCP Tool

The MCP tool enables integration with external Model Context Protocol servers.

### Global Config

| Config    | Type   | Default | Description                         |
| --------- | ------ | ------- | ----------------------------------- |
| `enabled` | bool   | false   | Enable MCP integration globally     |
| `servers` | object | `{}`    | Map of server name to server config |

### Per-Server Config

| Config     | Type   | Required | Description                                |
| ---------- | ------ | -------- | ------------------------------------------ |
| `enabled`  | bool   | yes      | Enable this MCP server                     |
| `type`     | string | no       | Transport type: `stdio`, `sse`, `http`     |
| `command`  | string | stdio    | Executable command for stdio transport     |
| `args`     | array  | no       | Command arguments for stdio transport      |
| `env`      | object | no       | Environment variables for stdio process    |
| `env_file` | string | no       | Path to environment file for stdio process |
| `url`      | string | sse/http | Endpoint URL for `sse`/`http` transport    |
| `headers`  | object | no       | HTTP headers for `sse`/`http` transport    |

### Transport Behavior

- If `type` is omitted, transport is auto-detected:
  - `url` is set → `sse`
  - `command` is set → `stdio`
- `http` and `sse` both use `url` + optional `headers`.
- `env` and `env_file` are only applied to `stdio` servers.

### Configuration Examples

#### 1) Stdio MCP server

```json
{
  "tools": {
    "mcp": {
      "enabled": true,
      "servers": {
        "filesystem": {
          "enabled": true,
          "command": "npx",
          "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
        }
      }
    }
  }
}
```

#### 2) Remote SSE/HTTP MCP server

```json
{
  "tools": {
    "mcp": {
      "enabled": true,
      "servers": {
        "remote-mcp": {
          "enabled": true,
          "type": "sse",
          "url": "https://example.com/mcp",
          "headers": {
            "Authorization": "Bearer YOUR_TOKEN"
          }
        }
      }
    }
  }
}
```

## Skills Tool

The skills tool configures skill discovery and installation via registries like ClawHub.

### Registries

| Config                             | Type   | Default              | Description             |
| ---------------------------------- | ------ | -------------------- | ----------------------- |
| `registries.clawhub.enabled`       | bool   | true                 | Enable ClawHub registry |
| `registries.clawhub.base_url`      | string | `https://clawhub.ai` | ClawHub base URL        |
| `registries.clawhub.auth_token`    | string | `""`                 | Optional Bearer token for higher rate limits |
| `registries.clawhub.search_path`   | string | `/api/v1/search`     | Search API path         |
| `registries.clawhub.skills_path`   | string | `/api/v1/skills`     | Skills API path         |
| `registries.clawhub.download_path` | string | `/api/v1/download`   | Download API path       |

### Configuration Example

```json
{
  "tools": {
    "skills": {
      "registries": {
        "clawhub": {
          "enabled": true,
          "base_url": "https://clawhub.ai",
          "auth_token": "",
          "search_path": "/api/v1/search",
          "skills_path": "/api/v1/skills",
          "download_path": "/api/v1/download"
        }
      }
    }
  }
}
```

## Environment Variables

All configuration options can be overridden via environment variables with the format `PICOCLAW_TOOLS_<SECTION>_<KEY>`:

For example:

- `PICOCLAW_TOOLS_WEB_SEARCH_PROVIDER=openai`
- `PICOCLAW_TOOLS_WEB_OPENAI_SEARCH_ENABLED=true`
- `PICOCLAW_TOOLS_EXEC_ENABLE_DENY_PATTERNS=false`
- `PICOCLAW_TOOLS_CRON_EXEC_TIMEOUT_MINUTES=10`
- `PICOCLAW_TOOLS_MCP_ENABLED=true`

Note: Nested map-style config (for example `tools.mcp.servers.<name>.*`) is configured in `config.json` rather than environment variables.
