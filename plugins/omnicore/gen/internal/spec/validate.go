package spec

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
)

// Options tunes validation for the few knobs that are genuinely the caller's.
type Options struct {
	// LangFallback downgrades "missing translation" from blocker to warning and
	// lets the emitter mark the gaps. Never silent: the report lists every one.
	LangFallback bool
	// ExistingVOs are the value objects the project already declares — all of
	// them, whoever wrote them. Declaring one again is refused: a second copy of
	// a rule is a rule that can disagree with itself, and reuse is what the spec
	// is for.
	ExistingVOs []string
	// ExistingVOKinds says what each of those value objects IS — "raw" (it
	// writes its own IsValid) or "enum" (membership). It answers the one
	// question a name cannot: a field declared `vo.kind: reuse` names a type
	// this spec never described, and a rule that validates it IN PLACE emits a
	// different call for each kind. A type missing from the map is one the
	// reader could not classify, and the rule is refused rather than guessed.
	ExistingVOKinds map[string]string
	// VOOwner says which entity's spec generated each one ("" = hand-written).
	// It separates the two questions the inventory answers: REFERENCING a value
	// object is open to everybody, REDECLARING one is refused to everybody
	// except the entity that already owns it — whose own re-run must not be
	// refused the file it wrote last time.
	VOOwner map[string]string
	// Neighbours are the names the project's OTHER specs already claim. A
	// collision here is a boot abort, so it is refused while the author is still
	// in the file rather than discovered by starting the service.
	Neighbours []Neighbour
}

// Neighbour is one already-declared entity of the same project.
type Neighbour struct {
	Path     string
	Entity   string
	ViewName string
	Route    string
	// Table and Fields are that entity's ROOT — what a READ JOIN declared here
	// traverses INTO. A join names a column of the target and lands it on a Go
	// field of this entity, so both the column's existence and its type come
	// from the target's own declaration rather than from a restatement.
	Table  string
	Fields []NeighbourField
	// Revision is that entity's optimistic-concurrency column: the one managed
	// column the framework's read path does NOT resolve, and therefore the one a
	// join must be refused for by name rather than by "no such column".
	Revision string
	// Children is what that spec declares its collections to be, so a role that
	// MOUNTS one can be checked against the declaration instead of trusted to
	// have restated it correctly.
	Children []NeighbourChild
}

// NeighbourChild is one collection of a neighbouring spec.
type NeighbourChild struct {
	Name    string
	Table   string
	OwnedBy string
	Fields  []NeighbourField
}

// NeighbourField is one field of it, in the spellings that must match.
type NeighbourField struct {
	Name   string
	Column string
	Type   string
	// Nullable and LivesOn are what a read join has to ask about the target and
	// about its own foreign key: an inner join is legal only over a NON-NULLABLE
	// key, and a traversal reaches the target's OWN table — never its shared
	// base or one of its siblings.
	Nullable bool
	LivesOn  string
}

// Validate checks a spec against the language's closed vocabularies AND against
// the framework's boot contract.
//
// The second half is the point: every check here corresponds to a condition the
// framework would otherwise discover at BOOT (a panic) or at the first write (a
// runtime 500). Catching them statically is INV-3 — a boot trap must never cost
// a boot to find.
func Validate(s *Spec, opt Options) *Problems {
	ps := &Problems{}

	validateHeader(s, ps)
	validateStorage(s, ps)
	validateFields(s, ps, opt)
	validateValueObjects(s, ps, opt)
	validateComposites(s, ps, opt)
	validateChildren(s, ps, opt)
	validateSiblings(s, ps, opt)
	validateLifecycle(s, ps)
	validateRules(s, ps, opt)
	validateNotifications(s, ps, opt)
	validateService(s, opt, ps)
	validateJoins(s, opt, ps)
	validateRead(s, joinReachOf(s, opt), ps)
	validateSurfaces(s, ps)
	validateAuthz(s, ps)

	checkNeighbours(s, opt, ps)
	ps.Sort()
	return ps
}

// checkNeighbours refuses a name another spec of this project already took.
//
// The framework keys its view registry by NAME and aborts the boot when two
// features declare the same one; routes collide the same way. Both are the kind
// of mistake that is obvious once seen and invisible while writing, because the
// other spec is in another file.
func checkNeighbours(s *Spec, opt Options, ps *Problems) {
	view := s.Read.View.Name
	for _, n := range opt.Neighbours {
		if isSelfNeighbour(s, n) {
			continue // this is the spec being checked, found on disk
		}
		// The "this is me" test is by PATH, not by entity name: keyed by name, a
		// second file declaring the same entity read as "my previous run" and
		// sailed past every collision check below — including the entity
		// collision itself, which gets its own refusal here.
		if n.Entity == s.Entity {
			ps.BlockerFix("entity",
				fmt.Sprintf("%s also declares the entity %q", n.Path, s.Entity),
				"an entity is declared by exactly one spec — remove one of the two files")
			continue
		}
		if view != "" && n.ViewName == view {
			ps.BlockerFix("read.view.name",
				fmt.Sprintf("%q is already the view name of %s (%s)", view, n.Entity, n.Path),
				"the framework keys the view registry by name and aborts the boot on a "+
					"duplicate — give this view its own name")
		}
		if s.Plural != "" && n.Route == s.Plural {
			ps.BlockerFix("plural",
				fmt.Sprintf("%q is already the plural of %s (%s), so both entities would "+
					"mount the same route", s.Plural, n.Entity, n.Path),
				"give this entity its own plural — it IS the route path")
		}
	}
}

var goIdentRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
var columnRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validateHeader(s *Spec, ps *Problems) {
	if s.SpecVersion != 1 {
		ps.BlockerFix("specVersion",
			fmt.Sprintf("unsupported spec version %d", s.SpecVersion),
			"this generator speaks specVersion 1")
	}
	if s.Entity == "" {
		ps.Blockerf("entity", "the entity name is required")
	} else if !goIdentRe.MatchString(s.Entity) {
		ps.BlockerFix("entity",
			fmt.Sprintf("%q is not a usable Go type name", s.Entity),
			"use PascalCase with no underscores, e.g. Student")
	}
	if s.Plural == "" {
		ps.BlockerFix("plural",
			"the plural of the entity name is required",
			"it reaches the route path and the listing types, and no rule can spell "+
				"it — declare it as your domain says it, e.g. plural: Matriculas")
	} else if !goIdentRe.MatchString(s.Plural) {
		ps.BlockerFix("plural",
			fmt.Sprintf("%q is not usable as a Go identifier", s.Plural),
			"PascalCase, letters and digits only")
	}
	if s.Language == "" {
		ps.WarnFix("language",
			"no language declared for the human-facing text",
			"set language (e.g. pt-BR) so descriptions and examples are generated consistently")
	}
}

func validateStorage(s *Spec, ps *Problems) {
	st := s.Storage
	if !StorageKinds.Has(st.Kind) {
		ps.BlockerFix("storage.kind",
			fmt.Sprintf("%q is not a storage kind", st.Kind),
			"one of: "+StorageKinds.String())
	}
	if st.Table == "" {
		ps.Blockerf("storage.table", "the table name is required")
	} else if engine := ReservedWord(st.Table); engine != "" {
		ps.BlockerFix("storage.table",
			fmt.Sprintf("the table name %q is a reserved word (%s)", st.Table, engine),
			"rename it — it would apply on some engines and be rejected on others")
	} else if !columnRe.MatchString(st.Table) {
		ps.BlockerFix("storage.table",
			fmt.Sprintf("%q is not a usable table name", st.Table),
			"lowercase, digits and underscores, starting with a letter")
	}
	if st.Description == "" {
		ps.WarnFix("storage.description",
			"the table has no description",
			"it becomes the table comment in the migration — one line is enough")
	}

	// Revision is MANDATORY on an entity/base schema: the framework fails to
	// construct the repository without it. Children and siblings must NOT have
	// it (checked with their own tables).
	if st.Managed.Revision == "" {
		ps.BlockerFix("storage.managed.revision",
			"the revision column is not declared",
			"entity and shared-base schemas require it; declare e.g. revision: revision")
	}
	// The managed columns are columns like any other; a reserved name here
	// breaks the same engines a reserved field column does.
	checkColumnName(st.Managed.Revision, "storage.managed.revision", ps)
	checkColumnName(st.Managed.CreatedAt, "storage.managed.createdAt", ps)
	checkColumnName(st.Managed.UpdatedAt, "storage.managed.updatedAt", ps)
	checkColumnName(st.Managed.ArchivedAt, "storage.managed.archivedAt", ps)

	switch st.Kind {
	case "flat":
		if st.Base != nil {
			ps.BlockerFix("storage.base",
				"a flat entity cannot declare a shared base",
				"either drop the base block or set storage.kind: sharedbase-role")
		}
	case "sharedbase-role":
		if st.Base == nil {
			ps.BlockerFix("storage.base",
				"a shared-base role must declare its base",
				"add the base block with table, naturalKey and link")
			return
		}
		validateBase(s, ps)
	}
}

func validateBase(s *Spec, ps *Problems) {
	b := s.Storage.Base
	if b.Table == "" {
		ps.Blockerf("storage.base.table", "the base table name is required")
	} else {
		checkTableName(b.Table, "storage.base.table", ps)
	}
	if !LinkModels.Has(b.Link) {
		ps.BlockerFix("storage.base.link",
			fmt.Sprintf("%q is not a link model", b.Link),
			"one of: "+LinkModels.String())
	}
	if b.OrphanPolicy != "" && !OrphanPolicies.Has(b.OrphanPolicy) {
		ps.BlockerFix("storage.base.orphanPolicy",
			fmt.Sprintf("%q is not an orphan policy", b.OrphanPolicy),
			"one of: "+OrphanPolicies.String())
	}
	if b.SchemaFunc == "" {
		ps.BlockerFix("storage.base.schemaFunc",
			"the base schema function needs a name",
			"the generator cannot singularise a table, so it asks — e.g. "+
				"schemaFunc: PessoaBase")
	} else if !goIdentRe.MatchString(b.SchemaFunc) {
		ps.BlockerFix("storage.base.schemaFunc",
			fmt.Sprintf("%q is not a usable Go function name", b.SchemaFunc),
			"PascalCase, letters and digits only")
	}
	if b.Link == "separate-fk" {
		if b.LinkColumn == "" {
			ps.BlockerFix("storage.base.linkColumn",
				"a separate-fk link needs the column that points at the identity",
				"a column name is declared, never derived — e.g. linkColumn: pessoa_id")
		} else {
			checkColumnName(b.LinkColumn, "storage.base.linkColumn", ps)
		}
		if !RowUniqueness.Has(b.RowUniqueness) {
			ps.BlockerFix("storage.base.rowUniqueness",
				"a separate-fk link must state how role rows are kept unique",
				"one of: "+RowUniqueness.String())
		}
	} else if b.LinkColumn != "" {
		ps.BlockerFix("storage.base.linkColumn",
			"a shared-pk link has no column to declare",
			"the role's own primary key IS the identity's; remove the key")
	} else if b.RowUniqueness != "" {
		ps.BlockerFix("storage.base.rowUniqueness",
			"rowUniqueness only applies to a separate-fk link",
			"a shared-pk link gets row uniqueness from the primary key; remove the key")
	}

	// The natural key derives the identity's primary key as a deterministic
	// UUIDv5. A nullable key would collapse every key-less record into ONE
	// identity — silent data corruption, so it is refused, not warned.
	if b.NaturalKey == "" {
		ps.BlockerFix("storage.base.naturalKey",
			"the natural key is required",
			"it derives the identity's primary key and is the dedup key")
		return
	}
	f := findField(s.Fields, b.NaturalKey)
	if f == nil {
		ps.BlockerFix("storage.base.naturalKey",
			fmt.Sprintf("%q does not name a field of this entity", b.NaturalKey),
			"the natural key must be one of the declared fields")
		return
	}
	if f.Nullable {
		ps.BlockerFix("storage.base.naturalKey",
			fmt.Sprintf("the natural key %q is nullable", b.NaturalKey),
			"a null key collapses every key-less record into one identity; make it required")
	}
	if f.LivesOn != "base" {
		ps.BlockerFix("storage.base.naturalKey",
			fmt.Sprintf("the natural key %q does not live on the base", b.NaturalKey),
			"set its livesOn: base — the key belongs to the shared identity, not the role")
	}
}

func validateFields(s *Spec, ps *Problems, opt Options) {
	if len(s.Fields) == 0 {
		ps.Blockerf("fields", "an entity needs at least one field")
		return
	}
	seenName := map[string]int{}
	seenCol := map[string]int{}
	for i, f := range s.Fields {
		where := fmt.Sprintf("fields[%d] (%s)", i, orUnnamed(f.Name))
		validateOneField(s, f, where, ps, false, false, opt)

		if j, dup := seenName[f.Name]; dup {
			ps.Blockerf(where, "the field name %q is already used by fields[%d]", f.Name, j)
		} else if f.Name != "" {
			seenName[f.Name] = i
		}
		if f.Runtime {
			continue
		}
		key := f.LivesOn + "." + f.Column
		if j, dup := seenCol[key]; dup {
			ps.Blockerf(where, "the column %q is already mapped by fields[%d] on the same table", f.Column, j)
		} else if f.Column != "" {
			seenCol[key] = i
		}
	}
}

// reservedFieldNames are the names an author cannot use, because something else
// already owns them on the generated aggregate.
//
// The first three belong to the FRAMEWORK's managed carrier. Declaring one as a
// mapped field is a boot panic; the ParentID case additionally silently
// overwrites the framework's own value.
//
// The rest belong to the ROW SCOPE, which synthesises them onto the aggregate
// from authz.dataAccess / authz.bypass — an author who declared one would get
// two Go struct fields with one name, which is a build failure with no line
// number pointing at the spec. They are refused UNCONDITIONALLY rather than only
// on the specs that synthesise them: the conditions live in the resolver, and a
// copy of them here would be a copy that drifts, quietly re-opening the
// collision on whichever spec shape moved.
var reservedFieldNames = map[string]string{
	"ID":       "the aggregate id comes from the framework's managed carrier",
	"ParentID": "the parent link is projected automatically as the read-only twin of ID",
	"Revision": "the revision column is declared under storage.managed, not as a field",

	"RequestingTenant": "the row scope synthesises this name for the caller's own tenant; " +
		"to carry the tenant into a rule of your own, declare a runtime field with " +
		"source: tenant under a different name",
	"RequestingSubject": "the row scope synthesises this name for the caller's own subject; " +
		"to carry the subject into a rule of your own, declare a runtime field with " +
		"source: subject under a different name",
	"RequestingMayCrossScope": "the row scope synthesises this name for authz.bypass; " +
		"to ask about a permission of your own, declare a runtime field with " +
		"source: permission under a different name",
	"RequestingIdentityPresent": "the row scope synthesises this name for \"was there a " +
		"caller at all\"; to ask the same question in a rule of your own, declare a " +
		"runtime field with source: present under a different name",
}

// isFacet distinguishes the two things isChild lumps together: a COLLECTION
// entry, whose uniqueness this build now generates, and a 1:1 FACET's field,
// whose uniqueness it still does not.
// ScopeSubjectName is the field the row scope narrows BY: the owner under
// owner-only access, the tenant under tenant access, and nothing at all when
// the rows are not scoped.
func ScopeSubjectName(s *Spec) string {
	switch s.Authz.DataAccess {
	case "owner-only":
		return s.Authz.OwnerField
	case "tenant":
		return s.Authz.TenantField
	}
	return ""
}

// validateBypassMaySet holds the "yields to the bypass" key to the ONE seat
// where it is safe.
//
// The value it lets through is written onto the entity whoever sent it, and
// what refuses a caller who may not state one is the row-scope guard — which
// compares exactly one field, the scope's subject. On any other field there is
// no comparison, so the body value would simply be taken, from everybody, on a
// field the spec advertises as server-assigned. That is a privilege escalation
// spelled as a convenience, and it is refused here rather than reviewed later.
func validateBypassMaySet(s *Spec, f Field, where string, ps *Problems, isChild bool) {
	if !f.BypassMaySet {
		return
	}
	at := where + ".bypassMaySet"
	if isChild {
		ps.BlockerFix(at,
			"an entry of a collection is not the subject of the row scope",
			"the field the scope narrows by belongs to the root, and so does this key")
		return
	}
	if f.AssignedFrom == "" {
		ps.BlockerFix(at,
			"this key says who may state a value the SERVER would otherwise assign, "+
				"and nothing assigns this field",
			"declare assignedFrom: identity-claim (with its claim) or identity-subject; "+
				"a field the client already sends needs no exception")
		return
	}
	if f.AssignedFrom == "derived" {
		ps.BlockerFix(at,
			"a derived field is computed from the entity's own fields, so there is no "+
				"caller — not even a privileged one — with a value to state",
			"drop bypassMaySet; if the value really comes from the request, it is not derived")
		return
	}
	subject := ScopeSubjectName(s)
	switch {
	case !Scoped(s.Authz.DataAccess):
		ps.BlockerFix(at,
			"nothing scopes the rows of this entity, so no caller crosses a scope",
			"this key belongs to authz.dataAccess: owner-only or tenant, where the "+
				"server fills the scope from the caller's identity")
	case s.Authz.Bypass == "":
		ps.BlockerFix(at,
			"no caller crosses the row scope, so the exception applies to nobody",
			"declare authz.bypass — the permission (or the *:* wildcard) that lets an "+
				"operator read and repair rows outside their own scope")
	case subject != f.Name:
		ps.BlockerFix(at,
			fmt.Sprintf("%q is not the field the row scope narrows by, and what refuses a "+
				"caller who may NOT state a value is that scope's own guard — over %q alone",
				f.Name, orUnnamed(subject)),
			fmt.Sprintf("declare it on %s, or leave this field server-assigned: on any "+
				"other field the value would be accepted from every caller",
				orUnnamed(subject)))
	}
	// The value rides on the INSERT body, so an entity that mounts no insert
	// offers it nowhere.
	if !contains(s.Modes, "insert") {
		ps.BlockerFix(at,
			"this entity has no insert verb, and the exception is on the insert alone — "+
				"a row does not change scope by being updated",
			"add insert to the entity's modes, or drop bypassMaySet")
	}
}

// Scoped reports whether a dataAccess narrows the rows by something about the
// caller, which is the precondition for anything crossing that scope.
func Scoped(dataAccess string) bool {
	return dataAccess == "owner-only" || dataAccess == "tenant"
}

// SourceOf answers where a runtime-only field is fed from, with the default
// materialised: a spec written before `source` existed says `claim`, which is
// the only thing runtime used to mean.
//
// Exported because the resolver needs the same answer, and a default decided
// twice in two layers is the generator bug that compiles and is wrong.
func SourceOf(f Field) string {
	if !f.Runtime {
		return ""
	}
	if f.Source == "" {
		return "claim"
	}
	return f.Source
}

// FromBody reports whether a field crosses the request body, the command and
// the entity without ever reaching a column.
func FromBody(f Field) bool { return SourceOf(f) == "body" }

// FromManual reports whether a field is on the aggregate and filled by nobody
// this generator writes. It is the field-level ELSE: the shape is declared here,
// the value is put there by hand-written code.
func FromManual(f Field) bool { return SourceOf(f) == "manual" }

// IdentitySourceOf names WHICH question about the caller feeds a runtime field,
// for the sources that ask the framework rather than reading a claim by name.
//
// Empty for a persisted field and for the two sources that are not a question
// about the identity: `claim` is a lookup by name, `body` is a value the caller
// sent. The resolver lowers this string straight onto the IR field, which is why
// it lives here and is exported — the row scope's SYNTHESISED fields already
// carry the same vocabulary, so a declared field and a synthesised one reach the
// command mapper's identity feed through one branch instead of two.
func IdentitySourceOf(f Field) string {
	switch src := SourceOf(f); src {
	case "", "claim", "body", "manual":
		return ""
	default:
		return src
	}
}

// validateConcretePermission holds a permission string to what the generated
// code will do with it: hand it to Identity.HasPermission, which accepts a
// concrete "resource:action" and NOTHING else.
//
// It is one function rather than two copies because two seats ask the same
// question — authz.bypass and a source: permission field — and the failure they
// exist to prevent is identical and expensive: HasPermission PANICS on a
// wildcard, so a spec that loads, generates and builds cleanly takes the service
// down on the first request that reaches the check. `*:*` is the one grant a
// caller can be tested for and it is asked with a different method entirely, so
// each caller decides whether that spelling belongs in ITS seat before calling
// here, and says so through wildcardFix.
func validateConcretePermission(permission, at, wildcardFix string, ps *Problems) {
	switch {
	case strings.Contains(permission, "*"):
		ps.BlockerFix(at,
			fmt.Sprintf("%q cannot be asked about — the framework's HasPermission "+
				"panics on a wildcard, since the CLAIM wildcards and the question "+
				"does not", permission),
			wildcardFix)
	case !strings.Contains(permission, ":"):
		ps.BlockerFix(at,
			fmt.Sprintf("%q is not a permission", permission),
			"spell it resource:action")
	}
}

// validateRuntimeField judges the field the table never sees.
//
// Both sources agree on what is refused for the same reason — there is no
// column, so uniqueness, redaction and a column name are all answers to a
// question nobody asked. They part on where the value comes from, and that
// decides two keys: `claim` needs one named and refuses `modes`; `body` refuses
// a claim and takes the write verbs its value rides on.
func validateRuntimeField(s *Spec, f Field, where string, ps *Problems, isChild bool) {
	// A collection entry (or a facet's row) has no write of its own to carry
	// the value on, and no identity of its own to read one from; the lowering
	// kept the field anyway and the migration emitted a column with an EMPTY
	// name.
	if isChild {
		ps.BlockerFix(where,
			"a runtime-only field belongs to the entity, not to a collection or facet",
			"declare it at the root; an entry is validated in the entity's context "+
				"and reads the entity's runtime fields from there")
		return
	}
	if f.Source != "" && !FieldSources.Has(f.Source) {
		ps.BlockerFix(where+".source",
			fmt.Sprintf("%q is not somewhere a runtime-only field can be fed from", f.Source),
			"one of: "+FieldSources.String())
		return
	}
	if f.Column != "" {
		ps.BlockerFix(where,
			"a runtime-only field cannot have a column",
			"a runtime field is never persisted — it is fed from the caller's token "+
				"(source: claim) or from the request body (source: body), and stops at "+
				"the entity")
	}
	if f.Unique != nil {
		ps.Blockerf(where, "a runtime-only field cannot be unique — it is never stored")
	}
	// Redaction masks the copies the framework makes of a ROW. A runtime-only
	// field is on no row: it is in no payload and no audit event to be masked in.
	if f.Redact != nil {
		ps.BlockerFix(where+".redact",
			"a runtime-only field is never persisted, so no copy of it exists to redact",
			"drop redact — the value lives for the length of one request and reaches "+
				"neither the outbox payload nor the audit event")
	}

	// renderIn is a source: manual key. The two other families are refused here,
	// once, rather than inside each branch — and neither refusal is a limitation
	// of this build.
	//
	// A source: body value is one the CALLER sent: rendering it back hands
	// someone their own password confirmation from a surface nobody expected to
	// carry one, which this generator has refused since the source existed. An
	// identity value is a fact the caller already holds, so the response would be
	// reflecting the token at whoever presented it. What renderIn exists for is
	// the value that came from NEITHER — minted server-side, stored only as a
	// hash, and therefore unreachable unless this one response carries it.
	if len(f.RenderIn) > 0 && !FromManual(f) {
		if FromBody(f) {
			ps.BlockerFix(where+".renderIn",
				"this value came from the caller's own request body, and rendering it "+
					"back hands them their own credential from a response nobody expected "+
					"to carry one",
				"drop renderIn — a source: body field is an INPUT, checked by a rule and "+
					"dropped. renderIn is for a value your code MINTS, which is source: manual")
		} else {
			ps.BlockerFix(where+".renderIn",
				fmt.Sprintf("a source: %s field is a fact about the caller, who already "+
					"holds it — rendering it reflects the token back at whoever presented it",
					SourceOf(f)),
				"drop renderIn — it exists for a value the SERVER minted and nothing else "+
					"can hand over, which is source: manual")
		}
		return
	}

	if FromManual(f) {
		validateManualRuntimeField(s, f, where, ps)
		return
	}
	if !FromBody(f) {
		validateIdentityRuntimeField(f, where, ps)
		return
	}

	// ── source: body ─────────────────────────────────────────────────────────
	if f.Permission != "" {
		ps.BlockerFix(where+".permission",
			"a source: body field is filled from the request, and a permission is a "+
				"question about the CALLER — a value the caller sends is not the answer to it",
			"drop permission, or set source: permission to ask the framework whether the "+
				"caller holds it")
	}
	if f.Claim != "" {
		ps.BlockerFix(where+".claim",
			"a source: body field is filled from the request, so naming a claim says "+
				"two different things about where the value comes from",
			"drop claim, or drop source: body to go back to reading the token")
	}
	if f.VO != nil && f.VO.Kind == "composite" {
		ps.BlockerFix(where+".vo",
			"a composite value object spells out which COLUMN each of its parts is "+
				"stored in, and a source: body field is stored in none",
			"use a raw or reuse value object, whose value is one scalar the request "+
				"can carry")
	}
	// A write-less entity has no body for the value to ride on, so the field is
	// declared and unreachable — which reads, in the generated code and in the
	// report, exactly like a field that works.
	if !contains(s.Modes, "insert") && !contains(s.Modes, "update") {
		ps.BlockerFix(where+".source",
			"this entity mounts no write verb, so no request body exists to carry the field",
			"give the entity an insert or an update verb under modes, or drop the field")
	}
	validateBodyFieldModes(s, f, where, ps)
}

// validateManualRuntimeField judges the field the generator declares and fills
// from nowhere.
//
// Almost everything it refuses, it refuses for the reason every runtime field
// does — there is no column — and those checks already ran. What is left is the
// set of keys that describe a value ARRIVING, and no generated verb brings this
// one: not a claim, not a permission, not a set of write verbs.
//
// The one key that DOES belong here is the mirror of that last refusal. Nothing
// generated puts a value in the field, and renderIn is how the value the
// author's own code put there gets out — so it is validated here, on the only
// source that accepts it.
func validateManualRuntimeField(s *Spec, f Field, where string, ps *Problems) {
	if f.Claim != "" {
		ps.BlockerFix(where+".claim",
			"a source: manual field is filled by your code, so naming a claim says "+
				"two different things about where the value comes from",
			"drop claim, or use source: claim to have the generator read the token")
	}
	if f.Permission != "" {
		ps.BlockerFix(where+".permission",
			"permission is what a source: permission field asks about, and this one "+
				"is source: manual",
			"drop permission, or set source: permission to ask whether the caller holds it")
	}
	if len(f.Modes) > 0 {
		ps.BlockerFix(where+".modes",
			"modes names the write verbs whose BODY carries the field, and no generated "+
				"verb carries a source: manual one",
			"drop modes. If a generated write is meant to carry the value after all, that "+
				"is source: body, and modes names which verbs")
	}
	validateRenderIn(s, f, where, ps)
	// A composite is NOT refused here, and the omission is deliberate: every
	// composite runtime field is already refused before this runs, by the
	// composite pass, because validateOneField returns early for one. A second
	// refusal would be a message nobody can reach — the shape that rots, since
	// nothing fails when it stops being true.
	//
	// Deliberately NOT refused here: an entity with no write verb. A source: body
	// field on one is declared and unreachable — no request body exists to carry
	// it — but this field never rode a request body to begin with, and the
	// aggregate and its BuildRules exist either way.
}

// validateIdentityRuntimeField judges a runtime-only field fed from the CALLER,
// which is every source but `body`.
//
// They split on what each one has to NAME. `claim` needs a claim name and
// nothing else; `permission` needs the permission and nothing else; the
// remaining three need neither, because the question they ask takes no argument.
// Each refuses the other's key BY NAME rather than ignoring it: a field that says
// `source: subject` and `claim: sub` is saying two different things about where
// its value comes from, and the one it does not mean would win silently.
func validateIdentityRuntimeField(f Field, where string, ps *Problems) {
	src := SourceOf(f)

	// modes is a body key: it names the write verbs whose BODY carries a value.
	// A fact about the caller rides every verb the entity has — the BODYLESS ones
	// included, which is exactly where an archive guard reads it.
	if len(f.Modes) > 0 {
		ps.BlockerFix(where+".modes",
			"modes names the write verbs whose BODY carries the field, and this one "+
				"is fed from the caller's identity",
			"drop modes — an identity reaches every verb, including the bodyless ones; "+
				"or set source: body if the caller is meant to send the value")
	}
	if src != "claim" && f.Claim != "" {
		ps.BlockerFix(where+".claim",
			fmt.Sprintf("source: %s asks the framework its own question about the caller, "+
				"so it looks no claim up by name", src),
			"drop claim — the accessor behind this source owns which claim it reads, and "+
				"for permission and super-admin that name is a deployment setting "+
				"(authorization.permissionsClaim). To read a claim by name instead, that "+
				"is source: claim")
	}
	if src != "permission" && f.Permission != "" {
		ps.BlockerFix(where+".permission",
			fmt.Sprintf("permission is what a source: permission field asks about, and "+
				"this one is source: %s", src),
			"drop permission, or set source: permission to ask whether the caller holds it")
	}
	// A value object is the DOMAIN validating a value somebody SENT. Nothing fed
	// from the identity is that, and the two sources fail the promise
	// differently — so they are refused separately and for their own reason.
	//
	// It was silently accepted on a claim field until this refusal, which is the
	// worse half of the bug: the value-object type was generated, and the
	// aggregate declared the field as the plain scalar anyway, so the rule the
	// author wrote in that type ran over nothing. A key that does nothing reads
	// exactly like a key that works.
	if f.VO != nil && f.VO.Kind != "" && f.VO.Kind != "none" {
		if src == "claim" {
			ps.BlockerFix(where+".vo",
				"a claim is asserted by the ISSUER and already verified by the token's "+
					"signature, so it is not a value this aggregate judges",
				"drop vo. Validating it here would answer 422 for a value the CALLER "+
					"never sent and cannot fix — a misconfigured issuer reported as the "+
					"caller's mistake. Where a value object DOES belong on a runtime "+
					"field is source: body, whose value is the caller's own")
		} else {
			ps.BlockerFix(where+".vo",
				fmt.Sprintf("source: %s is answered by the framework, so its value goes "+
					"through no constructor of yours", src),
				"drop vo — the field carries what the accessor returned (a subject, a "+
					"tenant, a yes/no), and a rule reads it directly")
		}
	}

	switch src {
	case "claim":
		if f.Claim == "" {
			ps.BlockerFix(where+".claim",
				"a runtime-only field does not say where its value comes from",
				"name the claim it is fed from, e.g. claim: email — the framework does "+
					"not opine on custom claim names, so there is no convention to fall "+
					"back on. If the value is the caller's identity ITSELF, the framework "+
					"answers that: source: subject | tenant | permission | super-admin | "+
					"present. If it comes from the REQUEST, say so with source: body and "+
					"drop claim (that is the password-confirmation shape)")
		}
		if f.Type != "string" && f.Type != "bool" {
			ps.BlockerFix(where+".type",
				"a runtime-only field read from a token claim is text or a flag",
				"set type: string, or type: bool for a yes/no claim")
		}

	case "subject", "tenant":
		whose := "subject"
		if src == "tenant" {
			whose = "tenant"
		}
		if f.Type != "string" {
			ps.BlockerFix(where+".type",
				fmt.Sprintf("the caller's %s arrives as text, and %q is not", whose, f.Type),
				"set type: string — a runtime field backs no column, so there is no "+
					"foreign key to be worth carrying the value as an id for")
		}

	case "permission":
		if f.Type != "bool" {
			ps.BlockerFix(where+".type",
				fmt.Sprintf("holding a permission is a yes/no, and %q is not", f.Type),
				"set type: bool")
		}
		if f.Permission == "" {
			ps.BlockerFix(where+".permission",
				"a source: permission field does not say WHICH permission it asks about",
				`name it, e.g. permission: "users:admin" — the same string the deployment `+
					"grants and the same one authz.permissions names")
			break
		}
		validateConcretePermission(f.Permission, where+".permission",
			`for the *:* grant itself use source: super-admin, which the framework `+
				`answers with Identity.IsSuperAdmin rather than with HasPermission. `+
				`Anything narrower has to be a concrete permission: grant something `+
				`like users:admin and name it here`, ps)

	case "super-admin", "present":
		if f.Type != "bool" {
			asks := "whether the caller holds the " + SuperAdminClaim + " grant"
			if src == "present" {
				asks = "whether the request carried an identity at all"
			}
			ps.BlockerFix(where+".type",
				fmt.Sprintf("source: %s asks %s, which is a yes/no, and %q is not",
					src, asks, f.Type),
				"set type: bool")
		}
	}
}

// validateBodyFieldModes holds the modes list to the verbs the entity actually
// has. A field declared on a verb the spec never mounts is a field the caller
// can never send — accepted silently, it reads as "declared and working".
func validateBodyFieldModes(s *Spec, f Field, where string, ps *Problems) {
	// An EMPTY list is not an absent one, and until this refusal the two were the
	// same answer: `modes: []` decoded to a list with nothing declared in it, fell
	// into the "omitted" branch of the resolver, and put the field on EVERY write
	// verb — the exact opposite of what it says. `check` answered that the spec
	// could be generated, and the output was byte-for-byte the output of writing
	// no modes at all.
	//
	// nil is the absent key; a non-nil empty slice is an author who wrote the
	// brackets.
	if f.Modes != nil && len(f.Modes) == 0 {
		ps.BlockerFix(where+".modes",
			"an empty modes list says no write verb carries the field, and this generator "+
				"reads it as every one of them",
			"for a field NO generated verb carries — one a hand-written operation fills, "+
				"so it must stay out of the ordinary write bodies — that is source: manual. "+
				"To have a generated write carry it, name the verbs: modes: [insert]")
		return
	}
	if len(f.Modes) == 0 {
		return
	}
	seen := map[string]bool{}
	for i, mode := range f.Modes {
		at := fmt.Sprintf("%s.modes[%d]", where, i)
		if !FieldModes.Has(mode) {
			ps.BlockerFix(at,
				fmt.Sprintf("%q is not a write verb whose body can carry a field", mode),
				"one of: "+FieldModes.String()+" — a PATCH is dispatched into the same "+
					"IfUpdate clause a PUT is, so `update` names both")
			continue
		}
		if seen[mode] {
			ps.Blockerf(at, "%q is listed twice", mode)
			continue
		}
		seen[mode] = true
		if !contains(s.Modes, mode) {
			ps.BlockerFix(at,
				fmt.Sprintf("this entity has no %s verb, so nothing would ever carry the field", mode),
				"add "+mode+" to the entity's modes, or drop it from this field's")
		}
	}
}

// validateRenderIn judges the OUTPUT side of a source: manual field: which write
// verbs answer with the value the author's own code minted.
//
// It is validateBodyFieldModes read in the other direction, and it is held to
// the same two rules for the same reasons — the vocabulary is closed, and a verb
// the entity does not mount is a response that never happens. Where the two part
// company is the omitted key: `modes` omitted means EVERY write verb, because a
// value the caller sends is a value every body can carry, while renderIn omitted
// means none. A value minted on insert is not one an update has in hand, so
// "every verb" would be a promise the entity cannot keep, and the default for a
// runtime field — in no response at all — is the safe half of the pair.
func validateRenderIn(s *Spec, f Field, where string, ps *Problems) {
	// The brackets, written out, say "no verb renders it" — which is what leaving
	// the key out already says, and the author who typed them meant something.
	// The same refusal `modes: []` gets, for the same reason: a key whose empty
	// form is indistinguishable from its absent form silently generates the
	// opposite of what somebody wrote.
	if f.RenderIn != nil && len(f.RenderIn) == 0 {
		ps.BlockerFix(where+".renderIn",
			"an empty renderIn list says no write verb renders the field, which is what "+
				"leaving the key out already says",
			"name the verb that mints the value — renderIn: [insert] — or drop the key")
		return
	}
	seen := map[string]bool{}
	for i, mode := range f.RenderIn {
		at := fmt.Sprintf("%s.renderIn[%d]", where, i)
		if !FieldModes.Has(mode) {
			ps.BlockerFix(at,
				fmt.Sprintf("%q is not a write verb whose response can render a field", mode),
				"one of: "+FieldModes.String()+" — `update` names both PUT and PATCH, the "+
					"same two values modes accepts, because it is the same axis")
			continue
		}
		if seen[mode] {
			ps.Blockerf(at, "%q is listed twice", mode)
			continue
		}
		seen[mode] = true
		if !contains(s.Modes, mode) {
			ps.BlockerFix(at,
				fmt.Sprintf("this entity has no %s verb, so no response would ever render "+
					"the field", mode),
				"add "+mode+" to the entity's modes, or drop it from this field's renderIn")
		}
	}
}

func validateOneField(s *Spec, f Field, where string, ps *Problems, isChild, isFacet bool, opt Options) {
	if f.Name == "" {
		ps.Blockerf(where, "the field name is required")
	} else if !goIdentRe.MatchString(f.Name) {
		ps.BlockerFix(where,
			fmt.Sprintf("%q is not a usable Go field name", f.Name),
			"use exported PascalCase, e.g. EnrollmentNumber")
	} else if why, reserved := reservedFieldNames[f.Name]; reserved {
		ps.BlockerFix(where,
			fmt.Sprintf("%q is a reserved field name", f.Name),
			why)
	}

	// A COMPOSITE field answers the single-column questions per PART, not once:
	// its type, column and length live under parts[], and validateComposites
	// owns every message about them. Asking for them here would refuse a correct
	// spec three times before it reached the check that explains the shape.
	if IsComposite(f) {
		validateFieldPlacement(s, f, where, ps, isChild)
		if f.Description == "" {
			ps.WarnFix(where, "no description",
				"it is what the aggregate's field comment says about the concept as a whole")
		}
		return
	}

	if !FieldTypes.Has(f.Type) {
		ps.BlockerFix(where,
			fmt.Sprintf("%q is not a persistable type", f.Type),
			"one of: "+FieldTypes.String())
	}

	// A zero-length VARCHAR is rejected outright by Postgres, SQL Server and
	// Oracle, so a string without a length would emit DDL that cannot apply.
	if f.Type == "string" && f.Length <= 0 && !f.Runtime {
		ps.BlockerFix(where,
			"a string field needs a length",
			"set length: N — a zero-length VARCHAR is rejected by postgres, sqlserver and oracle")
	}

	if f.Hidden && f.Runtime {
		ps.BlockerFix(where+".hidden",
			"a runtime-only field is in no response to begin with — it is never "+
				"persisted, so no read has anything to render",
			"drop hidden; it takes a PERSISTED field out of the responses while leaving "+
				"the column, the filters and the writes alone")
	}

	// source and modes are the two keys that only mean anything on a runtime
	// field. Read on a persisted one they would say "this is not stored", which
	// is the opposite of what the column says — so they are refused by name
	// rather than ignored.
	if !f.Runtime {
		if f.Source != "" {
			ps.BlockerFix(where+".source",
				"source says where a RUNTIME-only field is fed from, and this field has a column",
				"add runtime: true if the value must not be stored, or drop source — a "+
					"persisted field is filled from the request body like any other")
		}
		if len(f.Modes) > 0 {
			ps.BlockerFix(where+".modes",
				"modes says which write verbs carry a source: body field, and this field is persisted",
				"a persisted field is on every write verb the entity has; to keep one out "+
					"of the partial update, name it under update.patchExcludes")
		}
		if len(f.RenderIn) > 0 {
			ps.BlockerFix(where+".renderIn",
				"renderIn puts a RUNTIME value into a write response, and this field has "+
					"a column — every write already answers with it",
				"drop renderIn. To keep a persisted field out of the responses instead, "+
					"that is hidden: true; to mask it in the copies the framework makes of "+
					"the row, redact")
		}
		if f.Permission != "" {
			ps.BlockerFix(where+".permission",
				"permission is what a runtime field with source: permission asks about, "+
					"and this field has a column",
				"a column holds a value, not an answer about whoever is asking — carry the "+
					"grant into the rules with runtime: true and source: permission, or drop "+
					"permission")
		}
	}

	validateStamped(s, f, where, ps, isChild, isFacet)

	if f.Runtime {
		validateRuntimeField(s, f, where, ps, isChild)
		return
	}

	if f.AssignedFrom != "" {
		if isChild {
			ps.BlockerFix(where+".assignedFrom",
				"an entry of a collection is not server-assigned",
				"the write is addressed to the root, and it is the root that carries the "+
					"field the server fills")
		}
		if !AssignedFrom.Has(f.AssignedFrom) {
			ps.BlockerFix(where+".assignedFrom",
				fmt.Sprintf("%q is not a source the server can read", f.AssignedFrom),
				"one of: "+AssignedFrom.String())
		}
		fromIdentity := f.AssignedFrom == "identity-subject" || f.AssignedFrom == "identity-claim"
		if f.AssignedFrom == "identity-claim" && f.Claim == "" {
			ps.BlockerFix(where+".claim",
				"the field is filled from a claim but does not say which",
				"name it, e.g. claim: tenant_id — there is no convention to fall back on")
		}
		if f.AssignedFrom != "identity-claim" && f.Claim != "" {
			ps.BlockerFix(where+".claim",
				"this field is not filled from a claim, so naming one says two different things",
				"drop claim, or use assignedFrom: identity-claim")
		}
		// A claim ARRIVES as text, but what it names is often an id, and the
		// column that records it should then be the engine's own id type — a
		// VARCHAR that holds a UUID cannot carry a foreign key to a UUID column
		// on postgres, and changing a column's type later is a migration over
		// live data. Refusing `id` here forced that trade permanently, at the
		// moment the author was least equipped to make it: pick the honest
		// column and hand-write the rules, or pick the declarative rules and
		// give up the foreign key. So both are accepted, and the mapper parses
		// the claim into the id.
		//
		// Everything else stays refused: an identity is not a number, a flag or
		// a date. A derived value is another matter — it is whatever the
		// derivation produces — which is why this only guards the identity ones.
		if fromIdentity && f.Type != "string" && f.Type != "id" {
			ps.BlockerFix(where+".type",
				fmt.Sprintf("an identity is text or an id, and %q is neither", f.Type),
				"set type: string, or type: id when the claim names one and the column "+
					"should be the engine's own id type")
		}
		// The origin is an ADDRESS, and the framework hands it over as text —
		// `AppContext.ClientIP()` returns a string. `id` is refused along with
		// everything else, and deliberately: an IP is not this service's key for
		// anything, so borrowing the engine's id type for it would promise a
		// foreign key that has nothing to point at, and would fail to parse the
		// moment the value is the empty string.
		if f.AssignedFrom == "client-ip" && f.Type != "string" {
			ps.BlockerFix(where+".type",
				fmt.Sprintf("the request's origin arrives as text, and %q is not", f.Type),
				"set type: string — the address is a value to record and compare, never "+
					"a key this service resolves")
		}
		// "No caller may set this" and "this may have no value" are independent
		// statements, and only the identity ones imply each other: the server
		// always has a subject and always has the claim it required, so a column
		// filled from either is written on every insert and nullable would be
		// describing a state that cannot happen.
		//
		// `derived` makes no such promise. The generator writes no assignment
		// for it — a rule the author writes does — and that rule may legitimately
		// leave the value unset: a verification timestamp is null until the thing
		// is verified. Refusing it here forced the two workarounds this key
		// exists to remove: drop assignedFrom and let anyone holding the insert
		// permission claim the address is verified, or drop the field. The third
		// outcome, keeping both and taking the non-nullable column, is the one
		// consumers actually shipped — and it renders the zero time
		// ("0000-12-31T18:42:28-05:17") in every response the row appears in.
		if fromIdentity && f.Nullable {
			ps.BlockerFix(where+".nullable",
				"a field filled from the caller's identity is written on every insert, "+
					"so it is never null",
				"drop nullable — a caller who cannot supply it is a matter for the "+
					"permission, not for the column")
		}
		// `derived` says the server owns the value; nothing in this build can
		// know HOW it is computed, so the one thing that CAN be checked is that
		// somewhere claims to compute it. With no manual rule the field is
		// simply never written, and the column holds the zero value forever —
		// silently, which is the failure shape this generator refuses to ship.
		//
		// A NULLABLE derived field is not that shape. Null says "no value yet"
		// out loud, in the column and in every response, and what fills it is
		// often not an insert at all — a verification timestamp is written by
		// the verb that verifies. So the question narrows to whether ANY rule
		// claims the field, and a spec that computes it nowhere gets told what
		// it has actually declared: a column that stays null.
		switch {
		case f.AssignedFrom != "derived" || isChild:
		case !f.Nullable && !hasInsertManualRule(s):
			ps.WarnFix(where+".assignedFrom",
				"nothing in this spec computes this field",
				"a derived field is filled by a rules.manual entry scoped to insert — "+
					"declare it, or the column keeps its zero value and no error says so")
		case f.Nullable && len(s.Rules.Manual) == 0:
			ps.WarnFix(where+".assignedFrom",
				"nothing in this spec computes this field, and it is nullable",
				"that is a column which stays null until hand-written code writes it — "+
					"intended for a value some rows never have; if this one is computed on "+
					"insert, declare the rules.manual entry that computes it")
		}
	}

	validateBypassMaySet(s, f, where, ps, isChild)

	if f.Column == "" {
		ps.Blockerf(where, "the column name is required")
	} else if !columnRe.MatchString(f.Column) {
		ps.BlockerFix(where,
			fmt.Sprintf("%q is not a usable column name", f.Column),
			"lowercase, digits and underscores, starting with a letter")
	}
	if engine := ReservedWord(f.Column); engine != "" {
		ps.BlockerFix(where,
			fmt.Sprintf("the column name %q is a reserved word (%s)", f.Column, engine),
			reservedWordFix)
	}
	// The underscore namespace is reserved by the framework; a column starting
	// with it fails at boot.
	if strings.HasPrefix(f.Column, "_") {
		ps.BlockerFix(where,
			fmt.Sprintf("the column %q starts with an underscore", f.Column),
			"the underscore prefix is reserved by the framework")
	}

	validateFieldPlacement(s, f, where, ps, isChild)

	if f.VO != nil {
		if !VOKinds.Has(f.VO.Kind) {
			ps.BlockerFix(where+".vo.kind",
				fmt.Sprintf("%q is not a value-object kind", f.VO.Kind),
				"one of: "+VOKinds.String())
		}
		if f.VO.Kind == "reuse" && f.VO.Ref == "" {
			ps.BlockerFix(where+".vo",
				"reusing a value object needs its name",
				"set vo.ref to the existing type, e.g. Email")
		}
		// No guard on the inventory being non-empty: a project with NO value
		// objects is precisely where a reuse reference is certainly wrong, and
		// skipping the check there made it vacuous for every fresh project.
		if f.VO.Kind == "reuse" && f.VO.Ref != "" {
			found := false
			for _, name := range opt.ExistingVOs {
				if name == f.VO.Ref {
					found = true
					break
				}
			}
			if !found {
				known := "none — this project declares no value objects yet"
				if len(opt.ExistingVOs) > 0 {
					known = strings.Join(opt.ExistingVOs, ", ")
				}
				fix := fmt.Sprintf("declare it under valueObjects (kind: raw or enum), or "+
					"correct the name — known here: %s", known)
				// A notification named as a value object is a specific mistake
				// with a specific cause: they share the vos package, and an
				// inventory that once listed them made them look like the
				// candidates. Name it rather than let the author try the next
				// one on the list.
				if strings.HasSuffix(f.VO.Ref, "Notification") {
					fix = fmt.Sprintf("%q is a notification, not a value object — a "+
						"notification is what a rule RAISES; a value object is what a field "+
						"IS. %s", f.VO.Ref, fix)
				}
				ps.BlockerFix(where+".vo.ref",
					fmt.Sprintf("the project declares no value object named %q", f.VO.Ref), fix)
			}
		}
		if declaredHere(f.VO.Kind) && f.VO.Ref == "" {
			ps.BlockerFix(where+".vo",
				"a new value object needs a name",
				"set vo.ref and declare it under valueObjects")
		}
		// A raw/enum ref that names nothing used to pass here and surface as an
		// undefined type at go build. reuse has its own resolution above; these
		// two kinds declare IN THIS SPEC, so the declaration must be here.
		if declaredHere(f.VO.Kind) && f.VO.Ref != "" {
			if vo := findVO(s.ValueObjects, f.VO.Ref); vo == nil {
				ps.BlockerFix(where+".vo.ref",
					fmt.Sprintf("this spec declares no value object named %q", f.VO.Ref),
					"declare it under valueObjects, or use vo.kind: reuse for one another "+
						"entity already generated")
			} else if vo.Kind != f.VO.Kind {
				ps.Blockerf(where+".vo.kind",
					"the field says %q but the declaration under valueObjects says %q — "+
						"one of the two is wrong", f.VO.Kind, vo.Kind)
			}
		}
	}

	if f.Unique != nil && isFacet {
		// A facet is ONE row per owner, so "unique among the facets" is a
		// question about the owners, not about the facet — and nothing
		// downstream reads it: no index, no precheck, no report line. Accepting
		// it meant the author believed duplicates were refused when they were
		// not.
		ps.BlockerFix(where+".unique",
			"uniqueness of a facet's field is not generated by this build — no index "+
				"and no precheck would be emitted",
			"declare the uniqueness on a root field, or drop the key")
	}
	if f.Unique != nil && isChild && !isFacet {
		validateChildUnique(s, f, where, ps)
	}
	if f.Unique != nil && !isChild {
		if f.LivesOn == "base" {
			// The constraint is resolved against the role's table, but a
			// base-lived column exists only on the base table — the emitted
			// index named a column that was not there and the migration failed
			// on every engine.
			ps.BlockerFix(where+".unique",
				"a unique on a base-lived field would be created on the role's table, "+
					"where the column does not exist",
				"declare the uniqueness in the spec that owns the base, or move the "+
					"field to the role")
		}
		if !UniqueEnforcements.Has(f.Unique.Enforce) {
			ps.BlockerFix(where+".unique.enforce",
				fmt.Sprintf("%q is not an enforcement style", f.Unique.Enforce),
				"one of: "+UniqueEnforcements.String())
		}
		validateUniqueWithin(s, f, where, ps)
		// The precheck half only materialises when a domain service carries an
		// exists fact filtered by this field. Without this cross-check the
		// enforce string validated, the report repeated it, and the generated
		// service quietly had constraint-only behaviour.
		//
		// The filters must match the index EXACTLY — `within` plus this field —
		// which is the other half of the same story: a fact filtering by MORE
		// than the index covers asks a narrower question than the database
		// answers, so the domain accepts a value the constraint then refuses,
		// reported under a notification naming a reason that is not the reason.
		if f.Unique.Enforce == "service-precheck+constraint" {
			want := uniqueFilterSet(f)
			if !hasExistsFactFor(s, want) {
				reportPrecheckMismatch(s, f, want, where, ps)
			}
		}
		if f.Unique.Scope != "" && !UniqueScopes.Has(f.Unique.Scope) {
			ps.BlockerFix(where+".unique.scope",
				fmt.Sprintf("%q is not a uniqueness scope", f.Unique.Scope),
				"one of: "+UniqueScopes.String())
		}
		// active-only is defined by the archive column; without one it used to
		// fall back to a plain unique SILENTLY — permanently reserving values
		// the spec said should come free on archive.
		if f.Unique.Scope == "active-only" && s.Storage.Managed.ArchivedAt == "" {
			ps.BlockerFix(where+".unique.scope",
				"active-only scopes the uniqueness to the rows that are not archived, "+
					"and nothing here is ever archived",
				"declare storage.managed.archivedAt, or use scope: all")
		}
		if f.Type == "bool" {
			ps.BlockerFix(where+".unique",
				"a unique flag would allow at most one true and one false row",
				"uniqueness is for identifying values; drop the key")
		}
		if f.Unique.Notification == "" {
			ps.BlockerFix(where+".unique",
				"a unique field needs its own conflict notification",
				"declare one under notifications and name it here — the framework's "+
					"already-added notification reports a primary-key collision, not this")
		} else {
			validateNotificationRef(s, f.Unique.Notification, where+".unique.notification", ps)
		}
		if f.Nullable {
			ps.WarnFix(where,
				"a nullable field is declared unique",
				"most engines allow many NULLs past a unique index; confirm that is intended")
		}
	}

	validateRedact(s, f.Redact, where, redactSeat{
		Root:      !isChild && !isFacet,
		Scalar:    f.Type,
		Hidden:    f.Hidden,
		FieldName: f.Name,
		LivesOn:   f.LivesOn,
	}, ps)

	if f.Example == "" {
		ps.WarnFix(where,
			"no example value",
			"it is what Swagger's \"try it out\" shows; a plausible value costs nothing")
	}
	if f.Description == "" {
		ps.WarnFix(where,
			"no description",
			"it becomes the column comment in the migration")
	}
}

func validateFieldPlacement(s *Spec, f Field, where string, ps *Problems, isChild bool) {
	if isChild {
		if f.LivesOn != "" && f.LivesOn != "root" {
			ps.BlockerFix(where+".livesOn",
				"a child's fields live on the child's own table",
				"remove livesOn")
		}
		return
	}
	switch f.LivesOn {
	case "root":
		if s.Storage.Kind == "sharedbase-role" {
			ps.BlockerFix(where+".livesOn",
				"a shared-base role has no \"root\" table",
				"say base (the shared identity) or role (this role's own table)")
		}
	case "base", "role":
		if s.Storage.Kind != "sharedbase-role" {
			ps.BlockerFix(where+".livesOn",
				fmt.Sprintf("%q only exists in a shared-base model", f.LivesOn),
				"a flat entity places every field on root")
		}
	case "":
		ps.BlockerFix(where+".livesOn",
			"the field does not say which table it lives on",
			placementHint(s))
	default:
		if strings.HasPrefix(f.LivesOn, "sibling:") {
			name := strings.TrimPrefix(f.LivesOn, "sibling:")
			if findSibling(s.Siblings, name) == nil {
				ps.Blockerf(where+".livesOn", "there is no sibling named %q", name)
				return
			}
			// The placement parses and nothing implements it: the lowering puts
			// every root field on the root table regardless. Accepting it here
			// meant the spec said "facet" and the DDL said "root", silently.
			ps.BlockerFix(where+".livesOn",
				"placing a root field on a facet's table is not generated by this build — "+
					"the column would be created on the root table while the spec says otherwise",
				"declare the field under siblings[].fields, where the placement is real")
			return
		}
		ps.BlockerFix(where+".livesOn",
			fmt.Sprintf("%q is not a placement", f.LivesOn),
			placementHint(s))
	}
}

func placementHint(s *Spec) string {
	if s.Storage.Kind == "sharedbase-role" {
		return "one of: base | role — a field of a facet is declared under siblings[].fields"
	}
	return "root — a field of a facet is declared under siblings[].fields"
}

func validateValueObjects(s *Spec, ps *Problems, opt Options) {
	existing := map[string]bool{}
	for _, name := range opt.ExistingVOs {
		existing[name] = true
	}
	seen := map[string]bool{}
	for i, vo := range s.ValueObjects {
		where := fmt.Sprintf("valueObjects[%d] (%s)", i, orUnnamed(vo.Name))
		if vo.Name == "" {
			ps.Blockerf(where, "the value object needs a name")
		} else if !goIdentRe.MatchString(vo.Name) {
			ps.BlockerFix(where, fmt.Sprintf("%q is not a usable Go type name", vo.Name), "use PascalCase")
		} else if seen[vo.Name] {
			ps.Blockerf(where, "the value object %q is declared twice", vo.Name)
		} else if existing[vo.Name] && opt.VOOwner[vo.Name] != s.Entity && !HandWritten(vo) {
			ps.BlockerFix(where,
				fmt.Sprintf("the project already declares a value object named %q", vo.Name),
				fmt.Sprintf("reuse it instead — on the field write vo: {kind: reuse, ref: %s}; "+
					"a second copy is a rule that can drift from the first", vo.Name))
		} else {
			seen[vo.Name] = true
		}

		// The hand-written exemption above is the whole point of both spellings:
		// the type a declaration asks for is one the author WRITES, so the run
		// AFTER they write it finds the type in the project and would otherwise
		// be told to reuse the thing this spec asked for. A declaration has to
		// stay legal once it is honoured, or the feature works exactly once.

		// `written` is asked of every kind so a misspelling is refused where it is
		// written, and answered by exactly one: a scalar you write is `kind:
		// manual`, which says it with one key and carries the backing contract
		// that goes with it.
		if vo.Written != "" && !VOWritings.Has(vo.Written) {
			ps.BlockerFix(where+".written",
				fmt.Sprintf("%q does not say who writes the type", vo.Written),
				"one of: "+VOWritings.String())
		} else if vo.Written == "manual" && vo.Kind != "composite" {
			ps.BlockerFix(where+".written",
				fmt.Sprintf("written: manual keeps a DECLARED shape and hands over the file, "+
					"and a %s value object has no shape left to declare once its rule is yours",
					orUnnamed(vo.Kind)),
				"write kind: manual instead — it says the same thing for a value that "+
					"occupies one column, and its backing is the contract the mappers convert "+
					"through")
		}

		// A composite has no single underlying value — its parts are its value —
		// so the backing key is refused there rather than demanded.
		if vo.Kind != "composite" && !VOBackings.Has(vo.Backing) {
			ps.BlockerFix(where+".backing",
				fmt.Sprintf("%q is not a backing type", vo.Backing),
				"one of: "+VOBackings.String())
		}

		switch vo.Kind {
		case "composite":
			validateCompositeDeclaration(s, vo, where, ps, opt)
		case "raw":
			if vo.Regex == "" && vo.MinLength == 0 && vo.MaxLength == 0 && vo.Min == nil && vo.Max == nil {
				ps.BlockerFix(where,
					"a raw value object with no rule is just its underlying type",
					"give it a regex, a length bound or a numeric range — or declare the field as vo.kind: none")
			}
			// Each constraint family belongs to one backing, and the emitter
			// takes the spec at its word: a length bound on an int emitted
			// len(int), min/max on a string compared text to a number — both
			// compile errors — and a regex on an int emitted string(v), an
			// int→rune conversion that COMPILES and validates garbage silently.
			if vo.Backing == "int" {
				if vo.MinLength != 0 || vo.MaxLength != 0 {
					ps.BlockerFix(where,
						"length bounds measure text, and this value is a number",
						"use min/max, or back the value with a string")
				}
				if vo.Regex != "" {
					ps.BlockerFix(where+".regex",
						"a regex matches text, and this value is a number",
						"use min/max, or back the value with a string")
				}
			} else {
				if vo.Min != nil || vo.Max != nil {
					ps.BlockerFix(where,
						"min/max bound a number, and this value is text",
						"use minLength/maxLength or a regex, or back the value with an int")
				}
			}
			if vo.Regex != "" {
				if _, err := regexp.Compile(vo.Regex); err != nil {
					ps.Blockerf(where+".regex", "the pattern does not compile: %v", err)
				}
			}
			if vo.Notification == "" {
				ps.BlockerFix(where+".notification",
					"a raw value object needs the notification it raises when invalid",
					"declare one under notifications with package: vos")
			} else {
				validateNotificationRef(s, vo.Notification, where+".notification", ps)
			}
			if len(vo.Members) > 0 {
				ps.Blockerf(where+".members", "members belong to an enum value object, not a raw one")
			}
		case "manual":
			// The generator writes nothing for this type, so what it CAN check
			// is that the spec says enough for someone to write it — and that
			// the spec is not describing something it could have generated.
			if vo.Description == "" {
				ps.BlockerFix(where+".description",
					"a hand-written value object needs to say what it enforces",
					"one line, precise enough to implement — it is what the report asks "+
						"the implementer for, and an unnamed escape hatch degenerates into "+
						"an empty TODO")
			}
			if vo.Notification != "" || vo.UnknownNotification != "" {
				ps.BlockerFix(where,
					"the notification a hand-written value object raises is its own business",
					"the type you write chooses what to report through the "+
						"NotificationContext; declare the notification under notifications "+
						"and raise it from IsValid")
			}
			if vo.Regex != "" || vo.MinLength != 0 || vo.MaxLength != 0 ||
				vo.Min != nil || vo.Max != nil || len(vo.Members) > 0 {
				ps.BlockerFix(where,
					"this value object declares a rule the generator can express, and asks "+
						"to be hand-written anyway",
					"drop the rule keys and write the type, or set kind: raw / enum and let "+
						"the generator write it — one of the two, never a declaration that "+
						"says both")
			}
		case "enum":
			if len(vo.Members) == 0 {
				ps.BlockerFix(where+".members",
					"an enum value object needs its members",
					"list them with explicit values — the zero value is reserved for the unknown sentinel")
			}
			if vo.UnknownNotification == "" {
				ps.BlockerFix(where+".unknownNotification",
					"an enum value object needs the notification for an out-of-set value",
					"declare one under notifications with package: vos")
			} else {
				validateNotificationRef(s, vo.UnknownNotification, where+".unknownNotification", ps)
			}
			validateEnumMembers(vo, where, ps)
			if vo.Regex != "" || vo.MinLength != 0 || vo.MaxLength != 0 {
				ps.Blockerf(where, "format rules belong to a raw value object; an enum validates membership")
			}
		default:
			ps.BlockerFix(where+".kind",
				fmt.Sprintf("%q is not a value-object kind", vo.Kind),
				"raw (a format or range) | enum (a fixed set of values) | composite (a "+
					"value that spans SEVERAL fields, like Money{Amount, Currency}) | "+
					"manual (a rule none of those can express — you write the type)")
		}
	}
}

func validateEnumMembers(vo ValueObject, where string, ps *Problems) {
	seenName := map[string]bool{}
	seenVal := map[string]bool{}
	for i, m := range vo.Members {
		mw := fmt.Sprintf("%s.members[%d] (%s)", where, i, orUnnamed(m.Name))
		if m.Name == "" || !goIdentRe.MatchString(m.Name) {
			ps.BlockerFix(mw, "the member needs a PascalCase name", "e.g. Active")
		} else if seenName[m.Name] {
			ps.Blockerf(mw, "the member %q is declared twice", m.Name)
		} else {
			seenName[m.Name] = true
		}

		if m.Value == nil {
			ps.BlockerFix(mw,
				"the member has no explicit value",
				"declare it — an implicit sequence silently re-numbers when a member is inserted")
		} else {
			// The backing type decides how the member renders in Go. A
			// mismatch here produces code that does not compile.
			switch vo.Backing {
			case "int":
				if _, ok := m.Value.(int); !ok {
					ps.BlockerFix(mw,
						fmt.Sprintf("the value %v is not a number but the backing is int", m.Value),
						"use a number, or set backing: string")
				} else if m.Value.(int) == 0 {
					ps.BlockerFix(mw,
						"zero is reserved for the unknown sentinel",
						"start the members at 1")
				}
			case "string":
				sv, ok := m.Value.(string)
				if !ok {
					ps.BlockerFix(mw,
						fmt.Sprintf("the value %v is not a string but the backing is string", m.Value),
						"quote it, or set backing: int")
				} else if sv == "" {
					ps.Blockerf(mw, "the empty string is reserved for the unknown sentinel")
				}
			}
			key := fmt.Sprint(m.Value)
			if seenVal[key] {
				ps.Blockerf(mw, "the value %v is used by more than one member", m.Value)
			}
			seenVal[key] = true
		}
	}
}

// validateCollectionSpellings keeps the two names of two different collections
// from being the same word.
//
// Every key that addresses a collection accepts either spelling (see
// CollectionNamed), and that only stays unambiguous while one word names one
// collection. A spec with children [Permission/Permissions] and
// [Permissions/PermissionGroups] makes "Permissions" mean the second collection
// to one reader and the first to another — and the reader who loses is the
// author, who wrote a rule about one collection and got the other with no
// message at all.
func validateCollectionSpellings(s *Spec, ps *Problems) {
	for i := range s.Children {
		for j := range s.Children {
			if i == j || s.Children[i].Name == "" {
				continue
			}
			if s.Children[i].Name != s.Children[j].Plural {
				continue
			}
			ps.BlockerFix(fmt.Sprintf("children[%d].name", i),
				fmt.Sprintf("%q is also the collection name of children[%d] (%s), and every "+
					"key that addresses a collection accepts both spellings — so the word "+
					"names two different collections", s.Children[i].Name, j,
					orUnnamed(s.Children[j].Name)),
				"rename one of them: the entry type and the collection name are two "+
					"names for ONE collection, never a name shared between two")
		}
	}
}

func validateChildren(s *Spec, ps *Problems, opt Options) {
	validateCollectionSpellings(s, ps)
	seen := map[string]bool{}
	seenPlural := map[string]bool{}
	for i, c := range s.Children {
		where := fmt.Sprintf("children[%d] (%s)", i, orUnnamed(c.Name))
		if c.Name == "" || !goIdentRe.MatchString(c.Name) {
			ps.BlockerFix(where, "the child needs a PascalCase name", "e.g. Grade")
		} else if seen[c.Name] {
			ps.Blockerf(where, "the child %q is declared twice", c.Name)
		} else {
			seen[c.Name] = true
		}
		if c.Table == "" {
			ps.Blockerf(where+".table", "the child table name is required")
		} else {
			checkTableName(c.Table, where+".table", ps)
		}
		// The plural derives the read DTO field, the document segment and the
		// route segment. Deduping by NAME alone let two collections share a
		// plural and collide in every one of those places, and let a collection
		// shadow a root field in the read DTO.
		if c.Plural != "" {
			if seenPlural[c.Plural] {
				ps.Blockerf(where+".plural",
					"the collection name %q is already used by another child", c.Plural)
			}
			seenPlural[c.Plural] = true
			if findField(s.Fields, c.Plural) != nil {
				ps.Blockerf(where+".plural",
					"%q is also a field of the entity — the read DTO cannot carry both",
					c.Plural)
			}
		}
		// The collection name is a PERSISTED key — the document segment, the read
		// DTO's field, and the notification path. The framework declares it and
		// refuses to invent it; so does this.
		if c.Plural == "" {
			ps.BlockerFix(where+".plural",
				"the collection name is required",
				"it is the document segment, the read DTO field and the notification "+
					"path, all at once — declare it as the domain says it, e.g. "+
					"plural: Responsaveis")
		} else if !goIdentRe.MatchString(c.Plural) {
			ps.BlockerFix(where+".plural",
				fmt.Sprintf("%q is not valid as an exported Go field name", c.Plural),
				"first character A-Z, then letters or digits — the framework panics "+
					"at boot on anything else")
		}
		if c.ParentColumn == "" {
			ps.BlockerFix(where+".parentColumn",
				"the foreign key back to the owner is required",
				"a column name outlives the decision that made it — renaming one later "+
					"is a migration, so it is declared, e.g. parentColumn: matricula_id")
		} else {
			checkColumnName(c.ParentColumn, where+".parentColumn", ps)
		}
		if !ChildOwners.Has(c.OwnedBy) {
			ps.BlockerFix(where+".ownedBy",
				fmt.Sprintf("%q is not an owner", c.OwnedBy),
				"one of: "+ChildOwners.String())
		}
		if c.OwnedBy == "base" && s.Storage.Kind != "sharedbase-role" {
			ps.BlockerFix(where+".ownedBy",
				"only a shared-base model has a base to own children",
				"use ownedBy: root")
		}
		// A base-owned collection on a role that REUSES the base is MOUNTED: the
		// table, the schema and the entry type belong to the role that declared
		// the identity, and this spec only puts the collection on its own
		// surface. It used to be refused outright, which conflated writing the
		// storage with exposing it — and the cost of that refusal was every
		// route, command, request and test for the collection written by hand.
		if c.OwnedBy == "base" && s.Storage.Base != nil && s.Storage.Base.Reuse {
			validateMountedChild(s, c, where, ps, opt)
		}
		if c.OwnedBy == "role" && s.Storage.Kind != "sharedbase-role" {
			ps.BlockerFix(where+".ownedBy",
				"only a shared-base model has a role to own children",
				"use ownedBy: root")
		}
		if !EditStrategies.Has(c.EditStrategy) {
			ps.BlockerFix(where+".editStrategy",
				fmt.Sprintf("%q is not an edit strategy", c.EditStrategy),
				"atomic-replace (the root's update replaces the whole collection) | "+
					"per-child (add/update/archive by id)")
		}
		if len(c.Fields) == 0 {
			ps.Blockerf(where+".fields", "a child needs at least one field")
		}
		checkFieldDups(c.Fields, where, ps)
		if c.ArchivedAt != "" {
			checkColumnName(c.ArchivedAt, where+".archivedAt", ps)
		}
		for j, f := range c.Fields {
			validateOneField(s, f, fmt.Sprintf("%s.fields[%d] (%s)", where, j, orUnnamed(f.Name)), ps, true, false, opt)
		}
		for _, bi := range c.BusinessIdentity {
			if findField(c.Fields, bi) == nil {
				ps.Blockerf(where+".businessIdentity",
					"%q does not name a field of this child", bi)
			}
		}
		if len(c.BusinessIdentity) == 0 {
			ps.BlockerFix(where+".businessIdentity",
				"the child does not say what makes two entries the same",
				"list the fields that identify it in business terms — the framework matches "+
					"children by that, never by comparing every field")
		}
		// A soft removal must have somewhere to record it.
		if c.SoftRemove && c.ArchivedAt == "" {
			ps.BlockerFix(where+".archivedAt",
				"the child is soft-removable but declares no archive column",
				"name the column, or set softRemove: false for a hard delete")
		}
		if !c.SoftRemove && c.ArchivedAt != "" {
			ps.BlockerFix(where+".archivedAt",
				"an archive column is declared but the child is not soft-removable",
				"set softRemove: true, or drop the column")
		}
		if c.EditStrategy == "per-child" && len(c.BusinessIdentity) == 0 {
			ps.BlockerFix(where+".businessIdentity",
				"a per-entry collection needs a business identity",
				"it is what an ADD compares against to answer duplicate, and what makes "+
					"two entries the same one; declare the field(s) that identify an entry")
		}
		validateChildOperations(c, where, ps)
		validateChildChange(s, c, where, ps)
		validateChildPermissions(s, c, where, ps)
		// A per-entry collection mounts up to three verbs, and one of them can
		// be meaningless: if every writable field is part of the business
		// identity, the PUT's only possible effect is to turn entry A into entry
		// B while keeping A's row id — a revoke plus a grant wearing an edit's
		// clothes. There is nothing about such an entry that can change and
		// still leave it the same entry.
		//
		// Said, not refused: keeping A's row (and its created_at) through the
		// swap is a defensible thing to want, and it is the author's API. What
		// is not defensible is finding out by reading the generated routes.
		//
		// It is not said at all once the author has answered it, which is what
		// operations is for: a collection that mounts add and remove and no
		// change has no such verb to be surprised by.
		// Said about the PUT half only. A collection that serves patch alone has
		// no verb with this shape, and the same observation there is a BLOCKER
		// raised by validateChildChange — there would be nothing left for a
		// partial change to carry.
		if ChildServesPut(c) && len(c.Fields) > 0 &&
			len(c.BusinessIdentity) == len(c.Fields) {
			ps.WarnFix(where+".businessIdentity",
				"the collection has no field outside its business identity, so the "+
					"generated change verb can only replace one entry with another — "+
					"keeping the first one's row id",
				"if that is not a verb you want, drop it with operations: [add, remove] "+
					"— or add the field that is allowed to change (a note, a validity "+
					"date) and leave it out of businessIdentity. editStrategy: "+
					"atomic-replace is the third answer, and a different contract: the "+
					"root's update carries the whole collection, so an entry a partial "+
					"client omits is removed")
		}
		// The key reaches the ADD and only the ADD, so that is what the advice
		// names. It used to name the change path as well — "an update can edit
		// one entry into another's identity" — and that stopped being this key's
		// business at framework v0.63.0: ChangeAggregateChild now refuses a
		// replacement that takes an identity another ACTIVE entry holds, with the
		// framework's own EntityAlreadyAddedNotification. Declaring this one
		// would not make THAT rejection specific, and advice that recommends a
		// key for a case the key does not cover is worse than none.
		if MountsPerChildOp(c, "add") && c.SoftRemove && c.DuplicateNotification == "" {
			ps.WarnFix(where+".duplicateNotification",
				"no duplicate notification for a per-child collection",
				"an add can name an entry the collection already holds; naming a "+
					"notification makes the rejection specific")
		}
		validateNotificationRef(s, c.DuplicateNotification, where+".duplicateNotification", ps)
		// A per-entry verb addresses ONE entry, so its command carries the
		// entry's fields directly plus the id naming it — `<Child>ID`, taken
		// from the route segment. A field of the same name would land twice in
		// one struct, which the compiler refuses in generated code the author
		// did not write and cannot fix. Refuse it here, where the name is.
		//
		// Only the verbs that NAME an entry declare that field, so a collection
		// mounting add alone never collides — and refusing it there would be a
		// refusal about code this spec does not generate.
		if MountsPerChildOp(c, "change") || MountsPerChildOp(c, "remove") {
			clash := c.Name + "ID"
			for _, f := range c.Fields {
				if f.Name != clash {
					continue
				}
				ps.BlockerFix(where+".fields",
					fmt.Sprintf("%q collides with the path field the per-entry verbs "+
						"declare for this collection", clash),
					"the entry's own id already reaches the command from the route; "+
						"rename this field (or drop it, if it IS that id)")
			}
		}
	}
}

// validateChildOperations checks the key that says WHICH per-entry verbs a
// collection mounts.
//
// The key is a subtraction, so every mistake it can carry is one that silently
// removes a route: a misspelled verb, a verb selected on a collection that
// mounts none, an empty list that reads as "all of them" in YAML and as "none"
// to a reader. Each is refused with the whole set printed, because a missing
// endpoint is not something the author finds out about from the code — they
// find out from a client that gets a 404.
func validateChildOperations(c Child, where string, ps *Problems) {
	if c.Operations == nil {
		return
	}
	if c.EditStrategy != "per-child" {
		ps.BlockerFix(where+".operations",
			"operations picks among the PER-ENTRY verbs, and this collection mounts "+
				"none of them: an atomic replace edits the collection through the "+
				"root's own update",
			"drop it, or set editStrategy: per-child")
		return
	}
	if len(c.Operations) == 0 {
		ps.BlockerFix(where+".operations",
			"the list is empty, which is not the same as absent: absent mounts all "+
				"three verbs, and no collection mounts zero",
			"name the verbs you want — "+ChildOperations.String()+" — or drop the key")
		return
	}
	seen := map[string]bool{}
	for _, op := range c.Operations {
		switch {
		case !ChildOperations.Has(op):
			ps.BlockerFix(where+".operations",
				fmt.Sprintf("%q is not a per-entry verb", op),
				"one of: "+ChildOperations.String())
		case seen[op]:
			ps.Blockerf(where+".operations", "%q is named twice", op)
		}
		seen[op] = true
	}
}

// validateChildChange checks the key that says HOW the change verb takes its
// body — and insists, where the root does not have to, that the entry's
// business identity is off-limits to a partial one.
//
// The insistence is the whole difference between the two levels, and it is a
// CHECK rather than a silent exclusion so that the language stays the one the
// author already knows. At the root, a natural key is kept out of the PATCH
// body by naming it under update.patchExcludes; here the same words do the same
// job. What differs is the consequence of forgetting: the root's row is
// addressed by an id no body carries, so a natural key left patchable is at
// worst a re-key the rules can refuse, while an ENTRY's identity left patchable
// turns one grant into another while keeping the first one's row id — the exact
// shape children[].operations documents as the reason to drop change entirely,
// produced here by the generator itself. So the spec is refused, with the line
// to write.
//
// The other two refusals are the root's own, transposed. A partial change with
// nothing left to change accepts a body and does nothing (update.patchExcludes
// is refused the same way), and it is the honest end of the collection whose
// every field is its identity: the answer there is operations without change,
// not a verb with an empty body. And a collection named after its own entity
// would have this build write Patch<Name>Command twice into one package —
// generated code the author did not write and cannot fix.
func validateChildChange(s *Spec, c Child, where string, ps *Problems) {
	if c.Change != nil {
		if c.EditStrategy != "per-child" {
			ps.BlockerFix(where+".change",
				"change shapes a PER-ENTRY verb, and this collection mounts none of them: "+
					"an atomic replace is edited through the root's own update, in the "+
					"root's own shape",
				"drop it, or set editStrategy: per-child")
			return
		}
		if !MountsPerChildOp(c, "change") {
			ps.BlockerFix(where+".change",
				"change is shaped here but is not among the verbs this collection mounts",
				"add change to operations, or drop this block")
			return
		}
		if c.Change.Shape == "" {
			ps.BlockerFix(where+".change.shape",
				"the block is declared and does not say what shape the verb has",
				"patch (partial) | put (full body) | both — leaving the whole block out "+
					"means put, which is what a collection without it serves")
		} else if !UpdateShapes.Has(c.Change.Shape) {
			ps.BlockerFix(where+".change.shape",
				fmt.Sprintf("%q is not a change shape", c.Change.Shape),
				"one of: "+UpdateShapes.String())
		}
		for _, ex := range c.Change.PatchExcludes {
			// Same two spellings the root accepts: a composite may be excluded
			// whole, or one exposed part at a time.
			if findField(c.Fields, ex) == nil && findLogicalField(c.Fields, s, ex) == nil {
				ps.Blockerf(where+".change.patchExcludes",
					"%q does not name a field of this collection", ex)
			}
		}
	}
	if !ChildServesPatch(c) {
		return
	}

	excluded := func(name string) bool { return c.Change != nil && contains(c.Change.PatchExcludes, name) }
	var open []string
	for _, bi := range c.BusinessIdentity {
		if !excluded(bi) {
			open = append(open, bi)
		}
	}
	if len(open) > 0 {
		ps.BlockerFix(where+".change.patchExcludes",
			fmt.Sprintf("a partial change that accepts %s re-keys the entry while keeping "+
				"its row id — the audit trail then reads as one entry being edited where "+
				"two things happened", strings.Join(quoted(open), ", ")),
			fmt.Sprintf("name %s under change.patchExcludes — the identity of the entry "+
				"the caller addressed comes from what is STORED, never from the body; if "+
				"the entry really is meant to be swapped for another, that is "+
				"shape: put, or operations without change",
				strings.Join(quoted(open), ", ")))
	}

	patchable := 0
	for _, f := range c.Fields {
		if excluded(f.Name) {
			continue
		}
		if !IsComposite(f) {
			patchable++
			continue
		}
		for _, p := range f.Parts {
			if !excluded(ExposedName(p)) {
				patchable++
			}
		}
	}
	if patchable == 0 {
		ps.BlockerFix(where+".change.shape",
			"every field of the entry is excluded from the partial change, so it could "+
				"never change anything",
			"leave at least one field patchable — or, if the entry has nothing outside "+
				"its business identity, take the verb out with operations: [add, remove]")
	}

	if c.Name == s.Entity {
		ps.BlockerFix(where,
			fmt.Sprintf("the collection is named after its own entity, and a partial "+
				"change generates Patch%sCommand into the same package the entity's own "+
				"patch types live in", c.Name),
			"rename the collection — the two names travel together through every "+
				"generated type, and only one of them can hold the name")
	}
}

// quoted renders a list for a message, each entry in its own quotes. A refusal
// naming three fields runs them together otherwise, and the reader has to guess
// where one ends.
func quoted(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, fmt.Sprintf("%q", n))
	}
	return out
}

// validateChildPermissions checks the key that gates the per-entry verbs on
// their own, and the one case where the inheritance it replaces has nothing to
// inherit.
//
// Everything this key can get wrong is silent in the generated code, and silent
// in opposite directions. A permission on a verb the collection does not mount
// is dead configuration that reads, in the spec, as a route being guarded. A
// misspelled verb is the same thing wearing a plausible name. And a verb left
// to inherit from a root that serves no update, patch or insert inherits the
// empty string — a route with a permission requirement no claim can satisfy,
// which fails closed at runtime and says nothing at generation time.
func validateChildPermissions(s *Spec, c Child, where string, ps *Problems) {
	if c.EditStrategy != "per-child" {
		if c.Permissions != nil {
			ps.BlockerFix(where+".permissions",
				"permissions gates the PER-ENTRY verbs, and this collection mounts none "+
					"of them: an atomic replace is edited through the root's own update, "+
					"under the root's own permission",
				"drop it, or set editStrategy: per-child")
		}
		return
	}
	if c.Permissions != nil && len(c.Permissions) == 0 {
		ps.BlockerFix(where+".permissions",
			"the map is empty, which is not the same as absent: absent means every "+
				"entry verb keeps requiring what the root's update requires",
			"name the verb(s) you want gated separately — "+ChildOperations.String()+
				" — or drop the key")
		return
	}

	// Unknown keys first, sorted, so two runs over the same spec print the same
	// report: a map has no order, and a diagnostic that moves between runs is
	// one a reviewer cannot diff.
	var unknown []string
	for key := range c.Permissions {
		if !ChildOperations.Has(key) {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		ps.BlockerFix(where+".permissions",
			fmt.Sprintf("%q is not a per-entry verb", key),
			"one of: "+ChildOperations.String())
	}

	for _, key := range ChildOperations.List() {
		value, declared := c.Permissions[key]
		if !declared {
			continue
		}
		if !MountsPerChildOp(c, key) {
			ps.BlockerFix(where+".permissions",
				fmt.Sprintf("a permission is declared for %q but that verb is not mounted", key),
				"add it to operations, or remove the permission")
			continue
		}
		if strings.TrimSpace(value) == "" {
			ps.BlockerFix(where+".permissions."+key,
				"the permission is empty",
				"an empty string registers a route that no permission can satisfy")
			continue
		}
		// The same warning the root's permissions get, for the same reason: the
		// framework does not require the prefix, so a short claim is legitimate,
		// but a permission outside the resource's namespace is also what a typo
		// or a claim borrowed from another entity looks like.
		if s.Authz.Resource != "" && !strings.HasPrefix(value, s.Authz.Resource+":") {
			ps.WarnFix(where+".permissions."+key,
				fmt.Sprintf("%q is not namespaced by the declared resource %q",
					value, s.Authz.Resource),
				fmt.Sprintf("if that is unintended, spell it %s:<action> — a borrowed "+
					"claim grants the wrong thing quietly", s.Authz.Resource))
		}
	}

	// What the undeclared verbs fall back to. It is checked HERE, next to the
	// key that overrides it, because the key is also the fix: a collection on an
	// entity that serves no write of its own can still be gated, it just cannot
	// be gated by inheritance.
	if InheritedChildPermission(s) != "" {
		return
	}
	for _, op := range PerChildOperations(c) {
		if strings.TrimSpace(c.Permissions[op]) != "" {
			continue
		}
		ps.BlockerFix(where+".permissions",
			fmt.Sprintf("the %s verb is mounted and has no permission: it inherits the "+
				"root's update, and this entity serves no update, patch or insert to "+
				"inherit from", op),
			fmt.Sprintf("declare it — permissions: {%s: %s} — or drop the verb with "+
				"operations", op, CanonicalPermission(s.Authz.Resource, "update")))
	}
}

func validateSiblings(s *Spec, ps *Problems, opt Options) {
	seen := map[string]bool{}
	for i, sib := range s.Siblings {
		where := fmt.Sprintf("siblings[%d] (%s)", i, orUnnamed(sib.Name))
		if sib.Name == "" || !goIdentRe.MatchString(sib.Name) {
			ps.BlockerFix(where, "the sibling needs a PascalCase name", "e.g. Scholarship")
		} else if seen[sib.Name] {
			ps.Blockerf(where, "the sibling %q is declared twice", sib.Name)
		} else {
			seen[sib.Name] = true
		}
		if sib.Table == "" {
			ps.Blockerf(where+".table", "the sibling table name is required")
		} else {
			checkTableName(sib.Table, where+".table", ps)
		}
		checkFieldDups(sib.Fields, where, ps)
		// A facet's fields live on the SAME Go struct as its attachment node's
		// own fields — the split is storage, not shape — so a name shared with
		// the node is two struct fields with one name, a compile error nothing
		// pointed back at the spec for.
		nodeFields := s.Fields
		nodeLabel := "the entity"
		if strings.HasPrefix(sib.AttachTo, "child:") {
			if c := findChild(s.Children, strings.TrimPrefix(sib.AttachTo, "child:")); c != nil {
				nodeFields, nodeLabel = c.Fields, "the child "+c.Name
			}
		}
		for _, f := range sib.Fields {
			if f.Name != "" && findField(nodeFields, f.Name) != nil {
				ps.Blockerf(where+".fields",
					"%q is also a field of %s — the facet shares its struct, so the "+
						"name collides", f.Name, nodeLabel)
			}
		}

		switch {
		case sib.AttachTo == "root":
			if s.Storage.Kind == "sharedbase-role" {
				ps.BlockerFix(where+".attachTo",
					"a shared-base model has no \"root\" to attach a sibling to",
					"attach it to the role — a base-level 1:1 facet is nullable columns ON the base, "+
						"because a base has many roles and the framework panics on a base sibling")
			}
		case sib.AttachTo == "role":
			if s.Storage.Kind != "sharedbase-role" {
				ps.BlockerFix(where+".attachTo", "there is no role in a flat model", "use attachTo: root")
			}
		case sib.AttachTo == "base":
			ps.BlockerFix(where+".attachTo",
				"a sibling cannot attach to a shared base — the framework panics at boot",
				"a base has many roles, so the 1:1 does not hold; put the facet as nullable "+
					"columns on the base, or attach the sibling to the role")
		case strings.HasPrefix(sib.AttachTo, "child:"):
			name := strings.TrimPrefix(sib.AttachTo, "child:")
			c := findChild(s.Children, name)
			if c == nil {
				ps.Blockerf(where+".attachTo", "there is no child named %q", name)
			} else if c.OwnedBy == "base" {
				ps.BlockerFix(where+".attachTo",
					fmt.Sprintf("%q is a base child, and a sibling cannot attach to one", name),
					"the framework panics at boot; attach the facet to a role child instead")
			}
		default:
			ps.BlockerFix(where+".attachTo",
				fmt.Sprintf("%q is not an attachment node", sib.AttachTo),
				"one of: root | role | child:<name>")
		}

		if len(sib.Fields) == 0 {
			ps.Blockerf(where+".fields", "a sibling needs at least one field")
		}
		for j, f := range sib.Fields {
			fw := fmt.Sprintf("%s.fields[%d] (%s)", where, j, orUnnamed(f.Name))
			validateOneField(s, f, fw, ps, true, true, opt)
			// The facet is optional as a whole: an all-nil facet means "no row".
			// A non-nullable column in it could never be cleared.
			if !f.Nullable {
				ps.BlockerFix(fw,
					"a sibling's fields must be nullable",
					"the facet exists only when it has values; an absent facet means no row")
			}
		}
	}

	// PATCH cannot express "set this to null", so a clearable facet needs PUT.
	if len(s.Siblings) > 0 && s.Update.Shape == "patch" {
		ps.BlockerFix("update.shape",
			"the model has a sibling facet but the root only accepts PATCH",
			"PATCH cannot assign null, so the facet could never be cleared; use put or both")
	}
}

func validateLifecycle(s *Spec, ps *Problems) {
	if len(s.Modes) == 0 {
		ps.BlockerFix("modes", "the entity declares no modes", "at minimum: [display]")
	}
	has := map[string]bool{}
	for i, m := range s.Modes {
		if !Modes.Has(m) {
			ps.BlockerFix(fmt.Sprintf("modes[%d]", i),
				fmt.Sprintf("%q is not a mode", m), "one of: "+Modes.String())
			continue
		}
		if has[m] {
			ps.Warnf("modes", "the mode %q is listed twice", m)
		}
		has[m] = true
	}

	// Modes ⟺ the archive column: the framework cross-checks these at
	// repository construction and panics when they disagree.
	archiving := has["archive"] || has["unarchive"]
	if archiving && s.Storage.Managed.ArchivedAt == "" {
		ps.BlockerFix("storage.managed.archivedAt",
			"the entity archives but declares no archive column",
			"declare it (e.g. archivedAt: deleted_at) — the framework refuses the mismatch at boot")
	}
	if has["unarchive"] && !has["archive"] {
		ps.BlockerFix("modes",
			"unarchive without archive",
			"there is nothing to bring back; add archive or drop unarchive")
	}

	if s.Update.Shape != "" && !UpdateShapes.Has(s.Update.Shape) {
		ps.BlockerFix("update.shape",
			fmt.Sprintf("%q is not an update shape", s.Update.Shape),
			"one of: "+UpdateShapes.String())
	}
	if has["update"] && s.Update.Shape == "" {
		ps.BlockerFix("update.shape",
			"the entity updates but does not say how",
			"patch (partial, the common case) | put (full body) | both")
	}
	if !has["update"] && s.Update.Shape != "" {
		ps.BlockerFix("update.shape",
			"an update shape is declared but update is not among the modes",
			"add update to modes, or remove the shape")
	}
	for _, ex := range s.Update.PatchExcludes {
		// A composite may be excluded whole (its own name, which takes every part
		// with it) or one part at a time (an exposed name), because a partial
		// update sees the parts and the domain sees the value object.
		if findField(s.Fields, ex) == nil && findLogicalField(s.Fields, s, ex) == nil {
			ps.Blockerf("update.patchExcludes", "%q does not name a field of this entity", ex)
			continue
		}
		// Excluding a field no patch carries in the first place reads as a
		// decision and is not one. A stamped column is out of every write
		// surface already, so the line does nothing and the next reader has to
		// go and find that out.
		if ex := findField(s.Fields, ex); ex != nil && ex.Stamped != "" {
			ps.WarnFix("update.patchExcludes",
				fmt.Sprintf("%q is a stamped column, which no write request carries to begin with", ex.Name),
				"drop the exclusion — it changes nothing")
		}
	}
	// A patch with nothing left to patch accepts a body and changes nothing —
	// and it used to panic the generator instead of being refused here.
	if s.Update.Shape == "patch" || s.Update.Shape == "both" {
		patchable := 0
		for _, f := range s.Fields {
			if f.Runtime || f.AssignedFrom != "" || f.Stamped != "" || contains(s.Update.PatchExcludes, f.Name) {
				continue
			}
			patchable++
		}
		for _, sib := range s.Siblings {
			if strings.HasPrefix(sib.AttachTo, "child:") {
				continue
			}
			patchable += len(sib.Fields)
		}
		if patchable == 0 {
			ps.BlockerFix("update.patchExcludes",
				"every writable field is excluded, so a patch could never change anything",
				"leave at least one field patchable, or drop patch from update.shape")
		}
	}

	if s.Delete.Root != "" && !DeleteRoot.Has(s.Delete.Root) {
		ps.BlockerFix("delete.root",
			fmt.Sprintf("%q is not a delete semantic", s.Delete.Root),
			"one of: "+DeleteRoot.String())
	}
	if s.Delete.Children != "" && !DeleteChild.Has(s.Delete.Children) {
		ps.BlockerFix("delete.children",
			fmt.Sprintf("%q is not a delete semantic", s.Delete.Children),
			"one of: "+DeleteChild.String())
	}
	// The key parses and nothing reads it — a collection's removal semantics
	// are declared per child, on the child. Accepting it here meant the author
	// believed it changed something and it changed nothing.
	if s.Delete.Children != "" {
		ps.BlockerFix("delete.children",
			"a blanket delete semantic for the collections is not generated by this "+
				"build — nothing reads the key",
			"declare removal per collection with children[].softRemove, and drop this key")
	}
	// The HTTP verb must tell the truth: DELETE is a purge, archive is soft.
	hard := s.Delete.Root == "hard" || s.Delete.Root == "both"
	if hard && !has["delete"] {
		ps.BlockerFix("delete.root",
			"a hard delete is declared but delete is not among the modes",
			"add delete to modes, or set delete.root: soft")
	}
	if has["delete"] && !hard {
		ps.BlockerFix("delete.root",
			"the delete mode is declared but delete.root is not hard",
			"DELETE is exclusively an irreversible purge; a reversible removal is archive")
	}
	soft := s.Delete.Root == "soft" || s.Delete.Root == "both"
	if soft && !archiving {
		ps.BlockerFix("delete.root",
			"a soft delete is declared but the entity does not archive",
			"add archive (and usually unarchive) to modes")
	}
	validateArchiveWhen(s, has, ps)
}

// validateArchiveWhen checks the one lifecycle rule that changes what a write
// IS rather than whether it is allowed.
//
// Everything here is about the two ways it can be declared and mean nothing: an
// entity that cannot archive at all (the framework panics when the rule fires),
// and a trigger value the field can never hold (the condition compiles, never
// matches, and nothing reports it).
func validateArchiveWhen(s *Spec, has map[string]bool, ps *Problems) {
	aw := s.Delete.ArchiveWhen
	if aw == nil {
		return
	}
	const w = "delete.archiveWhen"
	if !has["archive"] {
		ps.BlockerFix(w,
			"this update is declared to finish as an archive, but the entity does not archive",
			"add archive to modes — the framework panics when a rule asks an entity that "+
				"forbids archiving to complete as one")
	}
	if !has["update"] {
		ps.BlockerFix(w,
			"this is a decision an UPDATE makes, and the entity serves no update",
			"add update to modes, or drop the key — the archive verb reaches the row "+
				"through its own door")
	}
	if aw.Field == "" {
		ps.BlockerFix(w+".field", "the field that decides is required",
			"name one field — the state, e.g. field: Status")
	}
	if aw.Equals == "" {
		ps.BlockerFix(w+".equals", "the value that means \"retire this row\" is required",
			"e.g. equals: closing")
	}
	// The same emptiness is tested twice on purpose: the blocker above reports
	// it, and this bails out only AFTER equals has had its say, so an author who
	// wrote neither key is told about both in one run instead of one per run.
	if aw.Field == "" {
		return
	}
	f := findField(s.Fields, aw.Field)
	if f == nil {
		ps.Blockerf(w+".field", "%q does not name a field of this entity", aw.Field)
		return
	}
	if f.Runtime {
		ps.BlockerFix(w+".field",
			"a runtime-only field is fed from the caller's token, so it does not say "+
				"anything about the row's state",
			"decide on a persisted field")
	}
	if f.Nullable {
		ps.BlockerFix(w+".field",
			"a nullable field cannot carry the decision: \"no value\" is not a state",
			"decide on a non-nullable field")
	}
	if f.Type != "string" {
		ps.BlockerFix(w+".field",
			"the trigger is compared as text, and this field is not text",
			"decide on a string field (an enum value object is the usual one)")
	}
	// When the field IS an enum declared here, the values are a closed set and a
	// typo is checkable — which is the whole difference between a condition that
	// fires and one that silently never does.
	if f.VO != nil && f.VO.Kind == "enum" {
		if vo := findVO(s.ValueObjects, f.VO.Ref); vo != nil {
			members := map[string]bool{}
			for _, mb := range vo.Members {
				members[fmt.Sprint(mb.Value)] = true
			}
			if !members[aw.Equals] {
				ps.Blockerf(w+".equals", "%q is not a member value of %s", aw.Equals, vo.Name)
			}
			if aw.Becomes != "" && !members[aw.Becomes] {
				ps.Blockerf(w+".becomes", "%q is not a member value of %s", aw.Becomes, vo.Name)
			}
		}
	}
	if aw.Becomes != "" && aw.Becomes == aw.Equals {
		ps.BlockerFix(w+".becomes",
			"the resting value is the trigger value, so the assignment does nothing",
			"drop becomes — the row is archived holding the trigger value either way")
	}
	warnUnreachableTrigger(s, aw, w, ps)
}

// warnUnreachableTrigger covers the two ways an update cannot MOVE the field to
// the trigger, which is the same silence the enum check above refuses: the
// condition compiles, no caller ever reaches it, and nothing says so.
//
// They are warnings rather than blockers because one path survives both: a row
// INSERTED already holding the trigger value. It is then retired by the next
// update that touches it — whatever that update changes — which is a stranger
// behaviour than the one the author was asking for, and worth saying either way.
func warnUnreachableTrigger(s *Spec, aw *ArchiveWhen, w string, ps *Problems) {
	// `both` and `put` still serve a full body, where the field is writable;
	// only a patch-only entity has no door left.
	if s.Update.Shape == "patch" && contains(s.Update.PatchExcludes, aw.Field) {
		ps.WarnFix(w+".field",
			fmt.Sprintf("%q is in update.patchExcludes, and patch is this entity's only "+
				"update shape", aw.Field),
			"no update can set the trigger, so the row retires only if it was INSERTED "+
				"holding it — drop the exclusion, serve put as well, or decide on another field")
	}
	for _, r := range s.Rules.List {
		if r.Kind != "immutable" || !contains(r.Fields, aw.Field) {
			continue
		}
		if !contains(r.Scope, "update") && !contains(r.Scope, "insertOrUpdate") {
			continue
		}
		ps.WarnFix(w+".field",
			fmt.Sprintf("%q is immutable on update (rule %q), so a write that moves it to "+
				"the trigger is refused before it can retire anything", aw.Field, r.ID),
			"decide on a mutable field, or narrow the immutability — the two rules are "+
				"asking the same field to never change and to change into a removal")
		return
	}
}

// frameworkNotifications are supplied by the framework and already translated.
// Naming one costs nothing; declaring a duplicate of one is noise.
var frameworkNotifications = map[string]bool{
	"RequiredFieldNotification":      true,
	"SchemaViolationNotification":    true,
	"RecordNotFoundNotification":     true,
	"EntityAlreadyAddedNotification": true,
	"EntityDoesNotExistNotification": true,
	"ArchiveNotAllowedNotification":  true,
}

// IsFrameworkNotification reports whether a name is one the FRAMEWORK supplies.
//
// It exists so the resolver can qualify a reference correctly: a notification
// the service declares is generated into the package that names it, while a
// framework one lives in the framework's own domain package and needs its
// qualifier. Keeping the set here, with the validator that already owns it,
// is what stops a second copy from disagreeing with this one.
func IsFrameworkNotification(name string) bool { return frameworkNotifications[name] }

func validateRules(s *Spec, ps *Problems, opt Options) {
	validateRuleSet(s, s.Rules, RuleScopeOfRoot(s), "rules", ps, opt)
	for i, c := range s.Children {
		where := fmt.Sprintf("children[%d].rules", i)
		validateRuleSet(s, c.Rules, RuleScopeOfChild(s, c), where, ps, opt)
		validateAggregateWideKinds(c, where, ps)
	}
	validateCompositeRuleTargets(s, ps)
}

// aggregateWideKinds ask what the AGGREGATE holds, not what one entry says: a
// collection to count, a caller to compare against the row's owner. A rule
// declared inside children[] runs scoped to a single entry, which has neither.
//
// They were accepted here and emitted nothing — the emitter received no model
// and wrote no clause, silently. The kinds are refused by name instead, because
// the fix is never "drop it": it is to declare the same rule at the root, where
// the collection is in scope, and the message says so.
var aggregateWideKinds = map[string]string{
	"childDuplicate": "it compares the entries of a collection with each other, so it is " +
		"declared at the root naming the collection: rules.list[].fields: [%s]",
	"groupCap": "it counts the rows of a collection, so it is declared at the root " +
		"naming the collection: rules.list[].fields: [%s]",
	"ownerCheck": "it compares the CALLER against the row's owner, and the owner is a " +
		"field of the entity — declare it at the root",
	"factRange": "it asks the domain service, and only the root is handed one — declare " +
		"it at the root, where the fact's arguments are fields of the entity",
}

func validateAggregateWideKinds(c Child, where string, ps *Problems) {
	for i, r := range c.Rules.List {
		fix, ok := aggregateWideKinds[r.Kind]
		if !ok {
			continue
		}
		if strings.Contains(fix, "%s") {
			// The collection name, which is what every message here shows now
			// that both spellings resolve. It used to have to be the singular —
			// the root-level shape check took only that one, so a fix spelling
			// the plural sent the author straight into a second refusal — and
			// that asymmetry is exactly what CollectionNamed removed.
			fix = fmt.Sprintf(fix, orUnnamed(c.Plural))
		}
		ps.BlockerFix(fmt.Sprintf("%s.list[%d] (%s).kind", where, i, orUnnamed(r.ID)),
			fmt.Sprintf("%q asks about the whole collection, and a rule declared here "+
				"sees one entry", r.Kind), fix)
	}
}

// validateRequiredOverValueObject catches the rule that makes one empty field
// answer twice.
//
// The framework validates every value-object field by reflection on every
// write — nothing declares it, and nothing can skip it short of
// IgnoreValueObject. A string-backed raw VO reports an empty value as
// RequiredFieldNotification (that is the shape this generator emits, and the
// shape the manual teaches), and an enum reports it as its unknown-member
// notification, because "" is not a member. So a `required` rule on such a
// field adds a SECOND notification for the one thing the caller got wrong, and
// the caller reads "Required field" twice for one empty value.
//
// A warning and not a blocker: the duplicate is noise, not a broken service,
// and a reused VO from the project may legitimately tolerate an empty value.
// Only value objects THIS spec declares are judged — for `vo.kind: reuse` the
// generator has not seen the IsValid and will not guess at it.
func validateRequiredOverValueObject(s *Spec, r Rule, scopeFields []Field, where string, ps *Problems) {
	if r.Kind != "required" {
		return
	}
	for _, name := range r.Fields {
		f, ok := fieldNamed(scopeFields, name)
		if !ok || f.VO == nil || f.Nullable {
			continue
		}
		vo, ok := declaredVO(s, f.VO.Ref)
		if !ok {
			continue
		}
		switch {
		case vo.Kind == "raw" && vo.Backing == "string":
			ps.WarnFix(where+".fields",
				fmt.Sprintf("%s is backed by the value object %s, and a string-backed one "+
					"already reports an empty value as RequiredFieldNotification — this rule "+
					"makes the caller receive it TWICE for one empty field", name, vo.Name),
				"drop this rule: the value object is validated automatically on every write, "+
					"so presence is already enforced. If what you need is that check EARLY — "+
					"because the rules below it depend on the value — declare kind: "+
					"valueObject over the field instead, with guard: true")
		case vo.Kind == "enum":
			ps.WarnFix(where+".fields",
				fmt.Sprintf("%s is backed by the enum %s, and an empty value is already "+
					"answered with %s — this rule adds a SECOND notification for the same "+
					"empty field", name, vo.Name, orUnnamed(vo.UnknownNotification)),
				"drop this rule: enum membership is validated automatically on every write. "+
					"If what you need is that check EARLY — because the rules below it depend "+
					"on the value — declare kind: valueObject over the field instead, with "+
					"guard: true")
		}
	}
}

func fieldNamed(fields []Field, name string) (Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

func declaredVO(s *Spec, ref string) (ValueObject, bool) {
	for _, vo := range s.ValueObjects {
		if vo.Name == ref {
			return vo, true
		}
	}
	return ValueObject{}, false
}

// RuleScopeOfRoot is every field a rule on the entity can talk about.
//
// A 1:1 facet is a STORAGE decision — the same Go struct, split across two
// tables so the columns can be null in bulk. Its fields are fields of the
// entity, reachable on the same receiver, and refusing a rule on one of them
// pushed invariants the DSL can perfectly express into the hand-written escape.
func RuleScopeOfRoot(s *Spec) []Field {
	out := append([]Field{}, s.Fields...)
	for _, sib := range s.Siblings {
		if !strings.HasPrefix(sib.AttachTo, "child:") {
			out = append(out, sib.Fields...)
		}
	}
	return out
}

// RuleScopeOfChild is the same for a collection: its own fields plus the fields
// of a facet declared inside it, which land on the child's type.
func RuleScopeOfChild(s *Spec, c Child) []Field {
	out := append([]Field{}, c.Fields...)
	for _, sib := range s.Siblings {
		if sib.AttachTo == "child:"+c.Name {
			out = append(out, sib.Fields...)
		}
	}
	return out
}

func validateRuleSet(s *Spec, rs Rules, scopeFields []Field, where string, ps *Problems, opt Options) {
	seenID := map[string]bool{}
	for i, r := range rs.List {
		w := fmt.Sprintf("%s.list[%d] (%s)", where, i, orUnnamed(r.ID))
		if r.ID == "" {
			ps.BlockerFix(w, "the rule needs an id", "a short stable slug, e.g. grade-range")
		} else if seenID[r.ID] {
			ps.Blockerf(w, "the rule id %q is used twice", r.ID)
		} else {
			seenID[r.ID] = true
		}
		if !RuleKinds.Has(r.Kind) {
			ps.BlockerFix(w+".kind",
				fmt.Sprintf("%q is not a rule kind", r.Kind),
				"one of: "+RuleKinds.String()+" — anything else goes under rules.manual")
			continue
		}
		validateRuleScope(r, w, ps)
		validateRuleFields(r, scopeFields, w, ps)
		validateRuleNotification(s, r, w, ps)
		validateRuleShape(s, r, scopeFields, w, ps, opt)
		validateRequiredOverValueObject(s, r, scopeFields, w, ps)
		validateEchoValue(r, scopeFields, w, ps)
	}
	// Across the whole set, not per rule: two rules that each look right can
	// still validate one value object twice in one pass.
	validateValueObjectHoists(rs, where, ps)

	for i, m := range rs.Manual {
		w := fmt.Sprintf("%s.manual[%d] (%s)", where, i, orUnnamed(m.ID))
		if m.ID == "" {
			ps.BlockerFix(w, "the manual rule needs an id", "it names the item in the hook file")
		} else if seenID[m.ID] {
			ps.Blockerf(w, "the rule id %q is used twice", m.ID)
		} else {
			seenID[m.ID] = true
		}
		// This is the check that keeps the escape hatch honest: an unnamed
		// residue becomes an empty TODO nobody can act on.
		if strings.TrimSpace(m.Description) == "" {
			ps.BlockerFix(w+".description",
				"a manual rule must say what it has to enforce",
				"this text is what the generated report tells the implementer to write; "+
					"without it the hook file is an empty TODO")
		}
		validateRuleScope(Rule{Scope: m.Scope}, w, ps)
		if m.Notification != "" {
			validateRuleNotification(s, Rule{Notification: m.Notification}, w, ps)
		}
	}
}

// validateEchoValue refuses an echo the emitter will not write.
//
// A `source: body` runtime field never travels back in a refusal, whatever the
// rule asked for — the emitter drops the echo on its own, because a field that
// exists to reach no copy of anything cannot be the one the 422 hands back. The
// DEFAULT needs no message: nothing was said, and nothing is lost. What is said
// out loud does, or `echoValue: true` becomes a key an author believes is in
// force while the build ignores it.
func validateEchoValue(r Rule, scopeFields []Field, w string, ps *Problems) {
	if r.EchoValue == nil || !*r.EchoValue {
		return
	}
	for _, fn := range r.Fields {
		f := findField(scopeFields, fn)
		if f == nil || !FromBody(*f) {
			continue
		}
		ps.BlockerFix(w+".echoValue",
			fmt.Sprintf("%q is fed from the REQUEST BODY, and such a value is never echoed "+
				"back in a refusal", fn),
			"drop echoValue — the rule still reports, attached to the same field, and "+
				"only the value is left out. A source: body field reaches no column, so no "+
				"payload, no audit event and no response; the canonical one is a password "+
				"confirmation, and a 422 carrying it would put the plaintext in the "+
				"response body and in every log that renders a notification")
		return
	}
}

func validateRuleScope(r Rule, w string, ps *Problems) {
	if len(r.Scope) == 0 {
		ps.BlockerFix(w+".scope",
			"the rule does not say when it applies",
			"one or more of: "+RuleScopes.String())
	}
	for _, sc := range r.Scope {
		if !RuleScopes.Has(sc) {
			ps.BlockerFix(w+".scope",
				fmt.Sprintf("%q is not a scope", sc),
				"one of: "+RuleScopes.String())
		}
	}
}

func validateRuleFields(r Rule, scopeFields []Field, w string, ps *Problems) {
	// A factRange names a FACT, not a field: the subject of the rule is a number
	// the service answers, and where the notification lands is attachTo's job.
	if r.Kind == "factRange" {
		if len(r.Fields) > 0 {
			ps.BlockerFix(w+".fields",
				"a factRange reads a fact, not a field of the entity",
				"drop fields — name the fact with fact:, and the field the answer is "+
					"about with attachTo:")
		}
		return
	}
	if len(r.Fields) == 0 {
		ps.Blockerf(w+".fields", "the rule names no field")
		return
	}
	// Two kinds read a COLLECTION rather than fields; what they name is checked
	// against the children, by validateRuleShape.
	if r.Kind == "childDuplicate" || r.Kind == "groupCap" {
		return
	}
	for _, fn := range r.Fields {
		if findField(scopeFields, fn) == nil {
			ps.Blockerf(w+".fields", "%q does not name a field in this scope", fn)
		}
	}
}

// validateFactRange keeps the rule and the fact it reads in step.
//
// The rule is the half that was missing: the language could DECLARE a count, a
// sum or a per-group aggregate and had no way to say what the number may be, so
// every author with a limit to enforce wrote the comparison by hand in the
// manual hook — for an invariant whose shape is always the same.
func validateFactRange(s *Spec, opt Options, r Rule, scopeFields []Field, w string, ps *Problems) {
	if r.Notification == "" {
		ps.Blockerf(w+".notification", "the rule needs the notification it raises")
	}
	if r.Min == nil && r.Max == nil {
		ps.BlockerFix(w,
			"the rule says nothing about what the fact's answer may be",
			"set max: (a ceiling), min: (a floor), or both")
	}
	if r.AttachTo == "" {
		ps.BlockerFix(w+".attachTo",
			"a fact's answer is not a field, so the notification has nowhere to land",
			"set attachTo: <field> — the field the caller should look at when the "+
				"limit is exceeded")
	} else if findField(scopeFields, r.AttachTo) == nil {
		ps.Blockerf(w+".attachTo", "%q does not name a field in this scope", r.AttachTo)
	}
	if r.Fact == "" {
		ps.BlockerFix(w+".fact",
			"the rule needs the fact whose answer it limits",
			"set fact: <name> — one of service.facts")
		return
	}
	if s.Service == nil || !s.Service.Required {
		ps.BlockerFix(w+".fact",
			"the rule reads a fact and the entity declares no service",
			"add service.required: true with the fact this rule names")
		return
	}
	// A fact answering SEVERAL numbers is addressed one number at a time —
	// `<Fact>.<As>` — the same dotted spelling every other key of this language
	// uses to reach inside something. A rule bounds ONE number, so the choice
	// cannot be left implicit: which of them is not something a generator may
	// pick.
	factName, slotName, dotted := strings.Cut(r.Fact, ".")
	var found *Fact
	for i := range s.Service.Facts {
		if s.Service.Facts[i].Name == factName {
			found = &s.Service.Facts[i]
			break
		}
	}
	if found == nil {
		ps.Blockerf(w+".fact", "%q does not name a fact of this entity's service", factName)
		return
	}
	switch {
	case dotted && len(found.Aggregates) == 0:
		ps.BlockerFix(w+".fact",
			fmt.Sprintf("%s answers ONE number, so there is nothing to reach inside", factName),
			fmt.Sprintf("name it plainly: fact: %s", factName))
		return
	case dotted && FindFactAggregate(*found, slotName) == nil:
		ps.BlockerFix(w+".fact",
			fmt.Sprintf("%s answers no number called %q", factName, slotName),
			"one of: "+strings.Join(factAggregateNames(*found), ", "))
		return
	case !dotted && len(found.Aggregates) > 0:
		ps.BlockerFix(w+".fact",
			fmt.Sprintf("%s answers several numbers and a range bounds one", factName),
			fmt.Sprintf("say which: fact: %s.<one of %s>", factName,
				strings.Join(factAggregateNames(*found), ", ")))
		return
	}
	if existsKind(found.Kind) {
		ps.BlockerFix(w+".fact",
			fmt.Sprintf("%s answers yes or no, and a range has nothing to compare it against",
				found.Kind),
			"limit a count, a sum, an avg, a min or a max — or enforce the exists "+
				"through unique.enforce, which is what it is for")
	}
	if found.Kind == "manual" && found.Returns != "int64" && found.Returns != "float64" {
		ps.BlockerFix(w+".fact",
			fmt.Sprintf("%s returns %s, which is not a number to compare", r.Fact, found.Returns),
			"limit a fact that answers int64 or float64")
	}
	// A declarative rule fills the fact's arguments from the ENTITY, so every
	// filter must be a value the entity is holding when the rule runs. A part of
	// an OPTIONAL composite is not: the value object is absent as a whole, and
	// there is no zero to pass that would not silently change the query the fact
	// runs. The service method is still generated and still callable — from
	// rules.manual, where the absent case is a branch someone writes on purpose.
	for _, fl := range append(FactFilterFields(found.Filters), found.Field) {
		if fl == "" {
			continue
		}
		owner := compositePartOwner(s.Fields, fl)
		if owner == nil || !owner.Nullable {
			continue
		}
		ps.BlockerFix(w+".fact",
			fmt.Sprintf("%s filters on %q, a part of the OPTIONAL composite value object "+
				"%q — when it is absent there is no value for this rule to pass", factName, fl, owner.Name),
			"call the fact from rules.manual, where the absent case is a branch you "+
				"write — or make the value object mandatory")
	}
	// And the same argument, one step further: an `in` asks about a SET, and the
	// entity carries one value per field. There is no honest way to fill that
	// parameter from the row being written — passing the single value would turn
	// the set into an equality and answer a question nobody asked.
	//
	// A set the SPEC pins is a different matter and stays legal here: it puts no
	// parameter in the signature, so there is nothing for this rule to fill.
	// A per-entry fact is about a COLLECTION, in either of its forms, and this
	// rule fills arguments from the root. Batched, the parameter is the whole
	// set of entries — the same argument the set operator gets refused for one
	// paragraph down. Once per entry, the value is on the collection's own
	// table and `e.<Field>` names nothing on the root, which generated a tree
	// that did not build.
	if found.PerEntry != "" {
		ps.BlockerFix(w+".fact",
			fmt.Sprintf("%s answers per entry of %s, and this rule fills a fact's "+
				"arguments from the ROOT being written", factName, found.PerEntry),
			fmt.Sprintf("call %s from rules.manual, where the collection is yours to "+
				"read and the keyed answer is yours to walk", factName))
		return
	}
	for _, coll := range factFilterCollections(s, *found) {
		ps.BlockerFix(w+".fact",
			fmt.Sprintf("%s is asked once per entry of %s, and this rule fills a fact's "+
				"arguments from the ROOT being written — which carries no field of that "+
				"collection", factName, coll),
			fmt.Sprintf("call %s from rules.manual, where the loop over the entries is "+
				"yours to write", factName))
		return
	}
	WalkFactFilters(found.Filters, "", func(n FactFilter, _ string) {
		if _, _, isGroup := n.Group(); isGroup || n.Pinned() || !TakesValue(n.Operator()) {
			return
		}
		// The aggregate id is the framework's, and it arrives LATER than this
		// rule: the row has no identity until the write is accepted, which is
		// exactly why the exclude-self gate passes an empty domain.ID on an
		// insert. There is no `e.ID` for this rule to fill the argument from —
		// left unrefused it emitted `e.` and the tree did not build.
		if n.Field == IdentityName {
			ps.BlockerFix(w+".fact",
				fmt.Sprintf("%s is narrowed by the aggregate id, and this rule fills a "+
					"fact's arguments from the entity — whose id is not minted until "+
					"after the rules have run", factName),
				fmt.Sprintf("call %s from rules.manual, where the insert case is a branch "+
					"you write — or use excludeSelf, which is the same id passed under the "+
					"gate the generator writes for it", factName))
			return
		}
		// A field a READ JOIN brings in is on the ENTITY, so `e.<Field>` compiles
		// — and on an INSERT there is no loaded row behind it, so what it passes
		// is the zero value. That is the same silence the stamped columns are
		// refused for below, arriving through a different door: the query would
		// run, return, and be about a tenant nobody named.
		if _, j, isJoin := JoinFactField(s, opt, n.Field); isJoin && j.InChild == "" {
			ps.BlockerFix(w+".fact",
				fmt.Sprintf("%s is narrowed by %s, which the join to %s fills — and this "+
					"rule fills a fact's arguments from the entity, which carries no value "+
					"there until it has been LOADED", factName, n.Field, j.To),
				fmt.Sprintf("on an insert the traversal has not run, so the zero would be "+
					"passed as though it were an answer. Call %s from rules.manual, where "+
					"the value's absence is a branch you write", factName))
			return
		}
		if TakesSet(n.Operator()) {
			ps.BlockerFix(w+".fact",
				fmt.Sprintf("%s takes the SET %q compares against, and this rule fills a "+
					"fact's arguments from the entity, which carries one value per field",
					factName, n.Field),
				fmt.Sprintf("pin the set in the fact (values: [...] on that filter) and the "+
					"rule has nothing to pass — or call %s from rules.manual, where the set "+
					"is yours to build", factName))
			return
		}
		// The stamped columns are the framework's, not the entity's: nothing
		// declares a field for them and the aggregate carries none, so there is
		// no `e.CreatedAt` for this rule to pass. Left unrefused it generated
		// exactly that and the tree did not build.
		if factField(s, n.Field) == nil && ManagedFilterField(s, n.Field) != nil {
			ps.BlockerFix(w+".fact",
				fmt.Sprintf("%s is narrowed by %s, which the framework stamps — this rule "+
					"fills a fact's arguments from the entity, and the entity carries no "+
					"such field", factName, n.Field),
				fmt.Sprintf("call %s from rules.manual, where the instant is yours to "+
					"choose — or drop that filter, which the query only needs when a "+
					"caller decides the window", factName))
		}
	})
}

// FindFactAggregate resolves one of a fact's numbers by the name it answers
// under.
func FindFactAggregate(f Fact, as string) *FactAggregate {
	for i := range f.Aggregates {
		if f.Aggregates[i].As == as {
			return &f.Aggregates[i]
		}
	}
	return nil
}

func factAggregateNames(f Fact) []string {
	out := make([]string, 0, len(f.Aggregates))
	for _, a := range f.Aggregates {
		out = append(out, a.As)
	}
	return out
}

func validateRuleNotification(s *Spec, r Rule, w string, ps *Problems) {
	validateNotificationRef(s, r.Notification, w+".notification", ps)
}

// validateNotificationRef refuses a notification name that resolves to nothing:
// neither declared under notifications nor supplied by the framework. Every key
// that names a notification must come through here — an unresolved name used to
// pass validation and surface three steps later as an undefined type at go
// build, with nothing pointing back at the spec line that caused it.
func validateNotificationRef(s *Spec, name, where string, ps *Problems) {
	if name == "" || frameworkNotifications[name] {
		return
	}
	for _, n := range s.Notifications {
		if n.Name == name {
			return
		}
	}
	ps.BlockerFix(where,
		fmt.Sprintf("%q is not declared", name),
		"declare it under notifications, or name one of the framework's: "+
			strings.Join(sortedKeys(frameworkNotifications), ", "))
}

func validateRuleShape(s *Spec, r Rule, scopeFields []Field, w string, ps *Problems, opt Options) {
	switch r.Kind {
	case "valueObject":
		validateValueObjectRule(s, r, scopeFields, w, ps, opt)
	case "factRange":
		validateFactRange(s, opt, r, scopeFields, w, ps)
	case "requiredIf":
		if r.Other == "" {
			ps.BlockerFix(w+".other",
				"a conditional requirement needs the field the condition reads",
				"set other: <field> — the requirement applies when that one carries a value")
		} else if findField(scopeFields, r.Other) == nil {
			ps.Blockerf(w+".other", "%q does not name a field in this scope", r.Other)
		}
		if r.Notification == "" {
			ps.Blockerf(w+".notification", "a conditional requirement needs the notification it raises")
		}
	case "transition":
		if len(r.Fields) != 1 {
			ps.BlockerFix(w+".fields",
				"a transition rule governs exactly one field — the state",
				"name it alone, e.g. fields: [Situacao]")
		}
		if len(r.Transitions) == 0 {
			ps.BlockerFix(w+".transitions",
				"a transition rule needs the moves it allows",
				"map each state to the states it may become, e.g. aberto: [suspenso, fechado]")
		}
		for _, sc := range r.Scope {
			if sc == "insert" {
				ps.BlockerFix(w+".scope",
					"a transition cannot apply on insert",
					"there is no previous state to move FROM; scope it to update")
			}
		}
		if r.Notification == "" {
			ps.Blockerf(w+".notification", "a transition rule needs the notification it raises")
		}
		for _, fn := range r.Fields {
			f := findField(scopeFields, fn)
			if f == nil {
				continue
			}
			if f.VO == nil || f.VO.Kind != "enum" {
				fix := "model the field as an enum value object first — otherwise the map " +
					"governs some values and silently ignores the rest"
				if f.VO != nil && f.VO.Kind == "reuse" {
					fix = "a reused value object's members live in the spec that declares " +
						"it, so the machine cannot be checked here — declare the transition " +
						"in the owning spec, or declare the enum in this one"
				}
				ps.BlockerFix(w,
					fmt.Sprintf("%q is not an enum declared in this spec, so its "+
						"transitions are not a checkable closed set", fn), fix)
			} else if vo := findVO(s.ValueObjects, f.VO.Ref); vo != nil {
				// The machine compares .Value() strings, so an int backing would
				// index a map[string] with an int — refuse it rather than emit it.
				if vo.Backing == "int" {
					ps.BlockerFix(w,
						fmt.Sprintf("%q is an int-backed enum, and transition states are "+
							"compared as text", fn),
						"give the enum a string backing, or drop the machine")
				} else {
					// Every state named in the map must BE a member value — a
					// typo'd state validated, generated, and silently never
					// fired, which is exactly the open set the enum requirement
					// above claims to close.
					members := map[string]bool{}
					for _, mb := range vo.Members {
						members[fmt.Sprint(mb.Value)] = true
					}
					for _, from := range sortedTransitionKeys(r.Transitions) {
						if !members[from] {
							ps.Blockerf(w+".transitions",
								"%q is not a member value of %s", from, vo.Name)
						}
						for _, to := range r.Transitions[from] {
							if !members[to] {
								ps.Blockerf(w+".transitions",
									"%q is not a member value of %s", to, vo.Name)
							}
						}
					}
				}
			}
			// A nullable state is refused rather than dereferenced: "no state
			// yet" is a STATE, and hiding it behind a nil pointer leaves the
			// machine with a value it cannot name — the emitted comparison used
			// to be a compile error, and even fixed it had no answer for nil.
			if f.Nullable {
				ps.BlockerFix(w,
					fmt.Sprintf("%q is nullable, and a state machine cannot move from or to an absent state", fn),
					"make the field non-nullable and model \"no state yet\" as an "+
						"explicit enum member")
			}
		}
	case "childDuplicate", "groupCap":
		if len(r.Fields) != 1 {
			ps.BlockerFix(w+".fields",
				fmt.Sprintf("a %s rule names the COLLECTION it reads, not fields", r.Kind),
				"name one child, e.g. fields: [Responsavel]")
			return
		}
		c := findChild(s.Children, r.Fields[0])
		if c == nil {
			ps.BlockerFix(w+".fields",
				fmt.Sprintf("%q does not name a child collection of this entity", r.Fields[0]),
				"the rule reads what the aggregate holds, so it can only look at a child")
			return
		}
		if r.Notification == "" {
			ps.Blockerf(w+".notification", "the rule needs the notification it raises")
		}
		if r.Kind == "childDuplicate" && len(c.BusinessIdentity) == 0 {
			ps.BlockerFix(w+".fields",
				fmt.Sprintf("%s declares no businessIdentity, so there is no definition of "+
					"two entries being the same", c.Name),
				"declare businessIdentity on the child")
		}
		if r.Kind == "groupCap" {
			if r.Cap <= 0 {
				ps.BlockerFix(w+".cap", "a cap needs a positive limit", "set cap: 1 or more")
			}
			// groupBy is OPTIONAL: with none, the cap is on the collection as a
			// whole, which is what "at most N of these" means when no key is
			// named. It used to be required, and the only way to write that rule
			// was to group by something — which caps every value of that
			// something equally, quietly enforcing a rule nobody declared.
			for _, g := range r.GroupBy {
				f := findField(c.Fields, g)
				if f == nil {
					ps.Blockerf(w+".groupBy", "%q does not name a field of %s", g, c.Name)
				} else if f.Nullable {
					// An optional grouping key has no bucket for an absent value
					// — counting nils together, apart, or not at all are three
					// different rules and the spec has not said which.
					ps.BlockerFix(w+".groupBy",
						fmt.Sprintf("%q is nullable, so an entry without it belongs to no group", g),
						"make the field non-nullable, or model \"none\" as an explicit value")
				}
			}
			// A cap with neither a key nor a restriction is a limit on the
			// collection's SIZE. That is a legitimate rule ("at most 10 photos"),
			// and it is also what you get by forgetting the restriction you meant
			// — so it is allowed only when the author says out loud that it is
			// deliberate. The description is where they say it, and it is the
			// text the report quotes back to a reviewer.
			if len(r.GroupBy) == 0 && r.Only == nil && strings.TrimSpace(r.Description) == "" {
				ps.BlockerFix(w,
					"the cap counts every entry of the collection: no key, no restriction",
					"if a limit on the collection's SIZE is what you mean, say so in "+
						"description: and it is accepted — otherwise add groupBy (a cap per "+
						"key) or only (a cap on the entries that match)")
			}
		}
		if r.Only != nil {
			if r.Kind != "groupCap" {
				ps.BlockerFix(w+".only",
					fmt.Sprintf("only restricts which entries are COUNTED, and %s counts nothing", r.Kind),
					"drop it, or use kind: groupCap")
			}
			if r.Only.Field == "" || r.Only.Equals == "" {
				ps.BlockerFix(w+".only",
					"a restriction needs the field and the value that makes an entry count",
					"only: {field: Situacao, equals: em_analise}")
			} else if f := findField(c.Fields, r.Only.Field); f == nil {
				ps.Blockerf(w+".only.field", "%q does not name a field of %s", r.Only.Field, c.Name)
			} else if f.Type != "string" {
				ps.BlockerFix(w+".only.field",
					"the restriction compares text, so the field it names is text",
					"restrict on a status or a type, not on a number or a date")
			}
		}
	case "comparison":
		if r.Other == "" {
			ps.BlockerFix(w+".other", "a comparison needs the field to compare against", "set other: <field>")
		} else if findField(scopeFields, r.Other) == nil {
			ps.Blockerf(w+".other", "%q does not name a field in this scope", r.Other)
		}
		if !ComparisonOps.Has(r.Operator) {
			ps.BlockerFix(w+".operator",
				fmt.Sprintf("%q is not a comparison operator", r.Operator),
				"one of: "+ComparisonOps.String())
		}
		if r.Notification == "" {
			ps.Blockerf(w+".notification", "a comparison needs the notification it raises")
		}
	case "range":
		if r.Min == nil && r.Max == nil {
			ps.BlockerFix(w, "a range needs a bound", "set min, max, or both")
		}
		if r.Notification == "" {
			ps.Blockerf(w+".notification", "a range needs the notification it raises")
		}
	case "length":
		if r.Min == nil && r.Max == nil {
			ps.BlockerFix(w, "a length rule needs a bound", "set min, max, or both")
		}
		for _, fn := range r.Fields {
			if f := findField(scopeFields, fn); f != nil && f.Type != "string" {
				ps.Blockerf(w, "%q is not a string, so it has no length", fn)
			}
		}
	case "ownerCheck":
		if r.OwnerField == "" {
			ps.BlockerFix(w+".ownerField",
				"an owner check needs the field carrying the caller's identity",
				"declare a runtime-only field and name it here")
		} else {
			f := findField(s.Fields, r.OwnerField)
			if f == nil {
				ps.Blockerf(w+".ownerField", "%q does not name a field of this entity", r.OwnerField)
			} else if !f.Runtime {
				ps.BlockerFix(w+".ownerField",
					fmt.Sprintf("%q is a persisted field", r.OwnerField),
					"the caller's identity is runtime-only — set runtime: true on it")
			} else if FromBody(*f) {
				// The whole point of the check is that the caller cannot choose
				// who they are. A source: body field is exactly what the caller
				// chooses, so this would have compared the row's owner against a
				// string the attacker typed — and passed.
				ps.BlockerFix(w+".ownerField",
					fmt.Sprintf("%q is fed from the REQUEST BODY, so the caller decides what "+
						"it holds and the check would compare the row against whatever they sent",
						r.OwnerField),
					"the caller's identity comes from the request identity — the direct "+
						"spelling is source: subject, which the framework answers with "+
						"Identity.Subject; source: claim with the claim's name reads a "+
						"different identifier off the same token")
			}
		}
		if r.AdminField != "" {
			f := findField(s.Fields, r.AdminField)
			switch {
			case f == nil:
				ps.Blockerf(w+".adminField", "%q does not name a field of this entity", r.AdminField)
			case !f.Runtime:
				ps.BlockerFix(w+".adminField",
					fmt.Sprintf("%q is a persisted field", r.AdminField),
					"whether the caller is an administrator comes from the request, not from "+
						"the row — set runtime: true on it")
			case FromBody(*f):
				// A privilege the caller sends is not a privilege.
				ps.BlockerFix(w+".adminField",
					fmt.Sprintf("%q is fed from the REQUEST BODY, so any caller could grant "+
						"themselves the bypass by sending it", r.AdminField),
					"the privilege comes from the caller — the direct spelling is "+
						"source: permission with the permission it takes, which asks the "+
						"same authorization model the routes are gated by; a boolean claim "+
						"(source: claim) answers a narrower question, resolving no resource "+
						"wildcard and no *:* grant")
			case f.Type != "bool":
				ps.BlockerFix(w+".adminField",
					fmt.Sprintf("%q is %s, and the bypass is a yes/no", r.AdminField, f.Type),
					"declare it as type: bool")
			}
		}
		// The compared field: the row's persisted owner, matched against a text
		// claim. Anything but a non-nullable string emitted a comparison that
		// did not compile, three steps after the spec said yes.
		if len(r.Fields) != 1 {
			ps.BlockerFix(w+".fields",
				"an owner check compares exactly one field of the row against the caller",
				"name the persisted owner field alone, e.g. fields: [DonoEmail]")
		} else if f := findField(scopeFields, r.Fields[0]); f != nil {
			switch {
			case f.Runtime:
				ps.BlockerFix(w+".fields",
					fmt.Sprintf("%q is runtime-only, and the check reads the ROW's owner", r.Fields[0]),
					"name the persisted field that records who owns the row")
			case f.Type != "string" && f.Type != "id":
				// `id` is accepted and unwrapped with Value() at the comparison.
				// Refusing it used to force a choice the author had no way to
				// see coming: keep the honest UUID column and hand-write the
				// rule, or take the declarative rule and lose the foreign key
				// the column could have carried — permanently, since a column's
				// type is a migration over live data.
				ps.BlockerFix(w+".fields",
					fmt.Sprintf("%q is %s, and the caller's identity is text or an id",
						r.Fields[0], f.Type),
					"the owner field is a string (usually assignedFrom: identity-subject), "+
						"or an id when the column should be the engine's own id type")
			case f.Nullable:
				ps.BlockerFix(w+".fields",
					fmt.Sprintf("%q is nullable, and a row without an owner cannot be checked", r.Fields[0]),
					"make the field non-nullable — assignedFrom fills it on every insert")
			}
		}
	case "immutable":
		if r.Notification == "" {
			ps.Blockerf(w+".notification", "an immutability rule needs the notification it raises")
		}
		for _, sc := range r.Scope {
			if sc == "insert" {
				ps.BlockerFix(w+".scope",
					"immutability cannot apply on insert",
					"there is no previous value to compare against; scope it to update")
			}
		}
	}

	// skipWhen is a modifier, not a kind: any rule may stand down when its
	// subject is absent, so it is checked once here rather than per case.
	if r.SkipWhen != "" && !SkipWhens.Has(r.SkipWhen) {
		ps.BlockerFix(w+".skipWhen",
			fmt.Sprintf("%q is not a skip condition", r.SkipWhen),
			"one of: "+SkipWhens.String())
	}
}

func validateNotifications(s *Spec, ps *Problems, opt Options) {
	seen := map[string]bool{}
	for i, n := range s.Notifications {
		where := fmt.Sprintf("notifications[%d] (%s)", i, orUnnamed(n.Name))
		if n.Name == "" || !goIdentRe.MatchString(n.Name) {
			ps.BlockerFix(where, "the notification needs a PascalCase name", "e.g. GradeOutOfRangeNotification")
		} else if seen[n.Name] {
			ps.Blockerf(where, "the notification %q is declared twice", n.Name)
		} else {
			seen[n.Name] = true
		}
		if frameworkNotifications[n.Name] {
			ps.BlockerFix(where,
				fmt.Sprintf("%q is a framework notification", n.Name),
				"use it by name; redeclaring it shadows the framework's own translations")
		}
		if !strings.HasSuffix(n.Name, "Notification") {
			ps.WarnFix(where,
				"the name does not end in Notification",
				"the struct name IS the translation key; the suffix keeps the catalogs readable")
		}
		if !Semantics.Has(n.Semantic) {
			ps.BlockerFix(where+".semantic",
				fmt.Sprintf("%q is not a semantic", n.Semantic),
				"one of: "+Semantics.String()+
					" — conflict is a duplicate, state-conflict is a wrong state; both are 409")
		}
		if n.Package != "" && !NotificationPackages.Has(n.Package) {
			ps.BlockerFix(where+".package",
				fmt.Sprintf("%q is not a package", n.Package),
				"one of: "+NotificationPackages.String())
		}
		validateTexts(n, where, ps, opt)
	}
}

func validateTexts(n Notification, where string, ps *Problems, opt Options) {
	langs := n.Text.Map()
	for _, code := range CatalogCodes {
		if strings.TrimSpace(langs[code]) != "" {
			continue
		}
		msg := fmt.Sprintf("no %s translation", strings.ToUpper(code))
		fix := "the framework carries seven catalogs and every key must exist in all of them"
		if opt.LangFallback {
			ps.WarnFix(where+".text."+code, msg,
				"--lang-fallback will emit a marked placeholder; replace it before shipping")
		} else {
			ps.BlockerFix(where+".text."+code, msg, fix)
		}
	}
	// A tvar the text never interpolates is dead; a placeholder with no tvar
	// renders literally as "{x}" to the end user.
	for _, tv := range n.TVars {
		found := false
		for _, txt := range langs {
			if strings.Contains(txt, "{"+tv+"}") {
				found = true
				break
			}
		}
		if !found {
			ps.BlockerFix(where+".tvars",
				fmt.Sprintf("the variable %q never appears in any translation", tv),
				fmt.Sprintf("write {%s} where the value belongs, or drop the variable", tv))
		}
	}
	placeholder := regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_]*)\}`)
	for code, txt := range langs {
		for _, m := range placeholder.FindAllStringSubmatch(txt, -1) {
			if !contains(n.TVars, m[1]) {
				ps.BlockerFix(where+".text."+code,
					fmt.Sprintf("the text interpolates {%s} but no such variable is declared", m[1]),
					fmt.Sprintf("add %q to tvars, or the end user sees the braces verbatim", m[1]))
			}
		}
	}
}

func validateService(s *Spec, opt Options, ps *Problems) {
	if s.Service == nil {
		return
	}
	if !s.Service.Required {
		if len(s.Service.Facts) > 0 {
			ps.BlockerFix("service.required",
				"facts are declared but the service is not required",
				"set required: true — the framework only passes a service when the entity asks for one")
		}
		return
	}
	if len(s.Service.Facts) == 0 {
		ps.BlockerFix("service.facts",
			"the entity requires a service but names no fact for it to answer",
			"declare what the rules need to know, or set required: false")
	}
	seen := map[string]bool{}
	for i, f := range s.Service.Facts {
		where := fmt.Sprintf("service.facts[%d] (%s)", i, orUnnamed(f.Name))
		if f.Name == "" || !goIdentRe.MatchString(f.Name) {
			ps.BlockerFix(where, "the fact needs a PascalCase name", "it becomes a method on the port")
		} else if seen[f.Name] {
			ps.Blockerf(where, "the fact %q is declared twice", f.Name)
		} else {
			seen[f.Name] = true
		}
		// A fact answers in ONE shape: a single number (kind) or a named set of
		// them (aggregates). Both together is an author asking for two answers
		// from one method, and picking one silently would generate a signature
		// they did not write.
		validateFactScope(s, f, where, ps)
		if len(f.Aggregates) > 0 {
			if f.Kind != "" {
				ps.BlockerFix(where,
					fmt.Sprintf("the fact declares kind: %s AND aggregates, which are two "+
						"different answers", f.Kind),
					"one or the other — `kind` answers one number, `aggregates` answers "+
						"several in one query")
				continue
			}
			validateFactAggregates(s, f, where, ps)
			validateFactFilters(s, opt, f, where, ps)
			validateGroupedFact(s, f, where, ps)
			validatePerEntryFact(s, f, where, ps)
			continue
		}
		if !FactKinds.Has(f.Kind) {
			ps.BlockerFix(where+".kind",
				fmt.Sprintf("%q is not a fact kind", f.Kind),
				"one of: "+FactKinds.String()+" — or declare `aggregates` instead, "+
					"which asks several numbers of the same rows in one query")
		}
		if f.Kind == "manual" {
			validateManualFact(f, where, ps)
			validateFactFilters(s, opt, f, where, ps)
			validatePerEntryFact(s, f, where, ps)
			continue
		}
		if f.Returns != "" {
			ps.BlockerFix(where+".returns",
				fmt.Sprintf("a %s fact already determines what it returns", f.Kind),
				"returns is only declared for a manual fact, where the generator has "+
					"no way to infer the signature")
		}
		if !existsKind(f.Kind) && f.Kind != "count" && f.Field == "" {
			ps.BlockerFix(where+".field",
				fmt.Sprintf("%s needs the field it aggregates", f.Kind),
				"set field: <field>")
		}
		validateFactFilters(s, opt, f, where, ps)
		if f.Field != "" && factField(s, f.Field) == nil {
			reportUnknownFactField(s, f.Field, where+".field", ps)
		}
		validateAggregatedField(s, f, where, ps)
		validateGroupedFact(s, f, where, ps)
		validatePerEntryFact(s, f, where, ps)
	}
}

// validateGroupedFact keeps a per-group fact answerable in ONE query.
//
// Everything it refuses would otherwise be discovered at run time, by the
// database or by a nil key: a grouping field that is not a column has nothing
// to GROUP BY, a nullable one has no bucket for the rows without a value, and
// grouping by the identity produces one group per row, which is the query
// nobody meant and the slowest way to ask nothing.
func validateGroupedFact(s *Spec, f Fact, where string, ps *Problems) {
	if len(f.GroupBy) == 0 {
		return
	}
	switch {
	case len(f.Aggregates) > 0:
		// Every entry of an aggregates list is already one of the groupable
		// kinds — exists and manual are not in that vocabulary at all — so
		// there is nothing left to refuse about the KIND here.
	case f.Kind == "count" || f.Kind == "sum" || f.Kind == "avg" || f.Kind == "min" || f.Kind == "max":
	case existsKind(f.Kind):
		ps.BlockerFix(where+".groupBy",
			fmt.Sprintf("%s answers yes or no, so there is nothing to report per group", f.Kind),
			"use count with the same groupBy — the number of matching rows per key")
	default:
		ps.BlockerFix(where+".groupBy",
			fmt.Sprintf("a %s fact is not computed by this generator, so it has no query to group", f.Kind),
			"drop groupBy, or use one of: count, sum, avg, min, max")
		return
	}
	seen := map[string]bool{}
	for _, g := range f.GroupBy {
		if seen[g] {
			ps.Blockerf(where+".groupBy", "%q is named twice as a grouping key", g)
			continue
		}
		seen[g] = true
		fld := factField(s, g)
		if fld == nil {
			reportUnknownFactField(s, g, where+".groupBy", ps)
			continue
		}
		if fld.Runtime {
			ps.BlockerFix(where+".groupBy",
				fmt.Sprintf("%q is a runtime-only field, so there is no column to group by", g),
				"group by a persisted field")
			continue
		}
		if fld.Nullable {
			ps.BlockerFix(where+".groupBy",
				fmt.Sprintf("%q is nullable, so a row without it belongs to no group", g),
				"make the field non-nullable, or model \"none\" as an explicit value")
		}
	}
}

// validateAggregatedField refuses an aggregation the framework cannot carry.
//
// It computes in the DATABASE and comes back in one of three carriers: a count
// (int64), an exact integer, or a float. There is no carrier for text, for a
// timestamp or for a boolean — so `max` over a name reads a float out of a
// string column, which compiles and then means nothing. The check exists
// because the spec was green for exactly that: kind and field type were each
// validated alone, and nothing asked whether the pair made sense.
func validateAggregatedField(s *Spec, f Fact, where string, ps *Problems) {
	if f.Field == "" || existsKind(f.Kind) || f.Kind == "count" || f.Kind == "manual" {
		return
	}
	validateAggregandType(s, f.Kind, f.Field, where+".field", ps)
}

// validateAggregandType is the check itself, reached from both shapes: a fact
// declaring one kind, and one entry of an aggregates list. Written once because
// the refusal is about the pair (what is computed, over what) and nothing else,
// and two copies of it would drift the day a carrier is added.
func validateAggregandType(s *Spec, kind, field, at string, ps *Problems) {
	fld := factField(s, field)
	if fld == nil {
		return // already reported as an unknown field
	}
	switch fld.Type {
	case "int", "int64", "float64":
		return
	}
	ps.BlockerFix(at,
		fmt.Sprintf("%s cannot aggregate %s, which is %s", kind, field, fld.Type),
		"aggregate a numeric field (int, int64, float64) — the database computes "+
			"these and the framework carries the answer as an exact integer or a float; "+
			"for anything else, make it a manual fact and write the query you mean")
}

// validateFactAggregates holds a multi-answer fact to the same bar as a
// single-answer one, plus the two things only a SET of answers can get wrong:
// two entries that reach the struct under one name, and an entry whose name
// collides with a grouping key sitting in the same struct.
func validateFactAggregates(s *Spec, f Fact, where string, ps *Problems) {
	if f.Returns != "" {
		ps.BlockerFix(where+".returns",
			"an aggregating fact already determines what it returns",
			"returns is only declared for a manual fact, where the generator has "+
				"no way to infer the signature")
	}
	if f.Field != "" {
		ps.BlockerFix(where+".field",
			"each entry of aggregates names the field IT aggregates",
			"move the field onto the entry that computes over it — the fact as a "+
				"whole aggregates nothing")
	}
	if len(f.Aggregates) == 1 {
		ps.BlockerFix(where+".aggregates",
			"the list holds ONE aggregate, which is what kind: says",
			"write kind: <"+f.Aggregates[0].Kind+"> (with field:, where it takes one) — "+
				"the list is for asking SEVERAL numbers of the same rows in one query")
		return
	}
	// The grouping keys share the answer's struct with the aggregates, so they
	// take part in the name check rather than colliding with it afterwards.
	names := map[string]string{}
	for _, g := range f.GroupBy {
		names[g] = "the grouping key " + g
	}
	for j, a := range f.Aggregates {
		at := fmt.Sprintf("%s.aggregates[%d]", where, j)
		if !AggregateKinds.Has(a.Kind) {
			ps.BlockerFix(at+".kind",
				fmt.Sprintf("%q is not something this list can compute", a.Kind),
				"one of: "+AggregateKinds.String()+" — exists is a probe rather than "+
					"an aggregate and manual has no generated query, so neither can share "+
					"a query with the others; ask those as facts of their own")
			continue
		}
		switch {
		case a.As == "":
			ps.BlockerFix(at+".as",
				"the entry has no name, and it becomes a field of the fact's answer",
				"set as: <PascalCase> — it is also what a factRange rule names to bound "+
					"this number (fact: "+orUnnamed(f.Name)+".<As>)")
			continue
		case !goIdentRe.MatchString(a.As):
			ps.BlockerFix(at+".as",
				fmt.Sprintf("%q cannot be a field of the generated answer", a.As),
				"PascalCase, letters and digits — it is a Go struct field, so it starts "+
					"with a capital or nothing downstream can read it")
			continue
		}
		if prev, clash := names[a.As]; clash {
			ps.BlockerFix(at+".as",
				fmt.Sprintf("%s already answers under the name %q", prev, a.As),
				"name this one something else — the answer is one struct, and two "+
					"fields of one name do not compile")
			continue
		}
		names[a.As] = "the aggregate " + a.As
		if a.Kind == "count" {
			if a.Field != "" {
				ps.BlockerFix(at+".field",
					"count counts ROWS, so there is no column for it to read",
					"drop the field — for \"how many have a value\", filter on that "+
						"column being present and count the rows that survive")
			}
			continue
		}
		if a.Field == "" {
			ps.BlockerFix(at+".field",
				fmt.Sprintf("%s needs the field it aggregates", a.Kind),
				"set field: <field>")
			continue
		}
		if factField(s, a.Field) == nil {
			reportUnknownFactField(s, a.Field, at+".field", ps)
			continue
		}
		validateAggregandType(s, a.Kind, a.Field, at+".field", ps)
	}
}

// validateManualFact keeps the ELSE honest.
//
// A manual fact is a promise that a human will write the body, so the spec must
// carry the two things that promise needs: what the answer MEANS, and what its
// type is. Without the first the stub is an empty TODO; without the second the
// generator cannot even declare the method it is asking someone to implement.
// validateFactFilters resolves every name a fact narrows by, and refuses the
// ones that resolve to nothing.
//
// It exists because a MANUAL fact's filters used to be validated nowhere at all:
// validateManualFact checked the return type and stopped, and the IR then
// dropped any name it could not resolve WITHOUT A WORD. The result was the worst
// shape this generator can ship — a green check, a successful generate, a tree
// that compiles (a method with no parameter is valid Go), and a port method that
// cannot answer the question it is named for. It was found when the rule needing
// it was being hand-written, three steps and two "the spec is correct" verdicts
// later.
//
// Every parameter the method will take is also proved DISTINCT here: two filters
// that camel-case to one name emit a signature that does not compile, which is
// generated code the author did not write and cannot fix.
func validateFactFilters(s *Spec, opt Options, f Fact, where string, ps *Problems) {
	// excludeSelf appends its own parameter, so it takes part in the collision
	// check rather than colliding with a field named SelfID after the fact.
	params := map[string]string{}
	if f.ExcludeSelf {
		params["selfID"] = "excludeSelf"
	}
	validateFactFilterNodes(s, opt, f, f.Filters, where+".filters", params, ps)
}

// validateFactFilterNodes walks one level of a fact's criteria tree.
//
// A node is a LEAF or a GROUP and the two are checked apart, because almost
// nothing they can get wrong is the same mistake: a leaf's failures are about
// the column and the operator, a group's are about what it contains.
func validateFactFilterNodes(s *Spec, opt Options, f Fact, nodes []FactFilter, at string, params map[string]string, ps *Problems) {
	for i, n := range nodes {
		w := fmt.Sprintf("%s[%d]", at, i)
		switch groups := n.DeclaredGroups(); {
		case len(groups) > 1:
			ps.BlockerFix(w,
				fmt.Sprintf("the node declares %s at once, and they are different questions",
					strings.Join(groups, " and ")),
				"one connective per node — nest the second one inside the first")
		case len(groups) == 1:
			validateFactFilterGroup(s, opt, f, n, groups[0], w, params, ps)
		default:
			validateFactFilterLeaf(s, opt, f, n, w, params, ps)
		}
	}
}

// validateFactFilterGroup checks a node that combines other nodes.
//
// Everything it refuses is a group pretending to be a leaf, or a group with
// nothing in it. Both would emit silently: an empty criteria.Or() is a
// condition that matches nothing, and a `field` beside an `any` is a comparison
// the emitter never reaches — the author would read their own spec and see a
// narrowing that is not in the query.
func validateFactFilterGroup(s *Spec, opt Options, f Fact, n FactFilter, group, w string, params map[string]string, ps *Problems) {
	if f.Kind == "manual" {
		ps.BlockerFix(w,
			"a manual fact has no generated query, so there is nothing to combine",
			"list the filters flat — they become the method's parameters, and how the "+
				"hand-written body combines them is its own decision")
		return
	}
	var leafKeys []string
	if n.Field != "" {
		leafKeys = append(leafKeys, "field")
	}
	if n.Op != "" {
		leafKeys = append(leafKeys, "op")
	}
	if n.As != "" {
		leafKeys = append(leafKeys, "as")
	}
	if n.Value != nil {
		leafKeys = append(leafKeys, "value")
	}
	if n.Values != nil {
		leafKeys = append(leafKeys, "values")
	}
	if len(leafKeys) > 0 {
		ps.BlockerFix(w,
			fmt.Sprintf("a %s node combines other conditions, so %s says nothing here",
				group, strings.Join(leafKeys, " and ")),
			fmt.Sprintf("move the comparison INTO the %s, as one more entry", group))
		return
	}
	_, kids, _ := n.Group()
	if len(kids) == 0 {
		ps.BlockerFix(w,
			fmt.Sprintf("the %s node holds no condition", group),
			"give it the conditions it combines, or drop the node — an empty group "+
				"narrows by nothing and would ship as a query nobody wrote")
		return
	}
	if len(kids) == 1 && group != "not" {
		ps.BlockerFix(w,
			fmt.Sprintf("the %s node holds ONE condition, so it combines nothing", group),
			fmt.Sprintf("write that condition directly where the %s is", group))
		return
	}
	validateFactFilterNodes(s, opt, f, kids, w+"."+group, params, ps)
}

// validateFactFilterLeaf checks one comparison: the field it names, the
// operator, and how the value reaches the query.
func validateFactFilterLeaf(s *Spec, opt Options, f Fact, n FactFilter, w string, params map[string]string, ps *Problems) {
	if strings.TrimSpace(n.Field) == "" {
		ps.BlockerFix(w, "the filter names no field",
			"name a field, or drop the entry — a node is either a comparison or a "+
				"group (all/any/not)")
		return
	}
	op := n.Operator()
	if !FactFilterOps.Has(op) {
		ps.BlockerFix(w+".op",
			fmt.Sprintf("%q is not a comparison a filter may make", n.Op),
			"one of: "+FactFilterOps.String())
		return
	}
	resolved, ok := resolveFactFilterField(s, opt, f, n.Field, w, ps)
	if !ok {
		return
	}
	if !validateFactFilterOperand(s, f, n, *resolved, op, w, ps) {
		return
	}
	if !TakesValue(op) || n.Pinned() {
		// Nothing reaches the signature, so there is no parameter to collide.
		return
	}
	name := n.ParamName()
	if !goIdentRe.MatchString(naming.Pascal(name)) {
		ps.BlockerFix(w+".as",
			fmt.Sprintf("%q is not usable as a parameter name", name),
			"a parameter is a Go identifier — letters and digits, starting with a letter")
		return
	}
	if prev, clash := params[name]; clash {
		ps.BlockerFix(w,
			fmt.Sprintf("%s and %s both reach the method as the parameter %q",
				prev, n.Field, name),
			"two parameters of one name do not compile; name one with `as:`, drop one, "+
				"or ask the two questions as two facts")
		return
	}
	params[name] = n.Field
}

// resolveFactFilterField finds the column a leaf names, in either spelling: a
// field of this entity (a composite's part included), or `<Collection>.<Field>`
// for a fact asked once per entry of a collection.
func resolveFactFilterField(s *Spec, opt Options, f Fact, name, at string, ps *Problems) (*Field, bool) {
	switch coll, fld, dotted := ChildFactField(s, name); {
	case dotted && (coll == nil || fld == nil):
		reportUnknownFactField(s, name, at, ps)
		return nil, false
	case dotted && f.Kind != "manual":
		// A computed fact IS a query this generator writes, and it writes it
		// against the entity's own table. The collection's field is on
		// another one, so there is no criteria this build could emit — and
		// inventing a join here would be a query shape nothing else in the
		// language can express or index.
		ps.BlockerFix(at,
			fmt.Sprintf("a %s fact is a query over this entity's own table, and %q "+
				"is a column of the collection's table", f.Kind, name),
			"ask it as kind: manual, whose body you write — or filter by a root "+
				"field, which the generated query can reach")
		return nil, false
	case dotted:
		return fld, true
	}
	if resolved := factField(s, name); resolved != nil {
		return resolved, true
	}
	// A field a READ JOIN brings in. It is a column of another table and the
	// framework reaches it for free: a root join is always in the FROM, and the
	// probe and the aggregate calls compile the same traversal FindAll does.
	// The generator was the half that could not name it, so "does an active row
	// exist whose owner's tenant is this one" had to be asked by hand.
	if joined, j, isJoin := JoinFactField(s, opt, name); isJoin {
		switch {
		case j.InChild != "":
			ps.BlockerFix(at,
				fmt.Sprintf("%q is brought in by the join declared on the collection %q, "+
					"and a child join is load-only", name, j.InChild),
				"it rides that collection's own batched SELECT and never reaches a "+
					"predicate — the same boundary every child field has. Filter by a "+
					"field of a ROOT join, or ask it as kind: manual")
			return nil, false
		case joined == nil:
			// Underivable type: a hand-written target with no `type` on the
			// field. validateJoinField already refuses that declaration, so
			// saying it twice here would send the author to the wrong line.
			return nil, false
		}
		return joined, true
	}
	// The aggregate id, under the fixed logical name the framework locks on
	// every schema. It was the one name this key could not say while everything
	// under it could: criteria.ByID is Where(Eq("ID", id)), the exclude-self
	// gate this same emitter writes is Ne("ID", selfID), and a listing already
	// narrows by it. A manual fact whose body needs the id had to re-derive it
	// from a natural key instead — a join that exists only to translate a value
	// the caller was holding.
	//
	// It cannot shadow anything: "ID" is a reserved field name, so no entity
	// declares one.
	if id := identityRead(name); id != nil {
		return id, true
	}
	// The entity's own fields answer FIRST, so nothing that resolved before
	// resolves differently now: a spec that happens to declare a field called
	// CreatedAt keeps whatever answer it had.
	if managed := ManagedFilterField(s, name); managed != nil {
		return managed, true
	}
	reportUnknownFactField(s, name, at, ps)
	return nil, false
}

// validateFactFilterOperand holds the operator and the column to the same
// question, and decides whether the value arrives as a parameter or as a
// constant written here.
//
// It is where "the spec is green and the query means nothing" is caught. Every
// refusal below produced code that compiled: `max` over a name did, and so does
// `isnull` on a NOT NULL column (a condition that is always false), `contains`
// over an integer (a LIKE against a number), and `in` with one value under a
// key called `value`.
func validateFactFilterOperand(s *Spec, f Fact, n FactFilter, fld Field, op, w string, ps *Problems) bool {
	if f.Kind == "manual" {
		// A manual fact emits no query: its filters exist only to shape the
		// method the author is being asked to write. So an operator that puts
		// nothing in the signature, and a constant that would live in a query
		// nobody generates, are both declarations with no effect — and the
		// shape this language refuses hardest is the one that looks like it did
		// something.
		switch {
		case !TakesValue(op):
			ps.BlockerFix(w+".op",
				fmt.Sprintf("%s asks about the column being empty, and a manual fact "+
					"writes no query for it to ask", op),
				"drop the filter — the hand-written body decides what it considers; "+
					"a filter here exists to put a value in the method's signature")
			return false
		case n.Pinned():
			ps.BlockerFix(w,
				"a constant belongs to a query, and a manual fact has none",
				"drop it — the value the hand-written body compares against is its own; "+
					"a filter here declares a PARAMETER the caller passes")
			return false
		}
	}
	// The archived scope and a condition on DeletedAt are two ways to say the
	// same thing, and under activeOnly they say opposite things. Both readings
	// ship a query that runs: one answers about nothing at all, the other
	// repeats a gate the translator already appended.
	if n.Field == "DeletedAt" && f.ActiveOnly {
		if op == "isnull" {
			ps.BlockerFix(w,
				"activeOnly already limits the query to rows that are not archived, "+
					"so this asks it a second time",
				"drop the filter — the scope is the shorter way to say it")
			return false
		}
		ps.BlockerFix(w,
			fmt.Sprintf("activeOnly removes every archived row, and %s on DeletedAt is "+
				"only ever true of one — together they match nothing", op),
			"drop activeOnly to ask about archived rows, or drop this condition")
		return false
	}
	switch op {
	case "isnull", "notnull":
		if !fld.Nullable {
			ps.BlockerFix(w+".op",
				fmt.Sprintf("%q is not nullable, so %s is the same answer for every row",
					n.Field, op),
				"drop the condition, or ask it of a nullable field — for \"is this row "+
					"archived\", activeOnly is the key that says so")
			return false
		}
		if n.As != "" {
			ps.BlockerFix(w+".as",
				fmt.Sprintf("%s takes no value, so there is no parameter to name", op),
				"drop `as`")
			return false
		}
	case "contains", "startswith", "endswith":
		if fld.Type != "string" {
			ps.BlockerFix(w+".op",
				fmt.Sprintf("%s matches TEXT and %q is %s", op, n.Field, fld.Type),
				"compare it with eq, ne or a range — a pattern match over a number is a "+
					"comparison against the value's rendering, which is not what any "+
					"engine indexes")
			return false
		}
	case "gt", "gte", "lt", "lte":
		switch fld.Type {
		case "bool":
			ps.BlockerFix(w+".op",
				fmt.Sprintf("%q is true/false, and there is no order between them", n.Field),
				"compare it with eq or ne")
			return false
		case "id":
			ps.BlockerFix(w+".op",
				fmt.Sprintf("%q is an identity, and one identity is not greater than "+
					"another", n.Field),
				"compare it with eq, ne or in — for \"everything written after this "+
					"one\", range over a timestamp instead")
			return false
		}
	}
	return validateFactFilterConstant(s, n, fld, op, w, ps)
}

// validateFactFilterConstant checks the value a leaf pins, when it pins one.
//
// The two keys are not interchangeable and are not made so here: `value` is one
// value, `values` is the set an `in` compares against. A single key covering
// both would have to guess whether a one-item list means a set of one or a
// scalar someone over-punctuated, and the generated signature differs.
func validateFactFilterConstant(s *Spec, n FactFilter, fld Field, op, w string, ps *Problems) bool {
	switch {
	case n.Value != nil && n.Values != nil:
		ps.BlockerFix(w,
			"the filter pins both a value and a set of values",
			"one or the other: `value` for a single comparison, `values` for in/nin")
		return false
	case !n.Pinned():
		return true
	case !TakesValue(op):
		ps.BlockerFix(w,
			fmt.Sprintf("%s compares against nothing, so there is no value to pin", op),
			"drop it")
		return false
	case n.As != "":
		ps.BlockerFix(w+".as",
			"the value is pinned in the spec, so no parameter carries it",
			"drop `as`, or drop the pinned value and let the caller pass one")
		return false
	case TakesSet(op) && n.Values == nil:
		ps.BlockerFix(w+".value",
			fmt.Sprintf("%s compares against a SET and `value` is one value", op),
			"write values: [a, b] — or use eq/ne, which take one")
		return false
	case !TakesSet(op) && n.Values != nil:
		ps.BlockerFix(w+".values",
			fmt.Sprintf("%s compares against ONE value and `values` is a set", op),
			"write value: <the value> — or use in/nin, which take a set")
		return false
	case n.Values != nil && len(n.Values) == 0:
		ps.BlockerFix(w+".values",
			fmt.Sprintf("the set is empty, so %s is the same answer for every row", op),
			"name the values the question is about, or drop the condition")
		return false
	}
	literals := n.Values
	if n.Value != nil {
		literals = []any{n.Value}
	}
	ok := true
	for _, lit := range literals {
		if !validateFactFilterLiteral(s, lit, fld, w, ps) {
			ok = false
		}
	}
	return ok
}

// validateFactFilterLiteral holds one pinned value to the column it compares
// against, so a typo is a refusal here rather than a query that quietly matches
// nothing.
//
// Over an ENUM the literal is the member's NAME, not its stored value: a spec
// that named the wire value would be spelling the same member twice in one
// project and would drift the day the storage value changes, which is exactly
// the freedom declaring an enum buys.
func validateFactFilterLiteral(s *Spec, lit any, fld Field, w string, ps *Problems) bool {
	if vo := factFilterEnum(s, fld); vo != nil {
		name, isText := lit.(string)
		if !isText || FindEnumMember(vo, name) == nil {
			ps.BlockerFix(w,
				fmt.Sprintf("%v is not a member of %s", lit, vo.Name),
				"name one of: "+strings.Join(enumMemberNames(vo), ", ")+
					" — the member's NAME; the generator writes its stored value into "+
					"the query")
			return false
		}
		return true
	}
	switch fld.Type {
	case "string":
		if _, ok := lit.(string); !ok {
			ps.Blockerf(w, "%q is text, and %v is not", fld.Name, lit)
			return false
		}
	case "int", "int64":
		if _, ok := lit.(int); !ok {
			ps.Blockerf(w, "%q is a whole number, and %v is not", fld.Name, lit)
			return false
		}
	case "float64":
		switch lit.(type) {
		case int, float64:
		default:
			ps.Blockerf(w, "%q is a number, and %v is not", fld.Name, lit)
			return false
		}
	case "bool":
		if _, ok := lit.(bool); !ok {
			ps.Blockerf(w, "%q is true/false, and %v is not", fld.Name, lit)
			return false
		}
	case "time":
		ps.BlockerFix(w,
			fmt.Sprintf("%q is a timestamp, and a timestamp written into a spec is a "+
				"query that ages", fld.Name),
			"let the caller pass it — drop the pinned value, and the method takes the "+
				"instant the rule is asking about")
		return false
	case "id":
		ps.BlockerFix(w,
			fmt.Sprintf("%q is an identity, and one pinned in a spec is a row someone "+
				"pasted", fld.Name),
			"let the caller pass it — drop the pinned value")
		return false
	}
	return true
}

// factFilterEnum answers whether the column a filter names is backed by a
// declared enum, which is what decides how a pinned literal is read.
func factFilterEnum(s *Spec, fld Field) *ValueObject {
	if fld.VO == nil || fld.VO.Ref == "" {
		return nil
	}
	vo := findVO(s.ValueObjects, fld.VO.Ref)
	if vo == nil || vo.Kind != "enum" {
		return nil
	}
	return vo
}

// FindEnumMember resolves a member by the Go name the spec declares it under.
func FindEnumMember(vo *ValueObject, name string) *EnumMember {
	for i := range vo.Members {
		if vo.Members[i].Name == name {
			return &vo.Members[i]
		}
	}
	return nil
}

func enumMemberNames(vo *ValueObject) []string {
	out := make([]string, 0, len(vo.Members))
	for _, m := range vo.Members {
		out = append(out, m.Name)
	}
	return out
}

// existsKind reports whether a fact answers a yes/no rather than a number.
//
// `notExists` is `exists` with the reading inverted and nothing else: the same
// probe, the same criteria, the same one query. Everything that is true of one
// because it answers a bool — no aggregated field, nothing to group, nothing
// for a range to compare — is true of the other, so the two are asked about
// together rather than remembered separately in five places.
func existsKind(kind string) bool { return kind == "exists" || kind == "notExists" }

// validateFactScope holds the archived gate to one spelling and to a question
// the entity can actually be asked.
func validateFactScope(s *Spec, f Fact, where string, ps *Problems) {
	if f.Scope == "" {
		return
	}
	if !FactScopes.Has(f.Scope) {
		ps.BlockerFix(where+".scope",
			fmt.Sprintf("%q is not a scope a fact may ask under", f.Scope),
			"one of: "+FactScopes.String())
		return
	}
	// Two keys governing one gate. Reconciling them silently would run a query
	// the author did not write, and the pair is easy to produce: `activeOnly`
	// is the older spelling of exactly one of these values.
	if f.ActiveOnly {
		ps.BlockerFix(where+".scope",
			fmt.Sprintf("the fact declares activeOnly AND scope: %s, and both govern the "+
				"archived gate", f.Scope),
			"keep one — activeOnly: true is scope: active, and scope says the other two "+
				"as well (all, archivedOnly)")
		return
	}
	if f.Kind == "manual" {
		ps.BlockerFix(where+".scope",
			"the archived scope describes a query this generator is not writing",
			"drop it — what the hand-written body considers is its own decision")
		return
	}
	// With no marker column the framework applies NO gate under any scope, so
	// `archivedOnly` would answer about every row rather than about none — a
	// query that runs, returns, and means the opposite of what it says.
	if ManagedColumn(s, "DeletedAt") == "" {
		ps.BlockerFix(where+".scope",
			fmt.Sprintf("scope: %s asks about the archived rows and this entity declares "+
				"no archive column", f.Scope),
			"declare storage.managed.archivedAt — with no marker column the framework "+
				"applies no gate at all, so this would ask about every row instead")
	}
}

// validatePerEntryFact keeps a batched per-entry fact answerable, and keyed by
// something an entry can be found by again.
//
// The shape it protects is the whole point of the key: ONE question about the
// whole collection, and an answer attributed per entry. Everything refused here
// is a way of writing that down that would compile into a method saying
// something else — a key two entries share, a key that cannot be looked up, or
// entries drawn from two different collections, which is not one loop.
func validatePerEntryFact(s *Spec, f Fact, where string, ps *Problems) {
	at := where + ".perEntry"
	collections := factFilterCollections(s, f)

	if f.PerEntry == "" {
		// The once-per-entry form, which stays legal: the rule loops and the
		// body is asked about ONE entry. It still has to be about one
		// collection — a fact filtered by two of them is a question with two
		// loops and no stated order between them, and what it generated was a
		// port documented as "asked once per entry of A and B".
		if len(collections) > 1 {
			ps.BlockerFix(where+".filters",
				fmt.Sprintf("the fact is filtered by fields of %s, and a per-entry question "+
					"is asked once per entry of ONE collection",
					strings.Join(collections, " and ")),
				"ask one fact per collection — two collections in one fact is a pair of "+
					"nested loops nobody wrote, and the port could only document it as "+
					"being about both at once")
		}
		return
	}

	if f.Kind != "manual" {
		computed := "a fact of kind " + f.Kind
		if len(f.Aggregates) > 0 {
			computed = "a fact answering several numbers"
		}
		ps.BlockerFix(at,
			fmt.Sprintf("%s is a query over this entity's own table, and the entries "+
				"are on the collection's", computed),
			"ask it as kind: manual, whose body you write — one FindAll with an In over "+
				"the keys is what the batched shape exists to make possible")
		return
	}

	coll, key, dotted := ChildFactField(s, f.PerEntry)
	switch {
	case !dotted:
		ps.BlockerFix(at,
			fmt.Sprintf("%q does not name an entry's field", f.PerEntry),
			"perEntry is <collection>.<field> — the key the answer is attributed to"+
				collectionNames(s))
		return
	case coll == nil || key == nil:
		reportUnknownPerEntryFilter(s, f.PerEntry, coll, key, at, ps)
		return
	}

	// Every per-entry filter must be about the collection the key is on.
	// Otherwise the carrier would hold fields of two different entries, and
	// there is no such thing as "the" entry to key the answer by.
	for _, other := range collections {
		if other == coll.Plural {
			continue
		}
		ps.BlockerFix(where+".filters",
			fmt.Sprintf("the answer is keyed by an entry of %q and the fact is also "+
				"filtered by a field of %q", coll.Plural, other),
			fmt.Sprintf("every per-entry filter names the collection perEntry does — one "+
				"entry contributes one carrier, and two collections have no shared entry "+
				"to key by. Ask %q with a fact of its own", other))
		return
	}

	if !perEntryKeyTypes.Has(key.Type) {
		ps.BlockerFix(at,
			fmt.Sprintf("%s is %s, which cannot key an answer", f.PerEntry, key.Type),
			"key by one of: "+perEntryKeyTypes.String()+". A time compares by wall "+
				"clock AND monotonic reading AND location, so two values that print the "+
				"same are two keys; a float can be NaN, which never equals itself, so the "+
				"entry can never be looked up again; a bool makes the map two buckets "+
				"rather than an answer per entry")
	}
	if key.Nullable {
		ps.BlockerFix(at,
			fmt.Sprintf("%s is nullable, so every entry without a value answers under one key",
				f.PerEntry),
			"key by a mandatory field of the entry — the collection's business identity "+
				"is normally the one")
	}
	// A set operator inside the batch is the one that reads as though it did
	// something. The entries arrive together already; an `in` on a per-entry
	// leaf would put a slice inside the carrier and say the entry carries many
	// values of that field, which no entry does.
	WalkFactFilters(f.Filters, "", func(n FactFilter, w string) {
		if _, _, isGroup := n.Group(); isGroup || n.Field == "" {
			return
		}
		if _, _, dotted := ChildFactField(s, n.Field); !dotted || !TakesSet(n.Operator()) {
			return
		}
		ps.BlockerFix(where+".filters"+w,
			fmt.Sprintf("%s compares against a SET, and this fact already asks about the "+
				"whole collection at once", n.Field),
			"drop the operator — an entry carries ONE value per field, and the batch is "+
				"what perEntry already made of the question")
	})
}

// perEntryKeyTypes is what may KEY a batched answer.
//
// It is deliberately NOT a vocabulary of the language: `perEntry` takes a field
// path, not one of these words, and registering it beside the closed sets would
// have `explain vocabulary` print a list of legal VALUES for a key that accepts
// none of them. The constraint lives in Fact.PerEntry's own documentation,
// which `explain keys` prints.
var perEntryKeyTypes = set("string", "int", "int64", "id")

// factFilterCollections names the collections a fact's filters reach into, in
// first-seen order and without repeats.
func factFilterCollections(s *Spec, f Fact) []string {
	var out []string
	seen := map[string]bool{}
	for _, name := range FactFilterFields(f.Filters) {
		coll, _, dotted := ChildFactField(s, name)
		if !dotted || coll == nil || seen[coll.Plural] {
			continue
		}
		seen[coll.Plural] = true
		out = append(out, coll.Plural)
	}
	return out
}

func validateManualFact(f Fact, where string, ps *Problems) {
	if strings.TrimSpace(f.Description) == "" {
		ps.BlockerFix(where+".description",
			"a manual fact must say what it answers and where the answer comes from",
			"this text is what the generated stub and the report tell the implementer "+
				"to write; without it the stub is an empty TODO")
	}
	if f.Returns == "" {
		ps.BlockerFix(where+".returns",
			"a manual fact must declare its return type",
			"one of: "+FactReturns.String()+" — the generator declares the method on "+
				"the port and cannot infer the signature of a body it is not writing")
	} else if !FactReturns.Has(f.Returns) {
		ps.BlockerFix(where+".returns",
			fmt.Sprintf("%q is not a return type a fact may declare", f.Returns),
			"one of: "+FactReturns.String())
	}
	if f.Field != "" {
		ps.BlockerFix(where+".field",
			"a manual fact aggregates nothing — the body is hand-written",
			"drop it, or use one of the computed kinds")
	}
	if f.ActiveOnly {
		ps.BlockerFix(where+".activeOnly",
			"the archived scope describes a query this generator is not writing",
			"drop it — what the hand-written body considers is its own decision")
	}
	if len(f.GroupBy) > 0 {
		ps.BlockerFix(where+".groupBy",
			"a manual fact has no generated query, so there is nothing to group",
			"drop it — the hand-written body decides how it groups; if the answer "+
				"comes from THIS service's own tables, a computed kind with groupBy "+
				"writes the GROUP BY for you")
	}
}

func validateRead(s *Spec, jr joinReach, ps *Problems) {
	r := s.Read
	display := contains(s.Modes, "display")
	reading := r.ByID || r.ByParams != nil
	if reading && !display {
		ps.BlockerFix("modes",
			"the entity serves reads but display is not among its modes",
			"add display to modes")
	}
	if !reading {
		if r.Backing != "" || r.View.Name != "" || len(r.Indexes) > 0 {
			ps.BlockerFix("read",
				"a read side is configured but no read operation is served",
				"set read.byId and/or read.byParams, or remove the read block")
		}
		return
	}

	if !ReadBackings.Has(r.Backing) {
		ps.BlockerFix("read.backing",
			fmt.Sprintf("%q is not a backing", r.Backing),
			"one of: "+ReadBackings.String())
	}
	if r.View.Name == "" {
		ps.Blockerf("read.view.name", "the view needs a name")
	} else if why := ReservedViewSuffix(r.View.Name); why != "" {
		ps.BlockerFix("read.view.name",
			fmt.Sprintf("%q %s", r.View.Name, why),
			"rename the view — the framework refuses the name at boot, in every "+
				"read-model family, because all of them share one namespace")
	}

	if r.Backing == "relational" {
		// A relational read model is a DIFFERENT TYPE from a projected one, and
		// none of these exist on it. They are not merely useless here — they
		// would be silently discarded, which is worse.
		if r.View.Version != 0 {
			ps.BlockerFix("read.view.version",
				"a relational read model has no version",
				"nothing is materialised, so there is no stored shape to grow stale "+
					"against, nothing to rebuild and no boot to refuse — remove the key, "+
					"or set read.backing: mongo")
		}
		if len(r.Indexes) > 0 {
			ps.BlockerFix("read.indexes",
				"a relational view has no collection to index",
				"the indexes belong on the tables, in the migration; remove them here")
		}
		if r.View.DeleteOnArchive {
			ps.BlockerFix("read.view.deleteOnArchive",
				"deleteOnArchive is a Mongo projection option",
				"a relational view reads the tables directly; remove it")
		}
		if r.View.TTLSeconds > 0 {
			ps.BlockerFix("read.view.ttlSeconds",
				"a time-to-live is a Mongo collection option",
				"remove it, or set read.backing: mongo")
		}
	} else if r.View.Version < 1 {
		ps.BlockerFix("read.view.version",
			"the view version must start at 1",
			"the framework uses it to decide when a rebuild is due; 0 is not a version")
	}

	for i, idx := range r.Indexes {
		where := fmt.Sprintf("read.indexes[%d]", i)
		if len(idx.Fields) == 0 {
			ps.Blockerf(where+".fields", "an index needs at least one field")
		}
		for _, fn := range idx.Fields {
			if !readableField(s, jr, fn) {
				reportUnreadable(s, fn, where+".fields", ps)
			}
		}
		if idx.Order != "" && !IndexOrders.Has(idx.Order) {
			ps.BlockerFix(where+".order",
				fmt.Sprintf("%q is not an order", idx.Order), "one of: "+IndexOrders.String())
		}
		if idx.Text && idx.Unique {
			ps.Blockerf(where, "a text index cannot be unique")
		}
	}

	if r.ByParams != nil {
		validateByParams(s, r, jr, ps)
	}

	seenManaged := map[string]bool{}
	for i, n := range r.Managed {
		where := fmt.Sprintf("read.managed[%d]", i)
		if !ManagedReads.Has(n) {
			ps.BlockerFix(where,
				fmt.Sprintf("%q is not a framework-stamped column", n),
				"one of: "+ManagedReads.String()+" — an entity's own column is declared "+
					"under fields[] and read like any other")
			continue
		}
		if seenManaged[n] {
			ps.Blockerf(where, "%q is listed twice", n)
			continue
		}
		seenManaged[n] = true
		// The logical name resolves to a column the storage declares, and only
		// then. Listing one the table does not have would project a field the
		// framework answers nothing for, on every row.
		if ManagedColumn(s, n) == "" {
			key := "storage.managed." + strings.ToLower(n[:1]) + n[1:]
			if n == "DeletedAt" {
				key = "storage.managed.archivedAt"
			}
			ps.BlockerFix(where,
				fmt.Sprintf("%s is exposed on the reads and this entity declares no such column", n),
				"declare "+key+", or drop it here")
		}
	}

	for i, fr := range r.FieldRestrict {
		where := fmt.Sprintf("read.fieldRestrict[%d]", i)
		if !readableField(s, jr, fr.Field) {
			reportUnreadable(s, fr.Field, where+".field", ps)
		}
		if fr.Permission == "" {
			ps.Blockerf(where+".permission", "a restricted field needs the permission that unlocks it")
		}
		// The two keys answer different questions and the answers contradict:
		// hidden says nobody receives the field, fieldRestrict says the callers
		// holding a permission do. Emitting both would generate a permission that
		// unlocks nothing, which reads in an authz review as an exposure that is
		// not there.
		if f := findField(s.Fields, fr.Field); f != nil && f.Hidden {
			ps.BlockerFix(where+".field",
				fmt.Sprintf("%q is declared hidden, so no caller receives it — there is "+
					"nothing for a permission to unlock", fr.Field),
				"drop one of the two: hidden takes the field out of every response, "+
					"fieldRestrict takes it out only for callers without the permission")
		}
	}

	validateComputed(s, r, jr, ps)

	if r.IdentityView != "" {
		if !IdentityViews.Has(r.IdentityView) {
			ps.BlockerFix("read.identityView",
				fmt.Sprintf("%q is not an identity-view action", r.IdentityView),
				"one of: "+IdentityViews.String())
		}
		if s.Storage.Kind != "sharedbase-role" {
			ps.BlockerFix("read.identityView",
				"only a shared-base model has an identity view",
				"remove the key")
		}
	}
}

// validateComputed checks the derived read fields.
//
// The rules all trace back to one fact: a computed field has NO COLUMN. So it
// cannot be filtered, cannot be ordered by, and cannot feed another computed
// field — and its sources have to be real, stored fields, because pushing them
// to the store is what makes `?fields=<computed>` work at all.
func validateComputed(s *Spec, r Read, jr joinReach, ps *Problems) {
	perEntry := 0
	for i := range s.Children {
		perEntry += len(s.Children[i].Computed)
	}
	if len(r.Computed) == 0 && perEntry == 0 {
		return
	}
	if !r.ByID && r.ByParams == nil {
		ps.BlockerFix("read.computed",
			"a computed field is a READ field, and this entity serves no read",
			"set read.byId and/or read.byParams, or drop the computed fields")
	}
	seen := map[string]bool{}
	for _, c := range r.Computed {
		seen[c.Name] = true
	}
	for i := range s.Children {
		validateChildComputed(s, s.Children[i], jr,
			fmt.Sprintf("children[%d] (%s)", i, orUnnamed(s.Children[i].Name)), ps)
	}
	for i, c := range r.Computed {
		where := fmt.Sprintf("read.computed[%d] (%s)", i, orUnnamed(c.Name))
		switch {
		case c.Name == "":
			ps.Blockerf(where, "the computed field needs a name")
		case !goIdentRe.MatchString(c.Name):
			ps.BlockerFix(where,
				fmt.Sprintf("%q is not a usable Go field name", c.Name),
				"use exported PascalCase, e.g. DisplayName")
		case readableField(s, jr, c.Name):
			ps.BlockerFix(where,
				fmt.Sprintf("%q is already a field of this entity", c.Name),
				"a computed field is one the STORE does not hold — rename it, or drop "+
					"the declaration and read the stored field")
		case collectionFieldNamed(s, c.Name) != nil:
			// The Result declares one Go field per collection, under the
			// collection's PLURAL — that name and no other, which is why this
			// asks about the plural rather than through CollectionNamed: the
			// entry TYPE is `<Name>RowResult` and collides with nothing, so
			// refusing a derived field called Guardian would be a refusal with
			// no defect behind it.
			//
			// A derived field taking the plural, though, is two struct fields
			// with one name — a compile error in a tree the author did not
			// write. The same collision is already refused for a stored field
			// (children[].plural against fields[]); this is the half the
			// computed key left open.
			ps.BlockerFix(where,
				fmt.Sprintf("%q is the collection name of children[] on this entity, and the "+
					"read shape already declares a field under it", c.Name),
				"rename the derived field — the read DTO cannot carry a collection and a "+
					"value under one name")
		}
		if c.Type == "" {
			ps.Blockerf(where+".type", "the computed field needs a type")
		} else if !FieldTypes.Has(c.Type) {
			ps.BlockerFix(where+".type",
				fmt.Sprintf("%q is not a field type", c.Type),
				"one of: "+FieldTypes.String())
		}
		if len(c.From) == 0 {
			ps.BlockerFix(where+".from",
				"the computed field names no source",
				"list the stored fields the derivation reads — they are what the "+
					"framework fetches when a caller selects this field, so a source left "+
					"out arrives nil")
		}
		for _, src := range c.From {
			if seen[src] {
				ps.BlockerFix(where+".from",
					fmt.Sprintf("%q is itself computed, so it has no column to push down", src),
					"name the STORED fields behind it instead")
				continue
			}
			if readableAsRootSource(s, jr, src) {
				continue
			}
			if coll := collectionScopedSource(s, jr, src); coll != nil {
				// The fix names the source AS THE PER-ENTRY KEY SPELLS IT —
				// bare, with no collection in front of it. Echoing back the
				// dotted form the author wrote would send them to a key that
				// refuses exactly that, which is the shape of defect this whole
				// round is about: a message arguing the opposite of the key it
				// points at.
				inScope := src
				if _, tail, dotted := strings.Cut(src, "."); dotted {
					inScope = tail
				}
				ps.BlockerFix(where+".from",
					fmt.Sprintf("%q belongs to the collection %s, and this derivation runs "+
						"once per DOCUMENT — what the root holds for a collection is a slice "+
						"of entries, so there is no single value to hand it", src, coll.Plural),
					fmt.Sprintf("derive it per entry: declare the field under children[] on %s, "+
						"with from: [%s] — the entry is the scope there, so the collection is "+
						"not spelled in front of it", coll.Plural, inScope))
				continue
			}
			reportUnreadable(s, src, where+".from", ps)
		}
		proveDerivationParamsDistinct(c.From, where, ps)
	}
	// A filter and a sort are both evaluated in the store. Declaring either
	// over a computed field is refused HERE rather than at the framework's boot
	// guard, so the author reads it against the spec they wrote.
	if bp := r.ByParams; bp != nil {
		for i, f := range bp.Filters {
			if computedNamed(s, f.Field) {
				ps.BlockerFix(fmt.Sprintf("read.byParams.filters[%d]", i),
					fmt.Sprintf("%q is computed — a filter is evaluated in the store, and "+
						"there is no column there to compare", f.Field),
					"filter on the stored fields it is derived from")
			}
		}
		for _, sf := range bp.Sort {
			if computedNamed(s, sf) {
				ps.BlockerFix("read.byParams.sort",
					fmt.Sprintf("%q is computed, so it backs no column to order by — "+
						"ordering happens in the store and the keyset cursor is built from "+
						"stored values", sf),
					"sort on the stored fields it is derived from")
			}
		}
		for _, sf := range bp.Controls.Search {
			if computedNamed(s, sf) {
				ps.BlockerFix("read.byParams.controls.search",
					fmt.Sprintf("%q is computed, so no index covers it", sf),
					"search the stored fields it is derived from")
			}
		}
	}
	for i, idx := range r.Indexes {
		for _, fn := range idx.Fields {
			if computedNamed(s, fn) {
				ps.BlockerFix(fmt.Sprintf("read.indexes[%d]", i),
					fmt.Sprintf("%q is computed — there is no stored value to index", fn),
					"index the stored fields it is derived from")
			}
		}
	}
	for i, fr := range r.FieldRestrict {
		if computedNamed(s, fr.Field) {
			ps.BlockerFix(fmt.Sprintf("read.fieldRestrict[%d]", i),
				fmt.Sprintf("%q is computed — Restrict scrubs a COLUMN from the "+
					"projection, sort and filter, and there is none", fr.Field),
				"restrict the stored fields it is derived from; the derivation then "+
					"receives them absent")
		}
	}
}

// computedNamed reports whether a name addresses a derived read field, in
// either scope: the root's own, or a collection's as `<collection>.<name>`.
//
// Everything that reaches the STORE consults it — filters, sort, search,
// indexes, fieldRestrict — because the answer is the same wherever the
// derivation lives: there is no column, so there is nothing to compare, order,
// index or scrub. The per-entry scope had to be added here in the same round it
// was added to the language, or `read.indexes: [Permissoes.Rotulo]` would have
// declared an index over a field the projection does not store.
func computedNamed(s *Spec, name string) bool {
	if head, tail, dotted := strings.Cut(name, "."); dotted {
		c := CollectionNamed(s.Children, head)
		if c == nil {
			return false
		}
		for _, cc := range c.Computed {
			if cc.Name == tail {
				return true
			}
		}
		return false
	}
	for _, cc := range s.Read.Computed {
		if cc.Name == name {
			return true
		}
	}
	return false
}

// entryReadableField resolves a name against ONE ENTRY of a collection — the
// scope a per-entry derivation runs in.
//
// Three sources, and they are exactly what the entry's Result carries: the
// entry's own fields (a composite's parts included, like everywhere else), the
// fields a join declared `inChild` brings onto it, and a 1:1 facet attached to
// the collection, whose fields are folded into the entry's struct.
//
// The ROOT's fields are deliberately absent, and that is the framework's rule
// rather than this generator's taste: a nested field's computed sources are
// recorded under the SAME segment prefix as the field, so naming a root field
// here would push `<collection>.<rootField>` down to a store that has no such
// path.
func entryReadableField(s *Spec, jr joinReach, c Child, name string) *Field {
	if f := fieldNamedIn(jr.child[c.Name], name); f != nil {
		return f
	}
	if f := findLogicalField(c.Fields, s, name); f != nil {
		return f
	}
	for i := range s.Siblings {
		attached, isChildFacet := strings.CutPrefix(s.Siblings[i].AttachTo, "child:")
		if !isChildFacet || CollectionNamed(s.Children, attached) == nil ||
			CollectionNamed(s.Children, attached).Name != c.Name {
			continue
		}
		if f := findLogicalField(s.Siblings[i].Fields, s, name); f != nil {
			return f
		}
	}
	return nil
}

// validateChildComputed is validateComputed's per-entry half.
//
// It asks the same questions in a smaller scope, and the two refusals worth
// reading are the ones that point at each other: a source the ENTRY does not
// hold but the root does belongs in read.computed, and a name the entry already
// holds is a stored field being shadowed. Everything else — the name being a Go
// identifier, the type being a type, `from` being non-empty — is the same
// contract as the root's, because it is the same declaration.
func validateChildComputed(s *Spec, c Child, jr joinReach, where string, ps *Problems) {
	if len(c.Computed) == 0 {
		return
	}
	if s.Read.Backing == "" && !s.Read.ByID && s.Read.ByParams == nil {
		return // the root gate already said this entity serves no read
	}
	if c.OwnedBy == "base" && s.Storage.Base != nil && s.Storage.Base.Reuse {
		ps.BlockerFix(where+".computed",
			fmt.Sprintf("%s belongs to the shared identity, and this role only EXPOSES it — "+
				"the entry's read shape is declared once, by the role that owns the identity",
				c.Plural),
			"declare the derivation on that role's spec; both roles then serve the same "+
				"derived field, which is the point of one shape per collection")
		return
	}
	seen := map[string]bool{}
	for _, cc := range c.Computed {
		seen[cc.Name] = true
	}
	for i, cc := range c.Computed {
		w := fmt.Sprintf("%s.computed[%d] (%s)", where, i, orUnnamed(cc.Name))
		switch {
		case cc.Name == "":
			ps.Blockerf(w, "the computed field needs a name")
		case !goIdentRe.MatchString(cc.Name):
			ps.BlockerFix(w,
				fmt.Sprintf("%q is not a usable Go field name", cc.Name),
				"use exported PascalCase, e.g. Rotulo")
		case cc.Name == IdentityName:
			ps.BlockerFix(w,
				"the entry's id is carried by the framework, so a derived field cannot take its name",
				"name the derived field for what it MEANS, not for the handle it rides next to")
		case entryReadableField(s, jr, c, cc.Name) != nil:
			ps.BlockerFix(w,
				fmt.Sprintf("%q is already a field of the collection %s", cc.Name, c.Plural),
				"a computed field is one the STORE does not hold — rename it, or drop "+
					"the declaration and read the stored field")
		}
		if cc.Type == "" {
			ps.Blockerf(w+".type", "the computed field needs a type")
		} else if !FieldTypes.Has(cc.Type) {
			ps.BlockerFix(w+".type",
				fmt.Sprintf("%q is not a field type", cc.Type),
				"one of: "+FieldTypes.String())
		}
		if len(cc.From) == 0 {
			ps.BlockerFix(w+".from",
				"the computed field names no source",
				fmt.Sprintf("list the ENTRY's fields the derivation reads, bare — the "+
					"entry is the scope, so %s is not spelled in front of them", c.Plural))
		}
		for _, src := range cc.From {
			if seen[src] {
				ps.BlockerFix(w+".from",
					fmt.Sprintf("%q is itself computed, so it has no column to push down", src),
					"name the STORED fields behind it instead")
				continue
			}
			if entryReadableField(s, jr, c, src) != nil {
				continue
			}
			if readableAsRootSource(s, jr, src) {
				ps.BlockerFix(w+".from",
					fmt.Sprintf("%q is a field of the entity, not of one entry of %s — and the "+
						"framework pushes a nested field's sources down under its OWN segment, "+
						"so this would ask the store for %s.%s", src, c.Plural, c.Plural, src),
					"name a field the entry holds; if the derivation is really about the "+
						"record as a whole, it belongs in read.computed")
				continue
			}
			ps.BlockerFix(w+".from",
				fmt.Sprintf("%q does not name a field of the collection %s", src, c.Plural),
				fmt.Sprintf("one of: %s", childFieldNames(c)))
		}
		proveDerivationParamsDistinct(cc.From, w, ps)
	}
}

// proveDerivationParamsDistinct keeps a derivation's signature COMPILABLE.
//
// Each source becomes a parameter under its camelCase name, so two sources that
// camel to one word emit `func Compute…(ctx …, idNumber string, idNumber string)`
// — generated code the author did not write and cannot fix from the spec that
// produced it. The collision is real rather than theoretical: a leading run of
// capitals lowercases as a unit, so `IDNumber` and `IdNumber` are one parameter,
// and a source listed twice is trivially one.
//
// `ctx` is claimed before the loop, because every derivation already takes the
// AppContext under that name. The domain-service facts have had this exact check
// since a manual fact could take two filters; the read side's derivations went
// without it, and the failure mode is the same one.
func proveDerivationParamsDistinct(from []string, where string, ps *Problems) {
	params := map[string]string{"ctx": ""}
	for _, src := range from {
		name := naming.Camel(src)
		prev, clash := params[name]
		switch {
		case !clash:
			params[name] = src
		case prev == src:
			ps.BlockerFix(where+".from",
				fmt.Sprintf("%q is listed twice, and each source is one parameter", src),
				"name it once — the derivation receives it once either way")
		case prev == "":
			ps.BlockerFix(where+".from",
				fmt.Sprintf("%q reaches the derivation as the parameter %q, which is the "+
					"AppContext's", src, name),
				"rename the field, or derive from a different one — two parameters of one "+
					"name do not compile")
		default:
			ps.BlockerFix(where+".from",
				fmt.Sprintf("%s and %s both reach the derivation as the parameter %q",
					prev, src, name),
				"two parameters of one name do not compile; drop one, or rename the field "+
					"so the two are told apart")
		}
	}
}

func validateByParams(s *Spec, r Read, jr joinReach, ps *Problems) {
	bp := r.ByParams
	for i, f := range bp.Filters {
		where := fmt.Sprintf("read.byParams.filters[%d] (%s)", i, orUnnamed(f.Field))
		// The lowering resolves a filter against the root's own fields (and its
		// root-attached facets) — nothing else. Validation used to bless the
		// wider set readableField accepts, and every spelling in the gap
		// (a collection's field, dotted or not) validated green and was then
		// silently dropped from the generated request type.
		fld := filterableField(s, jr, f.Field)
		if fld == nil {
			if readableField(s, jr, f.Field) {
				ps.BlockerFix(where,
					fmt.Sprintf("%q belongs to a collection, and a filter reaches only "+
						"the root's own fields — it would be silently dropped", f.Field),
					"filter on a root field, or promote the value to the root if the "+
						"listing must narrow by it")
			} else {
				reportUnreadable(s, f.Field, where, ps)
			}
			continue
		}
		if fld.Runtime {
			ps.Blockerf(where,
				"%q is runtime-only — it has no column for the filter to compare", f.Field)
			continue
		}
		// A mandatory filter is not generated by this build: the endpoint would
		// serve it as optional and nothing would say so.
		if f.Required {
			ps.BlockerFix(where+".required",
				"a mandatory filter is not generated by this build — the endpoint "+
					"would serve the parameter as optional",
				"drop the key; if the narrowing is a business rule, model it as its "+
					"own route or enforce it in authz.dataAccess")
		}
		if len(f.Ops) == 0 {
			ps.Blockerf(where+".ops", "the filter declares no operator")
		}
		for _, op := range f.Ops {
			if !FilterOps.Has(op) {
				ps.BlockerFix(where+".ops",
					fmt.Sprintf("%q is not a filter operator", op),
					"one of: "+FilterOps.String())
				continue
			}
			checkOpAgainstType(*fld, op, where, ps)
		}
	}
	// The two halves of the ordering contract travel together, and the framework
	// fails the BOOT on either alone. Refusing them here is the same verdict
	// reached where the spec can still name which half is missing — and where the
	// author has not yet built a service that panics on start.
	switch {
	case bp.Controls.OrderBy && len(bp.Sort) == 0:
		ps.BlockerFix("read.byParams.sort",
			"controls.orderBy is the SWITCH for ?orderBy= and nothing declares the "+
				"vocabulary it switches on — the endpoint would accept the parameter and "+
				"then refuse every token it could be given",
			"list the orderable paths under sort: — nothing is orderable until it is "+
				"named, because an unindexed sort is a blocking sort whose cost grows with "+
				"the matching set")
	case !bp.Controls.OrderBy && len(bp.Sort) > 0:
		ps.BlockerFix("read.byParams.controls.orderBy",
			fmt.Sprintf("sort declares %s orderable and this listing does not serve "+
				"?orderBy=, so the vocabulary reaches no wire", strings.Join(bp.Sort, ", ")),
			"set controls.orderBy: true, or drop sort")
	}
	seenSort := map[string]bool{}
	for _, sf := range bp.Sort {
		if seenSort[sf] {
			ps.BlockerFix("read.byParams.sort",
				fmt.Sprintf("%q is listed twice", sf),
				"a repeated path is not a harmless duplicate: the terms become the "+
					"reader's sort document, where a duplicated key is malformed")
			continue
		}
		seenSort[sf] = true
		// Ordering happens in the STORE, so the path needs a column there — the
		// same requirement a filter has, and the same set it draws from.
		if filterableField(s, jr, sf) != nil {
			continue
		}
		if readableField(s, jr, sf) {
			ps.BlockerFix("read.byParams.sort",
				fmt.Sprintf("%q is readable but has no column this listing can order by", sf),
				"order by a field of the entity's own row (its facets and the "+
					"framework-stamped columns under read.managed included) — a "+
					"collection's field has no single value per row to sort on")
			continue
		}
		reportUnreadable(s, sf, "read.byParams.sort", ps)
	}
	for _, sf := range bp.Controls.Search {
		if !readableField(s, jr, sf) {
			reportUnreadable(s, sf, "read.byParams.controls.search", ps)
			continue
		}
		if f := filterableField(s, jr, sf); f != nil && f.Type != "string" {
			ps.BlockerFix("read.byParams.controls.search",
				fmt.Sprintf("%q is not text, so a search cannot match it", sf),
				"search covers string fields")
		}
	}
	if len(bp.Controls.Search) > 0 {
		if r.Backing == "relational" {
			ps.BlockerFix("read.byParams.controls.search",
				"a relational view has no text index to serve a search",
				"drop the search, or set read.backing: mongo")
		} else if !hasTextIndex(r.Indexes) {
			ps.BlockerFix("read.indexes",
				"a search is served but no text index backs it",
				"declare an index with text: true over the searched fields")
		}
	}
	if bp.Controls.IncludeArchived && s.Storage.Managed.ArchivedAt == "" {
		ps.BlockerFix("read.byParams.controls.includeArchived",
			"the listing offers to include archived rows but nothing is ever archived",
			"remove the control, or declare storage.managed.archivedAt")
	}
}

// checkOpAgainstType keeps an operator from being offered where it cannot mean
// anything — ordering a boolean, or prefix-matching a number.
func checkOpAgainstType(f Field, op, where string, ps *Problems) {
	textOnly := map[string]bool{
		"startswith": true, "contains": true, "istartswith": true,
		"icontains": true, "ieq": true, "ine": true, "iin": true, "inin": true,
	}
	ordered := map[string]bool{"gt": true, "gte": true, "lt": true, "lte": true}
	if textOnly[op] && f.Type != "string" {
		ps.BlockerFix(where+".ops",
			fmt.Sprintf("%q only applies to text, and %s is %s", op, f.Name, f.Type),
			"use eq/ne/in/nin for this type")
	}
	if ordered[op] && (f.Type == "bool" || f.Type == "id") {
		ps.BlockerFix(where+".ops",
			fmt.Sprintf("%q needs an ordered type, and %s is %s", op, f.Name, f.Type),
			"use eq/ne for this type")
	}
}

func validateSurfaces(s *Spec, ps *Problems) {
	su := s.Surfaces
	gqlOn := su.GraphQL != nil && su.GraphQL.Enabled
	exportsOn := su.Exports != nil
	// Three independent switches, and the check is that at least ONE of them is
	// on. Exports counts: a spec that serves nothing but the spreadsheet is a
	// legitimate shape, and refusing it forced surfaces.rest: true on a project
	// that then published a CRUD API it never asked for.
	if !su.REST && !gqlOn && !exportsOn {
		ps.BlockerFix("surfaces",
			"the entity exposes no surface",
			"set surfaces.rest: true, surfaces.graphql.enabled: true, or declare surfaces.exports")
	}
	if gqlOn {
		for _, m := range su.GraphQL.Mutations {
			if !GraphQLMutations.Has(m) {
				ps.BlockerFix("surfaces.graphql.mutations",
					fmt.Sprintf("%q is not a mutable verb", m),
					"one of: insert | update | delete | archive | unarchive")
				continue
			}
			// A mutation for a verb the entity does not accept would reference
			// a command this run never emits.
			if !contains(s.Modes, m) {
				ps.BlockerFix("surfaces.graphql.mutations",
					fmt.Sprintf("a %s mutation is declared but %s is not among the modes", m, m),
					"add it to modes, or drop the mutation")
			}
		}
		if su.GraphQL.Connection != nil && *su.GraphQL.Connection && !contains(s.Modes, "display") {
			ps.BlockerFix("surfaces.graphql.connection",
				"a connection is a paged read but the entity has no display mode",
				"add display to modes")
		}
		validateGraphQLExposesSomething(s, ps)
	}
	warnAboutModesOnNoSurface(s, ps)
	validateChildSurfaces(s, ps)
	if su.Exports != nil {
		if s.Read.ByParams == nil {
			ps.BlockerFix("surfaces.exports",
				"an export writes out a listing, and no listing is served",
				"declare read.byParams, or remove the exports")
		}
		if su.Exports.CSV == nil && su.Exports.XLSX == nil {
			ps.BlockerFix("surfaces.exports",
				"the exports block names no format",
				"declare csv, xlsx, or both")
		}
		if su.Exports.CSV != nil {
			d := su.Exports.CSV.Delimiter
			if len([]rune(d)) != 1 {
				ps.BlockerFix("surfaces.exports.csv.delimiter",
					"the delimiter must be exactly one character",
					"e.g. \",\" or \";\"")
			}
		}
		if su.Exports.XLSX != nil && su.Exports.XLSX.Sheet == "" {
			ps.BlockerFix("surfaces.exports.xlsx.sheet",
				"the spreadsheet needs a sheet name", "e.g. Students")
		}
	}
}

// warnAboutModesOnNoSurface says out loud when a mode is generated and routed
// nowhere.
//
// A WARNING and not a blocker, and the difference is a legitimate shape: the
// command, its rules and its DTOs still exist, and a hand-written route may
// mount them — that is what a `source: manual` field is for. What must not
// happen is the silence. `modes: [insert, update]` under a surface that carries
// neither used to read exactly like a working API.
//
// The collection verbs ARE a blocker instead, because there the fix costs
// nothing: children[].operations drops the verb outright, and reaching that
// state takes an explicit narrowing rather than an omission.
func warnAboutModesOnNoSurface(s *Spec, ps *Problems) {
	if s.Surfaces.REST {
		return
	}
	gqlOn := s.Surfaces.GraphQL != nil && s.Surfaces.GraphQL.Enabled
	for _, m := range s.Modes {
		if m == "display" {
			// The reads: a display mode reaches the schema through the queries,
			// and the exports are a read surface of their own.
			if gqlOn || s.Surfaces.Exports != nil {
				continue
			}
			ps.WarnFix("modes",
				"display is declared and no surface serves a read: no REST routes, no schema, no export",
				"turn a surface on, or drop display")
			continue
		}
		if gqlOn && exposedAsMutation(s, m) {
			continue
		}
		ps.WarnFix("modes",
			fmt.Sprintf("the %s mode is generated but reaches no surface", m),
			fmt.Sprintf("expose it (surfaces.rest, or surfaces.graphql.mutations), drop %q from modes, "+
				"or ignore this if a hand-written route mounts the command", m))
	}
}

// exposedAsMutation is whether one mode is a mutation, reading the same default
// the IR resolves: an absent list narrows nothing.
func exposedAsMutation(s *Spec, mode string) bool {
	g := s.Surfaces.GraphQL
	if len(g.Mutations) == 0 {
		return GraphQLMutations.Has(mode)
	}
	return contains(g.Mutations, mode)
}

// validateGraphQLExposesSomething refuses a surface that is on and empty.
//
// Enabling GraphQL and narrowing everything off it is not a shape anyone means:
// the generated Mount function compiles, the registry is wired, the playground
// answers — and the schema has no field for this entity. It used to be
// reachable by simply not writing `mutations` on an entity with no reads, and
// nothing anywhere said the surface was hollow.
func validateGraphQLExposesSomething(s *Spec, ps *Problems) {
	g := s.Surfaces.GraphQL
	connection := (g.Connection == nil || *g.Connection) && s.Read.ByParams != nil
	byID := s.Read.ByID
	mutations := len(g.Mutations) > 0
	if g.Mutations == nil {
		// Absent narrows nothing, so every write verb among the modes is a
		// mutation — and `display` is not one of them.
		for _, m := range s.Modes {
			if GraphQLMutations.Has(m) {
				mutations = true
				break
			}
		}
	}
	children := false
	for _, c := range s.Children {
		if len(childGraphQLVerbs(s, c)) > 0 {
			children = true
			break
		}
	}
	if connection || byID || mutations || children {
		return
	}
	ps.BlockerFix("surfaces.graphql",
		"the GraphQL surface is on and exposes nothing: no query, no mutation, no collection verb",
		"give the entity a read (read.byId / read.byParams) or a write mode, or turn the surface off")
}

// childGraphQLVerbs is which per-entry verbs of one collection reach the schema,
// resolved against the entity's own surface. Empty means the collection is not
// on GraphQL at all — because the entity is not, because the collection said no,
// or because it mounts no per-entry verb to expose.
func childGraphQLVerbs(s *Spec, c Child) []string {
	if c.EditStrategy != "per-child" {
		return nil
	}
	if s.Surfaces.GraphQL == nil || !s.Surfaces.GraphQL.Enabled {
		return nil
	}
	if cs := c.Surfaces; cs != nil && cs.GraphQL != nil {
		if cs.GraphQL.Enabled != nil && !*cs.GraphQL.Enabled {
			return nil
		}
		if len(cs.GraphQL.Mutations) > 0 {
			return cs.GraphQL.Mutations
		}
	}
	return PerChildOperations(c)
}

// childOnREST is whether one collection's per-entry routes are mounted.
func childOnREST(s *Spec, c Child) bool {
	if !s.Surfaces.REST {
		return false
	}
	if cs := c.Surfaces; cs != nil && cs.REST != nil {
		return *cs.REST
	}
	return true
}

// validateChildSurfaces checks the collection-level seat: that it is declared
// where it can mean something, that it names verbs the collection actually
// mounts, that it does not try to widen past the entity, and — the one this key
// exists for — that no mounted verb ends up on NO surface.
func validateChildSurfaces(s *Spec, ps *Problems) {
	gqlOn := s.Surfaces.GraphQL != nil && s.Surfaces.GraphQL.Enabled
	for _, c := range s.Children {
		where := fmt.Sprintf("children[%s].surfaces", c.Name)
		cs := c.Surfaces
		if cs != nil && c.EditStrategy != "per-child" {
			ps.BlockerFix(where,
				"only a per-child collection has per-entry verbs to place on a surface",
				"set editStrategy: per-child, or drop the surfaces block")
			continue
		}
		if cs != nil {
			if cs.REST != nil && *cs.REST && !s.Surfaces.REST {
				ps.BlockerFix(where+".rest",
					"the collection asks for REST and the entity serves no REST surface",
					"set surfaces.rest: true on the entity, or drop this key")
			}
			if g := cs.GraphQL; g != nil {
				if g.Enabled != nil && *g.Enabled && !gqlOn {
					ps.BlockerFix(where+".graphql.enabled",
						"the collection asks for GraphQL and the entity serves no GraphQL surface",
						"set surfaces.graphql.enabled: true on the entity, or drop this key")
				}
				for _, v := range g.Mutations {
					if !ChildOperations.Has(v) {
						ps.BlockerFix(where+".graphql.mutations",
							fmt.Sprintf("%q is not a per-entry verb", v),
							"one of: add | change | remove")
						continue
					}
					if !MountsPerChildOp(c, v) {
						ps.BlockerFix(where+".graphql.mutations",
							fmt.Sprintf("%q is exposed but the collection does not mount it", v),
							fmt.Sprintf("add %s to children[%s].operations, or drop it here", v, c.Name))
					}
				}
			}
		}
		if c.EditStrategy != "per-child" {
			continue
		}
		// The point of the whole block: a verb the collection mounts and no
		// surface carries is generated code nobody can call — the exact silence
		// that made every collection REST-only and said nothing about it.
		onREST := childOnREST(s, c)
		onGQL := childGraphQLVerbs(s, c)
		for _, v := range PerChildOperations(c) {
			if onREST || contains(onGQL, v) {
				continue
			}
			ps.BlockerFix(where,
				fmt.Sprintf("the %s verb of %s reaches no surface: it would be generated and unreachable", v, c.Name),
				fmt.Sprintf("expose it (surfaces.rest, or children[%s].surfaces.graphql), or drop %q from operations", c.Name, v))
		}
	}
}

// validateRowScopePolicy checks the two knobs that decide what the row scope
// does at its EDGES: who may cross it, and what an absent identity means.
//
// Both used to be hardcoded. The bypass did not exist at all, so a platform
// operator was filtered to their own tenant like anybody else and could not
// support a customer through the API. The absent
// identity was an `else` branch scoping the read to "", so a freshly generated
// tenant-scoped entity answered every listing empty on the dev bench, which is
// the first place anybody runs it.
func validateRowScopePolicy(a Authz, ps *Problems) {
	scoped := a.DataAccess == "owner-only" || a.DataAccess == "tenant"

	if a.Bypass != "" {
		if !scoped {
			ps.BlockerFix("authz.bypass",
				fmt.Sprintf("there is no row scope to cross: dataAccess is %q", a.DataAccess),
				"drop it, or scope the rows with dataAccess: owner-only or tenant")
		}
		switch {
		case a.Bypass == SuperAdminClaim:
			// The superadmin wildcard, and the reason this is a `case` with no
			// body: it is the one wildcard a caller CAN be tested for, so the
			// refusal below must not swallow it. Which framework question the
			// guard asks instead of HasPermission is Authz.Bypass's own
			// documentation.
		default:
			// The mistake the wildcard branch exists to catch is the natural way
			// to write the intent, and it does not fail until a request arrives.
			// It is the same mistake a source: permission field can make, so the
			// judgement is shared and only the way OUT differs: there the escape
			// from `*:*` is a source of its own, here it is this very key.
			validateConcretePermission(a.Bypass, "authz.bypass",
				`"*:*" is the one exception, because the framework answers that question `+
					`with its own method (Identity.IsSuperAdmin) rather than with `+
					`HasPermission. Anything narrower has to be a concrete permission: `+
					`grant something like platform:cross-tenant and name it here`, ps)
		}
	}

	if a.NoIdentity != "" {
		if !scoped {
			ps.BlockerFix("authz.noIdentity",
				fmt.Sprintf("nothing is scoped by the identity: dataAccess is %q", a.DataAccess),
				"drop it, or scope the rows with dataAccess: owner-only or tenant")
		} else if !NoIdentityPolicies.Has(a.NoIdentity) {
			ps.BlockerFix("authz.noIdentity",
				fmt.Sprintf("%q is not a policy for an absent identity", a.NoIdentity),
				"one of: "+NoIdentityPolicies.String())
		}
	}
	// `refuse` is the deliberate one, and it is worth a line: it is the only
	// setting under which a bench with auth.mode disabled serves nothing and
	// accepts no write — which reads as a broken service rather than as a
	// policy, and is where an hour goes before anyone suspects the spec.
	//
	// stand-down, the default, gets no warning: it is what every other
	// identity-derived rule this generator writes already does, and warning on
	// the default would be noise on nearly every scoped spec.
	if scoped && a.NoIdentity == "refuse" {
		ps.WarnFix("authz.noIdentity",
			"with authentication disabled there is no identity, so this entity will "+
				"serve no rows and accept no write on a dev bench",
			"that is what refuse means and it may well be what you want; drop the key "+
				"for the default (stand-down), which applies the scope whenever a caller "+
				"is authenticated and steps aside only where there is nobody to scope to")
	}
}

func validateAuthz(s *Spec, ps *Problems) {
	a := s.Authz
	if a.Resource == "" {
		ps.BlockerFix("authz.resource",
			"the permission resource is required",
			"usually the entity name in lower case, e.g. student")
	}
	if a.DataAccess == "" {
		ps.BlockerFix("authz.dataAccess",
			"the spec does not say who may read or modify which rows",
			"anyone-with-permission is a valid answer, but it has to be stated")
	} else if !DataAccess.Has(a.DataAccess) {
		ps.BlockerFix("authz.dataAccess",
			fmt.Sprintf("%q is not a data-access model", a.DataAccess),
			"one of: "+DataAccess.String())
	}
	// The owner/tenant field must be PERSISTED: row scoping is a WHERE clause,
	// and a runtime-only field has no column to put in it. Accepting one here
	// used to produce the worst possible outcome — the spec validated, the
	// report said "owner-only", and the generated service quietly served every
	// row to any caller holding the read permission.
	switch a.DataAccess {
	case "owner-only":
		if a.OwnerField == "" {
			ps.BlockerFix("authz.ownerField",
				"owner-only access needs the field that identifies the owner",
				"name a persisted field the server fills from the caller's identity: "+
					"declare it with assignedFrom: identity-subject")
		} else if f := findField(s.Fields, a.OwnerField); f == nil {
			ps.Blockerf("authz.ownerField", "%q does not name a field of this entity", a.OwnerField)
		} else if f.Runtime {
			ps.BlockerFix("authz.ownerField",
				fmt.Sprintf("%q is runtime-only — it has no column, so the rows cannot be narrowed by it",
					a.OwnerField),
				"make it a persisted field the server fills from the caller's identity "+
					"(assignedFrom: identity-subject), so every row records its owner")
		}
	case "tenant":
		if a.TenantField == "" {
			ps.BlockerFix("authz.tenantField",
				"tenant access needs the field carrying the tenant",
				"name a persisted field the tenant claim is matched against: "+
					"declare it with assignedFrom: identity-claim and the claim's name")
		} else if f := findField(s.Fields, a.TenantField); f == nil {
			ps.Blockerf("authz.tenantField", "%q does not name a field of this entity", a.TenantField)
		} else if f.Runtime {
			ps.BlockerFix("authz.tenantField",
				fmt.Sprintf("%q is runtime-only — it has no column, so the rows cannot be narrowed by it",
					a.TenantField),
				"make it a persisted field the server fills from the tenant claim "+
					"(assignedFrom: identity-claim with claim: <name>), so every row records its tenant")
		}
	}

	validateRowScopePolicy(a, ps)

	// Permissions are cross-checked BOTH ways: a permission for an operation
	// that is not mounted is dead configuration, and a mounted operation with no
	// permission is a boot panic under an enabled authorization layer.
	ops := mountedOperations(s)
	for key := range a.Permissions {
		if !AuthzOperations.Has(key) {
			ps.BlockerFix("authz.permissions",
				fmt.Sprintf("%q is not an operation", key),
				"one of: "+AuthzOperations.String())
			continue
		}
		if !ops[key] {
			ps.BlockerFix("authz.permissions",
				fmt.Sprintf("a permission is declared for %q but that operation is not served", key),
				"add the mode/surface that mounts it, or remove the permission")
		}
		if strings.TrimSpace(a.Permissions[key]) == "" {
			ps.BlockerFix("authz.permissions."+key,
				"the permission is empty",
				"an empty string registers a route that no permission can satisfy")
		} else if a.Resource != "" && !strings.HasPrefix(a.Permissions[key], a.Resource+":") {
			// A WARNING, not a blocker: the framework itself does not require
			// the prefix, and a deliberately short claim ("bib:ler" for the
			// bibliotecario resource) is legitimate. But a permission outside
			// the resource's namespace is also what a typo or a claim borrowed
			// from another entity looks like — so it is said, not refused.
			ps.WarnFix("authz.permissions."+key,
				fmt.Sprintf("%q is not namespaced by the declared resource %q",
					a.Permissions[key], a.Resource),
				fmt.Sprintf("if that is unintended, spell it %s:<action> — a borrowed "+
					"claim grants the wrong thing quietly", a.Resource))
		}
	}
	for op := range ops {
		if _, ok := a.Permissions[op]; !ok {
			// The fix names the CANONICAL permission rather than a placeholder:
			// left to invent one, each spec picks a different word for the same
			// verb — create here, write there — and the deployment ends up
			// granting three things for one operation.
			ps.BlockerFix("authz.permissions",
				fmt.Sprintf("the %s operation is served but has no permission", op),
				fmt.Sprintf("add %s: %s, or whatever string your project already grants "+
					"for it — the permission is required (with authorization enabled a route "+
					"without one aborts boot); the spelling is only the suggested default, "+
					"in which PUT and PATCH share :update and unarchive shares :archive",
					op, CanonicalPermission(a.Resource, op)))
		}
	}
}

// mountedOperations is the single source of truth for "what this spec actually
// serves". Both the authz cross-check and the emitters read it, so a permission
// can never disagree with a route.
func mountedOperations(s *Spec) map[string]bool {
	ops := map[string]bool{}
	for _, m := range s.Modes {
		switch m {
		case "insert":
			ops["insert"] = true
		case "update":
			switch s.Update.Shape {
			case "put":
				ops["update"] = true
			case "patch":
				ops["patch"] = true
			case "both":
				ops["update"] = true
				ops["patch"] = true
			}
		case "delete":
			ops["delete"] = true
		case "archive":
			ops["archive"] = true
		case "unarchive":
			ops["unarchive"] = true
		}
	}
	if s.Read.ByID || s.Read.ByParams != nil {
		ops["read"] = true
	}
	return ops
}

// ---------------------------------------------------------------- helpers

func findField(fs []Field, name string) *Field {
	for i := range fs {
		if fs[i].Name == name {
			return &fs[i]
		}
	}
	return nil
}

// CollectionNamed resolves the collection a key addresses, in EITHER spelling.
//
// A collection has two names and both are real. `name` is the ENTRY's Go type —
// what a single row is — and `plural` is the COLLECTION's name, which the
// framework persists in three places at once: the document segment the
// projection nests the entries under, the read DTO's field, and the
// notification's wire path. Neither is a nickname for the other.
//
// The keys that address a collection used to disagree about which one they
// wanted, and they disagreed silently. joins[].inChild, rules.list[].fields and
// read.computed.from resolved the singular; service.facts[].filters resolved the
// plural — and its refusal argued that plural was "the name everything already
// uses", which was true of the framework and the exact opposite of what the
// other three keys did. One spec, one collection, three spellings, and the only
// way to learn which was which was to be refused.
//
// So every key takes both, and the generator canonicalises to `name` on the way
// into the IR — there is exactly one spelling below this line, and it is not the
// author's problem which. The singular answers first so that a collection
// deliberately named after another's plural still resolves to itself;
// validateCollectionSpellings refuses that overlap outright, which is what makes
// the order a tie-break rather than a policy.
func CollectionNamed(cs []Child, name string) *Child {
	for i := range cs {
		if cs[i].Name == name {
			return &cs[i]
		}
	}
	for i := range cs {
		if cs[i].Plural == name {
			return &cs[i]
		}
	}
	return nil
}

func findChild(cs []Child, name string) *Child { return CollectionNamed(cs, name) }

func findSibling(ss []Sibling, name string) *Sibling {
	for i := range ss {
		if ss[i].Name == name {
			return &ss[i]
		}
	}
	return nil
}

// readableField also accepts a child field addressed as Child.Field, which is
// how the read side names it inside the projected document.
// checkTableName applies the same reserved-word and format rules the root
// table gets. Every table a spec declares must come through here — the base,
// child and sibling tables used to skip it, which is precisely where a
// reserved name fails on SOME engines and passes on others.
// reservedWordFix is one sentence, in one place, because the refusal is the one
// an author is most likely to read as overreach: the word is reserved on an
// engine this project does not target, and renaming a column reads as busywork.
//
// It is not. The list is the UNION across the five engines on purpose, and the
// reason is that the decision is not reversible on the same terms: adding an
// engine later is a config change, while renaming a column that already holds
// data is a migration, a deploy and a window where the two names disagree.
const reservedWordFix = "rename it — the reserved list is the UNION across the five " +
	"engines, not the ones this project targets today, because adding an engine later " +
	"is a config change while renaming a column that already holds data is not"

func checkTableName(name, where string, ps *Problems) {
	if name == "" {
		return // required-ness is the caller's message; this checks the value
	}
	if engine := ReservedWord(name); engine != "" {
		ps.BlockerFix(where,
			fmt.Sprintf("the table name %q is a reserved word (%s)", name, engine),
			reservedWordFix)
	} else if !columnRe.MatchString(name) {
		ps.BlockerFix(where,
			fmt.Sprintf("%q is not a usable table name", name),
			"lowercase, digits and underscores, starting with a letter")
	}
}

// checkColumnName is checkTableName for columns.
func checkColumnName(name, where string, ps *Problems) {
	if name == "" {
		return
	}
	if engine := ReservedWord(name); engine != "" {
		ps.BlockerFix(where,
			fmt.Sprintf("the column name %q is a reserved word (%s)", name, engine),
			reservedWordFix)
	} else if !columnRe.MatchString(name) {
		ps.BlockerFix(where,
			fmt.Sprintf("%q is not a usable column name", name),
			"lowercase, digits and underscores, starting with a letter")
	}
}

// checkFieldDups refuses two fields of one struct sharing a name, and two
// columns of one table sharing a name — the root has always had this; children
// and siblings used to compile their duplicates straight into the generated
// entry type and the DDL.
func checkFieldDups(fields []Field, where string, ps *Problems) {
	seenName := map[string]bool{}
	seenCol := map[string]bool{}
	for i, f := range fields {
		fw := fmt.Sprintf("%s.fields[%d] (%s)", where, i, orUnnamed(f.Name))
		if f.Name != "" {
			if seenName[f.Name] {
				ps.Blockerf(fw, "the field %q is declared twice", f.Name)
			}
			seenName[f.Name] = true
		}
		if f.Column != "" {
			if seenCol[f.Column] {
				ps.Blockerf(fw+".column", "the column %q is declared twice", f.Column)
			}
			seenCol[f.Column] = true
		}
	}
}

// filterableField resolves a listing filter the way the LOWERING does: the
// root's own fields plus its root-attached facets, nothing dotted, nothing of
// a collection. Validation and lowering disagreeing here is exactly how a
// blessed filter used to vanish from the generated request type.
func filterableField(s *Spec, jr joinReach, name string) *Field {
	// A ROOT join's field is filterable and sortable: it rides the root SELECT,
	// so the store can compare and order by it like any column of the table.
	// A CHILD join's is not — narrowing the root by a field of a 1:N collection
	// is a pushdown one root SELECT cannot express.
	if f := fieldNamedIn(jr.root, name); f != nil {
		return f
	}
	// The aggregate id is answered first and unconditionally: it is on the
	// root's row before any field is declared, and nothing in the spec can put
	// it into — or keep it out of — the declared set.
	if f := identityRead(name); f != nil {
		return f
	}
	// A framework-stamped column the read exposes is filterable for the same
	// reason it is projectable: it lives on the ROOT's table and the schema
	// resolves its logical name itself. It is not in s.Fields — nothing declares
	// it there — so it is answered before the declared set is walked.
	if managedRead(s, name) {
		return &Field{Name: name, Type: "time", Column: ManagedColumn(s, name),
			Nullable: name == "DeletedAt", LivesOn: "root"}
	}
	if f := findLogicalField(s.Fields, s, name); f != nil {
		return f
	}
	for i := range s.Siblings {
		if strings.HasPrefix(s.Siblings[i].AttachTo, "child:") {
			continue // a child's facet is read through the child, not the listing
		}
		if f := findLogicalField(s.Siblings[i].Fields, s, name); f != nil {
			return f
		}
	}
	return nil
}

// isSelfNeighbour answers whether a discovered neighbour is the very file
// being validated. When either side carries no path (a spec built in memory),
// it falls back to the entity name — the historical behaviour, kept so tests
// that never touch disk still work.
//
// The two sides arrive in different forms — the spec under check by the path
// the caller typed (often absolute), the neighbour relative to the project
// root — so equality also holds when one is the other's tail at a path
// boundary. Comparing Clean() alone here made every spec its own "duplicate".
func isSelfNeighbour(s *Spec, n Neighbour) bool {
	if s.SourcePath == "" || n.Path == "" {
		return n.Entity == s.Entity
	}
	a, b := filepath.Clean(s.SourcePath), filepath.Clean(n.Path)
	if a == b {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasSuffix(a, sep+b) || strings.HasSuffix(b, sep+a)
}

// findVO resolves a value object declared in THIS spec.
func findVO(vos []ValueObject, name string) *ValueObject {
	for i := range vos {
		if vos[i].Name == name {
			return &vos[i]
		}
	}
	return nil
}

// hasExistsFactFor answers whether the service can ask "is this value taken?"
// for the field — the condition for the unique precheck to exist at all.
// uniqueFilterSet is the exact filter list the pre-check fact must carry: the
// scope the uniqueness is declared within, then the value being made unique.
//
// A COMPOSITE is unique as a tuple, so it contributes every one of its PARTS —
// the composite's own name names no column and cannot appear in a fact at all.
// Anything less answers a different question ("is this resource taken?" instead
// of "is this resource:action taken?") and would refuse writes the model allows.
func uniqueFilterSet(f Field) []string {
	want := append([]string{}, f.Unique.Within...)
	if IsComposite(f) {
		for _, p := range f.Parts {
			want = append(want, ExposedName(p))
		}
		return want
	}
	return append(want, f.Name)
}

// hasExistsFactFor reports whether some exists fact filters by EXACTLY want.
//
// Exactly, in both directions, because the fact's filters and the index's
// columns are two spellings of one decision and this is where they are held to
// it. Accepting a superset is what shipped a per-tenant pre-check beside a
// global index: the domain said the handle was free, the database said it was
// taken, and the caller was told the handle was taken in a tenant where it was
// not.
//
// And exactly in SHAPE too: the columns have to be compared for EQUALITY, one
// parameter each. A unique index answers "is this exact tuple present"; a
// pre-check that ranged, ORed, or pinned half the tuple in the spec would be
// the same disagreement wearing an operator instead of a missing column.
func hasExistsFactFor(s *Spec, want []string) bool {
	if s.Service == nil {
		return false
	}
	for _, fa := range s.Service.Facts {
		if fa.Kind != "exists" {
			continue
		}
		if got, plain := PlainEqFilters(fa.Filters); plain && sameNameSet(got, want) {
			return true
		}
	}
	return false
}

// sameNameSet compares two filter lists as SETS: the order a fact lists its
// filters in decides the parameter order of a generated method, and nothing
// about what the query means.
func sameNameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !contains(b, x) {
			return false
		}
	}
	return true
}

// reportPrecheckMismatch refuses a pre-check whose filters are not the index's
// columns, and says which of the four things went wrong: no fact at all, a fact
// that is missing the scope, a fact that filters by more than the index covers,
// or a fact that compares the right columns the wrong WAY.
func reportPrecheckMismatch(s *Spec, f Field, want []string, where string, ps *Problems) {
	list := strings.Join(want, ", ")
	// The closest candidate: an exists fact that at least mentions the value.
	// Naming it is what turns "declare a fact" into "fix the one you have".
	var near *Fact
	value := want[len(want)-1]
	if s.Service != nil {
		for i := range s.Service.Facts {
			fa := &s.Service.Facts[i]
			if fa.Kind == "exists" && contains(FactFilterFields(fa.Filters), value) {
				near = fa
				break
			}
		}
	}
	if near == nil {
		ps.BlockerFix(where+".unique.enforce",
			fmt.Sprintf("the precheck needs a domain service with an exists fact "+
				"filtered by %s, and this spec has none — only the constraint would "+
				"be generated", list),
			fmt.Sprintf("declare service with a fact {kind: exists, filters: [%s], "+
				"excludeSelf: true}, or use enforce: constraint-only", list))
		return
	}
	got, plain := PlainEqFilters(near.Filters)
	if !plain {
		// The columns may well be the right ones; the QUESTION is not. A unique
		// index answers "is this exact tuple present", so the pre-check that
		// stands in front of it has to ask exactly that — one equality per
		// column, each carrying a value the write is holding.
		ps.BlockerFix(where+".unique.enforce",
			fmt.Sprintf("the precheck %q narrows by more than equality, and the unique "+
				"index it stands in front of only knows how to answer \"is this exact "+
				"tuple present\" — the domain and the database would be asking two "+
				"different questions and reporting one under the other's notification",
				near.Name),
			fmt.Sprintf("give that fact plain filters: [%s] — an operator, an OR or a "+
				"pinned value belongs to a fact of its own, which a rule reads", list))
		return
	}
	var extra, missing []string
	for _, x := range got {
		if !contains(want, x) {
			extra = append(extra, x)
		}
	}
	for _, x := range want {
		if !contains(got, x) {
			missing = append(missing, x)
		}
	}
	if len(extra) > 0 {
		// The reported shape: filters [TenantID, Key] beside an index on
		// role_key alone. The fact is right and the declaration is missing.
		ps.BlockerFix(where+".unique",
			fmt.Sprintf("the precheck %q narrows by %s, and the unique index would not "+
				"cover %s — the domain would accept a value the database then refuses, "+
				"reported as though the value were taken",
				near.Name, strings.Join(got, ", "), strings.Join(extra, ", ")),
			fmt.Sprintf("say what the uniqueness is scoped by: within: [%s]",
				strings.Join(extra, ", ")))
		return
	}
	ps.BlockerFix(where+".unique.within",
		fmt.Sprintf("the uniqueness is scoped by %s and the precheck %q does not narrow "+
			"by %s, so it would answer about the whole table",
			strings.Join(f.Unique.Within, ", "), near.Name, strings.Join(missing, ", ")),
		fmt.Sprintf("give that fact filters: [%s]", list))
}

// validateChildUnique checks a uniqueness declared on a COLLECTION ENTRY.
//
// It used to be refused outright, and the refusal cost more than it saved.
// businessIdentity — the alternative it pointed at — is an in-process check over
// what one write carries, so it cannot see a CONCURRENT write: two requests each
// adding the same entry both pass it, and both rows land. The only backstop is
// an index, and with the key refused the author wrote that index by hand — into
// a migration the generator would then describe incorrectly, and with no way to
// register a binding for it, so the violation surfaced as a raw 500 where the
// root's equivalent is a clean 409.
//
// Two things are fixed here rather than declared, because a collection entry has
// no identity outside its owner:
//
//   - the index is ALWAYS scoped by the parent column, so "unique" means unique
//     within this aggregate — which is what businessIdentity means too, and this
//     is its backstop;
//   - the enforcement is constraint-only, because a per-entry pre-check would be
//     an exists query over the collection's own table and this build writes no
//     such query.
func validateChildUnique(s *Spec, f Field, where string, ps *Problems) {
	coll := childOwningColumn(s, f.Column)
	if f.Unique.Enforce != "" && f.Unique.Enforce != "constraint-only" {
		ps.BlockerFix(where+".unique.enforce",
			fmt.Sprintf("%q needs a service fact asking about the collection's own "+
				"table, and this build writes no such query", f.Unique.Enforce),
			"use enforce: constraint-only — the index refuses the duplicate and the "+
				"repository reports it as the notification named here; add a "+
				"businessIdentity (and a childDuplicate rule) if you also want the "+
				"duplicate caught inside ONE write, where the index cannot help")
	}
	if f.Unique.Notification == "" {
		ps.BlockerFix(where+".unique",
			"the constraint needs the conflict answer a duplicate raises",
			"name a notification declared under notifications, with semantic: conflict")
	}
	// active-only on an entry is defined by the ENTRY's archive column, not the
	// root's: a soft-removed entry is what frees the value, and a collection
	// that is not soft-removable has no such row to skip.
	if f.Unique.Scope == "active-only" && (coll == nil || coll.ArchivedAt == "") {
		ps.BlockerFix(where+".unique.scope",
			"active-only scopes the uniqueness to the entries that are not archived, "+
				"and this collection archives none",
			"set softRemove: true with an archivedAt column on the collection, or use "+
				"scope: all")
	}
	if f.Type == "bool" {
		ps.BlockerFix(where+".unique",
			"a unique flag would allow at most one true and one false entry per owner",
			"uniqueness is for identifying values; drop the key")
	}
	if f.Nullable {
		ps.BlockerFix(where+".unique",
			"NULLs do not collide, so a nullable entry field is unique for free",
			"make the field non-nullable, or drop the key")
	}
	for i, name := range f.Unique.Within {
		at := fmt.Sprintf("%s.unique.within[%d]", where, i)
		if coll == nil {
			break
		}
		scope := findField(coll.Fields, name)
		switch {
		case name == f.Name:
			ps.BlockerFix(at, "the field cannot be scoped by itself",
				"within names the OTHER fields of the entry the uniqueness is per — drop it")
		case scope == nil:
			ps.BlockerFix(at,
				fmt.Sprintf("%q does not name a field of the collection %q", name, coll.Plural),
				"an entry's uniqueness is scoped by other fields OF THE SAME ENTRY; the "+
					"owner is already part of the index and needs no declaring")
		case scope.Nullable:
			ps.BlockerFix(at,
				fmt.Sprintf("%q is nullable, and NULLs do not collide", name),
				"scope by a non-nullable field of the entry, or drop the scope")
		}
	}
}

// childOwningColumn finds the collection a child field belongs to, by the one
// thing that is unique across a spec's tables: its column, on its own table.
func childOwningColumn(s *Spec, column string) *Child {
	for i := range s.Children {
		for _, f := range s.Children[i].Fields {
			if f.Column == column {
				return &s.Children[i]
			}
		}
	}
	return nil
}

// validateUniqueWithin checks the scope columns themselves. Each has to be a
// column the index can name, and one that cannot be NULL — NULLs do not collide,
// so a nullable scope column scopes nothing and the uniqueness it promises is
// not the one the database enforces.
func validateUniqueWithin(s *Spec, f Field, where string, ps *Problems) {
	seen := map[string]bool{}
	for i, name := range f.Unique.Within {
		at := fmt.Sprintf("%s.unique.within[%d]", where, i)
		switch {
		case name == f.Name:
			ps.BlockerFix(at,
				"the field cannot be scoped by itself",
				"within names the OTHER fields the uniqueness is per — drop it")
			continue
		case seen[name]:
			ps.Blockerf(at, "%q is named twice", name)
			continue
		}
		seen[name] = true
		scope := findField(s.Fields, name)
		if scope == nil {
			scope = findLogicalField(s.Fields, s, name)
		}
		switch {
		case scope == nil:
			reportUnknownFactField(s, name, at, ps)
		case scope.Runtime:
			ps.BlockerFix(at,
				fmt.Sprintf("%q is runtime-only, so it has no column for the index to "+
					"cover", name),
				"scope by a persisted field — a claim that is never stored cannot "+
					"narrow a constraint")
		case scope.Nullable:
			ps.BlockerFix(at,
				fmt.Sprintf("%q is nullable, and NULLs do not collide — rows without a "+
					"value would each be unique", name),
				"scope by a non-nullable field, or drop the scope")
		case scope.LivesOn == "base":
			ps.BlockerFix(at,
				fmt.Sprintf("%q lives on the base table, and the index is created on "+
					"the role's", name),
				"scope by a field of this table")
		}
	}
}

// sortedTransitionKeys keeps the refusal order stable run to run.
func sortedTransitionKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// IdentityName is the logical name the aggregate id answers to. The framework's
// managed carrier resolves it on the root's schema (TableSchema.ID) and the
// projector writes it as the view's _id, so a listing may narrow and order by it
// without anything declaring it.
const IdentityName = "ID"

// identityRead renders the aggregate id as the field a QUERY needs: a listing's
// filter or sort, and a fact's filter.
//
// It is deliberately NOT part of the readable set. The id is not projected
// through the field pipeline — every response carries it because the framework
// puts it there — so the three keys that address a PROJECTED column (indexes,
// fieldRestrict, computed.from) have nothing here to address: an index would be
// declared over `id` while the document's key is `_id`, a restriction would be
// asked to scrub the handle the response is required to carry, and a derivation
// would read a value the reader never selects. What the STORE answers is what
// resolves here, and the framework maps the name itself on both of its sides:
// the view reader's own Eq("ID", …), and the aggregate loader's, whose ID slot
// is typed as an identity on every schema so a probe binds in the dialect's
// native id form rather than as text that matches nothing on three engines.
func identityRead(name string) *Field {
	if name != IdentityName {
		return nil
	}
	// No Length: an id column is sized by the engine's own id type, and no
	// Nullable: a row without one does not exist.
	return &Field{Name: IdentityName, Type: "id", Column: "id", LivesOn: "root"}
}

// managedRead reports whether the read side declared this framework-stamped
// column. It is the gate for everything else: a name not listed under
// read.managed is not readable, not filterable and not exportable, exactly as if
// the column did not exist.
func managedRead(s *Spec, name string) bool {
	for _, n := range s.Read.Managed {
		if n == name {
			return true
		}
	}
	return false
}

// ManagedColumn answers the physical column behind a managed logical name.
func ManagedColumn(s *Spec, name string) string {
	switch name {
	case "CreatedAt":
		return s.Storage.Managed.CreatedAt
	case "UpdatedAt":
		return s.Storage.Managed.UpdatedAt
	case "DeletedAt":
		return s.Storage.Managed.ArchivedAt
	}
	return ""
}

// readableAsRootSource is the set a ROOT derivation may read from, and it is
// deliberately narrower than readableField.
//
// The difference is the two dotted forms — a collection's own field and a
// collection's joined field — plus a facet attached to a collection. All three
// are readable, and none of them is on the root's Result: what the root holds
// for a collection is a SLICE of entries, so there is no single value to hand a
// derivation that runs once per document. Blessing them here is what let a spec
// validate green and then generate a signature one parameter short, with the
// field empty forever.
//
// Per-entry derivation is a real question and it has its own seat:
// children[].computed, which runs once per entry, where the entry IS in hand.
// collectionFieldNamed answers whether a name is the Go FIELD a collection
// occupies on the read shape — its `plural`, and only that.
func collectionFieldNamed(s *Spec, name string) *Child {
	for i := range s.Children {
		if s.Children[i].Plural == name {
			return &s.Children[i]
		}
	}
	return nil
}

func readableAsRootSource(s *Spec, jr joinReach, name string) bool {
	if fieldNamedIn(jr.root, name) != nil {
		return true
	}
	if managedRead(s, name) {
		return true
	}
	if findLogicalField(s.Fields, s, name) != nil {
		return true
	}
	for i := range s.Siblings {
		// A facet of a COLLECTION entry is folded into that entry, not into the
		// root — the same boundary the dotted forms above run into.
		if strings.HasPrefix(s.Siblings[i].AttachTo, "child:") {
			continue
		}
		if findLogicalField(s.Siblings[i].Fields, s, name) != nil {
			return true
		}
	}
	return false
}

// collectionScopedSource names the collection a source lives inside, or "" when
// the name has nothing to do with one.
//
// It is what turns a flat refusal into a direction. All three spellings mean the
// same thing — the value is on an ENTRY — and the author who wrote one of them
// was asking for a per-entry derivation, which is a key that exists.
func collectionScopedSource(s *Spec, jr joinReach, name string) *Child {
	if head, tail, dotted := strings.Cut(name, "."); dotted {
		c := CollectionNamed(s.Children, head)
		if c == nil {
			return nil
		}
		if fieldNamedIn(jr.child[c.Name], tail) != nil || findLogicalField(c.Fields, s, tail) != nil {
			return c
		}
		return nil
	}
	// The bare spelling: the author named the entry's field with no collection
	// in front of it, which resolves against the root and finds nothing.
	for i := range s.Children {
		if findLogicalField(s.Children[i].Fields, s, name) != nil {
			return &s.Children[i]
		}
	}
	for i := range s.Siblings {
		attached, isChildFacet := strings.CutPrefix(s.Siblings[i].AttachTo, "child:")
		if !isChildFacet {
			continue
		}
		if findLogicalField(s.Siblings[i].Fields, s, name) != nil {
			return CollectionNamed(s.Children, attached)
		}
	}
	return nil
}

func readableField(s *Spec, jr joinReach, name string) bool {
	if fieldNamedIn(jr.root, name) != nil {
		return true
	}
	if i := strings.Index(name, "."); i > 0 {
		if fieldNamedIn(jr.child[canonicalChildName(s, name[:i])], name[i+1:]) != nil {
			return true
		}
	}
	if managedRead(s, name) {
		return true
	}
	// The LOGICAL set, not the declared one: a composite's parts are what the
	// read side can name, and the composite itself is not — it has no single
	// value to project, sort or restrict.
	if findLogicalField(s.Fields, s, name) != nil {
		return true
	}
	for i := range s.Siblings {
		if findLogicalField(s.Siblings[i].Fields, s, name) != nil {
			return true
		}
	}
	if i := strings.Index(name, "."); i > 0 {
		if c := findChild(s.Children, name[:i]); c != nil {
			return findLogicalField(c.Fields, s, name[i+1:]) != nil
		}
	}
	return false
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func hasTextIndex(idx []Index) bool {
	for _, i := range idx {
		if i.Text {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func orUnnamed(s string) string {
	if s == "" {
		return "unnamed"
	}
	return s
}

// validateMountedChild checks a collection this role EXPOSES but does not own.
//
// The role that declared the shared identity also declared its collections, and
// this spec restates the shape so its own DTOs, projection and routes can be
// built from it. Two statements of one table can disagree, and the disagreement
// is not loud: both specs generate, both compile, and the second role's
// projection reads a document key the first never writes. So the declaration is
// found and compared, field by field.
//
// A missing declaration is refused outright rather than assumed: generating the
// surface for a table nobody creates produces routes that 500 on first use.
func validateMountedChild(s *Spec, c Child, where string, ps *Problems, opt Options) {
	var owner *Neighbour
	var declared *NeighbourChild
	for i := range opt.Neighbours {
		if isSelfNeighbour(s, opt.Neighbours[i]) {
			continue // this very file, not a neighbour
		}
		for j := range opt.Neighbours[i].Children {
			if opt.Neighbours[i].Children[j].Table == c.Table {
				owner, declared = &opt.Neighbours[i], &opt.Neighbours[i].Children[j]
				break
			}
		}
	}
	if declared == nil {
		ps.BlockerFix(where,
			fmt.Sprintf("no spec in this project declares the collection stored in %q", c.Table),
			"this role reuses an existing identity, so it can EXPOSE one of that "+
				"identity's collections but cannot create one — declare it in the spec "+
				"that owns the base, or make it a role collection (ownedBy: role)")
		return
	}
	if declared.OwnedBy != "base" {
		ps.BlockerFix(where+".ownedBy",
			fmt.Sprintf("%s declares that collection as %q, so it belongs to that role and not to the identity",
				owner.Entity, declared.OwnedBy),
			"a collection only two roles can share is one the IDENTITY owns — change "+
				"it there first, or give this role a collection of its own")
	}
	if declared.Name != c.Name {
		ps.BlockerFix(where+".name",
			fmt.Sprintf("%s calls the same collection %q", owner.Entity, declared.Name),
			"use the same name: the entry type is declared once, by that spec, and "+
				"this one refers to it")
	}
	byName := map[string]NeighbourField{}
	for _, f := range declared.Fields {
		byName[f.Name] = f
	}
	for i, f := range c.Fields {
		w := fmt.Sprintf("%s.fields[%d] (%s)", where, i, orUnnamed(f.Name))
		d, ok := byName[f.Name]
		if !ok {
			ps.BlockerFix(w,
				fmt.Sprintf("%s does not declare this field on the collection it owns", owner.Entity),
				"the table is created there, so a field only this spec knows about is a "+
					"column that does not exist — add it to "+owner.Path+" first")
			continue
		}
		if d.Column != f.Column || d.Type != f.Type {
			ps.BlockerFix(w,
				fmt.Sprintf("%s declares it as %s %s, this spec as %s %s",
					owner.Entity, d.Column, d.Type, f.Column, f.Type),
				"one table, one shape — the two specs have to agree, and "+owner.Path+
					" is the one that creates it")
		}
		delete(byName, f.Name)
	}
	if len(byName) > 0 {
		var missing []string
		for name := range byName {
			missing = append(missing, name)
		}
		sort.Strings(missing)
		ps.BlockerFix(where+".fields",
			fmt.Sprintf("%s declares fields this spec does not restate: %s",
				owner.Entity, strings.Join(missing, ", ")),
			"the collection is projected as ONE document segment, so a field left out "+
				"here is a field this role's readers never see — restate all of them")
	}
}

// hasInsertManualRule reports whether the spec names at least one hand-written
// rule that runs on insert — the only place a `derived` field can be computed
// inside the generated tree.
//
// It is deliberately coarse: it asks whether SOMETHING claims the insert path,
// not whether that something assigns this particular field. The generator
// cannot read the rule it did not write, and a check that pretends to would be
// a false green.
func hasInsertManualRule(s *Spec) bool {
	for _, mr := range s.Rules.Manual {
		if len(mr.Scope) == 0 {
			return true // no scope declared means every verb, insert included
		}
		for _, sc := range mr.Scope {
			if sc == "insert" || sc == "insertOrUpdate" {
				return true
			}
		}
	}
	return false
}

// declaredHere reports whether a field's vo.kind names a value object this spec
// must declare under valueObjects.
//
// `reuse` is the one that resolves elsewhere — against the project's existing
// types. `manual` resolves HERE even though the generator writes no code for
// it: the type does not exist yet, so the declaration is the only place that
// says what it will be, and the report reads it to ask for it.
func declaredHere(kind string) bool {
	return kind == "raw" || kind == "enum" || kind == "manual"
}

// validateStamped holds a framework-owned column to the shape the framework
// declares for it.
//
// Everything here is a BLOCKER rather than a warning, because every mistake in
// this pair compiles. A `stamped: time` on a non-nullable column emits a
// StampedTimeField over a time.Time and the framework panics at boot, which is
// not a thing a reviewer sees by reading the spec. A counter is the one place
// where BOTH nullabilities are honest declarations rather than a mistake: int64
// counts, *int64 counts and can also say it has no count — see the case below.
func validateStamped(s *Spec, f Field, where string, ps *Problems, isChild, isFacet bool) {
	if f.Stamped == "" {
		return
	}
	if !StampedKinds.Has(f.Stamped) {
		ps.BlockerFix(where+".stamped",
			fmt.Sprintf("%q is not a kind of stamp", f.Stamped),
			"one of: "+StampedKinds.String())
		return
	}
	// The two refusals the framework itself raises, moved to load time. A
	// sibling carries no framework-owned columns of its own, and a runtime field
	// has no column at all.
	if isFacet {
		ps.BlockerFix(where+".stamped",
			"a facet row is a 1:1 slice of the OWNER's row and carries no framework-owned "+
				"columns of its own — the framework refuses the declaration",
			"move the stamped column to the owner (a root field, or one under a shared "+
				"identity)")
		return
	}
	if isChild {
		ps.BlockerFix(where+".stamped",
			"this build does not lower a stamped column on a collection entry",
			"the framework stamps an aggregate child exactly as it stamps the root, but "+
				"an entry's fields go into its input DTO whole and there is no per-field "+
				"\"the server owns this one\" narrowing there yet — date the fact on the "+
				"ROOT, or take the collection's write path by hand")
		return
	}
	if f.Runtime {
		ps.BlockerFix(where+".stamped",
			"a runtime-only field has no column, and a stamp is a value written into one",
			"drop runtime: true to persist the fact, or drop stamped — nothing the "+
				"framework owns can live on a field it never writes")
		return
	}
	if strings.HasPrefix(f.LivesOn, "sibling:") {
		ps.BlockerFix(where+".stamped",
			"a facet row is a 1:1 slice of the OWNER's row and carries no framework-owned "+
				"columns of its own — the framework refuses the declaration",
			"move the stamped column to the owner (livesOn: root, or base/role under a "+
				"shared identity)")
		return
	}
	switch f.Stamped {
	case "time":
		if f.Type != "time" {
			ps.BlockerFix(where+".type",
				fmt.Sprintf("a stamped time is a timestamp, and %q is not", f.Type),
				"set type: time — or, if what you want is a count of events on this row, "+
					"stamped: counter with type: int64")
		}
		if !f.Nullable {
			ps.BlockerFix(where+".nullable",
				"until something stamps it the fact has not happened, and a non-nullable "+
					"timestamp reports year 1 instead of saying so",
				"set nullable: true — the framework declares the Go field as *time.Time "+
					"and refuses anything else")
		}
	case "counter":
		if f.Type != "int64" {
			ps.BlockerFix(where+".type",
				fmt.Sprintf("a stamped counter is an int64, and %q is not", f.Type),
				"set type: int64 — the counter is per ROW, not a table-wide sequence, so "+
					"there is no id type behind it")
		}
		// nullable is ALLOWED here, and it is the one stamped shape whose
		// nullability is not about the value being unknown. A plain int64
		// counter has no absence to write, so StampNull is refused on it by the
		// write; declaring the column nullable emits the counter over *int64,
		// which is the only shape that verb can land in. The increment is the
		// server's either way, so the pointer costs nothing else.
	}
	// The four keys that say something a framework-owned value cannot be.
	if f.AssignedFrom != "" {
		ps.BlockerFix(where+".assignedFrom",
			"assignedFrom says where the SERVER READS the value; a stamped column has no "+
				"source to read — the framework mints it",
			"drop one of the two. If the value comes from the caller's token or from the "+
				"entity's own fields, it is assignedFrom and not a stamp")
	}
	if f.VO != nil && f.VO.Kind != "" && f.VO.Kind != "none" {
		ps.BlockerFix(where+".vo",
			"a value object validates a value the domain supplies, and nothing supplies "+
				"this one — the column is never written from the struct",
			"drop vo")
	}
	if f.Unique != nil {
		ps.BlockerFix(where+".unique",
			"a business key is a value someone states and the write is refused over; a "+
				"stamped column is minted by the framework after that decision is made",
			"drop unique — to order or window rows by the stamp, that is the read side's "+
				"filters and indexes")
	}
	if f.Redact != nil {
		ps.BlockerFix(where+".redact",
			"redact keeps a real value in the column and masks the copies; a stamp is the "+
				"framework's own instant and there is nothing about it to hide",
			"drop redact")
	}
	if f.BypassMaySet {
		ps.BlockerFix(where+".bypassMaySet",
			"bypassMaySet lets a caller STATE a value the server would otherwise read off "+
				"their identity, and no caller states a stamp",
			"drop bypassMaySet")
	}
	// Same shape as the derived warning next door, and the same failure: a
	// column nothing ever fills, silently. The rule DSL validates and does not
	// mutate, so the Stamp call can only come from a hand-written rule.
	if len(s.Rules.Manual) == 0 {
		ps.WarnFix(where+".stamped",
			"nothing in this spec asks for this stamp",
			fmt.Sprintf("the request is e.Stamp(%q) inside a rules.manual entry you write "+
				"— without one the column is never written and no error says so", f.Name))
	}
}
