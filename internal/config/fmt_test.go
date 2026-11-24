package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func fmtAll(c *Config) string { return fmt.Sprintf("%v %+v %#v", c, c, c) }

// JSK-T107 mutation pair: a Secret placed in a struct that is JSON-encoded (a debug dump, an API body) must
// serialize as the redaction marker, never as the value.
func TestSecretNeverSerializesAsPlaintext(t *testing.T) {
	b, err := json.Marshal(struct{ K Secret }{NewSecret(canary)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), canary) || !strings.Contains(string(b), "[redacted]") {
		t.Fatalf("secret serialized as %s", b)
	}
}
