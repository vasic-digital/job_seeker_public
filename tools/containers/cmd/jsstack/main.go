// Command jsstack drives job_seeker's container workloads EXCLUSIVELY through the canonical Containers
// Submodule (digital.vasic.containers, section 11.4.76). It shells out to no runtime: podman and docker
// appear nowhere in this file. It follows the umbrella's _tools/containers pattern (JSK-T010):
//
//	jsstack probe                                      runtime + compose availability, starts nothing
//	jsstack port [-udp] NAME DEFAULT_PORT              discover-or-allocate a free port (service registry)
//	jsstack up   -file F -project P -container C [-build] [-wait-port N] [-timeout D]
//	                                                   compose up, then wait for the container to RUN and
//	                                                   the TCP port to accept (health.CheckTCP)
//	jsstack status -container C                        state, health, exit code, restart-relevant fields
//	jsstack logs -container C                          container output
//	jsstack down -file F -project P                    compose down (orphans removed)
//
// Every subcommand accepts the ecosystem-wide --runner=NAME (also --runner NAME; env CONTAINERS_RUNNER; flag
// wins) that FORCES the container runner (podman, docker, nerdctl, ... or auto = default order). An unknown
// or not-installed runner is refused with exit 2 and never replaced by another one; see the Containers
// module's docs/RUNNER_FLAG.md and docs/RUNNER.md here.
//
// Exit codes are three-valued: 0 ok, 1 real failure, 2 COULD NOT DETERMINE (never a pass).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"digital.vasic.containers/pkg/compose"
	"digital.vasic.containers/pkg/health"
	"digital.vasic.containers/pkg/logging"
	"digital.vasic.containers/pkg/runtime"
	"digital.vasic.containers/pkg/serviceregistry"
)

const (
	exitOK           = 0
	exitFail         = 1
	exitUndetermined = 2
	// logTail: the module's default Tail:"all" is passed to podman verbatim and podman rejects it
	// (rc 125, measured in the umbrella's _tools/containers/internal/orchestrate); "-1" is accepted.
	logTail = "-1"
)

func main() { os.Exit(run(os.Args[1:])) }

// choice is the runner selected for this invocation (flag > env > default order), set once by run.
var choice runtime.RunnerChoice

func run(args []string) int {
	val, rest, found, err := runtime.ParseRunnerFlag(args)
	if err == nil {
		choice, err = runtime.ResolveRunner(val, found, os.Getenv)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: %v\n", err)
		return exitUndetermined
	}
	args = rest
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		fmt.Println("usage: jsstack [--runner=NAME] probe|port|up|status|logs|down ...")
		fmt.Println("  --runner=NAME  force the container runner (podman|docker|nerdctl|cri-o|lxd|kubernetes|auto); also --runner NAME")
		fmt.Println("                 and env CONTAINERS_RUNNER; flag wins. An unknown or not-installed runner is refused (exit 2).")
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: jsstack probe|port|up|status|logs|down ...")
		return exitFail
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	switch args[0] {
	case "probe":
		return cmdProbe(ctx)
	case "port":
		return cmdPort(ctx, args[1:])
	case "up":
		return cmdUp(ctx, args[1:])
	case "status":
		return cmdStatus(ctx, args[1:])
	case "logs":
		return cmdLogs(ctx, args[1:])
	case "down":
		return cmdDown(ctx, args[1:])
	}
	fmt.Fprintf(os.Stderr, "jsstack: unknown command %q\n", args[0])
	return exitFail
}

func cmdProbe(ctx context.Context) int {
	rt, err := runtime.SelectRuntime(ctx, choice)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: no container runtime: %v\n", err)
		return exitUndetermined
	}
	v, _ := rt.Version(ctx)
	if _, err := compose.NewDefaultOrchestratorForRunner(".", logging.NewStdLogger("jsstack"), choice.Name); err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: no compose command: %v\n", err)
		return exitUndetermined
	}
	fmt.Printf("runtime=%s version=%s compose=available\n", rt.Name(), strings.TrimSpace(v))
	return exitOK
}

// cmdPort resolves a free port for NAME. Registry entries persist under the user cache dir so a second
// run reuses the same port while it is still free. With -udp the port must be free on UDP as well
// (HTTPS and HTTP/3 bind ONE number on both; FindAvailablePort tests TCP only).
func cmdPort(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("port", flag.ContinueOnError)
	udp := fs.Bool("udp", false, "port must also be free on UDP")
	dir := fs.String("registry-dir", "", "registry directory (default: <user cache dir>/job_seeker/service-registry)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: jsstack port [-udp] [-registry-dir D] NAME DEFAULT_PORT")
		return exitFail
	}
	name := fs.Arg(0)
	def, err := strconv.Atoi(fs.Arg(1))
	if err != nil || def < 1 || def > 65000 {
		fmt.Fprintln(os.Stderr, "jsstack: DEFAULT_PORT must be 1..65000")
		return exitFail
	}
	if *dir == "" {
		cd, err := os.UserCacheDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE the cache dir: %v\n", err)
			return exitUndetermined
		}
		*dir = filepath.Join(cd, "job_seeker", "service-registry")
	}
	if err := os.MkdirAll(*dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: registry dir: %v\n", err)
		return exitFail
	}
	reg := serviceregistry.New(serviceregistry.WithRegistryDir(*dir))
	if svc, ok := reg.Get(name); ok && tcpFree(svc.Port) && (!*udp || udpFree(svc.Port)) {
		fmt.Println(svc.Port)
		return exitOK
	}
	claimed := map[int]bool{}
	for n, s := range reg.GetAll() {
		if n != name {
			claimed[s.Port] = true
		}
	}
	limit := def + 10000
	for start := def; start < limit; {
		if ctx.Err() != nil {
			return exitUndetermined
		}
		c := reg.FindAvailablePort(start)
		if c == 0 || c >= limit {
			break
		}
		if !claimed[c] && (!*udp || udpFree(c)) {
			if err := reg.Register(name, c); err != nil {
				fmt.Fprintf(os.Stderr, "jsstack: register %s: %v\n", name, err)
				return exitFail
			}
			fmt.Println(c)
			return exitOK
		}
		start = c + 1
	}
	fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: no free port in [%d,%d)\n", def, limit)
	return exitUndetermined
}

func tcpFree(p int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func udpFree(p int) bool {
	c, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

type composeFlags struct {
	file, project, container string
}

func (c *composeFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&c.file, "file", "", "compose file")
	fs.StringVar(&c.project, "project", "jobseeker-gate", "compose project name")
	fs.StringVar(&c.container, "container", "", "container_name of the service")
}

func newOrch(root string) (compose.ComposeOrchestrator, error) {
	return compose.NewDefaultOrchestratorForRunner(root, logging.NewStdLogger("jsstack"), choice.Name)
}

func cmdUp(ctx context.Context, args []string) int {
	var cf composeFlags
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	cf.bind(fs)
	build := fs.Bool("build", false, "build the image first")
	waitPort := fs.Int("wait-port", 0, "TCP port that must accept before success")
	timeout := fs.Duration("timeout", 90*time.Second, "overall wait budget after up")
	if err := fs.Parse(args); err != nil || cf.file == "" || cf.container == "" {
		fmt.Fprintln(os.Stderr, "usage: jsstack up -file F -container C [-project P] [-build] [-wait-port N] [-timeout D]")
		return exitFail
	}
	abs, err := filepath.Abs(cf.file)
	if err != nil {
		return exitFail
	}
	if !requireComposeVars("up", abs) {
		return exitUndetermined
	}
	rt, err := runtime.SelectRuntime(ctx, choice)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: no container runtime: %v\n", err)
		return exitUndetermined
	}
	orch, err := newOrch(filepath.Dir(abs))
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: no compose command: %v\n", err)
		return exitUndetermined
	}
	proj := compose.ComposeProject{Name: cf.project, File: abs}
	_ = orch.Down(ctx, proj, compose.WithDownRemoveOrphans(true)) // stale container from an interrupted run
	if err := orch.Up(ctx, proj, compose.WithUpDetach(true), compose.WithRemoveOrphans(true), compose.WithBuildFirst(*build)); err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: compose up failed: %v\n", err)
		dumpLogs(ctx, rt, cf.container)
		return exitFail
	}
	// The container must reach RUNNING (read back through the module's runtime API) ...
	deadline := time.Now().Add(*timeout)
	for {
		st, err := rt.Status(ctx, cf.container)
		if err == nil && st.State == runtime.StateRunning {
			break
		}
		if err == nil && (st.State == runtime.StateStopped || st.State == "exited") {
			fmt.Fprintf(os.Stderr, "jsstack: FAIL: %s stopped with exit code %d\n", cf.container, st.ExitCode)
			dumpLogs(ctx, rt, cf.container)
			return exitFail
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: %s not running before the deadline (status err: %v)\n", cf.container, err)
			return exitUndetermined
		}
		time.Sleep(time.Second)
	}
	// ... and, when asked, the TCP port must accept (the module's own health checker).
	if *waitPort > 0 {
		for {
			r := health.CheckTCP(ctx, health.HealthTarget{Name: cf.container, Host: "127.0.0.1",
				Port: strconv.Itoa(*waitPort), Type: health.HealthTCP, Timeout: 2 * time.Second})
			if r != nil && r.Healthy {
				break
			}
			if time.Now().After(deadline) {
				fmt.Fprintf(os.Stderr, "jsstack: FAIL: port %d never accepted TCP\n", *waitPort)
				dumpLogs(ctx, rt, cf.container)
				return exitFail
			}
			time.Sleep(time.Second)
		}
	}
	fmt.Printf("up: %s running\n", cf.container)
	return exitOK
}

func cmdStatus(ctx context.Context, args []string) int {
	var cf composeFlags
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	cf.bind(fs)
	if err := fs.Parse(args); err != nil || cf.container == "" {
		fmt.Fprintln(os.Stderr, "usage: jsstack status -container C")
		return exitFail
	}
	rt, err := runtime.SelectRuntime(ctx, choice)
	if err != nil {
		return exitUndetermined
	}
	st, err := rt.Status(ctx, cf.container)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE status of %s: %v\n", cf.container, err)
		return exitUndetermined
	}
	fmt.Printf("name=%s state=%s health=%q exit=%d started=%s\n", st.Name, st.State, st.Health, st.ExitCode, st.StartedAt.Format(time.RFC3339))
	return exitOK
}

func cmdLogs(ctx context.Context, args []string) int {
	var cf composeFlags
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	cf.bind(fs)
	if err := fs.Parse(args); err != nil || cf.container == "" {
		fmt.Fprintln(os.Stderr, "usage: jsstack logs -container C")
		return exitFail
	}
	rt, err := runtime.SelectRuntime(ctx, choice)
	if err != nil {
		return exitUndetermined
	}
	dumpLogs(ctx, rt, cf.container)
	return exitOK
}

func dumpLogs(ctx context.Context, rt runtime.ContainerRuntime, name string) {
	rc, err := rt.Logs(ctx, name, runtime.WithTail(logTail))
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: (container logs unavailable: %v)\n", err)
		return
	}
	_, _ = io.Copy(os.Stdout, rc)
	if cerr := rc.Close(); cerr != nil { // the log command's own exit status surfaces here
		fmt.Fprintf(os.Stderr, "jsstack: WARNING reading container logs failed: %v\n", cerr)
	}
}

func cmdDown(ctx context.Context, args []string) int {
	var cf composeFlags
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	cf.bind(fs)
	if err := fs.Parse(args); err != nil || cf.file == "" {
		fmt.Fprintln(os.Stderr, "usage: jsstack down -file F [-project P]")
		return exitFail
	}
	abs, err := filepath.Abs(cf.file)
	if err != nil {
		return exitFail
	}
	if !requireComposeVars("down", abs) {
		return exitUndetermined
	}
	orch, err := newOrch(filepath.Dir(abs))
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: no compose command: %v\n", err)
		return exitUndetermined
	}
	if err := orch.Down(ctx, compose.ComposeProject{Name: cf.project, File: abs}, compose.WithDownRemoveOrphans(true)); err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: compose down: %v\n", err)
		return exitFail
	}
	fmt.Println("down: ok")
	return exitOK
}

// requiredVarRe matches compose "required variable" references: ${VAR:?msg} (unset or empty is an
// error) and ${VAR?msg} (unset is an error).
var requiredVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:?)\?`)

// missingRequiredComposeVars lists, sorted and de-duplicated, the required variables of the compose
// file that the environment does not satisfy. Checked BEFORE the compose front-end runs, because
// podman-compose answers a missing required variable with a Python traceback.
func missingRequiredComposeVars(file string) ([]string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range requiredVarRe.FindAllStringSubmatch(string(b), -1) {
		name := m[1]
		v, ok := os.LookupEnv(name)
		if (!ok || (m[2] == ":" && v == "")) && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// requireComposeVars prints a clean exit-2 message (documented in docs/RUNNER.md) when the compose file's
// required variables are not set; it returns true when the caller may proceed.
func requireComposeVars(sub, file string) bool {
	missing, err := missingRequiredComposeVars(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: cannot read compose file %s: %v\n", file, err)
		return false
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "jsstack: COULD NOT DETERMINE: %s needs these variables set for %s: %s (see the 'jsstack port' output and the compose file)\n",
			sub, file, strings.Join(missing, ", "))
		return false
	}
	return true
}
