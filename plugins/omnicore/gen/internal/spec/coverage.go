package spec

import (
	"fmt"
	"strings"
)

// The coverage gate is how INV-1 ("nothing generated half-way") is enforced in
// practice: the spec LANGUAGE is complete from day one, but each build declares
// which parts of it the emitters actually cover. Anything not covered is
// REFUSED with the phase that will bring it — never accepted and quietly dropped.
//
// A phase lands by flipping a flag here and deleting the matching refusal. The
// manifest test asserts that every declared capability is either implemented or
// refused, so a capability cannot fall through the gap.

// Capability names one slice of the language.
type Capability string

const (
	CapFlat             Capability = "flat storage"
	CapSharedBase       Capability = "shared-base role"
	CapValueObjects     Capability = "value objects"
	CapChildren         Capability = "child collections"
	CapSiblings         Capability = "sibling facets (with their clear path on both surfaces)"
	CapRulesDSL         Capability = "the declarative rule set"
	CapManualRules      Capability = "hand-written rules (hook file)"
	CapService          Capability = "domain service"
	CapMongoView        Capability = "mongo-backed view"
	CapRelationalView   Capability = "relational-backed view"
	CapREST             Capability = "REST surface"
	CapGraphQL          Capability = "GraphQL surface"
	CapExports          Capability = "CSV/XLSX exports"
	CapFieldRestrict    Capability = "field-level read restriction"
	CapIdentityView     Capability = "shared identity view"
	CapOwnerAccess      Capability = "owner-only data access (read filter AND write guard)"
	CapTenantAccess     Capability = "tenant data access (read filter AND write guard)"
	CapScopeBypass      Capability = "a permission that crosses the row scope (operator support)"
	CapPerEntryFact     Capability = "per-entry facts (a service question about ONE entry of a collection)"
	CapBatchedFact      Capability = "batched per-entry facts (ONE question about the whole collection, answered per entry — the loop leaves the rule and the answer stays keyed to the entry that caused it)"
	CapArchivedScope    Capability = "facts asked about the ARCHIVED rows alone (scope: archivedOnly)"
	CapJoinedFact       Capability = "facts narrowed by a field a read join brings in from another aggregate"
	CapScopedUnique     Capability = "uniqueness scoped by other fields (unique per tenant, per workspace)"
	CapChildUnique      Capability = "uniqueness of a collection entry (index per owner + its 409 binding)"
	CapGeneratedTests   Capability = "generated unit tests"
	CapPerChild         Capability = "per-entry child operations"
	CapPerChildPatch    Capability = "partial change of ONE collection entry (PATCH over the entry: the caller sends what moves, everything else — the business identity first — is read off what is stored)"
	CapAssignedField    Capability = "server-assigned fields (from the caller's identity)"
	CapClientIPField    Capability = "server-assigned fields from the REQUEST'S ORIGIN (the network address the framework resolved, written on insert, out of every write DTO — and empty for a write that did not come from an inbound request)"
	CapDerivedField     Capability = "server-derived fields (computed from the entity's own, kept out of every write DTO)"
	CapStampedField     Capability = "framework-stamped fields (the domain says WHEN, the framework supplies the VALUE: a nullable timestamp bound with the write's own instant, or a per-row counter incremented under the row's lock — never written from the struct, and out of every write DTO; a counter declared nullable is emitted over *int64, the one shape StampNull can clear)"
	CapMountedChild     Capability = "a shared identity's collection, exposed on a second role"
	CapGroupedFact      Capability = "per-group facts, computed by the database (GROUP BY)"
	CapFactCriteria     Capability = "facts narrowed by the full criteria vocabulary (a comparison other than equality, a set, an OR, or a value pinned in the spec)"
	CapMultiAggregate   Capability = "facts answering SEVERAL numbers in one query (count, sum, avg, min and max over the same rows, in a single pass)"
	CapStampedFilter    Capability = "facts narrowed by a framework-stamped column (CreatedAt, UpdatedAt, DeletedAt) — a time window, or the archived rows alone"
	CapIdentityFilter   Capability = "facts narrowed by the aggregate id (ID) — the framework's own fixed logical name, so a manual fact's body receives the id instead of re-deriving it from a natural key"
	CapCompositeVO      Capability = "composite value objects (a value spanning several columns)"
	CapManualVO         Capability = "hand-written value objects (declared here, written by you): a scalar with kind: manual, a composite with written: manual"
	CapArchiveOnUpdate  Capability = "an update that finishes as an archive (CompleteAsArchive)"
	CapComputedRead     Capability = "computed read fields (derived per document, no column)"
	CapPerEntryComputed Capability = "computed read fields on a COLLECTION ENTRY (derived per entry, no column)"
	CapGuardRule        Capability = "guard rules (a barrier that ends the validation pass once something has been rejected)"
	CapRedactedField    Capability = "redacted fields (the real value stays in the column; every copy the framework makes of the row carries a mask)"
	CapReadJoin         Capability = "read joins (reaching another aggregate across a foreign key: on the entity for the rules, and on a relational read model for filter/sort/projection)"
	CapReadJoinChain    Capability = "chained read joins (a traversal continued past its target with then, to any depth: every hop's fields land on the same struct, absence follows the PATH, and the whole chain reports absent together)"
	CapBodyRuntime      Capability = "body-sourced runtime fields (a value the caller sends that reaches the entity for a rule to check and never a column — a password confirmation)"
	CapBypassMaySet     Capability = "a server-assigned scope that yields to the bypass (the operator crossing the row scope states which tenant a new row belongs to)"
	CapManualRuntime    Capability = "manual runtime fields (the aggregate carries it, this generator fills it from nowhere, and no write DTO, command or OpenAPI schema mentions it — for a hand-written operation sharing a mode with a generated verb)"
	CapRenderedRuntime  Capability = "a runtime value rendered in the response of the write that minted it (a machine credential the caller receives once, whose hash is all the row keeps)"
	CapIdentityRuntime  Capability = "identity-sourced runtime fields (the caller's subject, tenant, a permission they hold, the super-admin grant or their mere presence, carried onto the entity so a rule can read it without a ctx the domain does not have)"
	CapAPIDocs          Capability = "caller-facing prose in the OpenAPI document (multi-line markdown on every operation, or on one, appended to the sentence the generator writes for the verb — plus each field's own description on the query parameters it is filterable by)"
	CapConflictAnswer   Capability = "shaping a unique field's conflict answer (which field it is reported against, and whether it carries the value that collided — including a COMPOSITE echoing its whole value through String(), which no single part could stand for)"
)

// implemented is the honest inventory of this build. Phase F1 ships the
// vertical slice; later phases flip these on as their golden lane goes green.
var implemented = map[Capability]bool{
	CapFlat:           true,
	CapAPIDocs:        true,
	CapConflictAnswer: true,
	CapRulesDSL:       true,
	CapManualRules:    true,
	CapREST:           true,
	CapRelationalView: true,

	CapValueObjects:     true,
	CapService:          true,
	CapGroupedFact:      true,
	CapFactCriteria:     true,
	CapMultiAggregate:   true,
	CapStampedFilter:    true,
	CapIdentityFilter:   true,
	CapChildren:         true,
	CapSiblings:         true,
	CapSharedBase:       true,
	CapIdentityView:     false, // F3
	CapMongoView:        true,
	CapGraphQL:          true,
	CapExports:          true,
	CapFieldRestrict:    true,
	CapOwnerAccess:      true,
	CapTenantAccess:     true,
	CapScopeBypass:      true,
	CapPerEntryFact:     true,
	CapBatchedFact:      true,
	CapArchivedScope:    true,
	CapJoinedFact:       true,
	CapScopedUnique:     true,
	CapChildUnique:      true,
	CapGeneratedTests:   true,
	CapPerChild:         true,
	CapPerChildPatch:    true,
	CapAssignedField:    true,
	CapClientIPField:    true,
	CapDerivedField:     true,
	CapStampedField:     true,
	CapMountedChild:     true,
	CapCompositeVO:      true,
	CapManualVO:         true,
	CapArchiveOnUpdate:  true,
	CapComputedRead:     true,
	CapPerEntryComputed: true,
	CapReadJoin:         true,
	CapReadJoinChain:    true,
	CapGuardRule:        true,
	CapRedactedField:    true,
	CapBodyRuntime:      true,
	CapBypassMaySet:     true,
	CapIdentityRuntime:  true,
	CapManualRuntime:    true,
	CapRenderedRuntime:  true,
}

// phaseOf names the phase that will deliver a capability, so a refusal tells the
// author when to expect it instead of just saying no.
var phaseOf = map[Capability]string{
	CapIdentityView: "F3",
}

// Implemented reports whether this build covers a capability.
func Implemented(c Capability) bool { return implemented[c] }

// AllCapabilities is the closed list, used by the manifest test.
func AllCapabilities() []Capability {
	return []Capability{
		CapFlat, CapSharedBase, CapValueObjects, CapChildren, CapSiblings,
		CapRulesDSL, CapManualRules, CapService, CapMongoView, CapRelationalView,
		CapREST, CapGraphQL, CapExports, CapFieldRestrict, CapIdentityView,
		CapOwnerAccess, CapTenantAccess, CapScopeBypass, CapScopedUnique,
		CapChildUnique, CapPerEntryFact, CapBatchedFact, CapArchivedScope, CapJoinedFact,
		CapGeneratedTests, CapPerChild, CapPerChildPatch,
		CapAssignedField, CapClientIPField, CapDerivedField, CapStampedField, CapMountedChild, CapGroupedFact, CapFactCriteria,
		CapMultiAggregate, CapStampedFilter, CapIdentityFilter,
		CapCompositeVO,
		CapManualVO, CapArchiveOnUpdate,
		CapComputedRead, CapPerEntryComputed, CapReadJoin, CapReadJoinChain, CapGuardRule,
		CapRedactedField, CapBodyRuntime, CapBypassMaySet, CapIdentityRuntime,
		CapManualRuntime, CapRenderedRuntime,
		CapAPIDocs, CapConflictAnswer,
	}
}

// CheckCoverage refuses every part of the spec this build cannot generate in
// full. It runs AFTER Validate, so an author fixes real spec problems before
// meeting a "not in this build yet" message.
func CheckCoverage(s *Spec) *Problems {
	ps := &Problems{}
	uses := func(c Capability, where, what string) {
		if implemented[c] {
			return
		}
		ps.BlockerFix(where,
			fmt.Sprintf("%s is not generated by this build", what),
			fmt.Sprintf("planned for %s — remove it from the spec to generate the rest now",
				phaseOf[c]))
	}

	if len(s.Joins) > 0 {
		uses(CapReadJoin, "joins", "read joins")
	}
	for i := range s.Joins {
		if len(s.Joins[i].Then) > 0 {
			uses(CapReadJoinChain, fmt.Sprintf("joins[%d].then", i), "a chained read join")
			break
		}
	}
	if s.Storage.Kind == "sharedbase-role" {
		uses(CapSharedBase, "storage.kind", "a shared-base role")
	}
	if len(s.ValueObjects) > 0 {
		uses(CapValueObjects, "valueObjects", "value objects")
	}
	for i, f := range s.Fields {
		if f.VO != nil && f.VO.Kind != "" && f.VO.Kind != "none" {
			uses(CapValueObjects, fmt.Sprintf("fields[%d].vo", i), "value-object fields")
			break
		}
	}
	for i, vo := range s.ValueObjects {
		if vo.Kind == "composite" {
			uses(CapCompositeVO, fmt.Sprintf("valueObjects[%d]", i), "composite value objects")
			break
		}
	}
	for i, f := range s.Fields {
		if FromBody(f) {
			uses(CapBodyRuntime, fmt.Sprintf("fields[%d].source", i),
				"a field fed from the request body that is never persisted")
			break
		}
	}
	for i, f := range s.Fields {
		if FromManual(f) {
			uses(CapManualRuntime, fmt.Sprintf("fields[%d].source", i),
				"a field the aggregate carries and no generated write fills")
			break
		}
	}
	for i, f := range s.Fields {
		if len(f.RenderIn) > 0 {
			uses(CapRenderedRuntime, fmt.Sprintf("fields[%d].renderIn", i),
				"a runtime value rendered in a write verb's response")
			break
		}
	}
	for i, f := range s.Fields {
		if IdentitySourceOf(f) != "" {
			uses(CapIdentityRuntime, fmt.Sprintf("fields[%d].source", i),
				"a field fed from the framework's own question about the caller")
			break
		}
	}
	for i, f := range s.Fields {
		if f.Stamped != "" {
			uses(CapStampedField, fmt.Sprintf("fields[%d].stamped", i),
				"a column whose value the framework mints")
			break
		}
	}
	for i, f := range s.Fields {
		if f.BypassMaySet {
			uses(CapBypassMaySet, fmt.Sprintf("fields[%d].bypassMaySet", i),
				"a server-assigned field the row-scope bypass may state")
			break
		}
	}
	if len(s.Children) > 0 {
		uses(CapChildren, "children", "child collections")
	}
	if len(s.Siblings) > 0 {
		uses(CapSiblings, "siblings", "sibling facets")
	}
	if s.Service != nil && s.Service.Required {
		uses(CapService, "service", "a domain service")
		for _, f := range s.Service.Facts {
			if len(f.GroupBy) > 0 {
				uses(CapGroupedFact, "service.facts[].groupBy",
					"a fact computed per group by the database")
				break
			}
		}
	}
	if s.Read.Backing == "mongo" {
		uses(CapMongoView, "read.backing", "a mongo-backed view")
	}
	for i, idx := range s.Read.Indexes {
		if idx.Partial != "" {
			ps.BlockerFix(fmt.Sprintf("read.indexes[%d].partial", i),
				"a partial index is not generated by this build",
				"the framework takes a document FILTER there (a bson.M), and this "+
					"language has no way to write one — a column name is not a filter, and "+
					"guessing what it should mean would index the wrong subset silently. "+
					"Drop it, or use sparse, which indexes the documents that HAVE the field")
		}
	}
	if len(s.Read.FieldRestrict) > 0 {
		uses(CapFieldRestrict, "read.fieldRestrict", "field-level read restriction")
	}
	if len(s.Read.Computed) > 0 {
		uses(CapComputedRead, "read.computed", "computed read fields")
	}
	for i := range s.Children {
		if len(s.Children[i].Computed) > 0 {
			uses(CapPerEntryComputed, fmt.Sprintf("children[%d].computed", i),
				"computed read fields on a collection entry")
			break
		}
	}
	if s.Read.IdentityView != "" && s.Read.IdentityView != "skip" {
		uses(CapIdentityView, "read.identityView", "a shared identity view")
	}
	if s.Surfaces.GraphQL != nil && s.Surfaces.GraphQL.Enabled {
		uses(CapGraphQL, "surfaces.graphql", "a GraphQL surface")
	}
	if s.Surfaces.Exports != nil {
		uses(CapExports, "surfaces.exports", "exports")
	}
	// A vocabulary VALUE can be dead exactly like a field: legal, validated, and
	// handled by no emitter. Refusing it is the difference between "not yet" and
	// silently doing something else — which for an edit strategy means the
	// author asks for per-entry endpoints and receives replace-everything.
	for i, c := range s.Children {
		if c.EditStrategy == "per-child" {
			uses(CapPerChild, fmt.Sprintf("children[%d].editStrategy", i),
				"per-entry child operations")
		}
		if ChildServesPatch(c) {
			uses(CapPerChildPatch, fmt.Sprintf("children[%d].change.shape", i),
				"a partial change of one entry")
		}
		if c.DuplicateNotification != "" && c.EditStrategy != "per-child" {
			ps.BlockerFix(fmt.Sprintf("children[%d].duplicateNotification", i),
				"a per-entry duplicate notification is only raised by a per-entry ADD, "+
					"and this collection is edited by atomic replace",
				"remove it, or set editStrategy: per-child if entries are meant to be "+
					"added one at a time")
		}
		// Same notification, the other way of not having an ADD: the collection
		// IS per-child and operations left the add verb out, so the only code
		// that would raise this is not generated.
		if c.DuplicateNotification != "" && c.EditStrategy == "per-child" &&
			!MountsPerChildOp(c, "add") {
			ps.BlockerFix(fmt.Sprintf("children[%d].duplicateNotification", i),
				"the notification a per-entry ADD raises, on a collection whose "+
					"operations do not mount one",
				"name add in operations, or drop the notification")
		}
		refuseRuleKinds(c.Rules, fmt.Sprintf("children[%d].rules", i), ps)
	}
	refuseRuleKinds(s.Rules, "rules", ps)

	for i, vo := range s.ValueObjects {
		if vo.DescriptionKeys {
			ps.BlockerFix(fmt.Sprintf("valueObjects[%d].descriptionKeys", i),
				"the per-value catalog entries are not asked for by a flag — every enum "+
					"member is registered under the key the framework derives for it "+
					"(\"<Type>.<value>\"), and what fills that entry is the member's own text",
				"remove it and declare valueObjects["+fmt.Sprint(i)+"].members[].text — "+
					"the entry is written from there, and a member left without text falls "+
					"back to its own name")
		}
	}

	switch s.Authz.DataAccess {
	case "owner-only":
		uses(CapOwnerAccess, "authz.dataAccess", "owner-only data access")
	case "tenant":
		uses(CapTenantAccess, "authz.dataAccess", "tenant data access")
	}
	if s.Authz.Bypass != "" {
		uses(CapScopeBypass, "authz.bypass", "a permission that crosses the row scope")
	}

	for i, f := range s.Fields {
		if f.Unique != nil && len(f.Unique.Within) > 0 {
			uses(CapScopedUnique, fmt.Sprintf("fields[%d].unique.within", i),
				"uniqueness scoped by another field")
		}
	}
	for i, c := range s.Children {
		for j, f := range c.Fields {
			if f.Unique != nil {
				uses(CapChildUnique, fmt.Sprintf("children[%d].fields[%d].unique", i, j),
					"uniqueness of a collection entry")
			}
		}
	}
	if s.Service != nil {
		for i, fa := range s.Service.Facts {
			for _, fl := range FactFilterFields(fa.Filters) {
				if _, _, dotted := ChildFactField(s, fl); dotted {
					uses(CapPerEntryFact, fmt.Sprintf("service.facts[%d].filters", i),
						"a fact asked per entry of a collection")
				}
			}
			if fa.PerEntry != "" {
				uses(CapBatchedFact, fmt.Sprintf("service.facts[%d].perEntry", i),
					"a fact asked once about a whole collection and answered per entry")
			}
			if fa.Scope == "archivedOnly" {
				uses(CapArchivedScope, fmt.Sprintf("service.facts[%d].scope", i),
					"a fact asked about the archived rows alone")
			}
			WalkFactFilters(fa.Filters, "", func(n FactFilter, _ string) {
				if _, _, isGroup := n.Group(); isGroup || n.Operator() != "eq" || n.Pinned() {
					uses(CapFactCriteria, fmt.Sprintf("service.facts[%d].filters", i),
						"a fact narrowed by more than equality")
				}
				if ManagedReads.Has(n.Field) && factField(s, n.Field) == nil {
					uses(CapStampedFilter, fmt.Sprintf("service.facts[%d].filters", i),
						"a fact narrowed by a column the framework stamps")
				}
				if n.Field == IdentityName {
					uses(CapIdentityFilter, fmt.Sprintf("service.facts[%d].filters", i),
						"a fact narrowed by the aggregate id")
				}
				if _, _, isJoin := JoinFactField(s, Options{}, n.Field); isJoin {
					uses(CapJoinedFact, fmt.Sprintf("service.facts[%d].filters", i),
						"a fact narrowed by a field a read join brings in")
				}
			})
			if len(fa.Aggregates) > 0 {
				uses(CapMultiAggregate, fmt.Sprintf("service.facts[%d].aggregates", i),
					"a fact answering several numbers in one query")
			}
		}
	}

	ps.Sort()
	return ps
}

// implementedRuleKinds is what the domain emitter actually writes. A kind
// outside it is refused rather than emitted as a comment: a rule that renders
// as a note reads, to anyone skimming, exactly like a rule that runs.
var implementedRuleKinds = map[string]bool{
	"required": true, "immutable": true, "length": true,
	"range": true, "comparison": true, "ownerCheck": true,
	"transition": true, "requiredIf": true,
	"childDuplicate": true, "groupCap": true, "factRange": true,
	"valueObject": true,
}

func refuseRuleKinds(rs Rules, where string, ps *Problems) {
	for i, r := range rs.List {
		w := fmt.Sprintf("%s.list[%d] (%s)", where, i, orUnnamed(r.ID))
		if r.Kind != "" && !implementedRuleKinds[r.Kind] {
			ps.BlockerFix(w+".kind",
				fmt.Sprintf("the %q rule is not generated by this build", r.Kind),
				"express it with an implemented kind ("+implementedKindList()+"), "+
					"or move it to rules.manual, where it becomes a named item in the "+
					"report and a stub in the hook file")
		}
		if r.ActionName != "" {
			ps.BlockerFix(w+".actionName",
				"gating a rule by action name is not generated by this build",
				"scope the rule to a verb instead (archive and unarchive have their own scopes)")
		}
		if len(r.Transitions) > 0 && r.Kind != "transition" {
			ps.BlockerFix(w+".transitions",
				fmt.Sprintf("transitions belong to a transition rule, not to a %q one", r.Kind),
				"set kind: transition, or drop the map")
		}
		if (len(r.GroupBy) > 0 || r.Cap > 0) && r.Kind != "groupCap" {
			ps.BlockerFix(w+".groupBy",
				fmt.Sprintf("a per-group cap belongs to a groupCap rule, not to a %q one", r.Kind),
				"set kind: groupCap, or drop groupBy/cap")
		}
		if r.AdminField != "" && r.Kind != "ownerCheck" {
			ps.BlockerFix(w+".adminField",
				fmt.Sprintf("an administrator bypass is only generated for an ownerCheck, not for a %q rule", r.Kind),
				"move the condition into rules.manual, where the bypass is yours to write")
		}
	}
	for i, mr := range rs.Manual {
		if mr.ActionName != "" {
			ps.BlockerFix(fmt.Sprintf("%s.manual[%d].actionName", where, i),
				"actionName is not carried into the hook file",
				"describe the condition in the description instead — that text is what "+
					"the report tells the implementer to write")
		}
	}
}

func implementedKindList() string {
	out := make([]string, 0, len(implementedRuleKinds))
	for _, k := range RuleKinds.List() {
		if implementedRuleKinds[k] {
			out = append(out, k)
		}
	}
	return strings.Join(out, ", ")
}
