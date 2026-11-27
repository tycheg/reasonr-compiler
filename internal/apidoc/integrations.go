package apidoc

import (
	"strconv"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// Integrations builds the integration service contract (RFC §18):
// a POST /requests/{System}/{request} operation for every external system
// request, with a JSON Schema of its arguments (without timeout), the
// engine headers and its JWT; x-reasonr-replies lists the reply commands of
// the model API (/openapi.json) open to the system role explicitly.
func Integrations(sc *model.Schema) Obj {
	g := &oaGen{s: sc, schemas: obj{}}
	paths := obj{}
	for i := range sc.Types {
		ti := &sc.Types[i]
		switch ti.Kind {
		case model.TypeEnum, model.TypeValue:
			g.schemas[ti.Name] = g.typeSchema(ti)
		case model.TypeExternal:
			replies := g.replies(ti.Role)
			for j := range ti.Methods {
				if mi := &ti.Methods[j]; mi.Kind == model.MethodRequest {
					paths["/requests/"+ti.Short()+"/"+mi.Name] = obj{"post": g.request(ti, mi, replies)}
				}
			}
		}
	}
	used := obj{}
	g.collect(paths, used)
	title := "ReasonR: integration service"
	if sc.Extension != "" {
		title += " (" + sc.Extension + ")"
	}
	return obj{
		"openapi": "3.1.0",
		"info": obj{"title": title, "version": strconv.FormatUint(sc.Version, 10),
			"description": "Engine requests to the integration service. The engine waits only for a 2xx: the system answers with a public command of the model API " +
				"(/openapi.json) from the system role, at once or later. Retries come with the same Idempotency-Key; answer a retry " +
				"with a command with the same key. Requests are delivered independently and in parallel; order is not guaranteed."},
		"servers": []any{obj{"url": "{integrations}", "variables": obj{"integrations": obj{"default": "http://localhost:8090",
			"description": "Integration service address: the url of the integrations.default or integrations.<System> transport"}}}},
		"paths": paths,
		"components": obj{
			"schemas": used,
			"securitySchemes": obj{"engine": obj{"type": "http", "scheme": "bearer", "bearerFormat": "JWT",
				"description": "Engine JWT, ES256: iss is the engine, aud the external system, tenant, jti the idempotency key, iat, exp. " +
					"Public keys: GET /.well-known/jwks.json of the engine, the key is chosen by kid."}},
		},
		"security": []any{obj{"engine": []string{}}},
	}
}

// request is the operation for request mi of external system ti.
func (g *oaGen) request(ti *model.TypeInfo, mi *model.MethodInfo, replies []any) obj {
	props, required := obj{}, []string{}
	for k := range mi.Params {
		if k == mi.TimeoutParam-1 {
			continue
		}
		p := &mi.Params[k]
		props[p.Name] = g.schema(p.Type)
		if !p.Type.Optional {
			required = append(required, p.Name)
		}
	}
	body := obj{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		body["required"] = required
	}
	header := func(name, desc string, required bool, schema obj) obj {
		return obj{"name": name, "in": "header", "required": required, "description": desc, "schema": schema}
	}
	params := []any{
		header("Idempotency-Key", "Request key: the same for all attempts", true, obj{"type": "string"}),
		header("X-Reasonr-Tenant", "Data tenant: the system's answer goes to it (a system token for all tenants names it with the same header)",
			true, obj{"type": "string"}),
		header("X-Reasonr-Attempt", "Attempt number, from 1", true, obj{"type": "integer", "minimum": 1}),
	}
	if mi.TimeoutParam != 0 {
		params = append(params, header("X-Reasonr-Deadline",
			"Answer deadline: the time of the model's timeout timer; after it the model has already reacted and delivery stops. No header means no deadline",
			false, obj{"type": "string", "format": "date-time"}))
	}
	op := obj{
		"operationId": ti.Name + "." + mi.Name,
		"summary":     "Request " + ti.Name + "." + mi.Name,
		"tags":        []string{ti.Name},
		"parameters":  params,
		"requestBody": obj{"required": true, "content": content(body)},
		"responses": obj{
			"2XX": obj{"description": "Request accepted: the service carries it out on its own"},
			"default": obj{"description": "Any other response is retried with the same Idempotency-Key per integrations.retry: " +
				"while the timeout timer is alive, or up to max_elapsed without a timer; then the request is dropped"},
		},
	}
	if ti.Role != "" {
		op["description"] = "The system answers with model API commands from role " + ti.Role + "."
		op["x-reasonr-role"] = ti.Role
		op["x-reasonr-replies"] = replies
	}
	return op
}

// replies lists the model's public commands whose access names role role
// explicitly: this is how a system answers (RFC §18).
func (g *oaGen) replies(role string) []any {
	out := []any{}
	if role == "" {
		return out
	}
	for i := range g.s.Types {
		ti := &g.s.Types[i]
		if ti.ID == model.IDNone {
			continue
		}
		for j := range ti.Methods {
			mi := &ti.Methods[j]
			if mi.Kind != model.MethodCommand || !mi.Public {
				continue
			}
			for _, a := range mi.Access {
				if a.Kind == model.AccessRole && a.Role == role {
					out = append(out, obj{"operationId": ti.Name + "." + mi.Name, "path": "/api/" + apiName(ti) + "/{id}/" + mi.Name})
					break
				}
			}
		}
	}
	return out
}

// collect moves into used the schemas v refers to, and those they refer to.
func (g *oaGen) collect(v any, used obj) {
	switch x := v.(type) {
	case obj:
		if r, ok := x["$ref"].(string); ok {
			name := strings.TrimPrefix(r, "#/components/schemas/")
			if _, seen := used[name]; !seen {
				used[name] = g.schemas[name]
				g.collect(g.schemas[name], used)
			}
		}
		for _, e := range x {
			g.collect(e, used)
		}
	case []any:
		for _, e := range x {
			g.collect(e, used)
		}
	}
}
