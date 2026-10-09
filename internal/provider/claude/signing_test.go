package claude

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// Vectors from CLIProxyAPI's captures of Claude Code 2.1.220.
const signBase = `{"model":"model-a","messages":[{"role":"user","content":[{"type":"text","text":"x"}]}],"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.220.test; cc_entrypoint=sdk-cli; cch=00000;"},{"type":"text","text":"system-x"}],"tools":[],"metadata":{"user_id":"meta-x"},"max_tokens":1,"thinking":{"type":"adaptive","display":"omitted"},"context_management":{"edits":[{"type":"clear_thinking_20251015","keep":"all"}]},"output_config":{"effort":"high"},"stream":true}`

func TestSignBody(t *testing.T) {
	cases := []struct{ body, want string }{
		{signBase, "7ee87"},
		{strings.Replace(signBase, `"model":"model-a"`, `"model":"model-b"`, 1), "7ee87"},
		{strings.Replace(signBase, `"text":"x"`, `"text":"y"`, 1), "b9cc8"},
		{strings.Replace(signBase, `"stream":true}`, `"stream":true,"extra_top":"extra"}`, 1), "2d622"},
		{strings.Replace(signBase, `"metadata":{"user_id":"meta-x"}`, `"metadata":{"user_id":"meta-x","max_tokens":999,"fallbacks":[{"model":"fallback-model"}]}`, 1), "4589b"},
		{strings.Replace(signBase, `"metadata":{"user_id":"meta-x"}`, `"metadata":{"user_id":"meta-x","max_tokens":999,"model":"nested-model","fallbacks":[{"model":"fallback-model"}]}`, 1), "0601b"},
		{`{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.220.test; cc_entrypoint=sdk-cli; cch=00000;"}]}`, "f2edb"},
	}
	for i, c := range cases {
		signed, err := signBody([]byte(c.body))
		if err != nil {
			t.Fatal(err)
		}
		text := gjson.GetBytes(signed, "system.0.text").String()
		_, after, _ := strings.Cut(text, "cch=")
		if got := after[:5]; got != c.want {
			t.Errorf("case %d: cch = %s, want %s", i, got, c.want)
		}
	}
}

func TestFingerprint(t *testing.T) {
	if got := computeFingerprint("CPA_OFFICIAL_BASEURL_CLI_SYSTEM_EMPTY_b82d4e", "2.1.258"); got != "1f4" {
		t.Fatalf("fingerprint = %s, want 1f4", got)
	}
}
