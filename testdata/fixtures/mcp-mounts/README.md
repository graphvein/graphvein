# mcp-mounts

A FastAPI router included at two prefixes (`/v1` and `/v2`), so each of
its routes is served at two production paths, and once more by a test
app (`tests/test_mounts.py`, prefix `/testing`), a test-scope path that
a production-scope filter or target never matches. The MCP session golden
(`internal/mcp/testdata/session-fixes.golden.json`) checks that every
served path counts: the `endpoints` path filter, a "METHOD /path" target
of `endpoint` and `callers`, and the rows' `served_paths` (T030).
No inert names: the fixture has no ignore file and no ignored
path.
