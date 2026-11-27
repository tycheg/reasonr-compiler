// Package std holds the standard catalog of UI primitives (RFC §22.8;
// RFC §22, decision 3): primitive declarations and default
// primitives for types. The catalog is included in every module
// implicitly, its names cannot be redeclared; platform runtimes implement it.
package std

import _ "embed"

// UIName is the catalog file name in diagnostics and the LSP.
const UIName = "std/ui.rr"

// UI is the source text of the catalog.
//
//go:embed ui.rr
var UI []byte
