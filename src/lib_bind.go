package fzf

// fzf.lib: Go functions bound to keys and events

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// BindMode controls how a Go binding is run
type BindMode int

const (
	// BindSync runs the function on fzf's event loop, like 'transform'. The
	// interface does not respond while it runs, so it should return quickly.
	BindSync BindMode = iota

	// BindBackground runs the function in a separate goroutine, like
	// 'bg-transform'. The returned actions are performed when it completes,
	// unless 'bg-cancel' was triggered in the meantime.
	BindBackground

	// BindForeground suspends the interface while the function runs, like
	// 'execute', so that it can use the terminal, e.g. to start an editor.
	BindForeground
)

// ActionFunc is a Go function bound to a key or event. It returns the
// actions to perform afterwards in the --bind syntax, e.g.
// "reload(...)+change-prompt(> )", or an empty string for none.
//
// ctx is cancelled when fzf quits, when the context given to RunContext is
// done, and for BindBackground also on 'bg-cancel'. Long-running functions
// should return then; their result is discarded.
//
// A panic in the function makes Run return ExitError with the panic as the
// error.
type ActionFunc func(ctx context.Context, state *State) string

// Action types of Go bindings. They are kept away from the upstream
// enumeration so that new upstream actions never collide with them.
const (
	actLibFunc actionType = 1<<16 + iota
	actLibFuncBg
	actLibFuncFg
)

var libActionNames = map[actionType]string{
	actLibFunc:   "go",
	actLibFuncBg: "bg-go",
	actLibFuncFg: "execute-go",
}

func libActionName(a actionType) (string, bool) {
	name, ok := libActionNames[a]
	return name, ok
}

func isLibAction(a actionType) bool {
	return a >= actLibFunc && a <= actLibFuncFg
}

// BindFunc binds a Go function to one or more comma-separated keys or events
// (e.g. "ctrl-f", "alt-x,f5", "focus", "change"), replacing the actions
// previously bound to them, including those from $FZF_DEFAULT_OPTS.
func (o *Options) BindFunc(keys string, mode BindMode, fn ActionFunc) error {
	if o.lib == nil {
		return errors.New("BindFunc requires Options created by Parse")
	}
	if fn == nil {
		return errors.New("BindFunc requires a function")
	}
	if mode < BindSync || mode > BindForeground {
		return fmt.Errorf("invalid bind mode: %d", mode)
	}
	chords, _, err := parseKeyChords(keys, "key name required")
	if err != nil {
		return err
	}
	o.lib.funcs = append(o.lib.funcs, fn)
	index := strconv.Itoa(len(o.lib.funcs) - 1)
	for key := range chords {
		o.Keymap[key] = []*action{{t: actLibFunc + actionType(mode), a: index}}
	}
	return nil
}

// callLib calls a bound function, turning a panic into a fatal error. The
// output of a cancelled function is discarded, so that its actions cannot
// race with fzf quitting.
func (t *Terminal) callLib(ctx context.Context, fn ActionFunc, state *State) (out string) {
	defer func() {
		if r := recover(); r != nil {
			out = ""
			t.fail(fmt.Errorf("panic in Go binding for %s: %v", state.Key, r))
		} else if ctx.Err() != nil {
			out = ""
		}
	}()
	return fn(ctx, state)
}

// runLibFunc runs the Go function of action a and passes its output to
// callback, which performs the returned actions. t.mutex must be held.
func (t *Terminal) runLibFunc(a action, callback func(string)) {
	index, _ := strconv.Atoi(a.a)
	fn := t.lib.funcs[index]
	state := t.libActionState()
	ctx := t.lib.context(a.t == actLibFuncBg)

	switch a.t {
	case actLibFunc:
		callback(t.callLib(ctx, fn, state))
	case actLibFuncFg:
		// Same sequence as executeCommand for a foreground process
		t.executing.Set(true)
		t.mutex.Unlock()
		t.uiMutex.Lock()
		t.tui.Pause(true)
		out := t.callLib(ctx, fn, state)
		t.tui.Resume(true, false)
		t.mutex.Lock()
		t.fullRedraw()
		t.flush()
		t.uiMutex.Unlock()
		t.executing.Set(false)
		callback(out)
	case actLibFuncBg:
		// Queued like captureAsync, so that dispatchAsync applies the same
		// concurrency limits and bg-cancel discards stale results
		version := t.bgVersion
		item := func(proceed bool) {
			if proceed {
				out := t.callLib(ctx, fn, state)
				t.callbackChan <- versionedCallback{version, func() { callback(out) }}
			}
		}
		t.bgQueue[a] = append(t.bgQueue[a], item)
	}
}
