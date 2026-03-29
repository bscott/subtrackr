# Release $ARGUMENTS

Execute the SubTrackr release workflow for version $ARGUMENTS.

## Pre-flight

1. Verify you're on the correct branch: `git branch --show-current` should be `$ARGUMENTS`
2. If not, create and checkout: `git checkout -b $ARGUMENTS`
3. Run `gh release list --limit 1` to confirm the previous version

## Track Work

Create beads issues for each work item in this release:
```bash
bd create --title="Description (#GitHub-issue)" --type=feature --priority=2
```

## Build & Test

1. Run `go build ./cmd/server` to verify compilation
2. Run `gofmt -l .` to check formatting — fix any issues with `gofmt -w`
3. Run `go vet ./...` to check for issues
4. Run `go test ./...` to verify all tests pass

## Create Draft Release

```bash
gh release create $ARGUMENTS --draft --title "$ARGUMENTS - Title" --notes "release notes here"
```

Write meaningful release notes covering what's new, bug fixes, and technical changes.

## Commit & Push

1. Stage changed files: `git add <specific files>`
2. Commit with conventional format — NO AI attribution in commit messages:
   ```
   git commit -m "$ARGUMENTS - Release Title

   - Change 1
   - Change 2"
   ```
3. Push: `git push -u origin $ARGUMENTS`

## Create Pull Request

```bash
gh pr create --title "$ARGUMENTS - Title" --body "summary and test plan, Closes #issues"
```

## Comment on Issues

Notify issue reporters:
```bash
gh issue comment <number> --body "Fixed in PR #XX. Description."
```

## After Merge (user tells you to publish)

```bash
gh release edit $ARGUMENTS --draft=false
gh release view $ARGUMENTS
```

The published tag triggers the Docker build workflow automatically.
