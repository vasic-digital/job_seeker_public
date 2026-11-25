package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"digital.vasic.containers/pkg/runtime"
)

// cmdDrill is the JSK-T092 restore drill into a scratch CONTAINER:
//
//	jsstack drill -image IMG -drill BIN -snapshot FILE [-timeout D]
//
// It runs, through runtime.Run of the Containers module (never a hand-built podman/docker argv), a throwaway
// container from IMG with the static jsdrill binary and the snapshot's directory mounted READ-ONLY. The restore
// happens in the container's own filesystem (scratch), which disappears with the container (--rm). The command
// passes only when the container exited 0 AND printed integrity_ok=true; the wall time of the whole drill
// (container start included) is recorded. Exit: 0 pass, 1 drill failed, 2 cannot determine (never a pass).
func cmdDrill(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("drill", flag.ContinueOnError)
	image := fs.String("image", "", "image to run the drill in (must already be present; nothing is pulled)")
	drill := fs.String("drill", "", "path of the static jsdrill binary (CGO_ENABLED=0)")
	snap := fs.String("snapshot", "", "snapshot file to restore")
	timeout := fs.Duration("timeout", 2*time.Minute, "overall budget")
	if err := fs.Parse(args); err != nil || *image == "" || *drill == "" || *snap == "" {
		fmt.Fprintln(os.Stderr, "usage: jsstack drill -image IMG -drill JSDRILL_BIN -snapshot FILE [-timeout D]")
		return exitFail
	}
	bin, err1 := filepath.Abs(*drill)
	file, err2 := filepath.Abs(*snap)
	if err1 != nil || err2 != nil {
		return exitUndetermined
	}
	for _, p := range []string{bin, file} {
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: %s is not a readable file\n", p)
			return exitUndetermined
		}
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	rt, err := runtime.SelectRuntime(ctx, choice)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: no container runtime: %v\n", err)
		return exitUndetermined
	}
	// The module's mount policy refuses system roots such as /tmp, so both inputs are COPIED into a private
	// 0700 staging directory under the user cache dir and that directory (two subdirs) is mounted read-only.
	cd, err := os.UserCacheDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE the cache dir: %v\n", err)
		return exitUndetermined
	}
	if err := os.MkdirAll(filepath.Join(cd, "job_seeker"), 0o700); err != nil {
		return exitUndetermined
	}
	stage, err := os.MkdirTemp(filepath.Join(cd, "job_seeker"), "drill-stage-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: staging dir: %v\n", err)
		return exitUndetermined
	}
	defer os.RemoveAll(stage)
	binDir, snapDir := filepath.Join(stage, "bin"), filepath.Join(stage, "snap")
	for _, d := range []string{binDir, snapDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			return exitUndetermined
		}
	}
	if err := stageFile(bin, filepath.Join(binDir, filepath.Base(bin)), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: staging the drill binary: %v\n", err)
		return exitUndetermined
	}
	if err := stageFile(file, filepath.Join(snapDir, filepath.Base(file)), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: staging the snapshot: %v\n", err)
		return exitUndetermined
	}
	start := time.Now()
	res, err := rt.Run(ctx, *image, []string{"/snap/" + filepath.Base(file), "/tmp"},
		runtime.WithRunMountAllowedRoots(stage),
		runtime.WithRunVolumes(binDir+":/drill:ro", snapDir+":/snap:ro"),
		runtime.WithRunEntrypoint("/drill/"+filepath.Base(bin)), // the image ENTRYPOINT is replaced
		runtime.WithRunNetwork("none"),
		runtime.WithRunUserns("keep-id"),
	)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: the drill container could not be run: %v\n", err)
		return exitUndetermined
	}
	if ctx.Err() != nil {
		fmt.Fprintln(os.Stderr, "jsstack: COULD NOT DETERMINE: drill budget exhausted")
		return exitUndetermined
	}
	out := strings.TrimSpace(res.Stdout)
	// 125/126/127 are the RUNTIME's own failure codes (image missing, cannot exec, binary not found): the drill
	// never ran, so this is "could not determine", never a verdict about the snapshot.
	if res.ExitCode == 125 || res.ExitCode == 126 || res.ExitCode == 127 {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: the container did not start the drill (exit %d, image %s): %s\n",
			res.ExitCode, *image, firstLine(res.Stderr))
		return exitUndetermined
	}
	if res.ExitCode != 0 || !strings.Contains(out, "integrity_ok=true") {
		fmt.Fprintf(os.Stderr, "jsstack: DRILL FAIL: runtime=%s container exit=%d wall=%s stdout=%q stderr=%q\n",
			rt.Name(), res.ExitCode, elapsed.Round(time.Millisecond), out, strings.TrimSpace(res.Stderr))
		return exitFail
	}
	fmt.Printf("drill: runtime=%s image=%s container_exit=0 wall=%s\n%s\n", rt.Name(), *image, elapsed.Round(time.Millisecond), out)
	return exitOK
}

// stageFile copies src to dst with the given mode (the drill never touches the original snapshot).
func stageFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
