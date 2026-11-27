// Package uigen builds the UI program model.UI from a checked model
// (RFC §22): apps, screens with numbered nodes, places,
// primitives and texts. Omitted parts that bind data are filled in here
// with fields with default primitives (RFC §22.9), so the platform runtime
// needs only the screen description. The input is a model without errors.
package uigen

import (
	"encoding/json"
	"math/big"
	"sort"

	"github.com/tycheg/reasonr-compiler/internal/constant"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// maxAutoDepth is the depth of nested fields the automatic UI builds: a
// value object may refer to itself through T?.
const maxAutoDepth = 4

// Generate builds the UI program.
func Generate(info *sema.Info) *model.UI {
	out := &model.UI{Apps: []model.UIApp{}, Screens: []model.UIScreen{}, Primitives: []model.UIPrimitive{}}
	u := info.UI
	if u == nil {
		return out
	}
	for _, p := range u.Primitives {
		out.Primitives = append(out.Primitives, primitive(p))
	}
	out.Types = []model.UIType{}
	for _, d := range info.DeclList {
		switch d.Kind {
		case types.EnumDecl:
			t := model.UIType{Name: d.Name}
			for _, v := range d.Values {
				t.Values = append(t.Values, v.Name)
			}
			out.Types = append(out.Types, t)
		case types.ValueDecl:
			t := model.UIType{Name: d.Name}
			for _, f := range d.Fields {
				t.Fields = append(t.Fields, model.UIParam{Name: f.Name, Type: f.Type.String()})
			}
			out.Types = append(out.Types, t)
		}
	}
	placeIDs := map[*sema.Place]int{} // node numbers of places on screens
	for _, s := range u.Screens {
		b := &builder{info: info, u: u, apps: s.Apps, ids: map[*sema.Node]int{}}
		us := model.UIScreen{Name: s.Name, Title: s.Title, Nodes: b.nodes(s.Nodes)}
		for _, p := range s.Params {
			us.Params = append(us.Params, model.UIParam{Name: p.Name, Type: p.Type.String()})
		}
		out.Screens = append(out.Screens, us)
		for _, a := range s.Apps {
			for _, pl := range a.Places {
				if pl.Screen == s {
					placeIDs[pl] = b.ids[pl.Node]
				}
			}
		}
	}
	for _, a := range u.Apps {
		b := &builder{info: info, u: u, apps: []*sema.App{a}, ids: map[*sema.Node]int{}}
		ua := model.UIApp{Name: a.Name, Title: a.Title, Language: a.Language, Platforms: a.Platforms,
			Roles: a.Roles, Auto: a.Auto, Nodes: b.nodes(a.Nodes), Screens: []string{}}
		for _, x := range a.Actors {
			ua.Actors = append(ua.Actors, x.Name)
		}
		if a.Me != nil {
			ua.Me = a.Me.Name
		}
		for _, s := range a.Screens {
			ua.Screens = append(ua.Screens, s.Name)
		}
		for _, pl := range a.Places {
			id := b.ids[pl.Node]
			screen := ""
			if pl.Screen != nil {
				id, screen = placeIDs[pl], pl.Screen.Name
			}
			ua.Places = append(ua.Places, model.UIPlace{Name: pl.Name, Screen: screen, Node: id,
				Type: pl.Node.Owner.Name, Method: pl.Node.Method.Name, Command: pl.Node.Method.Kind == types.Command})
		}
		out.Apps = append(out.Apps, ua)
	}
	if len(u.Texts) > 0 {
		out.Texts = map[string]map[string]string{}
		for lang, m := range u.Texts {
			out.Texts[lang] = map[string]string{}
			for key, t := range m {
				out.Texts[lang][key] = t.Value
			}
		}
	}
	return out
}

type builder struct {
	info *sema.Info
	u    *sema.UI
	apps []*sema.App
	ids  map[*sema.Node]int
	next int
}

func (b *builder) nodes(list []*sema.Node) []model.UINode {
	var out []model.UINode
	for _, n := range list {
		out = append(out, b.node(n))
	}
	return out
}

func (b *builder) id(n *sema.Node) int {
	b.next++
	if n != nil {
		b.ids[n] = b.next
	}
	return b.next
}

var nodeKinds = map[sema.NodeKind]string{
	sema.NodePrimitive: model.UIPrimitiveNode, sema.NodePart: model.UIPartNode,
	sema.NodeScreen: model.UIScreenNode, sema.NodeField: model.UIFieldNode, sema.NodeOn: model.UIOnNode,
}

func (b *builder) node(n *sema.Node) model.UINode {
	out := model.UINode{ID: b.id(n), Kind: nodeKinds[n.Kind], Key: n.Key, Label: n.Label, Field: n.Field, Text: n.TextKey}
	if n.Prim != nil {
		out.Primitive = n.Prim.Name
		out.Types = typeNames(n.Subst)
		out.Params = b.params(n.Prim, n.Params)
	}
	if n.Part != nil {
		out.Part = n.Part.Name
		out.Rows = rowPart(n.Parent, n.Part)
	}
	if n.Prim != nil && n.Prim.Children != nil {
		out.Rows = rowPart(n, n.Prim.Children)
	}
	if n.Kind == sema.NodeField {
		out.FieldType = n.FieldType.String()
		out.Input = n.Role == sema.RoleInput
	}
	if n.Method != nil {
		out.Bind = b.bind(n.Owner, n.Method, n.Args)
		if n.Target != nil {
			t := b.arg(n.Target)
			out.Bind.Target = &t
		}
	}
	if n.To != nil {
		out.To = &model.UITransition{Screen: n.To.Name, Args: b.args(n.ToArgs)}
	}
	if n.Event != nil {
		out.Event = n.Event.Name
	}
	out.Back = n.Back
	out.If = b.expr(n.Cond)
	out.Children = b.nodes(n.Children)
	for _, name := range n.Auto {
		out.Children = append(out.Children, b.autoPart(n, name)...)
	}
	return out
}

func (b *builder) bind(owner *types.Decl, m *types.Method, args []*sema.Arg) *model.UIBind {
	out := &model.UIBind{Type: owner.Name, Method: m.Name, Command: m.Kind == types.Command, Args: b.args(args)}
	if m.Kind == types.Query {
		out.Result = m.Result.String()
	}
	return out
}

var sourceNames = map[sema.SourceKind]string{
	sema.SrcExpr: model.UIFromExpr, sema.SrcParam: model.UIFromParam, sema.SrcMe: model.UIFromMe,
	sema.SrcNew: model.UIFromNew, sema.SrcData: model.UIFromData, sema.SrcRow: model.UIFromRow,
	sema.SrcForm: model.UIFromForm, sema.SrcEvent: model.UIFromEvent, sema.SrcProvided: model.UIFromProvided,
	sema.SrcPrimitive: model.UIFromPrimitive, sema.SrcInput: model.UIFromInput,
}

func (b *builder) args(list []*sema.Arg) []model.UIArg {
	var out []model.UIArg
	for _, a := range list {
		out = append(out, b.arg(a))
	}
	return out
}

func (b *builder) arg(a *sema.Arg) model.UIArg {
	out := model.UIArg{Name: a.Name, Type: a.Type.String(), From: sourceNames[a.Source], Input: a.Input, Default: a.Default}
	if a.Default && a.Param != nil {
		if v, ok := b.info.Defaults[a.Param]; ok {
			out.Value = constJSON(v, a.Type)
		}
	}
	switch a.Source {
	case sema.SrcExpr:
		out.Expr = b.expr(a.Code)
	case sema.SrcParam, sema.SrcProvided:
		out.Field = a.Field
	case sema.SrcData, sema.SrcRow, sema.SrcForm, sema.SrcEvent:
		out.Node, out.Field = b.ids[a.From], a.Field
	}
	return out
}

func (b *builder) expr(e *sema.Expr) *model.UIExpr {
	if e == nil {
		return nil
	}
	if e.IsConst {
		return &model.UIExpr{Value: constJSON(e.Const, e.Type)}
	}
	return &model.UIExpr{Func: e.Func, Args: b.args(e.Uses)}
}

func (b *builder) params(p *sema.Primitive, vals map[string]*sema.PrimValue) []model.UIPrimValue {
	var out []model.UIPrimValue
	for _, pp := range p.Params {
		v := vals[pp.Name]
		pv := model.UIPrimValue{Name: pp.Name}
		switch {
		case v == nil:
			continue
		case pp.Kind == sema.ParamValue && v.Value != nil:
			pv.Value = b.expr(v.Value)
		case pp.Kind == sema.ParamValue:
			if pp.Default == nil {
				continue
			}
			pv.Value = &model.UIExpr{Value: constJSON(pp.Default, pp.Type)}
		case pp.Kind == sema.ParamQuery || pp.Kind == sema.ParamSearch:
			if v.Method == nil {
				continue
			}
			pv.Query = b.bind(v.Owner, v.Method, v.Args)
		case pp.Kind == sema.ParamField:
			pv.Field = v.Field
		case pp.Kind == sema.ParamScreen:
			if v.Screen != nil {
				pv.Screen = v.Screen.Name
			}
		}
		out = append(out, pv)
	}
	return out
}

// autoPart builds an omitted part: value fields, query filters or command
// inputs with default primitives (RFC §22.7, §22.9).
func (b *builder) autoPart(n *sema.Node, name string) []model.UINode {
	p := n.Prim
	pt := p.Children
	if name != "children" {
		pt = p.Part(name)
	}
	partID := 0
	if pt != p.Children {
		partID = b.id(nil) // the part number comes before its fields, as for written parts
	}
	var fields []model.UINode
	switch pt.Binds {
	case sema.PartType:
		role := sema.RoleShow
		if p.Binds == sema.BindInput {
			role = sema.RoleInput
		}
		fields = b.autoFields(n.PartType(pt), role, 1)
	case sema.PartParams, sema.PartInputs:
		what := n.Owner.Name + "." + n.Method.Name + "."
		for _, a := range n.Args {
			if a.Input {
				fields = append(fields, b.autoField(a.Name, what+a.Name, a.Type, sema.RoleInput, 1))
			}
		}
		if t := n.Target; t != nil && t.Input {
			fields = append(fields, b.autoField(t.Name, n.Owner.Name, t.Type, sema.RoleInput, 1))
		}
	}
	if pt == p.Children {
		return fields
	}
	return []model.UINode{{ID: partID, Kind: model.UIPartNode, Part: pt.Name, Key: pt.Name, Auto: true, Rows: rowPart(n, pt), Children: fields}}
}

func (b *builder) autoFields(t types.Type, role sema.Role, depth int) []model.UINode {
	v, ok := types.Unwrap(t).(*types.Value)
	if !ok || depth > maxAutoDepth {
		return nil
	}
	var out []model.UINode
	for _, f := range v.Decl.Fields {
		out = append(out, b.autoField(f.Name, v.Decl.Name+"."+f.Name, f.Type, role, depth))
	}
	return out
}

func (b *builder) autoField(name, text string, t types.Type, role sema.Role, depth int) model.UINode {
	out := model.UINode{ID: b.id(nil), Kind: model.UIFieldNode, Field: name, Key: name, Text: text, FieldType: t.String(),
		Input: role == sema.RoleInput, Auto: true}
	d := b.u.DefaultFor(b.apps, t, role)
	if d == nil {
		return out // the check reported error E0707
	}
	out.Primitive = d.Prim.Name
	out.Types = typeNames(d.Prim.Infer(t))
	out.Params = b.params(d.Prim, d.Params)
	if pt := d.Prim.Children; pt != nil && pt.Binds == sema.PartType {
		out.Children = b.autoFields(t, role, depth+1)
	}
	return out
}

// rowPart: part pt of node n binds a row of its page or list.
func rowPart(n *sema.Node, pt *sema.Part) bool {
	if n == nil || n.Method == nil || n.Method.Kind != types.Query || pt.Binds != sema.PartType {
		return false
	}
	var elem types.Type
	switch r := n.Method.Result.(type) {
	case *types.Page:
		elem = r.Elem
	case *types.List:
		elem = r.Elem
	default:
		return false
	}
	return n.PartType(pt).String() == elem.String()
}

func typeNames(m map[string]types.Type) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range m {
		out[k] = v.String()
	}
	return out
}

// constJSON writes a constant in canonical JSON (RFC §31.3).
func constJSON(v constant.Value, t types.Type) json.RawMessage {
	data, _ := json.Marshal(constValue(v, types.Unwrap(t)))
	return data
}

func constValue(v constant.Value, t types.Type) any {
	switch v := v.(type) {
	case constant.None:
		return nil
	case bool, string:
		return v
	case constant.Text:
		return string(v)
	case constant.Enum:
		return v.Name
	case *big.Rat:
		if bt, ok := t.(*types.Basic); ok && (bt.Kind == types.Decimal || bt.Kind == types.Bigint) {
			if v.IsInt() {
				return v.Num().String()
			}
			return v.FloatString(decimals(v))
		}
		if v.IsInt() {
			return json.Number(v.Num().String())
		}
		f, _ := v.Float64()
		return f
	case constant.List:
		var elem types.Type
		switch c := t.(type) {
		case *types.List:
			elem = c.Elem
		case *types.Set:
			elem = c.Elem
		}
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = constValue(x, elem)
		}
		return out
	case constant.Struct:
		out := map[string]any{}
		var decl *types.Decl
		if vt, ok := t.(*types.Value); ok {
			decl = vt.Decl
		}
		names := make([]string, 0, len(v.Fields))
		for k := range v.Fields {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			var ft types.Type
			if decl != nil && decl.Field(k) != nil {
				ft = decl.Field(k).Type
			}
			out[k] = constValue(v.Fields[k], types.Unwrap(ft))
		}
		return out
	}
	return nil
}

// decimals is the number of digits after the point enough for an exact form.
func decimals(r *big.Rat) int {
	for n := 0; n < 30; n++ {
		d := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
		if new(big.Int).Mod(new(big.Int).Mul(r.Num(), d), r.Denom()).Sign() == 0 {
			return n
		}
	}
	return 30
}

func primitive(p *sema.Primitive) model.UIPrimitive {
	out := model.UIPrimitive{Name: p.Name, Std: p.Std, Binds: bindNames[p.Binds]}
	if p.BindType != nil {
		out.BindType = p.BindType.String()
	}
	for _, v := range p.Vars {
		out.Vars = append(out.Vars, model.UITypeVar{Name: v.Name, Constraint: v.Constraint})
	}
	for _, pp := range p.Params {
		up := model.UIPrimParam{Name: pp.Name, Kind: paramKinds[pp.Kind], Optional: pp.Optional, Auto: pp.Auto}
		if pp.Type != nil {
			up.Type = pp.Type.String()
		}
		for _, v := range pp.Provided {
			up.Provided = append(up.Provided, model.UIParam{Name: v.Name, Type: v.Type.String()})
		}
		if pp.Record != nil {
			up.Record = pp.Record.String()
		}
		if pp.FieldType != nil {
			up.FieldType = pp.FieldType.String()
		}
		out.Params = append(out.Params, up)
	}
	if p.Children != nil {
		c := part(p.Children)
		out.Children = &c
	}
	for _, pt := range p.Parts {
		out.Parts = append(out.Parts, part(pt))
	}
	for _, e := range p.Events {
		ue := model.UIEvent{Name: e.Name}
		for _, v := range e.Params {
			ue.Params = append(ue.Params, model.UIParam{Name: v.Name, Type: v.Type.String()})
		}
		out.Events = append(out.Events, ue)
	}
	return out
}

func part(pt *sema.Part) model.UIPart {
	out := model.UIPart{Name: pt.Name, Binds: partNames[pt.Binds]}
	if pt.Type != nil {
		out.Type = pt.Type.String()
	}
	return out
}

var (
	bindNames = map[sema.BindKind]string{
		sema.BindNone: "", sema.BindQuery: "query", sema.BindCommand: "command",
		sema.BindScreen: "screen", sema.BindField: "field", sema.BindInput: "input",
	}
	paramKinds = map[sema.ParamKind]string{
		sema.ParamValue: "value", sema.ParamQuery: "query", sema.ParamSearch: "search",
		sema.ParamField: "field", sema.ParamScreen: "screen",
	}
	partNames = map[sema.PartBinds]string{
		sema.PartNone: "", sema.PartType: "type", sema.PartParams: "params",
		sema.PartInputs: "inputs", sema.PartScreens: "screens",
	}
)
