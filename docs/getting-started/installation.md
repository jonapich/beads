---
title: Installation
description: Build the bd CLI from this fork with Docker and add the Claude Code plugin
---

A tool installed from someone else's release feed runs whatever they publish next. This fork avoids that: you build `bd` from a checkout you have reviewed, inside a container, and nothing else gets installed on your machine.

## Components Overview

| Component | What It Is | When You Need It |
|---|---|---|
| **bd CLI** | The command-line tool | Always |
| **Claude Code plugin** | Slash commands and session hooks for Claude Code | Optional, if you use Claude Code |

## Build bd

You need Git and Docker with Buildx. Run this from PowerShell:

```powershell
git clone https://github.com/jonapich/beads
cd beads
$env:BD_COMMIT = git rev-parse HEAD
$env:BD_BRANCH = git branch --show-current
docker buildx bake
```

The build writes `dist\windows-amd64\bd.exe`. Copy it onto your `PATH`:

```powershell
Copy-Item dist\windows-amd64\bd.exe $HOME\.local\bin\
```

`BD_COMMIT` and `BD_BRANCH` record the source revision, so `bd version` reports exactly what you are running. Every build input is pinned in the `Dockerfile`: the base image by digest, Go modules by `go.sum`, and the Dolt CLI by checksum.

## Verifying Installation

```bash
bd version
bd help
```

## IDE and Editor Integrations

The recommended approach for Claude Code, Cursor, Windsurf, and other editors with shell access is the CLI plus hooks:

```bash
cd your-project
bd init --quiet

# Choose one
bd setup claude   # Claude Code - installs SessionStart hooks
bd setup copilot  # GitHub Copilot CLI - creates .copilot-plugin/plugin.json + .github/copilot-instructions.md
bd setup cursor   # Cursor IDE - creates .cursor/rules/beads.mdc
bd setup aider    # Aider - creates .aider.conf.yml
bd setup codex    # Codex CLI - installs Beads skill, AGENTS.md guidance, and native hooks
bd setup factory  # Factory.ai Droid - creates/updates AGENTS.md
bd setup mux      # Mux - creates/updates AGENTS.md
```

Every recipe supports a check flag, such as `bd setup claude --check`. See [IDE Setup](/getting-started/ide-setup) for details.

### Claude Code Plugin

The plugin adds slash commands such as `/beads:ready`, `/beads:create`, and `/beads:close`:

```bash
# In Claude Code
/plugin marketplace add jonapich/beads
/plugin install beads
# Restart Claude Code
```

See [Claude Code Plugin](/integrations/claude-code-plugin) for complete plugin documentation.

## Updating bd

Pull the changes you have reviewed and rebuild:

```powershell
git pull
$env:BD_COMMIT = git rev-parse HEAD
$env:BD_BRANCH = git branch --show-current
docker buildx bake
Copy-Item dist\windows-amd64\bd.exe $HOME\.local\bin\
```

A new build can require a one-way schema migration before it opens an existing database. Read [Upgrading](/getting-started/upgrading) first.

## Uninstalling

To completely remove Beads from a repository, see [Uninstalling](/recovery/uninstalling).

## Next Steps

1. **Initialize a project**: `cd your-project && bd init`
2. **Learn the basics**: See [Quick Start](/getting-started/quickstart)
3. **Configure your agent**: See [IDE Setup](/getting-started/ide-setup), or run `bd setup --list`
4. **Explore examples**: Check out the [examples/](https://github.com/jonapich/beads/tree/main/examples) directory
