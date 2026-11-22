---
title: "bd prime"
description: "Output AI-optimized workflow context"
---

{/* AUTO-GENERATED: do not edit manually */}

Generated from `bd help --doc prime`.

Output essential Beads workflow context in AI-optimized markdown format.

Automatically detects if MCP server is active and adapts output:
- MCP mode: Brief workflow reminders
- CLI mode: The commands day-to-day work needs ('bd --help' lists the rest)

Designed for Claude Code, Gemini CLI, and Codex SessionStart hooks to prevent
agents from forgetting bd workflow after context compaction. The default output
stays under the 10,000 characters Claude Code delivers whole from a hook.

Config options:
- no-git-ops: When true, outputs stealth mode (no git commands in session close protocol).
  Set via: bd config set no-git-ops true
  Useful when you want to control when commits happen manually.
- agent.profile: Explicit policy profile for git/commit authority wording
  (conservative | minimal | team-maintainer; default conservative).
  Set via: bd config set agent.profile team-maintainer
  Or per-session: BD_AGENT_PROFILE=team-maintainer (env var takes precedence).
  See docs/getting-started/ide-setup.md#policy-profiles for what each profile means.
- prime.disabled: When true, bd prime prints nothing, as it does outside a
  workspace (an empty envelope under --hook-json), whatever its other flags.
  For sessions whose host briefs the agent itself but that inherit a
  project's bd prime hook, such as an orchestrator's agent sessions.
  Set per-session: BD_PRIME_DISABLED=true. Or: bd config set prime.disabled true

	Workflow customization:
	- Place a PRIME.md file to override the default workflow text. Checked in this
	  order, once bd resolves a workspace (outside one, prime emits no content):
	  (1) .beads/PRIME.md relative to the current directory (the -C target
	      when -C is set);
	  (2) PRIME.md in the .beads directory bd resolves for this workspace
	      (honors $BEADS_DIR, which -C overrides; a redirected .beads is
	      followed);
	  (3) the global PRIME.md in bd's user config dir:
	      ~/.config/beads/ on Linux ($XDG_CONFIG_HOME/beads/ if set),
	      ~/Library/Application Support/beads/ on macOS,
	      %AppData%\beads\ on Windows.
	- Use --export to dump the default content for customization.

Persistent memories (from bd remember) are not part of the default output. Use
--memories-only to print them, alone, even when a custom PRIME.md is present.

Memory caps, for --memories-only:
	Cap what is printed with --max-memories N and/or --max-memory-chars N (or
	the prime.max-memories / prime.max-memory-chars config keys; an explicit
	flag wins, and an explicit 0 forces unlimited). Caps apply at whole-memory
	boundaries, at least one memory is always emitted, and a banner ahead of
	the entries reports how many were elided and how to browse the rest with
	bd memories. --max-memory-chars caps the total bytes of the memory entries;
	the section header and elision banner are excluded from the budget.

```
bd prime [flags]
```

**Flags:**

```
      --export                 Output default content (ignores PRIME.md override)
      --full                   Force full CLI output (ignore MCP detection)
      --hook-json              Wrap output in the SessionStart hook JSON envelope (Claude Code, Gemini CLI, Codex)
      --max-memories int       With --memories-only, cap printed memories to N entries (0 = unlimited; falls back to the prime.max-memories config key)
      --max-memory-chars int   With --memories-only, cap the total bytes of memory entries, at whole-memory boundaries; section header and banner are not counted (0 = unlimited; falls back to the prime.max-memory-chars config key)
      --mcp                    Force MCP mode (minimal output)
      --memories-only          Output only persistent memories for compact hook contexts
      --no-memories            No effect: the default output no longer includes persistent memories
      --stealth                Stealth mode (no git operations, flush only)
```
