package channel_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/builtin"
)

// SC-033 (part 1): every registered channel refuses a zero Approved and none declares direct_send.
func TestEveryRegisteredChannelRefusesZeroApprovedAndNeverSends(t *testing.T) {
	s, err := builtin.New()
	if err != nil {
		t.Fatal(err)
	}
	ids := s.Channels.IDs()
	if len(ids) == 0 {
		t.Fatal("no channel registered: the enumeration would be vacuous")
	}
	for _, id := range ids {
		c, _ := s.Channels.Get(id)
		if c.Info().Caps["direct_send"] {
			t.Errorf("%s declares direct_send", id)
		}
		if _, err := c.Prepare(context.Background(), ext.Approved{}); err == nil {
			t.Errorf("%s accepted a zero Approved", id)
		}
		forged := ext.Approved{AppID: 1, DraftHash: "h", URL: "https://x.example/a"} // approvedBy unexported: Valid() is false
		if _, err := c.Prepare(context.Background(), forged); err == nil {
			t.Errorf("%s accepted a hand-built Approved", id)
		}
	}
}

// SC-033 (part 2): the production callers of the Approved constructor are exactly the approval service (none yet).
func TestNewApprovedCallersAreOnlyTheAllowList(t *testing.T) {
	allowed := map[string]bool{
		"internal/ext/ports.go": true, // the definition
	}
	root := "../../.."
	var bad []string
	sawDefinition := false
	werr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "docs/") || strings.HasPrefix(rel, "submodules/") || strings.HasPrefix(rel, "tools/") {
			return nil
		}
		b, _ := os.ReadFile(p)
		if !strings.Contains(string(b), "NewApproved(") {
			return nil
		}
		if allowed[rel] {
			sawDefinition = true
		}
		if !allowed[rel] {
			bad = append(bad, rel)
		}
		return nil
	})
	if werr != nil || !sawDefinition {
		t.Fatalf("scan incomplete: err=%v sawDefinition=%v", werr, sawDefinition)
	}
	if len(bad) > 0 {
		t.Fatalf("NewApproved is called outside the approval service: %v", bad)
	}
}
