package apidoc

import (
	"encoding/json"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// Tool builds an MCP tool description (RFC §31.3.2) for a public
// command or query: the name "Ordering.Order.Cancel", the description from
// ##, JSON Schemas of the input (arguments, plus the target id for a
// command other than a creation command) and the output, annotations. The
// schemas are self-contained: types go to $defs.
func Tool(sc *model.Schema, ti *model.TypeInfo, mi *model.MethodInfo) Obj {
	g := &oaGen{s: sc, schemas: obj{}}
	for i := range sc.Types {
		if t := &sc.Types[i]; t.Kind == model.TypeEnum || t.Kind == model.TypeValue {
			g.schemas[t.Name] = g.typeSchema(t)
		}
	}
	props, required := obj{}, []string{}
	if mi.Kind == model.MethodCommand && !mi.Create {
		props["id"] = obj{"description": "Identifier of " + ti.Name, "allOf": []any{g.schema(model.Type{Kind: model.KindID, Ref: ti.Ref})}}
		required = append(required, "id")
	}
	for k := range mi.Params {
		p := &mi.Params[k]
		if p.Type.Kind == model.KindPageRequest {
			props["size"] = obj{"type": "integer", "minimum": 1, "maximum": 100, "default": 20}
			props["cursor"] = obj{"type": "string"}
			continue
		}
		props[p.Name] = g.schema(p.Type)
		if !p.Type.Optional && p.Default == nil {
			required = append(required, p.Name)
		}
	}
	input := obj{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		input["required"] = required
	}
	var output obj
	if mi.Kind == model.MethodQuery && mi.Result != nil {
		output = obj{"type": "object", "required": []string{"result"}, "properties": obj{"result": g.schema(*mi.Result)}}
	} else {
		output = obj{"type": "object", "required": []string{"status"}, "properties": obj{
			"status":        obj{"enum": []string{"accepted", "rejected"}},
			"id":            obj{"description": "Identifier issued by the creation command"},
			"notifications": obj{"type": "array", "items": obj{"type": "object"}},
			"errors":        obj{"type": "array", "items": obj{"type": "object"}},
		}}
	}
	desc := mi.Doc
	if desc == "" {
		desc = map[model.MethodKind]string{model.MethodCommand: "Command ", model.MethodQuery: "Query "}[mi.Kind] + ti.Name + "." + mi.Name
	}
	tool := obj{
		"name":         ti.Name + "." + mi.Name,
		"title":        strings.SplitN(desc, "\n", 2)[0],
		"description":  desc,
		"inputSchema":  g.defs(input),
		"outputSchema": g.defs(output),
		"annotations": obj{
			"readOnlyHint":    mi.Kind == model.MethodQuery,
			"destructiveHint": mi.Kind == model.MethodCommand && mi.Destructive,
			"idempotentHint":  mi.Kind == model.MethodQuery,
		},
	}
	return tool
}

// defs makes a schema self-contained: references to type schemas go to $defs.
func (g *oaGen) defs(schema obj) obj {
	used := obj{}
	g.collect(schema, used)
	if len(used) > 0 {
		schema["$defs"] = used
	}
	b, _ := json.Marshal(schema)
	var out obj
	_ = json.Unmarshal([]byte(strings.ReplaceAll(string(b), "#/components/schemas/", "#/$defs/")), &out)
	return out
}
