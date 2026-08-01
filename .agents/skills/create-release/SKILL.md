---
name: create-release
description: Cut a versioned release of this Go project on GitHub — bump the CHANGELOG, tag, push (GitHub Actions builds and publishes the release), then enrich release notes with the GitHub CLI (gh). Use when the user asks to "create a release", "release vX", "fechar a release", or "publicar a versão".
license: MIT
compatibility: Requires git + gh (GitHub CLI, authenticated). Release artifacts are built by GitHub Actions (.github/workflows/release.yml).
---

# Releasing go-imapsync (GitHub + gh)

This project is hosted on **GitHub** (`github.com/jniltinho/go-imapsync`).
Use the **GitHub CLI (`gh`)**, not the Gitea API.

A release is: update `CHANGELOG.md`, commit, tag, push, **wait for Actions**, then
**enrich the release notes with `gh`**. The workflow
`.github/workflows/release.yml` builds the static binary and creates the GitHub
Release with the tarball on tag push (`v*`), usually with thin auto notes —
step 8 is required every time.

> ⚠️ Version comes from the **git tag** (`Makefile`: `git describe --tags` →
> ldflags `-X go-imapsync/cmd.version=…`). There is **no version constant in code**.

---

## Project facts

```bash
git remote get-url origin
# git@github.com:jniltinho/go-imapsync.git  (or https://github.com/jniltinho/go-imapsync.git)

OWNER=jniltinho
REPO=go-imapsync
LAST=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
echo "Last tag: ${LAST:-none}"
```

### Versioning

Series `0.x` (pre-1.0), tags always prefixed with `v`:

- **Feature** (new user-visible capability) → bump **minor**: `v0.1.0` → `v0.2.0`
- **Fix / docs / infra only** → bump **patch**: `v0.1.0` → `v0.1.1`
- **First release** → `v0.1.0`

```bash
if [ -z "$LAST" ]; then
  NEXT=v0.1.0
else
  IFS=. read -r MAJ MIN PAT <<< "${LAST#v}"
  # minor for features:
  NEXT="v${MAJ}.$((MIN+1)).0"
  # patch for fixes only:
  # NEXT="v${MAJ}.${MIN}.$((PAT+1))"
fi
echo "Next: $NEXT"
```

### Auth

```bash
gh auth status          # must show logged-in with repo scope
# Prefer SSH remotes; gh uses the logged-in account for API.
```

Never hardcode tokens in files or commits.

---

## Process

### 1. Prerequisites

```bash
git fetch origin
git checkout main
git pull --ff-only
git status        # clean except the release changes you will stage
make test && make build
```

### 2. Review changes since the last tag

```bash
if [ -n "$LAST" ]; then
  git log "$LAST"..HEAD --oneline
else
  git log --oneline
fi
```

### 3. Update `CHANGELOG.md`

Repo-root `CHANGELOG.md`, **Keep a Changelog**, **English**. Insert a new section
above the previous version; keep a top `## [Unreleased]` placeholder.

```markdown
## [Unreleased]

—

## [0.2.0] — YYYY-MM-DD

**One- or two-line summary.**

### Added
- ...
### Changed
- ...
### Fixed
- ...
```

Omit empty sections.

### 4. Commit + annotated tag

```bash
# Stage release docs only — never secrets, base/, docs/prints/, or agent skill noise unless intended
git add CHANGELOG.md README.md
# plus any intentional product commits already on main

git commit -m "chore(release): $NEXT — <short summary>"

git tag -a "$NEXT" -m "Release $NEXT — <short summary>"
```

### 5. Push main + tag (triggers GitHub Actions)

```bash
git push origin main
git push origin "$NEXT"
```

Tag push runs `.github/workflows/release.yml`: `make test`, `make release-cross`,
upload archives for **linux/amd64**, **darwin/arm64**, **windows/amd64**.

### 6. Wait for the release workflow

```bash
gh run list -R "$OWNER/$REPO" --workflow=release.yml --limit 5
# Watch the run for this tag:
gh run watch -R "$OWNER/$REPO"  # or pick run id from list

# Or poll until the release exists:
for i in $(seq 1 30); do
  if gh release view "$NEXT" -R "$OWNER/$REPO" >/dev/null 2>&1; then
    echo "release $NEXT is up"; break
  fi
  echo "[$i] waiting for $NEXT…"; sleep 15
done
```

### 7. Verify assets

```bash
gh release view "$NEXT" -R "$OWNER/$REPO"
gh release view "$NEXT" -R "$OWNER/$REPO" --json assets,url,tagName
```

Expect assets (version without leading `v` in the filename):

- `go-imapsync_<ver>_linux_amd64.tar.gz`
- `go-imapsync_<ver>_darwin_arm64.tar.gz`
- `go-imapsync_<ver>_windows_amd64.zip`

### 8. Enrich release notes with gh — REQUIRED every time

Do **not** leave auto-generated notes. Match the style used on
[go-postfixadmin releases](https://github.com/jniltinho/go-postfixadmin/releases):

- Title: just the tag (`v0.2.0`), not a long marketing string
- Body starts with `# Release vX.Y.Z`
- Sections with emoji headers (omit empty ones):
  - `## ✨ New Features` — `feat(...): …` bullets
  - `## 🔧 Improvements` — `fix` / `fix` / `chore` bullets
  - `## 🐛 Fixes` — when applicable
  - `## 🧹 Cleanup` — when applicable
  - `## 🧪 Tests` — when applicable
  - `## 📚 Documentation` — `docs(...): …`
  - `## 📦 Package` — asset name(s)
- End with **Full Changelog** compare link (same pattern as go-postfixadmin)

Derive bullets from `git log` since `$LAST` (conventional-commit style when possible)
and from the CHANGELOG entry for `$NEXT`.

```bash
# Compare link: previous tag → this tag (first release: use /commits/TAG)
if [ -n "$LAST" ] && [ "$LAST" != "$NEXT" ]; then
  FULL_CL="https://github.com/$OWNER/$REPO/compare/${LAST}...${NEXT}"
else
  FULL_CL="https://github.com/$OWNER/$REPO/commits/${NEXT}"
fi

# Inspect commits for this release (fill sections from these + CHANGELOG):
git log ${LAST:+$LAST..}HEAD --oneline

cat > /tmp/notes.md <<EOF
# Release $NEXT

## ✨ New Features

- feat(scope): short description

## 🔧 Improvements

- refactor(scope): short description
- chore(ci): short description

## 🐛 Fixes

- fix(scope): short description

## 📚 Documentation

- docs: short description

## 📦 Package

- linux/amd64: \`go-imapsync_${NEXT#v}_linux_amd64.tar.gz\`
- darwin/arm64: \`go-imapsync_${NEXT#v}_darwin_arm64.tar.gz\`
- windows/amd64: \`go-imapsync_${NEXT#v}_windows_amd64.zip\`

**Full Changelog**: $FULL_CL
EOF

# Drop empty sections before editing (keep the file tidy).

gh release edit "$NEXT" -R "$OWNER/$REPO" \
  --title "$NEXT" \
  --notes-file /tmp/notes.md

gh release view "$NEXT" -R "$OWNER/$REPO" --web   # optional
```

If the workflow has not created the release yet (or failed), you can create it once
the binary is built locally (fallback only):

```bash
make release-cross VERSION="$NEXT"
VERSION_NUM=${NEXT#v}
gh release create "$NEXT" \
  "dist/go-imapsync_${VERSION_NUM}_linux_amd64.tar.gz" \
  "dist/go-imapsync_${VERSION_NUM}_darwin_arm64.tar.gz" \
  "dist/go-imapsync_${VERSION_NUM}_windows_amd64.zip" \
  -R "$OWNER/$REPO" --title "$NEXT" --notes-file /tmp/notes.md
```

Prefer the Actions path when CI is green.

### 9. (Optional) OpenSpec

If the release closes an OpenSpec change, archive it with the project’s OpenSpec
workflow, then commit + push that separately.

---

## Quick reference

```bash
OWNER=jniltinho REPO=go-imapsync
LAST=$(git describe --tags --abbrev=0 2>/dev/null || true)
NEXT=v0.1.0   # or compute minor/patch from $LAST
make test && make build
# edit CHANGELOG.md
git add CHANGELOG.md && git commit -m "chore(release): $NEXT"
git tag -a "$NEXT" -m "Release $NEXT"
git push origin main && git push origin "$NEXT"
gh run watch -R "$OWNER/$REPO"
gh release edit "$NEXT" -R "$OWNER/$REPO" --title "..." --notes-file /tmp/notes.md
```

---

## CI workflow (`.github/workflows/release.yml`)

| Item | Status |
|------|--------|
| Trigger | push tags `v*` |
| `make test` | ✅ |
| `make release-cross` (linux amd64, darwin arm64, windows amd64) | ✅ |
| Assets `.tar.gz` / `.zip` for the three platforms | ✅ |
| Release created on tag | ✅ (`softprops/action-gh-release`) |
| Notes | ⚠️ thin until step 8 (`gh release edit`) |

## Guardrails

- **Never** treat “tag pushed” as done — enrich notes with `gh` every release.
- **Never** push a tag before `make test` / `make build` are green.
- **Never** stage secrets, `base/`, `docs/prints/`, `.env`, or log files.
- **Never** hardcode tokens; rely on `gh auth`.
- The tag is the version — no code constant to bump.
- Prefer a new patch/minor over rewriting a published tag.
