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
	CapScopedUnique     Capability = "uniqueness scoped by other fields (unique per tenant, per workspace)"
	CapChildUnique      Capability = "uniqueness of a collection entry (index per owner + its 409 binding)"
	CapGeneratedTests   Capability = "generated unit tests"
	CapPerChild         Capability = "per-entry child operations"
	CapAssignedField    Capability = "server-assigned fields (from the caller's identity)"
	CapDerivedField     Capability = "server-derived fields (computed from the entity's own, kept out of every write DTO)"
	CapMountedChild     Capability = "a shared identity's collection, exposed on a second role"
	CapGroupedFact      Capability = "per-group facts, computed by the database (GROUP BY)"
	CapCompositeVO      Capability = "composite value objects (a value spanning several columns)"
	CapManualVO         Capability = "hand-written value objects (declared here, written by you): a scalar with kind: manual, a composite with written: manual"
	CapArchiveOnUpdate  Capability = "an update that finishes as an archive (CompleteAsArchive)"
	CapComputedRead     Capability = "computed read fields (derived per document, no column)"
	CapPerEntryComputed Capability = "computed read fields on a COLLECTION ENTRY (derived per entry, no column)"
	CapGuardRule        Capability = "guard rules (a barrier that ends the validation pass once something has been rejected)"
	CapReadJoin         Capability = "read joins (reaching another aggregate across a foreign key: on the entity for the rules, and on a relational read model for filter/sort/projection)"
)

// implemented is the honest inventory of this build. Phase F1 ships the
// vertical slice; later phases flip these on as their golden lane goes green.
var implemented = map[Capability]bool{
	CapFlat:           true,
	CapRulesDSL:       true,
	CapManualRules:    true,
	CapREST:           true,
	CapRelationalView: true,

	CapValueObjects:     true,
	CapService:          true,
	CapGroupedFact:      true,
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
	CapScopedUnique:     true,
	CapChildUnique:      true,
	CapGeneratedTests:   true,
	CapPerChild:         true,
	CapAssignedField:    true,
	CapDerivedField:     true,
	CapMountedChild:     true,
	CapCompositeVO:      true,
	CapManualVO:         true,
	CapArchiveOnUpdate:  true,
	CapComputedRead:     true,
	CapPerEntryComputed: true,
	CapReadJoin:         true,
	CapGuardRule:        true,
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
		CapChildUnique, CapPerEntryFact, CapGeneratedTests, CapPerChild,
		CapAssignedField, CapDerivedField, CapMountedChild, CapGroupedFact, CapCompositeVO,
		CapManualVO, CapArchiveOnUpdate,
		CapComputedRead, CapPerEntryComputed, CapReadJoin, CapGuardRule,
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
			for _, fl := range fa.Filters {
				if _, _, dotted := ChildFactField(s, fl); dotted {
					uses(CapPerEntryFact, fmt.Sprintf("service.facts[%d].filters", i),
						"a fact asked per entry of a collection")
				}
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
