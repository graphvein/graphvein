package grammars

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestPinnedAssets(t *testing.T) {
	sums, err := os.ReadFile("SHA256SUMS")
	if err != nil {
		t.Fatal(err)
	}
	for _, language := range Available() {
		data, err := Load(language)
		if err != nil {
			t.Fatal(err)
		}
		expected := fmt.Sprintf("%x  wasitter-%s.wasm", sha256.Sum256(data), language)
		if !strings.Contains(string(sums), expected+"\n") {
			t.Fatalf("release checksum mismatch for %s", language)
		}
	}
}
