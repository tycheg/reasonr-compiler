package diag

import (
	"fmt"
	"regexp"
	"sort"
)

// Spec describes a diagnostic code in the registry.
type Spec struct {
	Code     Code
	Severity Severity
	Title    string // short name: "unterminated string"
	Format   string // message template (fmt); arguments are given when reported
}

var (
	registry   = map[Code]*Spec{}
	codeSyntax = regexp.MustCompile(`^[EW]\d{4}$`)
)

// define registers a code. Called only from codes.go during initialization.
func define(s Spec) *Spec {
	if !codeSyntax.MatchString(string(s.Code)) {
		panic(fmt.Sprintf("diag: invalid code %q", s.Code))
	}
	if (s.Code[0] == 'W') != (s.Severity == Warning) {
		panic(fmt.Sprintf("diag: code %s does not match its severity", s.Code))
	}
	if _, dup := registry[s.Code]; dup {
		panic(fmt.Sprintf("diag: code %s is declared twice", s.Code))
	}
	sp := &s
	registry[s.Code] = sp
	return sp
}

func errorSpec(code Code, title, format string) *Spec {
	return define(Spec{Code: code, Severity: Error, Title: title, Format: format})
}

func warningSpec(code Code, title, format string) *Spec {
	return define(Spec{Code: code, Severity: Warning, Title: title, Format: format})
}

// Lookup returns the description of a code or nil.
func Lookup(c Code) *Spec { return registry[c] }

// All returns all registry codes in ascending order.
func All() []*Spec {
	out := make([]*Spec, 0, len(registry))
	for _, s := range registry {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}
