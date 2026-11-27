// Package diag describes compiler diagnostics: codes, messages, notes,
// fixes and output for people and for agents (JSON).
package diag

import (
	"fmt"
	"sort"

	"github.com/tycheg/reasonr-compiler/internal/source"
)

// Message is a message template (fmt) with arguments. A Message argument
// is formatted with its own arguments.
type Message struct {
	Format string
	Args   []any
}

// String formats the message.
func (m Message) String() string {
	if len(m.Args) == 0 {
		return m.Format
	}
	return fmt.Sprintf(m.Format, m.Args...)
}

// Severity is the severity of a diagnostic.
type Severity uint8

const (
	Error Severity = iota
	Warning
)

var severityText = [...]string{
	Error:   "error",
	Warning: "warning",
}

func (s Severity) String() string { return severityText[s] }

// Code is a diagnostic code, such as E0504 or W1002.
type Code string

// Diagnostic is one diagnostic.
type Diagnostic struct {
	Spec  *Spec
	Range source.Range
	Args  []any // arguments of the Spec.Format template
	Notes []Note
	Fixes []Fix
}

// Note explains a diagnostic, possibly at its own position.
type Note struct {
	Range source.Range // may be empty
	Msg   Message
}

// Fix is a fix: a set of text edits applied together.
type Fix struct {
	Msg   Message
	Edits []Edit
}

// Edit replaces a range with text. An empty range (Start == End) is an insertion.
type Edit struct {
	Range   source.Range
	NewText string
}

// Code returns the diagnostic code.
func (d *Diagnostic) Code() Code { return d.Spec.Code }

// Severity returns the diagnostic severity.
func (d *Diagnostic) Severity() Severity { return d.Spec.Severity }

// Message returns the diagnostic text.
func (d *Diagnostic) Message() string {
	return Message{Format: d.Spec.Format, Args: d.Args}.String()
}

// Note adds a note and returns d for chaining.
func (d *Diagnostic) Note(r source.Range, format string, args ...any) *Diagnostic {
	d.Notes = append(d.Notes, Note{Range: r, Msg: Message{Format: format, Args: args}})
	return d
}

// Hint adds a hint without a text edit.
func (d *Diagnostic) Hint(format string, args ...any) *Diagnostic {
	d.Fixes = append(d.Fixes, Fix{Msg: Message{Format: format, Args: args}})
	return d
}

// Fix adds a fix with text edits.
func (d *Diagnostic) Fix(msg Message, edits ...Edit) *Diagnostic {
	d.Fixes = append(d.Fixes, Fix{Msg: msg, Edits: edits})
	return d
}

// Bag collects the diagnostics of a phase.
type Bag struct {
	list []*Diagnostic
}

// Add adds a diagnostic with code spec and returns it, so that notes and
// fixes can be added.
func (b *Bag) Add(spec *Spec, r source.Range, args ...any) *Diagnostic {
	d := &Diagnostic{Spec: spec, Range: r, Args: args}
	b.list = append(b.list, d)
	return d
}

// Append adds ready diagnostics.
func (b *Bag) Append(list ...*Diagnostic) { b.list = append(b.list, list...) }

// List returns the collected diagnostics.
func (b *Bag) List() []*Diagnostic { return b.list }

// Len returns the number of diagnostics.
func (b *Bag) Len() int { return len(b.list) }

// HasErrors reports whether the diagnostics include errors.
func (b *Bag) HasErrors() bool { return HasErrors(b.list) }

// HasErrors reports whether the diagnostics include errors.
func HasErrors(list []*Diagnostic) bool {
	for _, d := range list {
		if d.Severity() == Error {
			return true
		}
	}
	return false
}

// Sort orders diagnostics by file, position and code. Positions in a file
// set grow in the order files are added, so the order is stable.
func Sort(list []*Diagnostic) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Range.Start != b.Range.Start {
			return a.Range.Start < b.Range.Start
		}
		return a.Spec.Code < b.Spec.Code
	})
}
