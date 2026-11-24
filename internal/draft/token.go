package draft

import "github.com/vasic-digital/job_seeker/internal/ext"

// mintToken is the ONLY production caller of ext.NewApproved (SC-033, asserted by the channel package's allow-list
// test). The approved hash is the draft hash that verifyDraft just re-verified; the URL comes from the approved
// posting row (apply URL, else canonical), never from the caller, and every channel re-validates it itself.
func mintToken(a App, p postingRow, approver string) (ext.Approved, error) {
	url := p.ApplyURL
	if url == "" {
		url = p.Canon
	}
	return ext.NewApproved(a.ID, approver, a.DraftHash, a.DraftHash, url)
}
