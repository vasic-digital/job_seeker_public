package position

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// ErrEmbedderUnavailable means the optional local embedder cannot be used right now (not configured, not reachable, bad answer). Callers treat it as "feature absent":
// the f_body_emb weight is redistributed, it is never an error for the resolver (providers/04 section 3.3).
var ErrEmbedderUnavailable = errors.New("position: embedder unavailable")

// Embedder turns text into a vector. The only implementation shipped is local (loopback); it is optional and off unless configured.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// OllamaEmbedder talks to a LOOPBACK Ollama (nomic-embed-text by default). The body never leaves the host: any non-loopback endpoint is refused at construction.
type OllamaEmbedder struct {
	endpoint string
	model    string
	client   *http.Client
}

// DefaultEmbedModel is the embedding model of the local stack.
const DefaultEmbedModel = "nomic-embed-text"

// NewOllamaEmbedder validates base by PARSING it (scheme http, a literal loopback IP or the name localhost, no userinfo), never by prefix.
func NewOllamaEmbedder(base, model string) (*OllamaEmbedder, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%w: endpoint must be a plain http loopback URL", ErrEmbedderUnavailable)
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("%w: endpoint is not loopback", ErrEmbedderUnavailable)
	}
	if model == "" {
		model = DefaultEmbedModel
	}
	return &OllamaEmbedder{endpoint: u.Scheme + "://" + u.Host + "/api/embeddings", model: model, client: &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, // a redirect could leave loopback
	}}, nil
}

// Embed returns the embedding or ErrEmbedderUnavailable (wrapped) on any failure.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]string{"model": e.model, "prompt": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbedderUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: request failed", ErrEmbedderUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrEmbedderUnavailable, resp.StatusCode)
	}
	var out struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil || len(out.Embedding) == 0 {
		return nil, fmt.Errorf("%w: unreadable answer", ErrEmbedderUnavailable)
	}
	return out.Embedding, nil
}
