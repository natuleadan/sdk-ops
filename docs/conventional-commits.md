# Conventional Commits

This project follows [Conventional Commits](https://www.conventionalcommits.org/) for versioning and changelog generation.

## Format

```
<type>(<scope>): <description>
```

**Scope is required.** Pull requests will be rejected if any commit lacks a scope.

### Examples

```
feat(cli): add --crowdsec flag to infra init
fix(ssh): handle reconnection after port change
docs(readme): update installation guide
test(deploy): add unit tests for rollback
refactor(hardening): extract SSH config into builder
chore(ci): update Go version to 1.27
```

## Allowed Types

| Type     | Description               | Version bump |
|----------|---------------------------|--------------|
| `feat`   | New feature               | minor (or patch in 0.x) |
| `fix`    | Bug fix                   | patch        |
| `docs`   | Documentation             | none         |
| `refactor` | Code refactoring         | none         |
| `test`   | Adding or fixing tests    | none         |
| `chore`  | Build, CI, tooling        | none         |
| `ci`     | CI configuration changes  | none         |
| `perf`   | Performance improvement   | patch        |
| `style`  | Formatting, linting       | none         |
| `build`  | Build system changes      | none         |
| `revert` | Revert a previous commit  | none         |

## Breaking Changes

Add `!` after the type or `BREAKING CHANGE:` in the footer to trigger a major version bump:

```
feat(cli)!: change command output format

BREAKING CHANGE: new format drops legacy flags
```

## Release Flow (auto-detect, no manual workflow)

```
1. Push commits to PR on `main`
   → CI validates conventional commits format + scope presence
   → CI runs lint + tests + build

2. Merge squash to `main` with a conventional commit message

3. Push to `main` triggers CI:
   → reads the squash commit message
   → auto-detects version bump (see table below)
   → creates git tag vX.Y.Z
   → GoReleaser builds binaries + publishes GitHub Release
```

No manual "Run workflow" step needed. The CI reads the commit message on push to `main`.

## Version Calculation

| Condition | Example commit | Bump |
|-----------|---------------|------|
| `BREAKING` or `!` in subject | `fix(api)!: change response` | MAJOR |
| `release(major):` | `release(major): rewrite` | MAJOR |
| `release(minor):` | `release(minor): add pagination` | MINOR |
| `release(patch):` | `release(patch): hotfix` | PATCH |
| `feat:` | `feat(cli): add backup` | MINOR |
| `fix:` or `perf:` | `fix(ssh): handle timeout` | PATCH |
| `docs:`, `chore:`, `ci:`, etc. | `docs(readme): update` | SKIP (no release) |

**First release:** when no tags exist, CI forces v0.0.1 regardless of commit type.
