# Beyond the UI

The same index answers on the command line and to coding agents.

- `graphvein index` builds or updates the index of a repository, and
  `graphvein serve` opens this page at `http://127.0.0.1:7420/`, reachable
  only from your own computer. See [serve](../cli/serve.md) and
  [projects](../cli/project.md).
- `graphvein query`, `graphvein callers` and `graphvein impact` find a
  name, list what calls it and what depends on it, in the terminal. See
  [queries](../cli/queries.md).
- `graphvein mcp` is a read-only MCP server for coding agents: they ask for
  routes, callers, schema and change impact instead of reading the code
  again. It never changes the index or your code. See [mcp](../cli/mcp.md).
