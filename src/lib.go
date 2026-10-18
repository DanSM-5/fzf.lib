package fzf

// fzf.lib: API for using fzf as a library from Go programs.
//
// The library additions live in lib*.go files. Changes to upstream files are
// limited to small hooks marked with "fzf.lib" comments, so that merging from
// upstream stays manageable.

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/DanSM-5/fzf.lib/src/util"
)

// LibOptions configures Parse
type LibOptions struct {
	// UseDefaultOpts applies the options in $FZF_DEFAULT_OPTS
	UseDefaultOpts bool

	// UseDefaultOptsFile applies the options in the file at $FZF_DEFAULT_OPTS_FILE
	UseDefaultOptsFile bool

	// UseDefaultCommand allows $FZF_DEFAULT_COMMAND to provide the list when
	// Options.Input is nil and stdin is a terminal. Otherwise the built-in
	// directory walker is used in that case.
	UseDefaultCommand bool

	// Args are options in the command-line syntax, e.g. "--multi",
	// "--layout=reverse". They are applied after the defaults above.
	Args []string
}

// Parse creates Options for running fzf as a library. Unlike ParseOptions,
// the returned Options accept Go callbacks (BindFunc, SetPreviewFunc), and
// Run will never replace or re-execute the current process:
//
//   - --tmux is ignored, as fzf cannot re-run the host program in a popup
//   - 'become' makes Run return ExitBecome, see Options.Become
func Parse(lo LibOptions) (*Options, error) {
	opts := defaultOptions()
	opts.lib = &libState{useDefaultCommand: lo.UseDefaultCommand}
	opts, err := parseOptionsWith(opts, lo.UseDefaultOptsFile, lo.UseDefaultOpts, lo.Args)
	if err != nil {
		return nil, err
	}
	opts.Tmux = nil
	return opts, nil
}

// libState holds the library-specific part of Options
type libState struct {
	useDefaultCommand bool
	autoScheme        bool // --scheme was not given, decide when Run is called
	funcs             []ActionFunc
	preview           PreviewFunc
	become            *BecomeRequest

	mutex     sync.Mutex
	err       error
	ctx       context.Context // given to RunContext, nil for Run
	run       runContext      // valid during Run
	terminal  *Terminal
	cancelled bool // ctx was done before the terminal was attached
}

// useDefaultCommand is consulted by the reader. Only one Run is expected to
// be active at a time, so it is fine to keep it global.
var useDefaultCommand = true

func defaultCommand() string {
	if !useDefaultCommand {
		return ""
	}
	return os.Getenv("FZF_DEFAULT_COMMAND")
}

// prepare is called by Run after the options are post-processed. s is nil
// when the options were not created by Parse. The returned function is
// deferred until Run returns.
func (s *libState) prepare(opts *Options) func() {
	useDefaultCommand = s == nil || s.useDefaultCommand
	if s == nil {
		return func() {}
	}
	// Same heuristic as ParseOptions: the built-in walker produces file paths
	if s.autoScheme && opts.Input == nil && !opts.hasReloadOrTransformOnStart() && util.IsTty(os.Stdin) {
		opts.Scheme = "path"
		_, opts.Criteria, _ = parseScheme(opts.Scheme)
	}
	s.become = nil
	s.mutex.Lock()
	s.err = nil
	s.terminal = nil
	s.cancelled = false
	s.mutex.Unlock()
	return s.start(opts)
}

// setError records the error Run returns. The first one wins, as it is the
// cause of fzf quitting.
func (s *libState) setError(err error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.err == nil {
		s.err = err
	}
}

// error returns the error that made fzf quit, if it was caused by a Go callback
func (s *libState) error() error {
	if s == nil {
		return nil
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.err
}

// BecomeRequest is the command requested by the 'become' action. In library
// mode fzf does not replace the current process; Run returns ExitBecome and
// the caller decides what to do with the command.
type BecomeRequest struct {
	// Command with the placeholders replaced, to be run with $SHELL
	Command string
	// Env is the environment fzf would have used, including FZF_* variables
	Env []string
}

// Become returns the command requested by the 'become' action during the
// last Run, or nil
func (o *Options) Become() *BecomeRequest {
	if o.lib == nil {
		return nil
	}
	return o.lib.become
}

// Bind adds key bindings in the --bind syntax, e.g. "ctrl-r:reload(ls)+first"
func (o *Options) Bind(spec string) error {
	return parseKeymap(o.Keymap, spec)
}

// Entry is an item of the list
type Entry struct {
	// Index is the 0-based position of the item in the input
	Index int
	// Text of the item. ANSI color codes are removed when --ansi is set.
	Text string
}

// State is a snapshot of fzf's state given to Go callbacks
type State struct {
	Query  string
	Prompt string
	// Key is the name of the key or event that triggered the action
	Key string
	// Current is the item under the cursor, or nil if there is no match
	Current *Entry
	// Selected are the items selected in --multi mode
	Selected   []Entry
	TotalCount int
	MatchCount int
	// PreviewLines and PreviewColumns are the size of the preview window.
	// Only set for preview functions.
	PreviewLines   int
	PreviewColumns int
	// Env holds the FZF_* variables fzf exports to child processes
	// (FZF_POS, FZF_ACTION, FZF_PREVIEW_LABEL, ...), see the man page
	Env map[string]string
}

func (t *Terminal) newLibState(env []string, current *Item, selected []*Item) *State {
	s := &State{Env: make(map[string]string)}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, "FZF_") {
			s.Env[k] = v
		}
	}
	atoi := func(key string) int {
		n, _ := strconv.Atoi(s.Env[key])
		return n
	}
	s.Query = s.Env["FZF_QUERY"]
	s.Prompt = s.Env["FZF_PROMPT"]
	s.Key = s.Env["FZF_KEY"]
	s.TotalCount = atoi("FZF_TOTAL_COUNT")
	s.MatchCount = atoi("FZF_MATCH_COUNT")
	s.PreviewLines = atoi("FZF_PREVIEW_LINES")
	s.PreviewColumns = atoi("FZF_PREVIEW_COLUMNS")

	// minItem is used as a stand-in when there is no match
	if current != nil && current != &minItem {
		s.Current = &Entry{int(current.Index()), current.AsString(t.ansi)}
	}
	// The list for {+} falls back to the current item when nothing is selected
	if atoi("FZF_SELECT_COUNT") > 0 {
		for _, item := range selected {
			s.Selected = append(s.Selected, Entry{int(item.Index()), item.AsString(t.ansi)})
		}
	}
	return s
}

// libActionState returns the state for an action. t.mutex must be held.
func (t *Terminal) libActionState() *State {
	sels := t.sortSelected()
	items := make([]*Item, len(sels))
	for i, sel := range sels {
		items[i] = sel.item
	}
	return t.newLibState(t.environ(), t.currentItem(), items)
}

// fail makes fzf quit with ExitError, and Run return err
func (t *Terminal) fail(err error) {
	t.lib.setError(err)
	t.reqBox.Set(reqFatal, nil)
}
