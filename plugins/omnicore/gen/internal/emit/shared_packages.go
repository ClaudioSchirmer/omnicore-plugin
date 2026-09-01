package emit

import (
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// Where a generated declaration goes when MORE THAN ONE file needs it.
//
// The service layout puts a Result beside its Command and a wire pair beside
// its operation, and that rule answers almost everything: one verb, one file.
// It cannot answer the leftovers — the shape of a child entry is read by the
// root's insert, by the root's update, and by each of the entry's own verbs, so
// there is no single operation it belongs beside. Left in whichever file
// happened to declare it first, it turns that file into a place a reader has to
// already know about; bundled into a per-entity grab-bag, it undoes the
// granularity everywhere else.
//
// So the leftovers get their own seat, split by WHAT they are:
//
//   - dtos/ holds the structures — <Child>Result, <Child>RowResult, the wire
//     <Child>Row/Request/Response. Data, no behavior beyond the mapping methods
//     that ride the type.
//   - utils/ holds the functions several files call — the projectors that read a
//     collection back off the aggregate.
//
// Both are subpackages of the layer that owns them, never a service-wide dump:
// a command's shared shape stays under commands/, a read's under queries/, a
// wire shape under requests/. The layer boundary is the same one the rest of
// the layout draws.
const (
	cmdDTOPkg   = "internal/application/commands/dtos"
	cmdUtilPkg  = "internal/application/commands/utils"
	queryDTOPkg = "internal/application/queries/dtos"
	webDTOPkg   = "internal/web/requests/dtos"
)

// The import aliases those packages are always given.
//
// Aliased unconditionally, even where the bare name would compile: three of the
// four directories are named `dtos`, and `internal/application/dtos` — the
// child INPUT DTOs the layout has always had — is a fourth. A file importing
// two of them needs an alias, and one that spells the alias only sometimes is a
// file whose imports have to be re-derived every time a field is added. The
// alias names the layer, which is the question a reader arrives with: not
// "which dtos package is this", but "whose".
const (
	cmdDTOAlias   = "cmddtos"
	cmdUtilAlias  = "cmdutils"
	queryDTOAlias = "qrydtos"
	webDTOAlias   = "webdtos"
)

// childResultType is the write-side shape of one stored entry, as a file
// OUTSIDE commands/dtos names it.
func childResultType(c ir.Child) string { return cmdDTOAlias + "." + c.Name + "Result" }

// childResultTypeName is the same type as its own package declares it.
func childResultTypeName(c ir.Child) string { return c.Name + "Result" }

// childProjector and childOneProjector are the functions that read a collection
// back off the aggregate — the whole one, and one entry of it. They are
// EXPORTED because they left the commands package: every verb that answers with
// the entry calls the same one, which is the reason they are shared at all.
func childProjector(c ir.Child) string { return cmdUtilAlias + "." + c.Projector }

func childOneProjector(c ir.Child) string { return cmdUtilAlias + "." + childOneProjectorName(c) }

func childOneProjectorName(c ir.Child) string { return "ProjectOne" + c.Name }

// childRowResult is the Result twin of the wire's <Child>Row, as a file outside
// queries/dtos names it.
func childRowResult(c ir.Child) string { return queryDTOAlias + "." + childRowResultName(c) }

func childRowResultName(c ir.Child) string { return c.Name + "RowResult" }

// The wire shapes of one entry, as a file outside requests/dtos names them.
func childWireRow(c ir.Child) string { return webDTOAlias + "." + c.Name + "Row" }

func childWireRequest(c ir.Child) string { return webDTOAlias + "." + c.Name + "Request" }

func childWireResponse(c ir.Child) string { return webDTOAlias + "." + c.Name + "Response" }

// childWireRequestField is the FIELD name an embedded <Child>Request takes.
// Embedding a qualified type names the field after the type alone, so the
// literal that fills it spells the short name while the type spells the long
// one — and a generated composite literal that got that backwards would not
// compile.
func childWireRequestField(c ir.Child) string { return c.Name + "Request" }
