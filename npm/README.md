# homebutler-mcp

MCP server for homelab management. Manage servers, Docker containers, ports, alerts, and more — from any AI tool.

This is an npm wrapper for [homebutler](https://github.com/Higangssh/homebutler). It downloads the correct binary for your platform automatically.

## Quick Setup

Add to your MCP client config (Claude Code, Cursor, Claude Desktop, ChatGPT Desktop):

```json
{
  "mcpServers": {
    "homebutler": {
      "command": "npx",
      "args": ["-y", "homebutler-mcp"]
    }
  }
}
```

That's it. No manual installation needed.

## Try Without Real Servers

```json
{
  "mcpServers": {
    "homebutler": {
      "command": "npx",
      "args": ["-y", "homebutler-mcp", "--demo"]
    }
  }
}
```

Demo mode returns realistic fake data — perfect for trying it out.

## Available Tools

| Tool | Description |
|---|---|
| `system_status` | CPU, memory, disk, uptime |
| `docker_list` | List containers |
| `docker_restart` | Restart a container |
| `docker_stop` | Stop a container |
| `docker_logs` | Container log output |
| `wake` | Wake-on-LAN magic packet |
| `open_ports` | Open ports with process info |
| `network_scan` | Discover LAN devices |
| `alerts` | Resource threshold alerts |

All tools support an optional `server` parameter for multi-server management via SSH.

## Links

- [GitHub](https://github.com/Higangssh/homebutler)
- [Full documentation](https://github.com/Higangssh/homebutler#mcp-server)
- [Report issues](https://github.com/Higangssh/homebutler/issues)

## License

MIT
