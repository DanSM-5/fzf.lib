package fzf

// fzf.lib: the items fzf printed, as entries instead of strings

import "errors"

// Accepted returns the items fzf printed during the last Run, in the same
// order as Output: the selected items in --multi mode, otherwise the current
// one; the matches with --filter, --select-1 and --exit-0. It is nil when Run
// printed no item, e.g. when it was aborted.
//
// Entry.Index is the position in the input, so a slice of values can be
// indexed with it directly. Entry.Text is the whole item, whatever
// --accept-nth printed to Output.
func (o *Options) Accepted() []Entry {
	if o.lib == nil {
		return nil
	}
	o.lib.mutex.Lock()
	defer o.lib.mutex.Unlock()
	return o.lib.accepted
}

// accept records an item printed to the output. s is nil when the options
// were not created by Parse.
func (s *libState) accept(item *Item, ansi bool) {
	if s == nil {
		return
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.accepted = append(s.accepted, Entry{int(item.Index()), item.AsString(ansi)})
}

// errMintty is returned instead of re-executing the host program through
// winpty, or with MSYS=enable_pcon
var errMintty = errors.New("fzf.lib: fzf cannot use the mintty terminal directly; " +
	"set MSYS=enable_pcon before starting the program (mintty 3.4.5 or later), " +
	"pass --no-winpty (older mintty), or use another terminal such as Windows Terminal")

// checkTerminal reports a terminal that the CLI would only support by
// re-executing itself. s is nil when the options were not created by Parse.
func (s *libState) checkTerminal(opts *Options) error {
	if s == nil || opts.Filter != nil || !needWinpty(opts) {
		return nil
	}
	return errMintty
}
