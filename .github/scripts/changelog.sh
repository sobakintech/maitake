#!/usr/bin/env bash
# Prints release notes for version $1 (released at HEAD) from the Conventional Commits since
# the previous v* tag.
set -euo pipefail

version=$1
repo=${GITHUB_REPOSITORY:-sobakintech/maitake}
prev=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null || true)

declare -A sections=()
order=(breaking feat fix perf revert)
declare -A titles=([breaking]="⚠ Breaking Changes" [feat]=Features [fix]="Bug Fixes" [perf]=Performance [revert]=Reverts)
re='^([a-z]+)(\(([^)]+)\))?(!)?: (.+)$'

while IFS=$'\t' read -r sha subject; do
	[[ $subject =~ $re ]] || continue
	type=${BASH_REMATCH[1]} scope=${BASH_REMATCH[3]} bang=${BASH_REMATCH[4]} text=${BASH_REMATCH[5]}
	line="* ${scope:+**$scope:** }$text ([${sha:0:7}](https://github.com/$repo/commit/$sha))"
	[[ -n $bang ]] && type=breaking
	[[ -n ${titles[$type]:-} ]] || continue
	sections[$type]+="$line"$'\n'
done < <(git log --no-merges --reverse --format='%H%x09%s' "${prev:+$prev..}HEAD")

if [[ -n $prev ]]; then
	echo "## [$version](https://github.com/$repo/compare/$prev...v$version) ($(date -u +%Y-%m-%d))"
else
	echo "## $version ($(date -u +%Y-%m-%d))"
fi
empty=1
for type in "${order[@]}"; do
	[[ -n ${sections[$type]:-} ]] || continue
	printf '\n### %s\n\n%s' "${titles[$type]}" "${sections[$type]}"
	empty=0
done
if ((empty)); then
	printf '\nMaintenance only; no user-facing changes.\n'
fi
