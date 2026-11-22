---
title: "bd link"
description: "Link two issues with a dependency"
---

{/* AUTO-GENERATED: do not edit manually */}

Generated from `bd help --doc link`.

Link two issues with a dependency.

Shorthand for 'bd dep add &lt;id1&gt; &lt;id2&gt;'. By default creates a "blocks"
dependency (id2 blocks id1). Use --type to specify a different relationship.

Examples:
  bd link bd-123 bd-456                    # bd-456 blocks bd-123
  bd link bd-123 bd-456 --type related     # bd-123 related to bd-456
  bd link bd-123 bd-456 --type parent-child

```
bd link <id1> <id2> [flags]
bd link [command]
```

**Flags:**

```
  -t, --type string   Dependency type (blocks|tracks|related|parent-child|discovered-from) (default "blocks")
```

## bd link add

Attach a typed link to an issue.

Links are stored in the issue's metadata as link.&lt;kind&gt; arrays. Allowed kinds
come from config link.kinds (default: mock, artifact, capture, commit, doc).
Adding a URL that is already linked under the same kind updates its note.

Examples:
  bd link add bd-123 --kind mock --url https://example.com/mock.png --note "v2, dark theme"
  bd link add bd-123 --kind commit --url abc1234
  bd list --has-link mock

```
bd link add <id> --kind <kind> --url <url> [--note <text>] [flags]
```

**Flags:**

```
      --kind string   Link kind (config link.kinds; default mock, artifact, capture, commit, doc)
      --note string   Short note shown with the link
      --url string    Link target (URL, path, or commit hash)
```

## bd link list

List an issue's typed links

```
bd link list <id> [--kind <kind>] [flags]
```

**Flags:**

```
      --kind string   Only list links of this kind
```

## bd link rm

Remove a typed link from an issue

```
bd link rm <id> --url <url> [--kind <kind>] [flags]
```

**Aliases:** remove

**Flags:**

```
      --kind string   Only remove the link under this kind
      --url string    URL of the link to remove
```
