---
title: Fork Status
description: What this custom build's default branch adds over the last upstream beads release, and how the docs track it.
---

This site documents a custom build of beads: the default branch of
[jonapich/beads](https://github.com/jonapich/beads), a fork kept separate to
protect against supply chain attacks. The docs and the
[CLI Reference](/cli-reference/index) describe that branch as it stands, not
an upstream release. `bd version` names the commit you run.

## Base

The fork tracks upstream beads through reviewed merges. The default branch
contains upstream's main as of 2026-11-08, after the v1.3.4 release, plus the
changes below.

## Added in this fork

| Change | Where it is documented |
|---|---|
| Builds and tests run through `docker buildx bake`; nothing reaches upstream infrastructure | [Installation](/getting-started/installation) |
| Embedded Dolt collects garbage automatically, and `bd dolt push` skips a push with nothing to send | [Configuration](/reference/configuration#automatic-garbage-collection) |
| `bd prime`'s default briefing stays under the hook output limit | [IDE setup](/getting-started/ide-setup) |
| `bd show --field` / `--section` / `--context`, stages, typed links, kinded comments, the awaiting-user inbox, and stale-claim listing | [Agent Working Memory](/multi-agent/agent-memory) |
| Post-close sign-off: `bd close --signoff-file`, `bd signoff`, `bd list --signoff` | [Sign-off](/workflows/signoff) |
| Claim leases last 60 minutes by default, configurable with `bd config set lease.ttl 2h`, for claims, `bd heartbeat` and the default `bd reclaim` grace window, including the claims and heartbeats a `bd serve` process executes | [Configuration](/reference/configuration#claim-lease-length) |
| `bd prime` tells agents to sign their comments with `--author`, and `BEADS_COMMENT_AUTHOR` sets that author once per agent, for `bd comments add` and the `bd comment` shorthand (which also takes `--kind`) | [Configuration](/reference/configuration#comment-author) |
| A negative `export.interval` (e.g. `-1`) auto-exports after every change, so viewers such as `bv` see edits at once | [Configuration](/reference/configuration#tool-level-settings-configyaml) |
| Fewer embedded Dolt engine opens per command: the reads behind `bd show --json` (ID lookup and detail), `bd list` (config, routing check and page) and auto-export each share one open, a second `bd` waiting on the lock retries after 50 ms instead of 500 ms, and auto-backup runs after write commands only, not after reads | [Configuration](/reference/configuration#auto-backup) |

## How these docs stay current

Every change to the default branch updates these pages in the same commit,
and regenerates the CLI Reference from the branch's own `bd`
(`docs/cli-docs.pin` is set to `HEAD`).
