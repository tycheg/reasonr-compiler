package tools

import (
	"encoding/json"
	"io"
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/check"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// PIIReport is the personal data map by subject (reasonr pii).
type PIIReport struct {
	Subjects []PIISubject `json:"subjects"`
}

// PIISubject is the data of one subject. Type "" means any subject
// (personal parameters of external requests).
type PIISubject struct {
	Type    string    `json:"type"`
	Fields  []string  `json:"fields,omitempty"`  // Type.field
	Params  []string  `json:"params,omitempty"`  // Type.method(parameter)
	Flows   []PIIFlow `json:"flows,omitempty"`   // where the values go
	Forgets []string  `json:"forgets,omitempty"` // where the subject is forgotten
}

// PIIFlow is a flow: in member In, value Value goes into To.
type PIIFlow struct {
	In     string `json:"in"`
	Value  string `json:"value"`
	Kind   string `json:"kind"` // event, field, command, request, projection, notification, result, error
	Target string `json:"target"`
	Name   string `json:"name,omitempty"`
}

var flowKinds = map[check.FlowKind]string{
	check.FlowEvent: "event", check.FlowField: "field", check.FlowCommand: "command", check.FlowRequest: "request",
	check.FlowUpsert: "projection", check.FlowNotification: "notification", check.FlowResult: "result",
	check.FlowError: "error",
}

// PII builds the personal data map of a model.
func PII(m *pipeline.Model) *PIIReport {
	pm, _ := m.Checks.Of(check.PII).(*check.PIIMap)
	r := &PIIReport{Subjects: []PIISubject{}}
	if pm == nil {
		return r
	}
	subjects := map[string]*PIISubject{}
	get := func(d *types.Decl) *PIISubject {
		name := ""
		if d != nil {
			name = d.Name
		}
		s := subjects[name]
		if s == nil {
			s = &PIISubject{Type: name}
			subjects[name] = s
		}
		return s
	}
	for _, f := range pm.Fields {
		s := get(f.Subject)
		s.Fields = append(s.Fields, f.Owner.Name+"."+f.Var.Name)
	}
	for _, p := range pm.Params {
		s := get(p.Subject)
		s.Params = append(s.Params, p.Owner.Name+"."+p.Method.Name+"("+p.Var.Name+")")
	}
	for _, f := range pm.Flows {
		flow := PIIFlow{
			In: memberLabel(f.In, f.Member), Value: flowValue(m, f), Kind: flowKinds[f.Kind], Target: f.Target, Name: f.Name,
		}
		for _, d := range f.Subjects {
			s := get(d)
			s.Flows = append(s.Flows, flow)
		}
	}
	for _, f := range pm.Forgets {
		s := get(f.Subject)
		s.Forgets = append(s.Forgets, memberLabel(f.In, f.Member))
	}
	for _, s := range subjects {
		r.Subjects = append(r.Subjects, *s)
	}
	sort.Slice(r.Subjects, func(i, j int) bool {
		// Any subject comes last.
		a, b := r.Subjects[i].Type, r.Subjects[j].Type
		if (a == "") != (b == "") {
			return b == ""
		}
		return a < b
	})
	return r
}

func memberLabel(d *types.Decl, m ast.Member) string {
	return Member{Decl: d, Node: m}.Name()
}

// flowValue is the text of a flow value: an argument or an assigned expression.
func flowValue(m *pipeline.Model, f check.Flow) string {
	switch n := f.Node.(type) {
	case *ast.AssignStmt:
		return m.Source(rangeOf(n.Value))
	case *ast.ReturnStmt:
		return m.Source(rangeOf(n.Result))
	}
	return m.Source(rangeOf(f.Node))
}

func rangeOf(n ast.Node) source.Range { return source.Range{Start: n.Pos(), End: n.End()} }

// WriteJSON prints the map as JSON.
func (r *PIIReport) WriteJSON(w io.Writer) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

var (
	lblSubject    = "Subject %s"
	lblAnySubject = "Any subject (external request parameters)"
	lblPIIFields  = "fields"
	lblPIIParams  = "parameters"
	lblFlows      = "flows"
	lblForgets    = "forgotten in"
	lblNoPII      = "The model has no personal data."
	flowTexts     = map[string]string{
		"event":        "event %s(%s)",
		"field":        "field %s.%s",
		"command":      "command %s(%s)",
		"request":      "external request %s(%s)",
		"projection":   "projection %s.%s",
		"notification": "notification %s(%s)",
		"result":       "result of %s%s",
		"error":        "error %s(%s)",
	}
)

// WriteText prints the map for a person.
func (r *PIIReport) WriteText(w io.Writer) error {
	var b strings.Builder
	t := func(x string, args ...any) string { return diag.Message{Format: x, Args: args}.String() }
	if len(r.Subjects) == 0 {
		b.WriteString(t(lblNoPII) + "\n")
	}
	for i, s := range r.Subjects {
		if i > 0 {
			b.WriteByte('\n')
		}
		if s.Type == "" {
			b.WriteString(t(lblAnySubject) + "\n")
		} else {
			b.WriteString(t(lblSubject, s.Type) + "\n")
		}
		list := func(label string, items []string) {
			if len(items) > 0 {
				b.WriteString("  " + t(label) + ": " + strings.Join(items, ", ") + "\n")
			}
		}
		list(lblPIIFields, s.Fields)
		list(lblPIIParams, s.Params)
		if len(s.Flows) > 0 {
			b.WriteString("  " + t(lblFlows) + ":\n")
			for _, f := range s.Flows {
				b.WriteString("    " + f.In + ": " + f.Value + " → " + t(flowTexts[f.Kind], f.Target, f.Name) + "\n")
			}
		}
		list(lblForgets, s.Forgets)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
