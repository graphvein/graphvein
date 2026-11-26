# Agent-folder scanner fixture

The scanner test copies this fixture into a temporary directory and restores
`_agents/` to `.agents/` and `_cursor/` to `.cursor/`. Both directories must be
ignored at the repository root and under a nested directory. `visible.ts` and
this README remain visible. The committed names are inert and need no force-add.
