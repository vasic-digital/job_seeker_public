//go:build !linux

package audit

import "os"

func strengthOf(*os.File) string { return "policy" }
