package models

import "testing"

func TestCompareVersions(t *testing.T) {
	newerFirst := [][2]string{
		{"gpt-6.1-sol", "gpt-6-sol"},
		{"gpt-6-sol", "gpt-5.6-sol"},
		{"gpt-6.10-sol", "gpt-6.9-sol"},
		{"claude-opus-5-5", "claude-opus-5"},
		{"claude-opus-5", "claude-opus-4-8"},
		{"claude-opus-4-5-20251101", "claude-opus-4-1-20250805"},
		{"claude-opus-4-8", "claude-opus-4-20250514"},
		{"claude-sonnet-5-20270101", "claude-sonnet-5-20260101"},
	}
	for _, c := range newerFirst {
		if CompareVersions(c[0], c[1]) <= 0 || CompareVersions(c[1], c[0]) >= 0 {
			t.Errorf("%s should be newer than %s", c[0], c[1])
		}
	}
	if CompareVersions("gpt-6-sol", "gpt-6-sol") != 0 {
		t.Error("same model should compare equal")
	}
}
