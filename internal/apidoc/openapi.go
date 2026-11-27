// Package apidoc builds OpenAPI 3.1 descriptions of a model's API from its
// schema, without the engine: the model API (/openapi.json, reasonr api
// openapi) and the integration service contract (RFC §30–31).
package apidoc

import (
	"sort"
	"strconv"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// Obj is a JSON object with keys in alphabetical order (encoding/json).
type Obj = map[string]any

type obj = Obj

// OpenAPI builds the description of a model's API: paths of public commands
// and queries, schemas of value objects, enums and records, responses per
// RFC §31.3. published limits it to the methods of the external API contract
// (RFC §31.3.1); deprecated ones are marked deprecated with an x-sunset date.
func OpenAPI(sc *model.Schema, published bool) Obj {
	g := &oaGen{s: sc, schemas: obj{}}
	g.schemas["Notification"] = obj{"type": "object", "required": []string{"name", "type", "args"},
		"properties": obj{"name": obj{"type": "string"}, "type": obj{"type": "string", "description": "Type that declares the notification"},
			"args": obj{"type": "object"}}}
	g.schemas["BusinessError"] = obj{"type": "object", "required": []string{"name"},
		"properties": obj{"name": obj{"type": "string"}, "type": obj{"type": "string", "description": "Type that declares the error; absent for built-in errors"},
			"args": obj{"type": "object"}}}
	g.schemas["Accepted"] = obj{"type": "object", "required": []string{"status", "position", "notifications"},
		"properties": obj{
			"status":        obj{"const": "accepted"},
			"position":      obj{"type": "string", "pattern": "^[0-9]+$", "description": "Position of the unit of work: pass it in after of queries"},
			"notifications": obj{"type": "array", "items": ref("Notification"), "description": "Notifications to the command initiator"},
		}}
	g.schemas["Rejected"] = obj{"type": "object", "required": []string{"status", "errors"},
		"properties": obj{"status": obj{"const": "rejected"}, "errors": obj{"type": "array", "items": ref("BusinessError")}}}
	g.schemas["Failed"] = obj{"type": "object", "required": []string{"status", "error", "incident"},
		"properties": obj{"status": obj{"const": "failed"}, "error": obj{"const": "internalError"}, "incident": obj{"type": "string"}}}
	paths := obj{}
	for i := range sc.Types {
		ti := &sc.Types[i]
		switch ti.Kind {
		case model.TypeEnum, model.TypeValue:
			g.schemas[ti.Name] = g.typeSchema(ti)
		}
		for j := range ti.Methods {
			mi := &ti.Methods[j]
			full := ti.Name + "." + mi.Name
			if !mi.Public || published && !sc.API.Published(full) {
				continue
			}
			var path, method string
			var op obj
			switch {
			case mi.Kind == model.MethodCommand && mi.Create:
				// A creation command: no identifier, the engine issues it.
				path, method, op = "/api/"+apiName(ti)+"/"+mi.Name, "post", g.command(ti, mi)
			case mi.Kind == model.MethodCommand && ti.ID != model.IDNone:
				path, method, op = "/api/"+apiName(ti)+"/{id}/"+mi.Name, "post", g.command(ti, mi)
			case mi.Kind == model.MethodQuery && mi.Result != nil:
				path, method, op = "/api/"+apiName(ti)+"/"+mi.Name, "get", g.query(ti, mi)
			default:
				continue
			}
			if dep := sc.API.Deprecation(full); dep != nil {
				op["deprecated"] = true
				op["x-sunset"] = dep.Sunset
			}
			paths[path] = obj{method: op}
		}
	}
	title := "ReasonR API"
	if sc.Extension != "" {
		title += " (" + sc.Extension + ")"
	}
	return obj{
		"openapi": "3.1.0",
		"info":    obj{"title": title, "version": strconv.FormatUint(sc.Version, 10)},
		"servers": []any{obj{"url": "/"}},
		"paths":   paths,
		"components": obj{
			"schemas":         g.schemas,
			"securitySchemes": obj{"bearer": obj{"type": "http", "scheme": "bearer", "bearerFormat": "JWT"}},
		},
		"security": []any{obj{"bearer": []string{}}},
	}
}

type oaGen struct {
	s       *model.Schema
	schemas obj
}

func ref(name string) obj { return obj{"$ref": "#/components/schemas/" + name} }

func content(schema any) obj { return obj{"application/json": obj{"schema": schema}} }

// responses are the common responses of commands and queries.
func responses(ok obj) obj {
	rejected := func(d string) obj { return obj{"description": d, "content": content(ref("Rejected"))} }
	return obj{
		"200": ok,
		"400": rejected("invalidArgument: an argument failed typing or check"),
		"401": obj{"description": "Not authenticated"},
		"403": rejected("forbidden: the caller failed the access check"),
		"404": obj{"description": "No such type, method or route"},
		"422": rejected("Business errors"),
		"500": obj{"description": "Runtime failure", "content": content(ref("Failed"))},
	}
}

func (g *oaGen) command(ti *model.TypeInfo, mi *model.MethodInfo) obj {
	props, required := obj{}, []string{}
	for k := range mi.Params {
		p := &mi.Params[k]
		props[p.Name] = g.schema(p.Type)
		if !p.Type.Optional && p.Default == nil {
			required = append(required, p.Name)
		}
	}
	body := obj{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		body["required"] = required
	}
	params := []any{
		obj{"name": "Idempotency-Key", "in": "header", "schema": obj{"type": "string"},
			"description": "Retry key: a retry with the same key gets the same response"},
	}
	accepted := obj{"description": "Command accepted", "content": content(ref("Accepted"))}
	if mi.Create {
		accepted = obj{"description": "Command accepted; id is the identifier the engine issued", "content": content(obj{
			"allOf": []any{ref("Accepted"), obj{"type": "object", "required": []string{"id"},
				"properties": obj{"id": g.schema(model.Type{Kind: model.KindID, Ref: ti.Ref})}}},
		})}
	} else {
		params = append([]any{obj{"name": "id", "in": "path", "required": true, "description": "Identifier of " + ti.Name,
			"schema": g.schema(model.Type{Kind: model.KindID, Ref: ti.Ref})}}, params...)
	}
	return obj{
		"operationId": ti.Name + "." + mi.Name,
		"summary":     "Command " + ti.Name + "." + mi.Name,
		"tags":        []string{ti.Name},
		"parameters":  params,
		"requestBody": obj{"required": false, "content": content(body)},
		"responses":   responses(accepted),
	}
}

func (g *oaGen) query(ti *model.TypeInfo, mi *model.MethodInfo) obj {
	params := []any{}
	for k := range mi.Params {
		p := &mi.Params[k]
		if p.Type.Kind == model.KindPageRequest {
			params = append(params,
				obj{"name": "size", "in": "query", "schema": obj{"type": "integer", "minimum": 1, "maximum": 100, "default": 20}},
				obj{"name": "cursor", "in": "query", "schema": obj{"type": "string"}})
			continue
		}
		param := obj{"name": p.Name, "in": "query", "required": !p.Type.Optional && p.Default == nil}
		t := p.Type
		t.Optional = false
		if RawJSON(t) && t.Kind != model.KindInt && t.Kind != model.KindFloat && t.Kind != model.KindBool {
			param["content"] = content(g.schema(t))
		} else {
			param["schema"] = g.schema(t)
		}
		params = append(params, param)
	}
	params = append(params, obj{"name": "after", "in": "query", "schema": obj{"type": "string", "pattern": "^[0-9]+$"},
		"description": "Respond once asynchronous projections have applied this position"})
	r := responses(obj{"description": "Query result", "content": content(g.schema(*mi.Result))})
	r["503"] = obj{"description": "notReady: asynchronous projections have not caught up with after", "content": content(ref("Rejected")),
		"headers": obj{"Retry-After": obj{"schema": obj{"type": "integer"}}}}
	return obj{"operationId": ti.Name + "." + mi.Name, "summary": "Query " + ti.Name + "." + mi.Name,
		"tags": []string{ti.Name}, "parameters": params, "responses": r}
}

// typeSchema is the schema of an enum or a value object.
func (g *oaGen) typeSchema(ti *model.TypeInfo) obj {
	if ti.Kind == model.TypeEnum {
		return obj{"type": "string", "enum": ti.Enum}
	}
	return g.record(ti)
}

func (g *oaGen) record(ti *model.TypeInfo) obj {
	props, required := obj{}, []string{}
	for k := range ti.Fields {
		f := &ti.Fields[k]
		props[f.Name] = g.schema(f.Type)
		if !f.Type.Optional && f.Default == nil {
			required = append(required, f.Name)
		}
	}
	sort.Strings(required)
	o := obj{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

// schema is the JSON Schema of a value of type t in canonical JSON.
func (g *oaGen) schema(t model.Type) any {
	if t.Optional {
		t.Optional = false
		return obj{"anyOf": []any{g.schema(t), obj{"type": "null"}}}
	}
	switch t.Kind {
	case model.KindInt:
		return obj{"type": "integer", "format": "int32"}
	case model.KindBigint:
		return obj{"type": "string", "pattern": "^-?[0-9]+$", "format": "int64"}
	case model.KindFloat:
		return obj{"type": "number", "format": "double"}
	case model.KindDecimal:
		return obj{"type": "string", "pattern": "^-?[0-9]+(\\.[0-9]+)?$", "format": "decimal"}
	case model.KindString:
		return obj{"type": "string"}
	case model.KindBool:
		return obj{"type": "boolean"}
	case model.KindBytes:
		return obj{"type": "string", "contentEncoding": "base64"}
	case model.KindUUID, model.KindTimerID:
		return obj{"type": "string", "format": "uuid"}
	case model.KindDate:
		return obj{"type": "string", "format": "date"}
	case model.KindTime:
		return obj{"type": "string", "pattern": "^[0-2][0-9]:[0-5][0-9]:[0-5][0-9](\\.[0-9]+)?$"}
	case model.KindTimestamp:
		return obj{"type": "string", "format": "date-time"}
	case model.KindDuration:
		return obj{"type": "string", "pattern": "^-?([0-9]+(d|h|m|s|ms|us|ns))+$"}
	case model.KindEnum, model.KindRecord:
		ti := g.s.Type(t.Ref)
		if ti == nil {
			return obj{}
		}
		if ti.Kind == model.TypeEnum || ti.Kind == model.TypeValue {
			return ref(ti.Name)
		}
		name := ti.Name + "Record"
		if _, ok := g.schemas[name]; !ok {
			g.schemas[name] = obj{} // against recursion
			g.schemas[name] = g.record(ti)
		}
		return ref(name)
	case model.KindID:
		return g.id(t.Ref)
	case model.KindList, model.KindSet:
		o := obj{"type": "array", "items": g.schema(*t.Elem)}
		if t.Kind == model.KindSet {
			o["uniqueItems"] = true
		}
		return o
	case model.KindMap:
		key := *t.Key
		if key.Kind == model.KindString || key.Kind == model.KindEnum || key.Kind == model.KindID && g.stringID(key.Ref) {
			return obj{"type": "object", "additionalProperties": g.schema(*t.Elem)}
		}
		return obj{"type": "array", "items": obj{"type": "array", "prefixItems": []any{g.schema(key), g.schema(*t.Elem)},
			"minItems": 2, "maxItems": 2}}
	case model.KindPage:
		return obj{"type": "object", "required": []string{"items", "next"}, "properties": obj{
			"items": obj{"type": "array", "items": g.schema(*t.Elem)},
			"next":  obj{"anyOf": []any{obj{"type": "string"}, obj{"type": "null"}}, "description": "Cursor of the next page"},
		}}
	case model.KindPageRequest:
		return obj{"type": "object", "required": []string{"size"}, "properties": obj{
			"size": obj{"type": "integer", "minimum": 1, "maximum": 100}, "cursor": obj{"anyOf": []any{obj{"type": "string"}, obj{"type": "null"}}}}}
	}
	return obj{}
}

// base is the type whose identifier a derived one holds.
func (g *oaGen) base(t model.TypeRef) *model.TypeInfo {
	ti := g.s.Type(t)
	for range 64 {
		if ti == nil || ti.ID != model.IDDerived {
			return ti
		}
		ti = g.s.Type(ti.IDType)
	}
	return nil
}

func (g *oaGen) stringID(t model.TypeRef) bool {
	ti := g.base(t)
	return ti != nil && ti.ID == model.IDGiven && ti.IDType == 0
}

func (g *oaGen) id(t model.TypeRef) any {
	ti := g.base(t)
	if ti == nil {
		return obj{}
	}
	switch ti.ID {
	case model.IDUUID:
		return obj{"type": "string", "format": "uuid"}
	case model.IDSequence:
		return obj{"type": "string", "pattern": "^[0-9]+$"}
	case model.IDGiven:
		if ti.IDType != 0 {
			return g.schema(model.Type{Kind: model.KindRecord, Ref: ti.IDType})
		}
		return obj{"type": "string"}
	}
	return obj{}
}

// apiName is the type name in an API path: the full name (Ordering.Order).
func apiName(ti *model.TypeInfo) string { return ti.Name }

// RawJSON reports whether a query argument of type t is passed in the query
// string as JSON text (numbers, booleans, records, collections) rather than as is.
func RawJSON(t model.Type) bool {
	switch t.Kind {
	case model.KindInt, model.KindFloat, model.KindBool, model.KindRecord, model.KindList,
		model.KindSet, model.KindMap, model.KindPageRequest:
		return true
	}
	return false
}
