package reasonr

import (
	"github.com/tycheg/reasonr-compiler/internal/apicontract"
	"github.com/tycheg/reasonr-compiler/internal/apidoc"
)

// The descriptions of a model's API for its clients: OpenAPI, MCP tools and
// the views of released API versions (RFC §31.3). A document is a JSON
// object as map[string]any.

// OpenAPI is the OpenAPI document of the model's HTTP API: with published,
// only what the API contract publishes (outside test mode).
func OpenAPI(sc *Schema, published bool) map[string]any { return apidoc.OpenAPI(sc, published) }

// IntegrationsOpenAPI is the OpenAPI document of the requests the model
// sends to the integration service.
func IntegrationsOpenAPI(sc *Schema) map[string]any { return apidoc.Integrations(sc) }

// Tool is the MCP tool of the method mi of type ti.
func Tool(sc *Schema, ti *TypeInfo, mi *MethodInfo) map[string]any { return apidoc.Tool(sc, ti, mi) }

// RawJSON reports whether a value of type t is passed as JSON as it is (a
// json field) rather than by its schema.
func RawJSON(t Type) bool { return apidoc.RawJSON(t) }

// APIView is the schema as a client of the released API version v sees it:
// former names, defaults of new parameters.
func APIView(sc *Schema, v *APIVersion) *Schema { return apicontract.View(sc, v) }

// APISnapshot is the API version the schema publishes on date.
func APISnapshot(sc *Schema, api *API, date string) APIVersion {
	return apicontract.Snapshot(sc, api, date)
}
