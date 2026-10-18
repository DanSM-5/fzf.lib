package fzf

// End-to-end tests of RunContext and Go bindings. fzf needs a terminal, so
// each scenario re-executes the test binary (TestLibHelper) inside a private
// tmux server, drives it with keys, and reads back the result it writes.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	libHelperEnv = "FZF_LIB_HELPER"
	libResultEnv = "FZF_LIB_RESULT"
)

type libRunResult struct {
	Code      int
	Err       string
	Notes     []string
	ElapsedMs int64
}

// TestLibHelper is not a test by itself; it runs a scenario when executed by
// runLibScenario
func TestLibHelper(t *testing.T) {
	scenario := os.Getenv(libHelperEnv)
	if scenario == "" {
		t.Skip("helper process for the RunContext tests")
	}

	var mutex sync.Mutex
	notes := []string{}
	note := func(s string) {
		mutex.Lock()
		defer mutex.Unlock()
		notes = append(notes, s)
	}

	args := []string{}
	if scenario == "deferred" {
		args = append(args, "--sync")
	}
	opts, err := Parse(LibOptions{Args: args})
	if err != nil {
		t.Fatal(err)
	}

	input := make(chan string, 3)
	input <- "one"
	input <- "two"
	input <- "three"
	if scenario != "deferred" {
		// --sync waits for the end of the input, which never comes
		close(input)
	}
	opts.Input = input
	opts.Output = make(chan string, 10)

	blocking := func(name string) ActionFunc {
		return func(ctx context.Context, s *State) string {
			note(name + " started")
			<-ctx.Done()
			note(name + " cancelled")
			// Would end fzf with ExitOk if the output of a cancelled function
			// were not discarded
			return "accept"
		}
	}

	ctx := context.Background()
	timeout := 10 * time.Second
	switch scenario {
	case "timeout", "deferred":
		timeout = 300 * time.Millisecond
	case "cancelled":
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		cancel()
	case "sync-binding":
		timeout = 1500 * time.Millisecond
		opts.BindFunc("ctrl-b", BindSync, blocking("sync"))
	case "fg-binding":
		timeout = 1500 * time.Millisecond
		opts.BindFunc("ctrl-b", BindForeground, blocking("fg"))
	case "bg-cancel":
		opts.BindFunc("ctrl-b", BindBackground, blocking("bg"))
		opts.Bind("ctrl-x:bg-cancel")
	case "bg-done":
		opts.BindFunc("ctrl-b", BindBackground, func(ctx context.Context, s *State) string {
			note("bg " + s.Current.Text)
			return "change-prompt(done> )"
		})
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	code, err := RunContext(ctx, opts)
	result := libRunResult{Code: code, ElapsedMs: time.Since(start).Milliseconds()}
	if err != nil {
		result.Err = err.Error()
	}
	mutex.Lock()
	result.Notes = notes
	mutex.Unlock()
	bytes, _ := json.Marshal(result)
	// Rename, so that the test never reads a partially written file
	path := os.Getenv(libResultEnv)
	if err := os.WriteFile(path+".tmp", bytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
}

type libScenario struct {
	t      *testing.T
	socket string
	result string
}

func (s *libScenario) tmux(args ...string) string {
	cmd := exec.Command("tmux", append([]string{"-L", s.socket, "-f", "/dev/null"}, args...)...)
	cmd.Env = withoutEnv(os.Environ(), "TMUX")
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.t.Fatalf("tmux %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (s *libScenario) waitFor(what string, cond func() bool) {
	s.t.Helper()
	for start := time.Now(); !cond(); time.Sleep(20 * time.Millisecond) {
		if time.Since(start) > 10*time.Second {
			s.t.Fatalf("timed out waiting for %s; screen:\n%s", what, s.tmux("capture-pane", "-p"))
		}
	}
}

func (s *libScenario) waitForScreen(text string) {
	s.t.Helper()
	s.waitFor(fmt.Sprintf("%q on screen", text), func() bool {
		return strings.Contains(s.tmux("capture-pane", "-p"), text)
	})
}

func (s *libScenario) keys(keys ...string) {
	s.tmux(append([]string{"send-keys"}, keys...)...)
}

func (s *libScenario) wait() libRunResult {
	s.t.Helper()
	s.waitFor("the result", func() bool {
		_, err := os.Stat(s.result)
		return err == nil
	})
	bytes, err := os.ReadFile(s.result)
	if err != nil {
		s.t.Fatal(err)
	}
	var result libRunResult
	if err := json.Unmarshal(bytes, &result); err != nil {
		s.t.Fatalf("%v: %s", err, bytes)
	}
	return result
}

func withoutEnv(env []string, name string) []string {
	filtered := []string{}
	for _, kv := range env {
		if !strings.HasPrefix(kv, name+"=") {
			filtered = append(filtered, kv)
		}
	}
	return filtered
}

func runLibScenario(t *testing.T, scenario string) *libScenario {
	t.Helper()
	if testing.Short() {
		t.Skip("runs fzf in tmux")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required")
	}
	dir := t.TempDir()
	s := &libScenario{
		t:      t,
		socket: fmt.Sprintf("fzf-lib-test-%d-%s", os.Getpid(), scenario),
		result: filepath.Join(dir, "result.json"),
	}
	command := fmt.Sprintf("env %s=%s %s=%s FZF_DEFAULT_OPTS= %s -test.run='^TestLibHelper$'; sleep 60",
		libHelperEnv, scenario, libResultEnv, s.result, os.Args[0])
	s.tmux("new-session", "-d", "-x", "80", "-y", "24", command)
	t.Cleanup(func() {
		exec.Command("tmux", "-L", s.socket, "kill-server").Run()
	})
	return s
}

func checkInterrupted(t *testing.T, result libRunResult, cause error) {
	t.Helper()
	if result.Code != ExitInterrupt || result.Err != cause.Error() {
		t.Errorf("expected ExitInterrupt with %q, got %+v", cause, result)
	}
}

func TestRunContextTimeout(t *testing.T) {
	s := runLibScenario(t, "timeout")
	result := s.wait()
	checkInterrupted(t, result, context.DeadlineExceeded)
	if result.ElapsedMs < 300 || result.ElapsedMs > 3000 {
		t.Errorf("elapsed: %dms", result.ElapsedMs)
	}
}

func TestRunContextCancelledBeforeStart(t *testing.T) {
	s := runLibScenario(t, "cancelled")
	checkInterrupted(t, s.wait(), context.Canceled)
}

func TestRunContextDeferredStart(t *testing.T) {
	// With --sync and unfinished input, the interface never starts on its own
	s := runLibScenario(t, "deferred")
	checkInterrupted(t, s.wait(), context.DeadlineExceeded)
}

func TestRunContextCancelsSyncBinding(t *testing.T) {
	s := runLibScenario(t, "sync-binding")
	s.waitForScreen("3/3")
	s.keys("C-b")
	result := s.wait()
	checkInterrupted(t, result, context.DeadlineExceeded)
	if strings.Join(result.Notes, ",") != "sync started,sync cancelled" {
		t.Errorf("notes: %v", result.Notes)
	}
}

func TestRunContextCancelsForegroundBinding(t *testing.T) {
	s := runLibScenario(t, "fg-binding")
	s.waitForScreen("3/3")
	s.keys("C-b")
	result := s.wait()
	checkInterrupted(t, result, context.DeadlineExceeded)
	if strings.Join(result.Notes, ",") != "fg started,fg cancelled" {
		t.Errorf("notes: %v", result.Notes)
	}
}

func TestBgCancelCancelsBackgroundBinding(t *testing.T) {
	s := runLibScenario(t, "bg-cancel")
	s.waitForScreen("3/3")
	s.keys("C-b")
	s.keys("C-x")
	// The result of the cancelled function ('accept') is discarded, so fzf
	// keeps running until it is aborted
	time.Sleep(300 * time.Millisecond)
	s.keys("C-c")
	result := s.wait()
	if result.Code != ExitInterrupt || result.Err != "" {
		t.Errorf("expected abort, got %+v", result)
	}
	if strings.Join(result.Notes, ",") != "bg started,bg cancelled" {
		t.Errorf("notes: %v", result.Notes)
	}
}

func TestBackgroundBindingResult(t *testing.T) {
	s := runLibScenario(t, "bg-done")
	s.waitForScreen("3/3")
	s.keys("C-b")
	s.waitForScreen("done>")
	s.keys("Enter")
	result := s.wait()
	if result.Code != ExitOk || result.Err != "" || strings.Join(result.Notes, ",") != "bg one" {
		t.Errorf("result: %+v", result)
	}
}
