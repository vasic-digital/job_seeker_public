package match

import "os"

// rulesDir lets gates/verify_match.sh point the suite at a mutated rules copy (paired proof).
func rulesDir() string {
	if d := os.Getenv("JS_RULES_DIR"); d != "" {
		return d
	}
	return "../../rules"
}

func writeFile(p, s string) error       { return os.WriteFile(p, []byte(s), 0o600) }
func readFile(p string) ([]byte, error) { return os.ReadFile(p) }
