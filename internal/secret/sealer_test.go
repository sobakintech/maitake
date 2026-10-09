package secret

import (
	"path/filepath"
	"testing"
)

func TestSealer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyring")
	s, err := LoadOrCreateSealer(path)
	if err != nil {
		t.Fatal(err)
	}
	sealed := s.Seal("mtk_secret")
	again, err := LoadOrCreateSealer(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := again.Open(sealed); err != nil || got != "mtk_secret" {
		t.Fatalf("round trip: %q %v", got, err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := again.Open(sealed); err == nil {
		t.Fatal("tampered value decrypted")
	}
}
