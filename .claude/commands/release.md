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

## Merge & Pin the Release Commit

After CI passes on the PR:

```bash
gh pr merge <pr-number> --merge --delete-branch
git checkout main
git pull --ff-only
RELEASE_SHA=$(git rev-parse HEAD)
```

## Verify Docker Build on Main

Every merge to main triggers `docker-publish.yml`, which pushes `:main` and `:sha-*` images. Do NOT publish until the build for `$RELEASE_SHA` succeeds.

```bash
RUN_ID=$(gh run list --workflow=docker-publish.yml --commit "$RELEASE_SHA" --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$RUN_ID" --exit-status
```

The workflow cancels in-progress runs on the same ref, so a later merge to main can cancel this build. If the run shows as cancelled rather than failed, re-run it (`gh run rerun "$RUN_ID"`) and watch again before publishing.

## Publish (only when the user tells you to)

```bash
# Publish the draft and create its tag at the verified commit, not moving main
gh release edit $ARGUMENTS --target "$RELEASE_SHA" --draft=false

# Verify the published tag resolves to the verified commit
git fetch --tags origin
test "$(git rev-parse '$ARGUMENTS^{commit}')" = "$RELEASE_SHA"
gh release view $ARGUMENTS
```

The published tag triggers a second Docker build for the same commit, publishing the semver image tag (without the leading `v`) and `:latest`.
