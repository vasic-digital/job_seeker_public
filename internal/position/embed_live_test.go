package position

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func cosine(a, b []float32) float64 {
	var d, na, nb float64
	for i := range a {
		d += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return d / (math.Sqrt(na) * math.Sqrt(nb))
}

// JSK-T193 follow-up: the REAL OllamaEmbedder against a REAL loopback Ollama. Opt-in: set JSK_OLLAMA_LOOPBACK (e.g. http://127.0.0.1:11434).
// Unset is a SKIP with the topology named, never a pass. An embedding model that is not installed is reported as such (the embedder is
// "unavailable"), not papered over. Evidence goes to $JSK_EVIDENCE_DIR/embedder-live.txt (normalise with scripts/evnorm.sh -w).
func TestOllamaEmbedderLive(t *testing.T) {
	base := os.Getenv("JSK_OLLAMA_LOOPBACK")
	if base == "" {
		t.Skip("SKIP topology: set JSK_OLLAMA_LOOPBACK to a loopback Ollama base URL (needs the nomic-embed-text model installed)")
	}
	e, err := NewOllamaEmbedder(base, os.Getenv("JSK_EMBED_MODEL"))
	if err != nil {
		t.Fatalf("loopback endpoint refused: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a, err := e.Embed(ctx, bodyA)
	if err != nil {
		t.Fatalf("embedder unavailable against the real server (is the model installed?): %v", err)
	}
	a2, _ := e.Embed(ctx, bodyA)
	near, _ := e.Embed(ctx, "Senior backend engineer wanted: build payment services in Go, own the ledger, Postgres and Kafka, mentor juniors, remote in Europe.")
	far, err := e.Embed(ctx, "Seasonal barista wanted for a coffee shop in Lisbon; early shifts, tips and free coffee.")
	if err != nil || len(near) == 0 || len(far) == 0 {
		t.Fatalf("embed failed: %v", err)
	}
	cs, cn, cf := cosine(a, a2), cosine(a, near), cosine(a, far)
	if len(a) < 64 || len(near) != len(a) || len(far) != len(a) {
		t.Fatalf("dimensions: %d %d %d", len(a), len(near), len(far))
	}
	if cs < 0.999 || !(cn > cf) || cn < 0.6 {
		t.Fatalf("semantic ordering broken: self=%.4f near=%.4f far=%.4f", cs, cn, cf)
	}
	line := fmt.Sprintf("model=%s dim=%d cos_self=%.4f cos_near=%.4f cos_far=%.4f\n", orDefault(os.Getenv("JSK_EMBED_MODEL")), len(a), cs, cn, cf)
	t.Log(line)
	if dir := os.Getenv("JSK_EVIDENCE_DIR"); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "embedder-live.txt"), []byte("real OllamaEmbedder against a loopback Ollama\n"+line), 0o644)
	}
}

func orDefault(m string) string {
	if m == "" {
		return DefaultEmbedModel
	}
	return m
}
