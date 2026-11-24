// Package jsonld is reference StandardReader #1: schema.org JobPosting embedded as application/ld+json.
package jsonld

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"example.com/jsk-sketch/ext"
)

// Reader extracts the first JobPosting from a JSON-LD block or a bare JSON-LD document.
type Reader struct{}

func (Reader) Info() ext.Info {
	return ext.Info{ID: "schemaorg_jobposting", Kind: "standard_reader", Version: "1.0.0", Priority: 10, Spec: "schema.org JobPosting (checked 2026-11)"}
}

func (Reader) Sniff(ct string, head []byte) bool {
	return strings.Contains(ct, "ld+json") || bytes.Contains(head, []byte(`"JobPosting"`))
}

type org struct {
	Name string `json:"name"`
}
type addr struct {
	Country string `json:"addressCountry"`
}
type place struct {
	Address addr `json:"address"`
}
type doc struct {
	Type        string `json:"@type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Posted      string `json:"datePosted"`
	LocType     string `json:"jobLocationType"` // TELECOMMUTE == fully remote
	Org         org    `json:"hiringOrganization"`
	Loc         place  `json:"jobLocation"`
}

func (Reader) Read(_ context.Context, raw []byte) (ext.Posting, bool, error) {
	var d doc
	if err := json.Unmarshal(raw, &d); err != nil {
		return ext.Posting{}, false, err
	}
	if d.Type != "JobPosting" {
		return ext.Posting{}, false, nil
	}
	p := ext.Posting{Title: d.Title, Company: d.Org.Name, Body: d.Description, URL: d.URL}
	switch d.LocType {
	case "TELECOMMUTE":
		p.RemoteMode = "remote"
	case "":
		// absent is not "onsite": leave empty so eligibility says Unknown
	default:
		p.RemoteMode = "onsite"
	}
	if c := d.Loc.Address.Country; c != "" {
		p.Countries = []string{c}
	}
	if t, err := time.Parse("2006-01-02", d.Posted); err == nil {
		p.PostedAt = t
	}
	return p, true, nil
}
