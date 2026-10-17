# maitake

## Commits and versioning

Releases are cut automatically from Conventional Commit subjects (`.github/scripts/next-version.sh`), so the commit type decides the version bump:

- `fix:`, `perf:`, `revert:` → patch (`0.7.0` → `0.7.1`)
- `feat:` → minor (`0.7.0` → `0.8.0`)
- `docs:`, `ci:`, `chore:`, `build:`, `refactor:`, `test:` → no release

The highest bump among all commits since the last tag wins, so one `feat:` in a push makes it a minor release no matter how many fixes ride along.

Mark breaking changes with `!` after the type or scope (`feat!:`, `fix(api)!:`), or a `BREAKING CHANGE:` line in the body. Use it whenever existing users have to act on upgrade: removed or renamed config/env vars, changed API behavior or response shapes, DB changes that can't be rolled back, dropped endpoints or providers. This triggers a major release, except while on `0.x`, where it only bumps the minor version.
