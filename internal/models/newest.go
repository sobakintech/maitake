package models

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// Listed is a model an upstream account says it can use.
type Listed struct {
	ID      string
	Created time.Time // zero when upstream doesn't say
	// Preview models exist upstream but aren't generally offered: hidden or not yet active.
	Preview bool
}

// Served is a listed model and the accounts that reported it.
type Served struct {
	ID       string
	Provider string
	Created  time.Time
	Accounts []int64
	// PreviewAccounts list the model as a preview.
	PreviewAccounts []int64
}

// Inventory is what a pool's usable accounts have reported about their models.
type Inventory struct {
	Models []Served
	// Unreported are accounts with no list yet, by provider; they might serve anything.
	Unreported map[string][]int64
	// CheckedAt is the most recent successful report.
	CheckedAt time.Time
}

// versionOf pulls the release numbers out of an ID: "gpt-6.1-sol" gives [6 1],
// "claude-opus-4-5-20251101" gives [4 5] and its date stamp.
func versionOf(id string) (nums []int, date string) {
	for _, part := range strings.Split(strings.ToLower(id), "-") {
		if len(part) == 8 && isDigits(part) {
			date = part
			continue
		}
		pieces := strings.Split(part, ".")
		if !slices.ContainsFunc(pieces, func(p string) bool { return !isDigits(p) }) {
			for _, p := range pieces {
				n, _ := strconv.Atoi(p)
				nums = append(nums, n)
			}
		}
	}
	return nums, date
}

// CompareVersions orders model IDs by release: the version numbers in the ID, then its date
// stamp. It is positive when a is the later release.
func CompareVersions(a, b string) int {
	an, ad := versionOf(a)
	bn, bd := versionOf(b)
	if c := slices.Compare(an, bn); c != 0 {
		return c
	}
	return strings.Compare(ad, bd)
}
