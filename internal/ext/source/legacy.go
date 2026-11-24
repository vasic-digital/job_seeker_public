// Package source adapts the existing providers.Provider adapters to the ext.SourceAdapter port and gives plugins
// an ext.Fetcher backed by the egress client (JSK-T227). The shim changes no behaviour: Wrap(p).Fetch returns the
// same postings, skips, raw body, cursor and call proof as p.Fetch over the same client.
package source

import (
	"context"

	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// Fetcher is the ext.Fetcher over the egress client (allow-list, https-only and IP denylist apply).
type Fetcher struct{ c *egress.Client }

// NewFetcher wraps c.
func NewFetcher(c *egress.Client) *Fetcher { return &Fetcher{c: c} }

// Get implements ext.Fetcher with providers.Get.
func (f *Fetcher) Get(ctx context.Context, rawURL string, hdr map[string]string) ([]byte, ext.CallProof, error) {
	b, p, err := providers.Get(ctx, f.c, rawURL, hdr)
	return b, proofOf(p), err
}

func proofOf(p providers.CallProof) ext.CallProof {
	return ext.CallProof{Host: p.Host, ResolvedIP: p.ResolvedIP, TLSSubject: p.TLSSubject, RespSHA256: p.RespSHA256, Status: p.Status, At: p.At}
}

// Legacy is a providers.Provider presented as an ext.SourceAdapter.
type Legacy struct {
	p providers.Provider
	c *egress.Client
}

// Wrap adapts p. c is the egress client p.Fetch needs; Fetch refuses a Fetcher that is not backed by it.
func Wrap(p providers.Provider, c *egress.Client) *Legacy { return &Legacy{p: p, c: c} }

// Info implements ext.Plugin.
func (l *Legacy) Info() ext.Info {
	return ext.Info{ID: l.p.Name(), Kind: "source", Version: "1.0.0", Priority: 50, Spec: "legacy providers.Provider adapter"}
}

// Limits implements ext.SourceAdapter.
func (l *Legacy) Limits() ext.Limits {
	x := l.p.Limits()
	return ext.Limits{MinInterval: x.MinInterval, MaxPages: x.MaxPages, MaxPerDay: x.MaxPerDay, PageSize: x.PageSize}
}

// Fetch performs exactly one request through the provider.
func (l *Legacy) Fetch(ctx context.Context, f ext.Fetcher, q ext.Query) (ext.Batch, error) {
	ef, ok := f.(*Fetcher)
	if !ok || ef == nil || ef.c == nil || ef.c != l.c || l.c == nil {
		return ext.Batch{}, &ext.ErrFailClosed{Why: "a legacy provider can only be driven through the egress fetcher it was wrapped with"}
	}
	b, err := l.p.Fetch(ctx, l.c, providers.Query{Search: q.Search, Category: q.Category, Cursor: q.Cursor, Limit: q.Limit})
	out := ext.Batch{Postings: b.Postings, Call: proofOf(b.Call), NextCursor: b.NextCursor, Raw: b.Raw}
	if len(b.Skipped) > 0 {
		out.Skipped = map[string]int{}
		for _, s := range b.Skipped {
			out.Skipped[s.Class]++
		}
	}
	return out, err
}

// LegalNotice implements ext.SourceAdapter by re-reading the raw body.
func (l *Legacy) LegalNotice(b ext.Batch) string { return l.p.LegalNotice(providers.Batch{Raw: b.Raw}) }

var _ ext.SourceAdapter = (*Legacy)(nil)
