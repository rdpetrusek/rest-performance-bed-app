@AGENTS.md

## Claude Code

<!-- Claude Code reads CLAUDE.md, not AGENTS.md: the import above is how it gets the
shared rules. Keep shared instructions in AGENTS.md and only Claude-specific ones here. -->

- The Angular workspace has its own `ui/.claude/CLAUDE.md` (Angular best practices); it
  loads when working on files under `ui/`.
- This is physical hardware the user owns and depends on. Before sending anything to
  `PUT /api/firmware` or otherwise writing to the bed's flash, stop and get explicit,
  session-specific authorization - a general "go ahead" on other control endpoints does
  not cover firmware writes.
