package vm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
)

// An acceptance test (RFC §27) is a bytecode function: the VM runs it as a
// regular function, hands the steps (test.*) to the engine driver and checks
// then itself (expect.*): when step errors exactly as a set, notifications
// and external requests as "contains", only the written arguments
// (decision 4 of the plan).

// Driver is the engine a test runs on (RFC §27.3): memory calls the engine
// directly, http goes through the API. Step values lie in its arena and are
// valid until the next driver call.
type Driver interface {
	Command(ctx context.Context, call model.CommandCall) (*Step, error)
	Query(ctx context.Context, call model.QueryCall) (*Step, error)
	Advance(ctx context.Context, d time.Duration) (*Step, error)
	NewID(ctx context.Context, a *model.Arena, t model.TypeRef) (model.Value, error)
}

// Step is the outcome of a step: errors, delivered notifications (To is the
// recipient) and external requests, the query result, the errors of
// commands external system fakes called in the step's cascade.
type Step struct {
	// Unavailable: the action cannot be done through the UI, there is no
	// applicable place (RFC §27.3, item 3). The reason is for the report.
	Unavailable   string
	Errors        []model.BusinessError
	Notifications []model.Notification
	Requests      []model.ExternalRequest
	FakeErrors    []FakeError
	Result        model.Value
	Arena         *model.Arena
}

// FeatureDriver is a driver that turns on a test's feature flags (the
// feature line at the start of a test, RFC §23.4, decision 8): a flag
// is turned on for the whole system before the first step. In a test
// without a feature line all flags are off.
type FeatureDriver interface {
	EnableFeature(ctx context.Context, name string) error
}

// SectionDriver is a driver that cares about the section of a step: UI
// drivers run given through the test API, and when and then through UI
// places (RFC §27.3). Section is called before every step.
type SectionDriver interface {
	Section(s Section)
}

// FakeError is the errors of a model command a fake called (RFC §18,
// decision 11).
type FakeError struct {
	Call   model.FakeCall
	Errors []model.BusinessError
}

// Section is a test section.
type Section uint8

const (
	Given Section = iota
	When
	Then
)

var sectionNames = [...]string{"given", "when", "then"}

func (s Section) String() string { return sectionNames[s] }

// FailureKind is the kind of a test failure.
type FailureKind uint8

const (
	FailStep                FailureKind = iota // a given step or a then query ended with errors (Actual)
	FailFault                                  // a runtime failure (Message)
	FailMissingError                           // the expected error is not among the when errors (Actual)
	FailUnexpectedError                        // a when error was not expected (Actual[0])
	FailMissingNotification                    // the expected notification is missing (Actual: the sent ones)
	FailMissingRequest                         // the expected external request is missing (Actual: the sent ones)
	FailMismatch                               // field Field of the result: Expected, got Actual[0]
	FailNoResult                               // the query result for matches is none
	FailFake                                   // a command a fake called ended with errors (Step is the call, Actual the errors)
	FailUI                                     // the action is not available in the UI: no applicable place (Message)
)

var failureKindNames = [...]string{
	"step", "fault", "missingError", "unexpectedError", "missingNotification", "missingRequest", "mismatch", "noResult", "fake", "ui",
}

func (k FailureKind) String() string { return failureKindNames[k] }

// Failure is a test failure. Values are in language form, identifiers by
// the names of test variables.
type Failure struct {
	Kind     FailureKind
	Section  Section
	File     string // the step's file relative to the module; "" if unknown
	Line     int
	Step     string // the step or check: "[d1].Accept() as d1", "error plateTaken"
	Field    string
	Expected string
	Actual   []string
	Message  string
	// Run is the number of the test run from 1 if there are several runs
	// (RFC §27.3); 0 for the first or only one.
	Run int
	// Driver is the driver of the run (api, web); "" for memory.
	Driver string
}

// StepLog is a test step and its outcome (reasonrd test -v).
type StepLog struct {
	Section       Section
	Text          string
	Errors        []string
	Notifications []string
	Requests      []string
}

// TestResult is the outcome of a test.
type TestResult struct {
	Name     string
	Failures []Failure
	Steps    []StepLog
}

// Passed reports whether the test passed.
func (t *TestResult) Passed() bool { return len(t.Failures) == 0 }

// testRun is the state of a running test.
type testRun struct {
	d       Driver
	f       *bytecode.Func
	base    int // the registers of the test function on the stack
	prog    *bytecode.Program
	res     *TestResult
	pc      int // the address of the current test instruction
	whenPC  int // the address of the when step
	section Section
	steps   int // steps after test.when
	query   string
	// The outcome of the when step in the test arena.
	errors        []model.BusinessError
	expected      []bool // the when error was expected in then
	notifications []model.Notification
	requests      []model.ExternalRequest
	// unavailable: the when step cannot be done through the UI. If the test
	// expects errors, the UI did not allow what the model would forbid: the
	// error expectations are met (unavailableOK); otherwise it is a ui failure.
	unavailable   string
	unavailableOK bool
	faker         *faker // the scenario's range and faker
	phase         Phase
	// stat is the run statistics for perf.stat (performance test checks).
	stat func(key string) (model.Value, error)
}

// errStop: the test stopped after a step failed.
var errStop = errors.New("test stopped")

// Tests returns the names of the program's tests in declaration order.
func (m *Machine) Tests() []string {
	var out []string
	for i := range m.prog.Funcs {
		// "test\x00then…" is a performance test check, not a test.
		if f := &m.prog.Funcs[i]; f.Kind == bytecode.FuncTest && !strings.Contains(f.Name, "\x00") {
			out = append(out, f.Name)
		}
	}
	return out
}

// RunTest runs test name on driver d. error means there is no such test;
// failures and runtime failures are in the result.
func (m *Machine) RunTest(ctx context.Context, name string, d Driver) (*TestResult, error) {
	return m.RunTestWith(ctx, name, d, TestOptions{})
}

// RunTestWith is RunTest with run settings: the faker seed and the phase.
func (m *Machine) RunTestWith(ctx context.Context, name string, d Driver, o TestOptions) (*TestResult, error) {
	fi := m.prog.FuncIndex(bytecode.FuncTest, name)
	if fi == bytecode.NoFunc {
		return nil, fmt.Errorf("%w: test %q", ErrNotFound, name)
	}
	r := m.start(ctx, testEnv{d}, model.Caller{Kind: model.CallerTest}, nil, time.Time{})
	defer func() {
		model.PutArena(r.a)
		r.test = nil
		r.finish()
	}()
	t := &testRun{d: d, f: &m.prog.Funcs[fi], prog: m.prog, res: &TestResult{Name: name}, faker: newFaker(name, o), phase: o.Phase}
	r.test = t
	_, err := r.callSafe(fi)
	switch {
	case errors.Is(err, errStop):
	case err != nil:
		t.fail(Failure{Kind: FailFault, Message: err.Error()})
		if flt, ok := err.(*Fault); ok && flt.Func == t.f.Name && flt.Line > 0 {
			last := &t.res.Failures[len(t.res.Failures)-1]
			last.File, last.Line = flt.File, flt.Line
		}
	default:
		t.section, t.pc = When, t.whenPC
		for i, be := range t.errors {
			if !t.expected[i] {
				t.fail(Failure{Kind: FailUnexpectedError, Actual: []string{r.errorText(&be)}})
			}
		}
		if t.unavailable != "" && !t.unavailableOK {
			t.fail(Failure{Kind: FailUI, Message: t.unavailable})
		}
	}
	return t.res, nil
}

// PerfCheck evaluates then check name of a performance test (a function
// with a bool result, lower.PerfCheck): stat returns a statistics field by
// key lower.PerfKey.
func (m *Machine) PerfCheck(ctx context.Context, name string, stat func(key string) (model.Value, error)) (bool, error) {
	fi := m.prog.FuncIndex(bytecode.FuncTest, name)
	if fi == bytecode.NoFunc {
		return false, fmt.Errorf("%w: check %q", ErrNotFound, name)
	}
	r := m.start(ctx, testEnv{}, model.Caller{Kind: model.CallerTest}, nil, time.Time{})
	defer func() {
		model.PutArena(r.a)
		r.test = nil
		r.finish()
	}()
	r.test = &testRun{f: &m.prog.Funcs[fi], prog: m.prog, res: &TestResult{Name: name}, stat: stat}
	v, err := r.callSafe(fi)
	if err != nil {
		return false, err
	}
	return v.Kind == model.KindBool && v.Bool(), nil
}

// callSafe calls a test function; a VM panic becomes a runtime failure.
func (r *run) callSafe(fi int32) (res model.Value, err error) {
	defer func() {
		if p := recover(); p != nil {
			res, err = model.None, caught(p)
		}
	}()
	return r.call(fi, nil, frame{})
}

func (t *testRun) fail(f Failure) {
	f.Section = t.section
	f.File, f.Line = t.prog.Position(t.f, t.pc)
	t.res.Failures = append(t.res.Failures, f)
}

// testEnv is the Env of a test function: identifiers only (new T.Id).
type testEnv struct{ d Driver }

func (e testEnv) Load(context.Context, *model.Arena, model.TypeRef, model.Value) (model.Record, uint64, bool, error) {
	return model.Record{}, 0, false, ErrUnsupported
}

func (e testEnv) Find(context.Context, *model.Arena, *model.FindSpec) (model.Rows, error) {
	return model.Rows{}, ErrUnsupported
}

func (e testEnv) NewID(ctx context.Context, a *model.Arena, t model.TypeRef) (model.Value, error) {
	return e.d.NewID(ctx, a, t)
}

// testFeature turns feature flag i on in the test driver.
func (r *run) testFeature(i int) error {
	name := r.m.schema.Features[i].Name
	fd, ok := r.test.d.(FeatureDriver)
	if !ok {
		return fmt.Errorf("the test driver does not turn on feature flags: %s", name)
	}
	if err := fd.EnableFeature(r.ctx, name); err != nil {
		return fmt.Errorf("feature flag %s: %w", name, err)
	}
	return nil
}

// testCaller is the caller of a step: an actor (the as register) or a role.
func (r *run) testCaller(s *bytecode.Site, actor uint32, regs []model.Value) (model.Caller, string) {
	c := model.Caller{Kind: model.CallerTest, Subject: model.None}
	if actor != bytecode.NoReg {
		c.Subject = regs[actor]
		c.Actor = c.Subject.Type
		return c, r.show(c.Subject)
	}
	if s.Role != "" {
		c.Roles = []string{s.Role}
	}
	return c, s.Role
}

// stepArgs is a record of all method parameters: the written site
// arguments with fixed, the rest none (the interpreter substitutes defaults).
func (r *run) stepArgs(s *bytecode.Site, fixed int, regs []model.Value) model.Record {
	params := r.m.schema.Type(s.Type).Methods[s.Method].Params
	m := r.a.Mark()
	for range params {
		r.a.Push(model.None)
	}
	for i, arg := range s.Args[fixed:] {
		f := i
		if s.Fields != nil {
			f = int(s.Fields[i])
		}
		r.a.SetStack(m+f, regs[arg])
	}
	return r.a.End(model.KindRecord, 0, m).Record()
}

// begin marks a step: when is the first step after test.when, then follows.
func (t *testRun) begin() {
	if t.section == Given {
		return
	}
	t.steps++
	if t.steps == 1 {
		t.section, t.whenPC = When, t.pc
	} else {
		t.section = Then
	}
}

// enter tells the driver the section of a step.
func (t *testRun) enter() {
	if sd, ok := t.d.(SectionDriver); ok {
		sd.Section(t.section)
	}
}

// testStep handles the outcome of a step: the log, given and then errors, the when outcome.
func (r *run) testStep(text string, st *Step, err error) error {
	t := r.test
	if err != nil {
		t.fail(Failure{Kind: FailFault, Step: text, Message: err.Error()})
		return errStop
	}
	if st.Unavailable != "" {
		t.res.Steps = append(t.res.Steps, StepLog{Section: t.section, Text: text, Errors: []string{st.Unavailable}})
		if t.section != When {
			t.fail(Failure{Kind: FailUI, Step: text, Message: st.Unavailable})
			return errStop
		}
		t.unavailable = text + ": " + st.Unavailable
		t.errors, t.expected, t.notifications, t.requests = nil, nil, nil, nil
		return nil
	}
	log := StepLog{Section: t.section, Text: text}
	errs := make([]model.BusinessError, len(st.Errors))
	for i, be := range st.Errors {
		errs[i] = model.BusinessError{Owner: be.Owner, Name: be.Name, Args: r.a.Copy(st.Arena, be.Args.Value()).Record()}
		log.Errors = append(log.Errors, r.errorText(&errs[i]))
	}
	ns := make([]model.Notification, len(st.Notifications))
	for i, n := range st.Notifications {
		ns[i] = model.Notification{
			Owner: n.Owner, Name: n.Name, Args: r.a.Copy(st.Arena, n.Args.Value()).Record(), To: r.a.Copy(st.Arena, n.To),
		}
		log.Notifications = append(log.Notifications, r.notificationText(&ns[i]))
	}
	rs := make([]model.ExternalRequest, len(st.Requests))
	for i, rq := range st.Requests {
		rs[i] = model.ExternalRequest{System: rq.System, Request: rq.Request, Args: r.a.Copy(st.Arena, rq.Args.Value()).Record()}
		log.Requests = append(log.Requests, r.requestText(&rs[i]))
	}
	t.res.Steps = append(t.res.Steps, log)
	if len(st.FakeErrors) > 0 {
		for _, fe := range st.FakeErrors {
			args := r.a.Copy(st.Arena, fe.Call.Args.Value()).Record()
			ti := r.m.schema.Type(fe.Call.Entity)
			mi := &ti.Methods[fe.Call.Command]
			call := "[" + r.show(r.a.Copy(st.Arena, fe.Call.ID)) + "]." + r.argsText(mi.Name, mi.Params, args) +
				" (" + r.typeName(fe.Call.System) + ")"
			var actual []string
			for _, be := range fe.Errors {
				be.Args = r.a.Copy(st.Arena, be.Args.Value()).Record()
				actual = append(actual, r.errorText(&be))
			}
			t.fail(Failure{Kind: FailFake, Step: call, Actual: actual})
		}
		return errStop
	}
	switch {
	case t.section == When:
		t.errors, t.notifications, t.requests = errs, ns, rs
		t.expected = make([]bool, len(errs))
	case len(errs) > 0:
		t.fail(Failure{Kind: FailStep, Step: text, Actual: log.Errors})
		return errStop
	}
	return nil
}

// testCmd is a step: an entity command on behalf of an actor or a role.
func (r *run) testCmd(s *bytecode.Site, regs []model.Value) error {
	t := r.test
	t.begin()
	t.enter()
	caller, as := r.testCaller(s, s.Args[1], regs)
	var id model.Value
	var text string
	if s.Args[0] == bytecode.NoReg {
		// A fake command goes to its single instance, without as.
		id = model.FakeID(s.Type)
		text = r.typeName(s.Type) + "." + r.callText(r.methodName(s.Type, s.Method), s, 2, regs)
	} else {
		id = regs[s.Args[0]]
		text = "[" + r.show(id) + "]." + r.callText(r.methodName(s.Type, s.Method), s, 2, regs) + " as " + as
	}
	st, err := t.d.Command(r.ctx, model.CommandCall{
		Entity: s.Type, ID: id, Command: s.Method, Args: r.stepArgs(s, 2, regs), Arena: r.a, Caller: caller,
	})
	return r.testStep(text, st, err)
}

// testQuery is a step: a query; the result is copied into the test arena.
func (r *run) testQuery(s *bytecode.Site, regs []model.Value) (model.Value, error) {
	t := r.test
	t.begin()
	t.enter()
	caller, as := r.testCaller(s, s.Args[0], regs)
	text := r.typeName(s.Type) + "." + r.callText(r.methodName(s.Type, s.Method), s, 1, regs) + " as " + as
	t.query = text
	st, err := t.d.Query(r.ctx, model.QueryCall{
		Type: s.Type, Query: s.Method, Args: r.stepArgs(s, 1, regs), Arena: r.a, Caller: caller,
	})
	if err := r.testStep(text, st, err); err != nil {
		return model.None, err
	}
	return r.a.Copy(st.Arena, st.Result), nil
}

// testTime is a time offset ±d step: the scenario clock shifts by d; going
// forward fires the timers that came due (RFC §27.5).
func (r *run) testTime(d model.Value) error {
	t := r.test
	t.begin()
	t.enter()
	text := "time offset +" + string(appendDuration(nil, d.Int()))
	if d.Int() < 0 {
		text = "time offset -" + string(appendDuration(nil, -d.Int()))
	}
	st, err := t.d.Advance(r.ctx, d.Duration())
	return r.testStep(text, st, err)
}

// argsMatch compares the written site arguments (after fixed) with arguments args.
func (r *run) argsMatch(args model.Record, s *bytecode.Site, fixed int, regs []model.Value) bool {
	vals := r.a.Fields(args)
	for i, arg := range s.Args[fixed:] {
		f := i
		if s.Fields != nil {
			f = int(s.Fields[i])
		}
		if f >= len(vals) || !r.a.Equal(vals[f], regs[arg]) {
			return false
		}
	}
	return true
}

// expectError is then: an error of the when step.
func (r *run) expectError(s *bytecode.Site, regs []model.Value) {
	t := r.test
	t.section = Then
	name := s.Name
	if s.Type != 0 {
		name = r.methodName(s.Type, s.Method)
	}
	if t.unavailable != "" {
		t.unavailableOK = true // the UI did not let the action with an error run
		return
	}
	for i := range t.errors {
		be := &t.errors[i]
		if be.Owner == s.Type && be.Name == name && r.argsMatch(be.Args, s, 0, regs) {
			t.expected[i] = true
			return
		}
	}
	actual := make([]string, len(t.errors))
	for i := range t.errors {
		actual[i] = r.errorText(&t.errors[i])
	}
	exp := bare(r.callText(name, s, 0, regs))
	t.fail(Failure{Kind: FailMissingError, Step: "error " + exp, Expected: exp, Actual: actual})
}

// expectNotify is then: a notification of the when step; argument 0 is the recipient or NoReg.
func (r *run) expectNotify(s *bytecode.Site, regs []model.Value) {
	t := r.test
	t.section = Then
	exp := bare(r.callText(r.methodName(s.Type, s.Method), s, 1, regs))
	if s.Args[0] != bytecode.NoReg {
		exp += " to " + r.show(regs[s.Args[0]])
	}
	actual := make([]string, len(t.notifications))
	for i := range t.notifications {
		n := &t.notifications[i]
		if n.Owner == s.Type && n.Name == s.Method && r.argsMatch(n.Args, s, 1, regs) &&
			(s.Args[0] == bytecode.NoReg || r.a.Equal(n.To, regs[s.Args[0]])) {
			return
		}
		actual[i] = r.notificationText(n)
	}
	t.fail(Failure{Kind: FailMissingNotification, Step: "notification " + exp, Expected: exp, Actual: actual})
}

// expectRequest is then: an external request of the when step.
func (r *run) expectRequest(s *bytecode.Site, regs []model.Value) {
	t := r.test
	t.section = Then
	exp := r.typeName(s.Type) + "." + bare(r.callText(r.methodName(s.Type, s.Method), s, 0, regs))
	actual := make([]string, len(t.requests))
	for i := range t.requests {
		rq := &t.requests[i]
		if rq.System == s.Type && rq.Request == s.Method && r.argsMatch(rq.Args, s, 0, regs) {
			return
		}
		actual[i] = r.requestText(rq)
	}
	t.fail(Failure{Kind: FailMissingRequest, Step: "request " + exp, Expected: exp, Actual: actual})
}

// expectMatch is then: the query result matches in the written fields.
func (r *run) expectMatch(s *bytecode.Site, regs []model.Value) {
	t := r.test
	t.section = Then
	ti := r.m.schema.Type(s.Type)
	v := regs[s.Args[0]]
	items := make([]string, 0, len(s.Args)-1)
	fieldOf := func(i int) int {
		if s.Fields != nil {
			return int(s.Fields[i])
		}
		return i
	}
	for i, arg := range s.Args[1:] {
		items = append(items, ti.Fields[fieldOf(i)].Name+": "+r.show(regs[arg]))
	}
	step := t.query + " matches (" + strings.Join(items, ", ") + ")"
	if v.Kind != model.KindRecord {
		t.fail(Failure{Kind: FailNoResult, Step: step, Expected: ti.Name, Actual: []string{r.show(v)}})
		return
	}
	vals := r.a.Elems(v)
	for i, arg := range s.Args[1:] {
		f := fieldOf(i)
		if f < len(vals) && r.a.Equal(vals[f], regs[arg]) {
			continue
		}
		got := model.None
		if f < len(vals) {
			got = vals[f]
		}
		t.fail(Failure{Kind: FailMismatch, Step: step, Field: ti.Fields[f].Name, Expected: r.show(regs[arg]), Actual: []string{r.show(got)}})
	}
}

// ---------------------------------------------------------------------------
// Writing values and steps for messages.

// show returns a value in language form: strings in quotes, identifiers by
// the names of test variables.
func (r *run) show(v model.Value) string {
	r.quote = true
	start := r.a.StringStart()
	r.text(v)
	s := r.a.Text(r.a.StringEnd(start))
	r.quote = false
	return s
}

// varName returns the name of the test variable that holds identifier v.
func (r *run) varName(v model.Value) string {
	t := r.test
	if t == nil {
		return ""
	}
	regs := r.stack[t.base:]
	for i, name := range t.f.RegNames {
		if name != "" && i < len(regs) && regs[i].Kind == model.KindID && r.a.Equal(regs[i], v) {
			return name
		}
	}
	return ""
}

func (r *run) typeName(t model.TypeRef) string {
	if ti := r.m.schema.Type(t); ti != nil {
		return ti.Name
	}
	return "?"
}

func (r *run) methodName(t model.TypeRef, m model.MethodRef) string {
	if ti := r.m.schema.Type(t); ti != nil && int(m) < len(ti.Methods) {
		return ti.Methods[m].Name
	}
	return "?"
}

// callText is "name(param: value, …)" by the written site arguments.
func (r *run) callText(name string, s *bytecode.Site, fixed int, regs []model.Value) string {
	var params []string
	if s.Type != 0 {
		for _, p := range r.m.schema.Type(s.Type).Methods[s.Method].Params {
			params = append(params, p.Name)
		}
	} else {
		params = bytecode.BuiltinErrors[s.Name]
	}
	items := make([]string, 0, len(s.Args)-fixed)
	for i, arg := range s.Args[fixed:] {
		f := i
		if s.Fields != nil {
			f = int(s.Fields[i])
		}
		n := "?"
		if f < len(params) {
			n = params[f]
		}
		items = append(items, n+": "+r.show(regs[arg]))
	}
	return name + "(" + strings.Join(items, ", ") + ")"
}

// bare drops empty parentheses: errors, notifications and requests without
// arguments are written without them.
func bare(call string) string { return strings.TrimSuffix(call, "()") }

// argsText is "name(param: value, …)" by all arguments of a record.
func (r *run) argsText(name string, params []model.FieldInfo, args model.Record) string {
	vals := r.a.Fields(args)
	if len(vals) == 0 {
		return name
	}
	items := make([]string, len(vals))
	for i, v := range vals {
		n := ""
		if i < len(params) {
			n = params[i].Name + ": "
		}
		items[i] = n + r.show(v)
	}
	return name + "(" + strings.Join(items, ", ") + ")"
}

func (r *run) errorText(be *model.BusinessError) string {
	var params []model.FieldInfo
	if ti := r.m.schema.Type(be.Owner); ti != nil {
		for i := range ti.Methods {
			if ti.Methods[i].Kind == model.MethodError && ti.Methods[i].Name == be.Name {
				params = ti.Methods[i].Params
			}
		}
	} else {
		for _, n := range bytecode.BuiltinErrors[be.Name] {
			params = append(params, model.FieldInfo{Name: n})
		}
	}
	return r.argsText(be.Name, params, be.Args)
}

func (r *run) notificationText(n *model.Notification) string {
	mi := &r.m.schema.Type(n.Owner).Methods[n.Name]
	return r.argsText(mi.Name, mi.Params, n.Args) + " to " + r.show(n.To)
}

func (r *run) requestText(rq *model.ExternalRequest) string {
	ti := r.m.schema.Type(rq.System)
	mi := &ti.Methods[rq.Request]
	return ti.Name + "." + r.argsText(mi.Name, mi.Params, rq.Args)
}
