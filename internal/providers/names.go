package providers

import "sort"

// ShippedNames returns the sorted names of the built-in adapters that have a recorded API URL (the robots-gate
// input). It is the provider axis of the SC-032 scope matrix, so a new adapter joins the matrix by getting a URL.
func ShippedNames() []string {
	out := make([]string, 0, len(apiURLs))
	for n := range apiURLs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
