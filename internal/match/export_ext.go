package match

// Exported views of the private normalisers and factors, used by the extension plugins (JSK-T228) so a plugin
// compares exactly as the legacy gate does. No behaviour of its own.

// NormText is the matching normalisation (lowercase, diacritics folded, space-wrapped tokens).
func NormText(s string) string { return normalize(s, true) }

// NormName is NormText trimmed (company comparison).
func NormName(s string) string { return norm1(s) }

// AnnualFactors returns a copy of the period-to-annual factors from score.yaml.
func (r *Rules) AnnualFactors() map[string]float64 {
	out := make(map[string]float64, len(r.Score.Salary.AnnualFactors))
	for k, v := range r.Score.Salary.AnnualFactors {
		out[k] = v
	}
	return out
}
