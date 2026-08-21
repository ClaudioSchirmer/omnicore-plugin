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
	validateRules(s, ps)
	validateNotifications(s, ps, opt)
	validateService(s, ps)
	validateRead(s, ps)
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

// reservedFieldNames are owned by the framework's managed carrier. Declaring one
// as a mapped field is a boot panic; the ParentID case additionally silently
// overwrites the framework's own value.
var reservedFieldNames = map[string]string{
	"ID":       "the aggregate id comes from the framework's managed carrier",
	"ParentID": "the parent link is projected automatically as the read-only twin of ID",
	"Revision": "the revision column is declared under storage.managed, not as a field",
}

// isFacet distinguishes the two things isChild lumps together: a COLLECTION
// entry, whose uniqueness this build now generates, and a 1:1 FACET's field,
// whose uniqueness it still does not.
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
			"a runtime-only field is in no response to begin with — it is fed from the "+
				"caller's token and exists for the rules to read",
			"drop hidden; it takes a PERSISTED field out of the responses while leaving "+
				"the column, the filters and the writes alone")
	}

	if f.Runtime {
		// A collection entry (or a facet's row) has no request identity of its
		// own; the lowering kept the field anyway and the migration emitted a
		// column with an EMPTY name.
		if isChild {
			ps.BlockerFix(where,
				"a runtime-only field belongs to the entity, not to a collection or facet",
				"declare it at the root; an entry is validated in the entity's context "+
					"and reads the entity's runtime fields from there")
			return
		}
		if f.Column != "" {
			ps.BlockerFix(where,
				"a runtime-only field cannot have a column",
				"runtime fields are fed from the request identity and never persisted")
		}
		if f.Unique != nil {
			ps.Blockerf(where, "a runtime-only field cannot be unique — it is never stored")
		}
		if f.Claim == "" {
			ps.BlockerFix(where+".claim",
				"a runtime-only field does not say which claim it comes from",
				"name it, e.g. claim: email — the framework does not opine on custom "+
					"claim names, so there is no convention to fall back on")
		}
		if f.Type != "string" && f.Type != "bool" {
			ps.BlockerFix(where+".type",
				"a runtime-only field is read from a token claim, so it is text or a flag",
				"set type: string, or type: bool for a yes/no claim")
		}
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
		if f.Nullable {
			ps.BlockerFix(where+".nullable",
				"a server-assigned field is always written, so it is never null",
				"drop nullable — a caller who cannot supply it is a matter for the "+
					"permission, not for the column")
		}
		// `derived` says the server owns the value; nothing in this build can
		// know HOW it is computed, so the one thing that CAN be checked is that
		// somewhere claims to compute it. With no manual rule the field is
		// simply never written, and the column holds the zero value forever —
		// silently, which is the failure shape this generator refuses to ship.
		if f.AssignedFrom == "derived" && !isChild && !hasInsertManualRule(s) {
			ps.WarnFix(where+".assignedFrom",
				"nothing in this spec computes this field",
				"a derived field is filled by a rules.manual entry scoped to insert — "+
					"declare it, or the column keeps its zero value and no error says so")
		}
	}

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

func validateChildren(s *Spec, ps *Problems, opt Options) {
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
		if MountsPerChildOp(c, "change") && len(c.Fields) > 0 &&
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
		if MountsPerChildOp(c, "add") && c.SoftRemove && c.DuplicateNotification == "" {
			fix := "the update path can edit one entry into another's identity; naming a " +
				"notification makes the rejection specific"
			if !MountsPerChildOp(c, "change") {
				fix = "an add can name an entry the collection already holds; naming a " +
					"notification makes the rejection specific"
			}
			ps.WarnFix(where+".duplicateNotification",
				"no duplicate notification for a per-child collection", fix)
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
		}
	}
	// A patch with nothing left to patch accepts a body and changes nothing —
	// and it used to panic the generator instead of being refused here.
	if s.Update.Shape == "patch" || s.Update.Shape == "both" {
		patchable := 0
		for _, f := range s.Fields {
			if f.Runtime || f.AssignedFrom != "" || contains(s.Update.PatchExcludes, f.Name) {
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

func validateRules(s *Spec, ps *Problems) {
	validateRuleSet(s, s.Rules, ruleScopeOfRoot(s), "rules", ps)
	for i, c := range s.Children {
		where := fmt.Sprintf("children[%d].rules", i)
		validateRuleSet(s, c.Rules, ruleScopeOfChild(s, c), where, ps)
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
			// The child's NAME, not its plural: the root-level shape check (and
			// the emitter) resolve the collection by name, so a fix that spelled
			// the plural sent the author straight into a second refusal.
			fix = fmt.Sprintf(fix, orUnnamed(c.Name))
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
					"so presence is already enforced")
		case vo.Kind == "enum":
			ps.WarnFix(where+".fields",
				fmt.Sprintf("%s is backed by the enum %s, and an empty value is already "+
					"answered with %s — this rule adds a SECOND notification for the same "+
					"empty field", name, vo.Name, orUnnamed(vo.UnknownNotification)),
				"drop this rule: enum membership is validated automatically on every write")
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

// ruleScopeOfRoot is every field a rule on the entity can talk about.
//
// A 1:1 facet is a STORAGE decision — the same Go struct, split across two
// tables so the columns can be null in bulk. Its fields are fields of the
// entity, reachable on the same receiver, and refusing a rule on one of them
// pushed invariants the DSL can perfectly express into the hand-written escape.
func ruleScopeOfRoot(s *Spec) []Field {
	out := append([]Field{}, s.Fields...)
	for _, sib := range s.Siblings {
		if !strings.HasPrefix(sib.AttachTo, "child:") {
			out = append(out, sib.Fields...)
		}
	}
	return out
}

// ruleScopeOfChild is the same for a collection: its own fields plus the fields
// of a facet declared inside it, which land on the child's type.
func ruleScopeOfChild(s *Spec, c Child) []Field {
	out := append([]Field{}, c.Fields...)
	for _, sib := range s.Siblings {
		if sib.AttachTo == "child:"+c.Name {
			out = append(out, sib.Fields...)
		}
	}
	return out
}

func validateRuleSet(s *Spec, rs Rules, scopeFields []Field, where string, ps *Problems) {
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
		validateRuleShape(s, r, scopeFields, w, ps)
		validateRequiredOverValueObject(s, r, scopeFields, w, ps)
	}

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
func validateFactRange(s *Spec, r Rule, scopeFields []Field, w string, ps *Problems) {
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
	var found *Fact
	for i := range s.Service.Facts {
		if s.Service.Facts[i].Name == r.Fact {
			found = &s.Service.Facts[i]
			break
		}
	}
	if found == nil {
		ps.Blockerf(w+".fact", "%q does not name a fact of this entity's service", r.Fact)
		return
	}
	if found.Kind == "exists" {
		ps.BlockerFix(w+".fact",
			"exists answers yes or no, and a range has nothing to compare it against",
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
	for _, fl := range append(append([]string{}, found.Filters...), found.Field) {
		if fl == "" {
			continue
		}
		owner := compositePartOwner(s.Fields, fl)
		if owner == nil || !owner.Nullable {
			continue
		}
		ps.BlockerFix(w+".fact",
			fmt.Sprintf("%s filters on %q, a part of the OPTIONAL composite value object "+
				"%q — when it is absent there is no value for this rule to pass", r.Fact, fl, owner.Name),
			"call the fact from rules.manual, where the absent case is a branch you "+
				"write — or make the value object mandatory")
	}
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

func validateRuleShape(s *Spec, r Rule, scopeFields []Field, w string, ps *Problems) {
	switch r.Kind {
	case "factRange":
		validateFactRange(s, r, scopeFields, w, ps)
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

func validateService(s *Spec, ps *Problems) {
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
		if !FactKinds.Has(f.Kind) {
			ps.BlockerFix(where+".kind",
				fmt.Sprintf("%q is not a fact kind", f.Kind),
				"one of: "+FactKinds.String())
		}
		if f.Kind == "manual" {
			validateManualFact(f, where, ps)
			validateFactFilters(s, f, where, ps)
			continue
		}
		if f.Returns != "" {
			ps.BlockerFix(where+".returns",
				fmt.Sprintf("a %s fact already determines what it returns", f.Kind),
				"returns is only declared for a manual fact, where the generator has "+
					"no way to infer the signature")
		}
		if f.Kind != "exists" && f.Kind != "count" && f.Field == "" {
			ps.BlockerFix(where+".field",
				fmt.Sprintf("%s needs the field it aggregates", f.Kind),
				"set field: <field>")
		}
		validateFactFilters(s, f, where, ps)
		if f.Field != "" && factField(s, f.Field) == nil {
			reportUnknownFactField(s, f.Field, where+".field", ps)
		}
		validateAggregatedField(s, f, where, ps)
		validateGroupedFact(s, f, where, ps)
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
	switch f.Kind {
	case "count", "sum", "avg", "min", "max":
	case "exists":
		ps.BlockerFix(where+".groupBy",
			"exists answers yes or no, so there is nothing to report per group",
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
	if f.Field == "" || f.Kind == "exists" || f.Kind == "count" || f.Kind == "manual" {
		return
	}
	fld := factField(s, f.Field)
	if fld == nil {
		return // already reported as an unknown field
	}
	switch fld.Type {
	case "int", "int64", "float64":
		return
	}
	ps.BlockerFix(where+".field",
		fmt.Sprintf("%s cannot aggregate %s, which is %s", f.Kind, f.Field, fld.Type),
		"aggregate a numeric field (int, int64, float64) — the database computes "+
			"these and the framework carries the answer as an exact integer or a float; "+
			"for anything else, make it a manual fact and write the query you mean")
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
func validateFactFilters(s *Spec, f Fact, where string, ps *Problems) {
	// excludeSelf appends its own parameter, so it takes part in the collision
	// check rather than colliding with a field named SelfID after the fact.
	params := map[string]string{}
	if f.ExcludeSelf {
		params["selfID"] = "excludeSelf"
	}
	for i, fl := range f.Filters {
		at := fmt.Sprintf("%s.filters[%d]", where, i)
		if strings.TrimSpace(fl) == "" {
			ps.BlockerFix(at, "the filter is empty", "name a field, or drop the entry")
			continue
		}
		var resolved *Field
		switch coll, fld, dotted := ChildFactField(s, fl); {
		case dotted && (coll == nil || fld == nil):
			reportUnknownFactField(s, fl, at, ps)
			continue
		case dotted && f.Kind != "manual":
			// A computed fact IS a query this generator writes, and it writes it
			// against the entity's own table. The collection's field is on
			// another one, so there is no criteria this build could emit — and
			// inventing a join here would be a query shape nothing else in the
			// language can express or index.
			ps.BlockerFix(at,
				fmt.Sprintf("a %s fact is a query over this entity's own table, and %q "+
					"is a column of the collection's table", f.Kind, fl),
				"ask it as kind: manual, whose body you write — or filter by a root "+
					"field, which the generated query can reach")
			continue
		case dotted:
			resolved = fld
		default:
			if resolved = factField(s, fl); resolved == nil {
				reportUnknownFactField(s, fl, at, ps)
				continue
			}
		}
		name := naming.Camel(resolved.Name)
		if prev, clash := params[name]; clash {
			ps.BlockerFix(at,
				fmt.Sprintf("%s and %s both reach the method as the parameter %q",
					prev, fl, name),
				"two parameters of one name do not compile; drop one, or ask the two "+
					"questions as two facts")
			continue
		}
		params[name] = fl
	}
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

func validateRead(s *Spec, ps *Problems) {
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
	}
	if r.View.Version < 1 {
		ps.BlockerFix("read.view.version",
			"the view version must start at 1",
			"the framework uses it to decide when a rebuild is due; 0 is not a version")
	}

	if r.Backing == "relational" {
		// A relational view has no Mongo collection, so these are not merely
		// useless — they would be silently discarded, which is worse.
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
	}

	for i, idx := range r.Indexes {
		where := fmt.Sprintf("read.indexes[%d]", i)
		if len(idx.Fields) == 0 {
			ps.Blockerf(where+".fields", "an index needs at least one field")
		}
		for _, fn := range idx.Fields {
			if !readableField(s, fn) {
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
		validateByParams(s, r, ps)
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
		if !readableField(s, fr.Field) {
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

	validateComputed(s, r, ps)

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
func validateComputed(s *Spec, r Read, ps *Problems) {
	if len(r.Computed) == 0 {
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
	for i, c := range r.Computed {
		where := fmt.Sprintf("read.computed[%d] (%s)", i, orUnnamed(c.Name))
		switch {
		case c.Name == "":
			ps.Blockerf(where, "the computed field needs a name")
		case !goIdentRe.MatchString(c.Name):
			ps.BlockerFix(where,
				fmt.Sprintf("%q is not a usable Go field name", c.Name),
				"use exported PascalCase, e.g. DisplayName")
		case readableField(s, c.Name):
			ps.BlockerFix(where,
				fmt.Sprintf("%q is already a field of this entity", c.Name),
				"a computed field is one the STORE does not hold — rename it, or drop "+
					"the declaration and read the stored field")
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
			if !readableField(s, src) {
				reportUnreadable(s, src, where+".from", ps)
			}
		}
	}
	// A filter and a sort are both evaluated in the store. Declaring either
	// over a computed field is refused HERE rather than at the framework's boot
	// guard, so the author reads it against the spec they wrote.
	if bp := r.ByParams; bp != nil {
		for i, f := range bp.Filters {
			if seen[f.Field] {
				ps.BlockerFix(fmt.Sprintf("read.byParams.filters[%d]", i),
					fmt.Sprintf("%q is computed — a filter is evaluated in the store, and "+
						"there is no column there to compare", f.Field),
					"filter on the stored fields it is derived from")
			}
		}
		for _, sf := range bp.Sort {
			if seen[sf] {
				ps.BlockerFix("read.byParams.sort",
					fmt.Sprintf("%q is computed, so it backs no column to order by — "+
						"ordering happens in the store and the keyset cursor is built from "+
						"stored values", sf),
					"sort on the stored fields it is derived from")
			}
		}
		for _, sf := range bp.Controls.Search {
			if seen[sf] {
				ps.BlockerFix("read.byParams.controls.search",
					fmt.Sprintf("%q is computed, so no index covers it", sf),
					"search the stored fields it is derived from")
			}
		}
	}
	for i, idx := range r.Indexes {
		for _, fn := range idx.Fields {
			if seen[fn] {
				ps.BlockerFix(fmt.Sprintf("read.indexes[%d]", i),
					fmt.Sprintf("%q is computed — there is no stored value to index", fn),
					"index the stored fields it is derived from")
			}
		}
	}
	for i, fr := range r.FieldRestrict {
		if seen[fr.Field] {
			ps.BlockerFix(fmt.Sprintf("read.fieldRestrict[%d]", i),
				fmt.Sprintf("%q is computed — Restrict scrubs a COLUMN from the "+
					"projection, sort and filter, and there is none", fr.Field),
				"restrict the stored fields it is derived from; the derivation then "+
					"receives them absent")
		}
	}
}

func validateByParams(s *Spec, r Read, ps *Problems) {
	bp := r.ByParams
	for i, f := range bp.Filters {
		where := fmt.Sprintf("read.byParams.filters[%d] (%s)", i, orUnnamed(f.Field))
		// The lowering resolves a filter against the root's own fields (and its
		// root-attached facets) — nothing else. Validation used to bless the
		// wider set readableField accepts, and every spelling in the gap
		// (a collection's field, dotted or not) validated green and was then
		// silently dropped from the generated request type.
		fld := filterableField(s, f.Field)
		if fld == nil {
			if readableField(s, f.Field) {
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
		if filterableField(s, sf) != nil {
			continue
		}
		if readableField(s, sf) {
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
		if !readableField(s, sf) {
			reportUnreadable(s, sf, "read.byParams.controls.search", ps)
			continue
		}
		if f := filterableField(s, sf); f != nil && f.Type != "string" {
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
	if !su.REST && !gqlOn {
		ps.BlockerFix("surfaces",
			"the entity exposes no surface",
			"set surfaces.rest: true and/or surfaces.graphql.enabled: true")
	}
	if gqlOn {
		for _, m := range su.GraphQL.Mutations {
			if !Modes.Has(m) || m == "display" {
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
		if su.GraphQL.Connection && !contains(s.Modes, "display") {
			ps.BlockerFix("surfaces.graphql.connection",
				"a connection is a paged read but the entity has no display mode",
				"add display to modes")
		}
	}
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

// validateRowScopePolicy checks the two knobs that decide what the row scope
// does at its EDGES: who may cross it, and what an absent identity means.
//
// Both used to be hardcoded. The bypass did not exist at all, so a platform
// operator was filtered to their own tenant like anybody else and could not
// support a customer through the API — and could not even ASK, because
// HasPermission panics on the wildcard a superadmin claim carries. The absent
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
			// refusal below must not swallow it. How the test is written without
			// panicking is Authz.Bypass's own documentation.
		case strings.Contains(a.Bypass, "*"):
			// The mistake this exists to catch, because it is the natural way to
			// write the intent and it does not fail until a request arrives.
			ps.BlockerFix("authz.bypass",
				fmt.Sprintf("%q cannot be asked about — the framework's HasPermission "+
					"panics on a wildcard, since the CLAIM wildcards and the question "+
					"does not", a.Bypass),
				`"*:*" is the one exception, because a superadmin answers true to every `+
					`concrete question and the generated guard can test for exactly that. `+
					`Anything narrower has to be a concrete permission: grant something `+
					`like platform:cross-tenant and name it here`)
		case !strings.Contains(a.Bypass, ":"):
			ps.BlockerFix("authz.bypass",
				fmt.Sprintf("%q is not a permission", a.Bypass),
				"spell it resource:action")
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

func findChild(cs []Child, name string) *Child {
	for i := range cs {
		if cs[i].Name == name {
			return &cs[i]
		}
	}
	return nil
}

func findSibling(ss []Sibling, name string) *Sibling {
	for i := range ss {
		if ss[i].Name == name {
			return &ss[i]
		}
	}
	return nil
}

// findAnyField looks through the root and its siblings — everything that lands
// on the aggregate's own struct.
func findAnyField(s *Spec, name string) *Field {
	if f := findField(s.Fields, name); f != nil {
		return f
	}
	for i := range s.Siblings {
		if f := findField(s.Siblings[i].Fields, name); f != nil {
			return f
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
func filterableField(s *Spec, name string) *Field {
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
func hasExistsFactFor(s *Spec, want []string) bool {
	if s.Service == nil {
		return false
	}
	for _, fa := range s.Service.Facts {
		if fa.Kind == "exists" && sameNameSet(fa.Filters, want) {
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
// columns, and says which of the three things went wrong: no fact at all, a
// fact that is missing the scope, or a fact that filters by more than the index
// covers.
func reportPrecheckMismatch(s *Spec, f Field, want []string, where string, ps *Problems) {
	list := strings.Join(want, ", ")
	// The closest candidate: an exists fact that at least mentions the value.
	// Naming it is what turns "declare a fact" into "fix the one you have".
	var near *Fact
	value := want[len(want)-1]
	if s.Service != nil {
		for i := range s.Service.Facts {
			fa := &s.Service.Facts[i]
			if fa.Kind == "exists" && contains(fa.Filters, value) {
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
	var extra, missing []string
	for _, x := range near.Filters {
		if !contains(want, x) {
			extra = append(extra, x)
		}
	}
	for _, x := range want {
		if !contains(near.Filters, x) {
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
				near.Name, strings.Join(near.Filters, ", "), strings.Join(extra, ", ")),
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

// identityRead renders the aggregate id as the field a filter or a sort needs.
//
// It is deliberately NOT part of the readable set. The id is not projected
// through the field pipeline — every response carries it because the framework
// puts it there — so the three keys that address a PROJECTED column (indexes,
// fieldRestrict, computed.from) have nothing here to address: an index would be
// declared over `id` while the document's key is `_id`, a restriction would be
// asked to scrub the handle the response is required to carry, and a derivation
// would read a value the reader never selects. Filtering and ordering are the
// two questions the STORE answers, and those the framework resolves by name.
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

func readableField(s *Spec, name string) bool {
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
