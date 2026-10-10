#!/usr/bin/env bash
# Prints the next version from the Conventional Commits since the last v* tag, or nothing when
# none of them warrant a release. Before 1.0, breaking changes bump the minor version.
set -euo pipefail

prev=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null || true)
IFS=. read -r major minor patch <<<"${prev:-v0.0.0}"
major=${major#v}

bump=0 # 1 patch, 2 minor, 3 major
re='^([a-z]+)(\([^)]+\))?(!)?: '
while IFS= read -r subject; do
	[[ $subject =~ $re ]] || continue
	if [[ -n ${BASH_REMATCH[3]} ]]; then
		bump=3
	elif [[ ${BASH_REMATCH[1]} == feat ]]; then
		((bump < 2)) && bump=2
	elif [[ ${BASH_REMATCH[1]} =~ ^(fix|perf|revert)$ ]]; then
		((bump < 1)) && bump=1
	fi
done < <(git log --no-merges --format=%s "${prev:+$prev..}HEAD")

# Breaking changes also count when only stated in a commit body.
if ((bump < 3)) && git log --no-merges --format=%b "${prev:+$prev..}HEAD" | grep -q '^BREAKING[ -]CHANGE:'; then
	bump=3
fi
((bump == 3 && major == 0)) && bump=2

case $bump in
3) echo "$((major + 1)).0.0" ;;
2) echo "$major.$((minor + 1)).0" ;;
1) echo "$major.$minor.$((patch + 1))" ;;
esac
