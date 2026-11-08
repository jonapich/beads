#Requires -Version 7
<#
.SYNOPSIS
    Merges the original project into this fork and leaves the result for review.

.DESCRIPTION
    This is the only way changes from the original project enter the fork. It
    merges upstream/main without committing, drops every path listed in
    removed-paths.txt, then runs the upstream-reference guard.

    Nothing is committed or pushed. Read the staged diff first: a sync is where
    a supply chain attack would arrive.

.PARAMETER Ref
    The upstream ref to merge. Defaults to upstream/main.
#>
[CmdletBinding()]
param(
    [string] $Ref = 'upstream/main'
)

$ErrorActionPreference = 'Stop'
Set-Location (git rev-parse --show-toplevel)

if (git status --porcelain) {
    throw 'The working tree is not clean. Commit or stash your changes first.'
}

git fetch --no-tags upstream
if ($LASTEXITCODE) { throw 'git fetch upstream failed.' }

$incoming = git rev-list --count "HEAD..$Ref"
if ($incoming -eq '0') {
    "Already up to date with $Ref."
    return
}

# A conflict exits non-zero, so check that the merge started instead.
git merge --no-commit --no-ff $Ref
git rev-parse -q --verify MERGE_HEAD *> $null
if ($LASTEXITCODE) { throw "The merge of $Ref did not start." }

$patterns = Get-Content scripts/fork/removed-paths.txt | Where-Object { $_ -and $_ -notmatch '^\s*#' }
$pattern = $patterns -join '|'
$dropped = @(git -c core.quotepath=off ls-files | Where-Object { $_ -cmatch $pattern } | Sort-Object -Unique)
if ($dropped.Count) {
    $list = New-TemporaryFile
    try {
        $dropped | Set-Content -LiteralPath $list
        git rm -rfq --pathspec-from-file=$list
        if ($LASTEXITCODE) { throw 'git rm failed while dropping removed paths.' }
    }
    finally {
        Remove-Item -LiteralPath $list
    }
}

docker buildx bake guard
$guardFailed = $LASTEXITCODE -ne 0
$conflicts = @(git diff --name-only --diff-filter=U)

""
"Merged $incoming commit(s) from $Ref and dropped $($dropped.Count) path(s). Nothing is committed."
if ($conflicts.Count) {
    "Resolve these conflicts:"
    $conflicts | ForEach-Object { "  $_" }
}
if ($guardFailed) {
    "The guard failed. Fix or reject the references it listed above."
}
"Next: review with 'git diff --cached', run 'docker buildx bake test', then 'git commit'."
"To abandon the sync: git merge --abort"

if ($conflicts.Count -or $guardFailed) { exit 1 }
