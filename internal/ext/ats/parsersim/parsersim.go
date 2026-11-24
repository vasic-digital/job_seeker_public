// Package parsersim is the ParserSimulator that extracts what an ATS parser would see from a PDF, using the
// poppler command-line tools through exec (JSK-T238, Q-ARCH-5: no new Go PDF dependency). A missing tool is
// ErrToolMissing, which callers turn into an Unknown verdict, never into a pass.
//
// Links come from `pdftohtml -xml`: it reports the target of URI links, the file of GoToR links and an empty href
// for Launch and other actions, which is exactly the distinction the link gate needs. Compressed object streams
// are handled by poppler, not by this package.
package parsersim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

var (
	// ErrToolMissing: a required poppler tool is not installed.
	ErrToolMissing = errors.New("parsersim: required tool not installed")
	// ErrUnreadable: the tools ran but could not parse the document.
	ErrUnreadable = errors.New("parsersim: document unreadable")
)

// DefaultMaxBytes bounds the document handed to the tools.
const DefaultMaxBytes = 20 << 20

// PopplerSim implements ext.ParserSimulator over pdfinfo, pdftotext and pdftohtml.
type PopplerSim struct {
	MaxBytes int64         // 0: DefaultMaxBytes
	Timeout  time.Duration // per tool call; 0: 20s
}

// Info implements ext.Plugin.
func (PopplerSim) Info() ext.Info {
	return ext.Info{ID: "poppler_pdf", Kind: "parser_simulator", Version: "1.0.0", Priority: 10, Spec: "poppler pdfinfo/pdftotext/pdftohtml",
		Caps: map[string]bool{"needs_tools": true}}
}

var (
	reAnchor = regexp.MustCompile(`(?s)<a href="([^"]*)">(.*?)</a>`)
	reTag    = regexp.MustCompile(`<[^>]+>`)
)

func (s PopplerSim) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrToolMissing, name)
	}
	to := s.Timeout
	if to == 0 {
		to = 20 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	cmd := exec.CommandContext(cctx, path, args...)
	cmd.Env = []string{"LC_ALL=C.UTF-8", "PATH=" + os.Getenv("PATH")}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &limited{w: &out, max: 8 << 20}, &errb
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrToolMissing, name)
		}
		return nil, ErrUnreadable
	}
	return out.Bytes(), nil
}

type limited struct {
	w   *bytes.Buffer
	max int
}

func (l *limited) Write(p []byte) (int, error) {
	if l.w.Len()+len(p) > l.max {
		return 0, errors.New("output limit")
	}
	return l.w.Write(p)
}

// Extract implements ext.ParserSimulator. The reader is copied to a private temporary file that is always removed.
func (s PopplerSim) Extract(ctx context.Context, _ ext.Artifact, r io.Reader) (ext.ExtractedDoc, error) {
	var doc ext.ExtractedDoc
	max := s.MaxBytes
	if max == 0 {
		max = DefaultMaxBytes
	}
	f, err := os.CreateTemp("", "jsk-pdf-*.pdf") // 0600
	if err != nil {
		return doc, err
	}
	defer os.Remove(f.Name())
	n, err := io.Copy(f, io.LimitReader(r, max+1))
	f.Close()
	if err != nil {
		return doc, err
	}
	if n > max {
		return doc, fmt.Errorf("parsersim: document larger than %d bytes", max)
	}
	info, err := s.run(ctx, "pdfinfo", f.Name())
	if err != nil {
		return doc, err
	}
	for _, l := range strings.Split(string(info), "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "Pages":
			doc.Pages, _ = strconv.Atoi(strings.TrimSpace(v))
		case "Tagged":
			doc.Tagged = strings.TrimSpace(v) == "yes"
		}
	}
	if doc.Pages == 0 {
		return doc, ErrUnreadable
	}
	txt, err := s.run(ctx, "pdftotext", "-enc", "UTF-8", f.Name(), "-")
	if err != nil {
		return doc, err
	}
	doc.Text = string(txt)
	x, err := s.run(ctx, "pdftohtml", "-xml", "-stdout", "-i", "-q", f.Name())
	if err != nil {
		return doc, err
	}
	doc.Links = append(doc.Links, parseAnchors(string(x))...)
	// URI annotations that cover no text are invisible to pdftohtml; pdfinfo -url lists them.
	have := map[string]bool{}
	for _, l := range doc.Links {
		have[l.Target] = true
	}
	if ui, err := s.run(ctx, "pdfinfo", "-url", f.Name()); err == nil {
		for _, ln := range strings.Split(string(ui), "\n") {
			fs := strings.Fields(ln)
			if len(fs) >= 3 && fs[0] != "Page" {
				if _, err := strconv.Atoi(fs[0]); err == nil {
					t := fs[len(fs)-1]
					if !have[t] {
						have[t] = true
						doc.Links = append(doc.Links, classify(t, ""))
					}
				}
			}
		}
	} else {
		return doc, err
	}
	rawBytes, err := os.ReadFile(f.Name())
	if err != nil {
		return doc, err
	}
	sc := StructScan(rawBytes)
	doc.Actions, doc.Incomplete = sc.Actions, sc.Incomplete
	return doc, nil
}

// classify derives the scheme of a link target. A scheme counts only when the URL is well formed for it: http and
// https need a host ("https:foo" is not an absolute URL), other schemes are recorded as they are so the allow-list
// can refuse them.
func classify(target, label string) ext.Link {
	l := ext.Link{Target: target, Label: label}
	if u, err := url.Parse(target); err == nil && u.Scheme != "" {
		sc := strings.ToLower(u.Scheme)
		if (sc == "http" || sc == "https") && u.Host == "" {
			return l
		}
		l.Scheme = sc
	}
	return l
}

// parseAnchors reads the <a href> elements of pdftohtml -xml output. Attribute values and labels may span lines.
func parseAnchors(x string) []ext.Link {
	var out []ext.Link
	for _, m := range reAnchor.FindAllStringSubmatch(x, -1) {
		target := html.UnescapeString(m[1])
		label := strings.Join(strings.Fields(html.UnescapeString(reTag.ReplaceAllString(m[2], ""))), " ")
		out = append(out, classify(target, label))
	}
	return out
}
