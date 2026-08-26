// Package ir turns a validated spec plus what was discovered about the project
// into a fully explicit model.
//
// Every default is materialised HERE. Downstream, the emitters read the model
// and nothing else — no spec lookups, no "if unset then". That separation is
// what keeps a default from being decided two ways in two layers, which is the
// classic generator bug that compiles and is wrong.
package ir

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// Model is the resolved entity.
type Model struct {
	Entity   Names
	Module   string
	Language string

	Table            string
	TableDescription string
	Base             *Base
	Managed          Managed

	Fields   []Field // persisted on the root/role table, in spec order
	Runtime  []Field // runtime-only (authz), never persisted
	Children []Child
	Siblings []Sibling

	Clauses     []Clause
	ManualRules []ManualRule
	HasHookFile bool
	// ArchiveWhen is the lifecycle decision an ordinary update can reach: when
	// set, the generated IfUpdate clause ends by asking the framework to finish
	// THIS write as an archive.
	ArchiveWhen *ArchiveWhen

	Notifications []Notification
	ValueObjects  []ValueObject
	Service       *ServiceModel

	// Joins are the READ JOINS this repository declares: read-only traversals
	// across a foreign key into another aggregate. They are deliberately NOT in
	// Fields — nothing about them is persisted, so no write shape, no migration
	// and no TableSchema may ever see them — and every consumer that DOES serve
	// them names them explicitly.
	Joins []Join

	Ops  []Operation
	Read ReadModel

	Authz    Authz
	Surfaces Surfaces
	// PatchExcludes are fields a partial update may NOT touch — a natural key, or
	// anything whose change has to go through a deliberate operation instead.
	PatchExcludes map[string]bool

	Dialects []string
	Ordinal  map[string]int

	Constraints []Constraint
}

// Names carries every spelling of the entity so no emitter has to derive one.
type Names struct {
	Pascal       string // Student
	Camel        string // student
	Snake        string // student
	PluralPascal string // Students
	PluralCamel  string // students
	PluralSnake  string // students
	Route        string // /students
}

type Managed struct {
	CreatedAt  string
	UpdatedAt  string
	ArchivedAt string
	Revision   string
	Archiving  bool
}

// Field is a resolved field: the Go type is decided, the wire name is decided,
// the label key is decided.
type Field struct {
	Name     string
	Column   string
	SpecType string
	// GoType/BaseGoType are the WIRE types — the underlying scalar. Commands and
	// DTOs carry these, never the value object: the wire boundary speaks in
	// primitives and the cast happens in the command mapper.
	GoType     string // already includes the pointer for a nullable field
	BaseGoType string // without the pointer
	// EntityType is what the AGGREGATE declares. It is the value-object type when
	// the field has one, so the framework can discover and validate it by type.
	EntityType     string
	BaseEntityType string
	VOKind         string // "" | raw | enum | reuse
	Nullable       bool
	Length         int
	JSONName       string
	LabelKey       string
	// Text is the field's LABEL per catalog code, from the spec. A catalog the
	// spec left out is absent here too, and the catalog emitter falls back to
	// the field's own name — the label is deliberately NOT derived from
	// Description, which is a sentence about the field, not its name.
	Text        map[string]string
	Example     string
	Description string
	Unique      *Unique
	Runtime     bool
	// Hidden keeps the field out of every response body while leaving it stored,
	// filterable, sortable and writable. It is read by the three Response structs
	// and by nothing else: the Result the query fills stays whole, so a computed
	// read field can still derive FROM a hidden source and `?fields=` can still
	// push it down.
	Hidden bool
	// Claim is the JWT claim name a `source: claim` runtime field is read by.
	// Empty for every other source: the identity ones ask the framework's own
	// accessors, which own the claim names they consult.
	Claim string
	// Permission is the concrete resource:action a `source: permission` runtime
	// field asks Identity.HasPermission about.
	//
	// It is a field of its own and not a second meaning for Claim, matching the
	// spec language it is lowered from — the emitter reads one of the two
	// depending on IdentitySource, and a single field carrying either would make
	// that read correct by coincidence.
	Permission string
	// Synthesised marks a runtime field this resolver invented for the ROW SCOPE
	// rather than one the author declared.
	//
	// The identity feed does not care, and that is the whole design. Exactly one
	// emitter does: the generated command test, which grants the caller what the
	// DECLARED permission fields ask about so their value arrives true — and must
	// not grant the row scope's bypass, because the mapper test's caller is the
	// ordinary one, and handing it the operator's permission would make every
	// generated scope assertion pass for the wrong reason.
	Synthesised bool
	// Source is where a RUNTIME field's value comes from: "claim" (the caller's
	// token) or "body" (the request itself). Empty for a persisted field.
	//
	// The two share everything below the entity — neither has a column, so
	// neither reaches the TableSchema, the migration, the outbox payload, the
	// audit event or any response — and differ above it: a claim field is absent
	// from every write DTO and filled by the identity feed, a body field is a
	// normal request field the mapper assigns like any other.
	Source string
	// Modes are the write verbs whose body carries a "body" field, at the
	// granularity the rule gates have: "insert" and "update", where update means
	// both the full replacement and the patch. Always populated for such a
	// field, empty for every other.
	Modes []string
	// IdentitySource says WHICH question about the caller fills a runtime field,
	// naming the framework accessor that answers it: "subject" (Identity.Subject),
	// "tenant" (the configured tenant claim, via Identity.TenantID), "permission"
	// (whether the caller holds Permission, via Identity.HasPermission),
	// "super-admin" (whether they hold `*:*`, via Identity.IsSuperAdmin) or
	// "present" (whether there was an identity at all, which is the nil check
	// itself and no accessor).
	//
	// Empty means the field is fed some other way: read from Claims BY NAME
	// (source: claim, where the framework does not opine on custom claim names so
	// there is no convention to fall back on), or sent by the caller
	// (source: body), or not a runtime field at all.
	//
	// Both the fields this resolver SYNTHESISES for the row scope and the ones an
	// author DECLARES land in this same vocabulary, which is the point: the
	// command mapper's identity feed writes one branch per question, and where the
	// question came from is not its business.
	IdentitySource string
	// AssignedFrom names where the server reads this field's value when the
	// client is not allowed to send it. Empty for an ordinary field.
	AssignedFrom string
	// BypassMaySet says the caller who crosses the row scope may state this
	// value instead of having it read off their own identity. Set only on the
	// scope's SUBJECT, which is the one field the row-scope guard compares — and
	// so the one field where a value from a caller who may not state one is
	// answered rather than taken.
	BypassMaySet bool
	// WireOptional makes the field a POINTER on the wire whatever its column
	// says, so "did the caller send this?" is answerable.
	//
	// It is not `nullable` and must not be confused with it: nullable is about
	// the COLUMN accepting NULL, this is about the REQUEST being allowed to omit
	// a value the server would otherwise supply. It is set on the copy that goes
	// into one verb's command, never on the field the table is built from.
	WireOptional bool
	LivesOn      string
	// Facet names the 1:1 facet a field is stored in, when it is not stored on
	// its owner's own table. The Go type carries it like any other field — the
	// split is physical — so only the two emitters that write TABLES care.
	Facet string
	// Redaction is set when the field is declared with a redact block: the real
	// value stays in the column and in the hydrated entity, and every copy the
	// framework makes of the row carries a mask instead. Nil for every other
	// field, and the schema emitter is the only place it changes what is
	// written — Field(...) becomes RedactedField(...).
	Redaction *Redaction
	// Composite is set when this field is one PART of a composite value object.
	// Everything above the schema treats it as an ordinary field under its
	// exposed name; this is the back-reference the four domain-facing emitters
	// read to put the value object back together. Nil for every other field.
	Composite *CompositePart
}

type Unique struct {
	Enforce      string
	Notification string
	Scope        string
	// Within names the fields the uniqueness is scoped BY — "unique per tenant".
	// It sizes the index and is held to the pre-check fact's filters, so the
	// domain and the database cannot disagree about what is unique.
	Within []string
}

// Clause is one BuildRules block: a verb gate plus the checks inside it.
type Clause struct {
	Gate  string // IfInsert | IfUpdate | IfInsertOrUpdate | IfArchive | IfUnarchive | IfDelete
	Rules []Rule
}

type Rule struct {
	ID           string
	Kind         string
	Fields       []Field
	Other        *Field
	Operator     string
	Min, Max     *float64
	Notification string
	AttachTo     string
	EchoValue    bool
	Description  string
	OwnerField   *Field
	// AdminField is the runtime flag that lets a privileged caller through an
	// owner check. It is a SEPARATE question from the permission: the permission
	// says who may attempt the verb at all, this says who may attempt it on a row
	// that is not theirs.
	AdminField *Field
	// VOEnum says, per subject field of a `valueObject` rule, whether that value
	// object is validated by MEMBERSHIP — domain.ValidateEnum — rather than by
	// calling its own IsValid. The two are different calls and picking the wrong
	// one does not compile, so the choice is resolved here, once, where the
	// spec and the project's own vos package can both be read: a field declared
	// `vo.kind: reuse` names a type this spec never described.
	VOEnum map[string]bool
	// Guard makes the rule a barrier: the emitters put r.StopIfInvalid() on the
	// line after its block, so nothing below runs once anything above has
	// rejected. It rides on the rule rather than being a clause of its own
	// because that is what makes it positional — it lands where the rule was
	// declared, and the rule keeps its declared position.
	Guard bool
	// FactName is the fact a factRange reads, by name; Fact is the resolved one,
	// bound after the service is. Two steps because the rules are lowered before
	// the port is, and a rule holding a copy of a fact resolved too early would
	// silently miss the params the service adds.
	FactName string
	Fact     *Fact
	// Transitions is the allowed state machine: from → the states it may move to.
	Transitions map[string][]string
	// Collection names the child collection a rule reads, for the two kinds that
	// look at the whole set rather than at one record.
	Collection string
	GroupBy    []string
	Cap        int
	// OnlyField/OnlyEquals restrict WHICH entries a set-wide rule counts. A cap
	// with no restriction applies to every value of the grouping field equally,
	// which is a different rule from the one a domain usually means: "at most 3
	// under review" is not "at most 3 of each status".
	OnlyFieldName string
	OnlyField     *Field
	OnlyEquals    string
	// Hoisted records that this rule was declared on a COLLECTION and moved to
	// the root, because only the root can see what the entries were before the
	// write. The report says so: a rule that runs somewhere other than where it
	// was written is worth a line rather than a surprise.
	Hoisted bool
	// SkipWhen makes the rule stand down instead of firing: "empty" when the
	// subject carries no value, "null" when the pointer is nil. It is what tells
	// "you may leave this out, but if you fill it in it must be valid" apart from
	// "you must fill this in".
	SkipWhen string
}

type ManualRule struct {
	ID           string
	Description  string
	Gates        []string
	Notification string
	AttachTo     string
}

type Notification struct {
	Name    string
	Package string
	// Moved records that the resolver placed this notification somewhere other
	// than where the spec said, because the spec's choice could not compile. The
	// report says so: a type that is not where its author put it is worth one
	// line rather than a surprise.
	Moved    bool
	Semantic string
	TVars    []string
	Text     map[string]string
	Missing  []string // languages filled with a marked placeholder
}

// Operation is one mounted endpoint, fully decided: which handler, which
// command, which permission, which status.
type Operation struct {
	Verb       string // insert | update | patch | delete | archive | unarchive | byId | byParams
	Method     string // fiber.MethodPost …
	Path       string
	Permission string
	Status     string // fiber.StatusCreated …

	CommandType string
	ResultType  string
	CommandBase string
	HandlerType string
	InputMethod string // ToEntity | ApplyTo | ApplyPartiallyTo | ""

	RequestType  string
	ResponseType string
	Bodyless     bool
	Write        bool

	Summary     string
	Description string
}

// Join is one resolved read-join declaration.
type Join struct {
	// Kind is "inner" or "left"; it decides the framework verb AND whether the
	// mapped Go fields are nullable.
	Kind string
	// Target is the joined entity, and TargetSchemaFunc the schema function the
	// generated repository hands to the framework.
	Target           string
	TargetSchemaFunc string
	// FKColumn is the foreign key on the JOINING table.
	FKColumn string
	// Child is the collection this join hangs off, empty for a root join;
	// ChildSchemaFunc is that collection's schema function.
	Child           string
	ChildSchemaFunc string
	// Fields are what the traversal brings back. Their Column is the column on
	// the TARGET — the join's own side — not a column of this entity, which has
	// none for them.
	Fields []Field
	// TargetHandWritten says no spec of this project declares the target, so
	// every field's type and nullability came from the AUTHOR rather than from a
	// declaration the generator could read. Nothing downstream changes; it is
	// carried so the gen-report can tell a reviewer which rows were taken on
	// somebody's word and are checked by the framework at boot instead.
	TargetHandWritten bool
}

// Verb is the framework call this declaration renders as.
func (j Join) Verb() string {
	if j.Kind == "inner" {
		return "InnerJoin"
	}
	return "LeftJoin"
}

type ReadModel struct {
	Enabled         bool
	DeleteOnArchive bool
	TTLSeconds      int
	Indexes         []Index
	FieldRestrict   []FieldRestrict
	Backing         string
	ViewName        string
	Version         int
	MaxLimit        int
	ByID            bool
	ByParams        bool
	Filters         []Filter
	// Sortable is the ordering VOCABULARY, in declaration order: the leaves that
	// carry a `sort:` tag on the Request DTO. A leaf that is also filtered gets
	// the tag beside its `filter:`; one that is not becomes a leaf of its own,
	// orderable and carrying no value on the wire.
	Sortable []Field
	// Managed are the framework-stamped columns this read exposes, resolved into
	// ordinary-looking fields so the Result, the Responses and the exports need
	// no special case. They are deliberately NOT in m.Fields: the aggregate
	// declares no Go field for them, no write DTO carries them, and the migration
	// already created the columns.
	Managed   []Field
	Controls  spec.Controls
	QueryByID string
	QueryList string
	// ResultByID / ResultList are the application-layer Result types the two
	// reads declare — the read-side twin of a command's Result. The framework
	// fills one per document and the Query's FromQueryResult hook sees it
	// BEFORE any transport does, so a field absent from the Result can reach
	// no wire.
	ResultByID string
	ResultList string
	// Computed are the derived read fields: no column, filled by the hook the
	// generator writes once and the author owns from then on.
	Computed []ComputedField
	// JoinFields are the ROOT joins' fields, as the read model serves them.
	//
	// Populated on a RELATIONAL backing only, and that is not a policy choice: a
	// join leaves the TableSchema untouched, so a Mongo projection over the same
	// entity never carries these columns. Putting them in the projected Result
	// would serve a zero value on every document.
	//
	// A CHILD join's fields are not here — they live on the collection's own
	// entry (Child.JoinFields), which is where the document carries them.
	JoinFields []Field
}

// ComputedField is a read field with no column behind it.
type ComputedField struct {
	Name string
	// GoType/BaseGoType are the DERIVED value's type. Nullability is not a
	// property of the declaration: the derivation may always decline to produce
	// a value, so the wire shape is a pointer wherever the surrounding shape is.
	GoType      string
	BaseGoType  string
	JSONName    string
	LabelKey    string
	Text        map[string]string
	Example     string
	Description string
	// Sources are the Result field names the derivation reads. They travel to
	// the wire as the `computed:"A,B"` tag, which is what makes `?fields=`
	// fetch the columns behind the derivation instead of a name no column has.
	Sources []string
	// SourceFields are those same sources RESOLVED, in declaration order, and
	// they are the only form the emitters may read.
	//
	// They exist because resolution used to happen twice, against two different
	// sets: the validator blessed a name and the emitter looked it up again in a
	// narrower one, dropping what it could not find with a bare `continue`. The
	// two halves cannot drift apart when there is only one of them, and a name
	// that resolves to nothing is now a hard failure of Resolve — a derivation
	// that quietly loses a parameter publishes a permanently empty field, which
	// is the one outcome nothing downstream can detect.
	SourceFields []Field
}

type Filter struct {
	Field Field
	Ops   []string
}

type Authz struct {
	DataAccess  string
	OwnerField  *Field
	TenantField *Field
	// Bypass is what crosses the row scope, empty when nothing does: a concrete
	// permission, or the framework's super-admin wildcard. NoIdentity is what an
	// absent identity means — always resolved, never empty, so the emitters read
	// a decision rather than a default.
	Bypass string
	// BypassWildcard says the bypass is the SUPER-ADMIN WILDCARD rather than a
	// permission anybody can be granted. It changes how the question is asked
	// and nothing else: HasPermission panics on a wildcard, so the guard asks
	// Identity.IsSuperAdmin() instead, and the two emitters that write that
	// guard have to know which of the two they are writing.
	BypassWildcard bool
	NoIdentity     string
	// ScopeField and BypassField are RUNTIME fields this resolver synthesises
	// for a scoped dataAccess: the caller's own scope value, and whether they
	// hold the bypass. They are what carries the identity into BuildRules, which
	// is the only place a write can be refused for being outside its scope —
	// the read filter lives in the query and never sees a write at all.
	ScopeField  *Field
	BypassField *Field
	// PresenceField answers "was there an identity at all", which the scope
	// value cannot: an empty scope is either no identity or a token without the
	// claim, and only the first is confined to a dev bench. Synthesised only
	// under stand-down, which is the one policy that acts on the difference.
	PresenceField *Field
}

// SuperAdminMethod is the framework method a generated guard calls when what
// crosses the row scope is the SUPER-ADMIN WILDCARD rather than a permission
// somebody was granted.
//
// HasPermission panics on a wildcard — the claim wildcards, the question does
// not — so "does this caller hold `*:*`?" is not asked through it. It has its
// own method: IsSuperAdmin reports the `*:*` grant directly, is nil-safe,
// honours the configured permissions claim name, and shares the parsed-claim
// cache with HasPermission.
//
// The name lives here rather than in the emitter because the REPORT names it
// too: a reviewer who meets the call in the generated code has to be able to
// find out what it is, and the answer must not be two answers.
const SuperAdminMethod = "IsSuperAdmin"

// SuperAdminGrant is the permissions-claim entry SuperAdminMethod reports on.
// The emitters need it as a VALUE (a generated test hands it to a fixture
// Identity), which the method name is not.
const SuperAdminGrant = spec.SuperAdminClaim

// Scoped reports whether the rows are narrowed by who is asking.
func (a Authz) Scoped() bool {
	return a.DataAccess == "owner-only" || a.DataAccess == "tenant"
}

// ScopeSubject is the persisted field a write is checked against.
func (a Authz) ScopeSubject() *Field {
	if a.DataAccess == "tenant" {
		return a.TenantField
	}
	return a.OwnerField
}

// Constraint is a database constraint the migration creates AND the repository
// binds, so the violation surfaces as a clean notification instead of a 500.
type Constraint struct {
	Kind         string // primary-key | unique
	Table        string
	Columns      []string
	Notification string
	Field        string
	// Scope is "all" or "active-only". It decides whether an archived row keeps
	// holding the value: the difference between a document number that can never
	// be reused and one that comes free when the row is archived.
	Scope string
	// Within names the fields the uniqueness is scoped BY, in the spec's own
	// words. Columns already carries them — this is what the REPORT says, and a
	// column list is not what an author declared.
	Within []string
	// Archived is the column an active-only constraint skips by. It is carried
	// per constraint rather than read off the model because a COLLECTION
	// archives by its own column: an entry freed for reuse is a soft-removed
	// entry, which has nothing to do with whether the root is archived.
	Archived string
	// Collection names the collection this constraint belongs to, empty for the
	// root's. It is what lets the notification be reported under the collection
	// the caller sent, and the report say which table the index is on.
	Collection string
}

// Resolve builds the model. The spec must already be valid and covered.
func Resolve(s *spec.Spec, p *discover.Project) (*Model, error) {
	m := &Model{
		Module:           p.ModulePath,
		Language:         s.Language,
		Table:            s.Storage.Table,
		TableDescription: s.Storage.Description,
		Dialects:         p.Dialects,
		Ordinal:          p.NextOrdinal,
		Authz:            Authz{DataAccess: s.Authz.DataAccess},
	}
	m.Entity = resolveNames(s.Entity, s.Plural)
	m.Base = resolveBase(s)
	m.Managed = Managed{
		CreatedAt:  s.Storage.Managed.CreatedAt,
		UpdatedAt:  s.Storage.Managed.UpdatedAt,
		ArchivedAt: s.Storage.Managed.ArchivedAt,
		Revision:   s.Storage.Managed.Revision,
		Archiving:  s.Storage.Managed.ArchivedAt != "",
	}

	for _, f := range s.Fields {
		if spec.IsComposite(f) {
			// One spec field, N logical ones — the expansion that lets every
			// emitter above the schema keep reading a flat list.
			m.Fields = append(m.Fields, expandComposite(s, m.Entity.Pascal, f)...)
			continue
		}
		rf := resolveField(m.Entity.Pascal, f)
		if f.Runtime {
			if rf.Source == "body" {
				rf.Modes = bodyFieldModes(f.Modes, s.Modes)
			}
			m.Runtime = append(m.Runtime, rf)
		} else {
			m.Fields = append(m.Fields, rf)
		}
	}

	m.Children = resolveChildren(s, m)
	m.Siblings = resolveSiblings(s, m)
	attachChildFacets(m)
	// After the facets are folded in, not before: a rule on a facet field of a
	// child has to find that field.
	var hoisted []Clause
	for i := range m.Children {
		entry, up := splitByScope(s.Children[i].Rules)
		m.Children[i].Clauses = resolveClausesFor(entry, m.Children[i].Fields)
		m.Children[i].ManualRules = resolveManualRules(s.Children[i].Rules)
		m.Children[i].HasHookFile = len(m.Children[i].ManualRules) > 0
		hoisted = append(hoisted, hoistToRoot(up, &m.Children[i])...)
	}
	m.Notifications = resolveNotifications(s)
	placeNotifications(s, m)
	m.ValueObjects = resolveValueObjects(s)
	m.Service = resolveService(s, m)
	m.Clauses = resolveClauses(s, m)
	m.Clauses = mergeClauses(m.Clauses, hoisted)
	bindOnlyFields(m)
	bindValueObjectRules(s, p, m)
	bindFacts(m)
	m.Clauses = appendUniqueClauses(m)
	m.ManualRules = resolveManualRules(s.Rules)
	m.HasHookFile = len(m.ManualRules) > 0
	m.ArchiveWhen = resolveArchiveWhen(s, m)
	m.PatchExcludes = map[string]bool{}
	for _, name := range s.Update.PatchExcludes {
		m.PatchExcludes[name] = true
		// Excluding a composite by its own name excludes every part of it: the
		// wire has the parts, the spec named the concept, and taking half of a
		// value object out of a patch is not a shape anyone asked for.
		for _, g := range Composites(m.Fields) {
			if g.Owner() != name {
				continue
			}
			for _, p := range g.Parts {
				m.PatchExcludes[p.Name] = true
			}
		}
	}
	// Before the read: a relational read model serves the ROOT joins' fields, so
	// the read side has to be resolved with them already in hand.
	resolveJoins(s, p, m)
	// After the joins, for the same reason the read model is: a field a join
	// declared `inChild` is on the entry now, and a per-entry derivation may
	// read it.
	if err := bindChildComputedSources(m); err != nil {
		return nil, err
	}
	read, err := resolveRead(s, m)
	if err != nil {
		return nil, err
	}
	m.Read = read
	m.Ops = resolveOps(s, m)
	// After the ops, not with the children: what an undeclared per-entry verb
	// inherits is the root's update permission, and that only exists once the
	// root's operations are resolved.
	resolveChildPermissions(s, m)
	m.Constraints = resolveConstraints(s, m)
	m.Surfaces = resolveSurfaces(s)
	if f := lookupField(m, s.Authz.OwnerField); f != nil {
		m.Authz.OwnerField = f
	}
	if f := lookupField(m, s.Authz.TenantField); f != nil {
		m.Authz.TenantField = f
	}
	resolveRowScope(s, m)
	return m, nil
}

// resolveRowScope completes a scoped dataAccess: the policy at its edges, and
// the runtime fields that carry the caller into the WRITE path.
//
// The write path is the half that was missing. A read is narrowed inside the
// query, where the identity is already in hand; a write is checked in
// BuildRules, where the entity is all there is — so the caller's own scope has
// to travel onto the entity as a runtime field, exactly as a hand-written
// ownerCheck already did. Synthesising it is what makes the guard follow from
// `dataAccess` instead of from remembering to declare three more things.
// standsDown reports whether anything in this model tolerates a request that
// carried no identity — which is exactly what needs to tell "no identity" apart
// from "an identity without the claim".
func standsDown(m *Model) bool {
	if m.Authz.Scoped() && m.Authz.NoIdentity == "stand-down" {
		return true
	}
	for _, c := range m.Clauses {
		for _, r := range c.Rules {
			if r.Kind == "ownerCheck" {
				return true
			}
		}
	}
	return false
}

func resolveRowScope(s *spec.Spec, m *Model) {
	m.Authz.Bypass = s.Authz.Bypass
	m.Authz.BypassWildcard = s.Authz.Bypass == spec.SuperAdminClaim
	m.Authz.NoIdentity = s.Authz.NoIdentity
	if m.Authz.NoIdentity == "" {
		// stand-down, because that is what every OTHER identity-derived rule
		// this generator writes already does: an ownerCheck tolerates an absent
		// principal, and it has to — with auth.mode disabled there is no
		// identity on any request, so a rule that fired anyway would reject
		// every call on the bench where the entity is first run. A row scope
		// that alone failed closed would be the odd one out, and the surprise
		// would land on the one profile nobody is watching for surprises.
		//
		// It is safe because the guard asks whether an identity was PRESENT,
		// never whether the scope came out empty — see PresenceField. Absent
		// identity is confined to auth.mode: disabled, which the framework
		// refuses outside APP_PROFILE=dev; a token that merely lacks the claim
		// is an ordinary production request and is still refused.
		//
		// `refuse` stays available for a service that wants the scope enforced
		// even with authentication off.
		m.Authz.NoIdentity = "stand-down"
	}
	// Whether there was an identity AT ALL — a fact no VALUE can carry.
	//
	// An empty scope means either "no identity" (only reachable with auth.mode
	// disabled, which the framework refuses outside APP_PROFILE=dev) or "a real,
	// signed token that carries no such claim", and the second is an ordinary
	// production request. The read side tells them apart for free, because it
	// branches on Identity() != nil and has an else; the domain sees only the
	// entity, where both arrive as "". Every rule that stands down for an absent
	// principal therefore has to ask THIS, or it stands down for the wrong one
	// too — which is a token without the claim walking through the check.
	//
	// Synthesised only where something stands down, which is where the
	// distinction is acted on: a row scope under stand-down, and every
	// ownerCheck (which tolerates an absent principal by design, and must
	// therefore tolerate the RIGHT one). A field nothing reads would sit on the
	// aggregate as noise — import pruning cannot remove a struct field.
	if standsDown(m) {
		m.Authz.PresenceField = &Field{
			Name: "RequestingIdentityPresent", GoType: "bool", BaseGoType: "bool",
			SpecType: "bool", Runtime: true, IdentitySource: "present", Synthesised: true,
			Description: "Whether the request carried an identity at all",
		}
		m.Runtime = append(m.Runtime, *m.Authz.PresenceField)
	}
	if !m.Authz.Scoped() || m.Authz.ScopeSubject() == nil {
		return
	}
	source, what := "tenant", "tenant"
	if m.Authz.DataAccess == "owner-only" {
		source, what = "subject", "owner"
	}
	m.Authz.ScopeField = &Field{
		Name: "Requesting" + naming.Pascal(source), GoType: "string", BaseGoType: "string",
		SpecType: "string", Runtime: true, IdentitySource: source, Synthesised: true,
		Description: "The caller's own " + what + ", from the request identity",
	}
	m.Runtime = append(m.Runtime, *m.Authz.ScopeField)
	if m.Authz.Bypass == "" {
		return
	}
	// A wildcard bypass is fed by a DIFFERENT question, so it is a different
	// source: the feed cannot hand `*:*` to HasPermission, which panics on it —
	// the framework answers that one with Identity.IsSuperAdmin.
	asked, held := "permission", "holds "+m.Authz.Bypass
	if m.Authz.BypassWildcard {
		asked, held = "super-admin", "is a super-admin ("+spec.SuperAdminClaim+")"
	}
	m.Authz.BypassField = &Field{
		Name: "RequestingMayCrossScope", GoType: "bool", BaseGoType: "bool",
		SpecType: "bool", Runtime: true, IdentitySource: asked, Synthesised: true,
		Permission:  m.Authz.Bypass,
		Description: "Whether the caller " + held + ", which crosses the row scope",
	}
	m.Runtime = append(m.Runtime, *m.Authz.BypassField)
}

func resolveNames(entity, plural string) Names {
	return Names{
		Pascal:       entity,
		Camel:        naming.Camel(entity),
		Snake:        naming.Snake(entity),
		PluralPascal: plural,
		PluralCamel:  naming.Camel(plural),
		PluralSnake:  naming.Snake(plural),
		Route:        "/" + naming.Snake(plural),
	}
}

// goTypeOf maps a spec type to Go. The set is closed and mirrors what the
// framework can persist; anything else was refused at validation.
func goTypeOf(specType string) string {
	switch specType {
	case "string":
		return "string"
	case "int":
		return "int"
	case "int64":
		return "int64"
	case "float64":
		return "float64"
	case "bool":
		return "bool"
	case "time":
		return "time.Time"
	case "id":
		return "domain.ID"
	default:
		return "string"
	}
}

func resolveField(entity string, f spec.Field) Field {
	base := goTypeOf(f.Type)
	goType := base
	if f.Nullable {
		goType = "*" + base
	}
	// A value-object field is declared on the aggregate as the VO type; the wire
	// keeps the underlying scalar.
	entityBase, voKind := base, ""
	if f.VO != nil && f.VO.Kind != "" && f.VO.Kind != "none" {
		voKind = f.VO.Kind
		entityBase = "vos." + f.VO.Ref
	}
	entityType := entityBase
	if f.Nullable {
		entityType = "*" + entityBase
	}
	label := f.LabelKey
	if label == "" {
		label = entity + f.Name + "Field"
	}
	out := Field{
		Name: f.Name, Column: f.Column, SpecType: f.Type,
		GoType: goType, BaseGoType: base,
		EntityType: entityType, BaseEntityType: entityBase, VOKind: voKind,
		Nullable: f.Nullable, Length: f.Length,
		JSONName: naming.Camel(f.Name), LabelKey: label, Text: f.Text.Map(),
		Example: f.Example, Description: f.Description, Runtime: f.Runtime, Claim: f.Claim,
		Source:         spec.SourceOf(f),
		IdentitySource: spec.IdentitySourceOf(f),
		Permission:     f.Permission,
		Hidden:         f.Hidden,
		AssignedFrom:   f.AssignedFrom,
		BypassMaySet:   f.BypassMaySet,
		LivesOn:        f.LivesOn,
		Redaction:      resolveRedaction(f.Redact, entity, f.Name),
	}
	if f.Unique != nil {
		scope := f.Unique.Scope
		if scope == "" {
			scope = "all"
		}
		out.Unique = &Unique{
			Enforce: f.Unique.Enforce, Notification: f.Unique.Notification,
			Scope: scope, Within: f.Unique.Within,
		}
	}
	return out
}

// gateOf maps a spec scope to the framework's rule-DSL clause.
//
// archive and unarchive have their OWN gates: a rule left under the update gate
// simply never fires on an archive transition, which is silent and easy to miss.
func gateOf(scope string) string {
	switch scope {
	case "insert":
		return "IfInsert"
	case "update":
		return "IfUpdate"
	case "insertOrUpdate":
		return "IfInsertOrUpdate"
	case "archive":
		return "IfArchive"
	case "unarchive":
		return "IfUnarchive"
	case "delete":
		return "IfDelete"
	}
	return "IfInsertOrUpdate"
}

// gateOrder keeps emitted clauses in a stable, readable order rather than map order.
var gateOrder = map[string]int{
	"IfInsert": 0, "IfInsertOrUpdate": 1, "IfUpdate": 2,
	"IfArchive": 3, "IfUnarchive": 4, "IfDelete": 5,
}

// GateRank is that same order, for an emitter grouping rules by verb outside
// this package. A gate nobody listed sorts last rather than first, so an
// unknown one is visible at the bottom instead of silently leading the file.
func GateRank(gate string) int {
	if n, ok := gateOrder[gate]; ok {
		return n
	}
	return len(gateOrder)
}

// canonicalCollection is the ONE spelling of a collection below this line.
//
// A key that addresses a collection accepts either name the collection has —
// the entry type's `name` or the collection's `plural`; see
// spec.CollectionNamed for why both are real, and why they used to disagree
// from key to key. Everything under the IR resolves collections by `name`: the
// schema function a join calls is <Name>Schema, a facet's owner is matched
// against Child.Name, and the emitters walk m.Children by name. So the author's
// spelling is translated exactly once, here, and no emitter ever learns there
// were two.
//
// An unresolvable name is returned unchanged rather than blanked: validation has
// already refused it, and turning it into "" downstream would swap a reported
// blocker for a rule that silently applies to nothing.
func canonicalCollection(children []spec.Child, written string) string {
	if c := spec.CollectionNamed(children, written); c != nil {
		return c.Name
	}
	return written
}

func resolveClauses(s *spec.Spec, m *Model) []Clause {
	return resolveClauseSet(s.Rules, s.Children, func(n string) *Field { return lookupField(m, n) })
}

// resolveClauseSet turns declared rules into the clauses an emitter writes.
//
// There is ONE of these, deliberately. The root and a collection used to have a
// resolver each, and the collection's was a copy that had fallen behind: it
// never carried Transitions, GroupBy, Cap, SkipWhen, AdminField or OwnerField.
// A `transition` declared inside children[] therefore validated, generated its
// notification and its translations, and emitted a clause with no edges — no
// error, no refusal, nothing in the report, and an author who found it by
// reading the generated file and seeing an empty IfUpdate. Two copies of one
// mapping is how that happens; the lookup is the only part that legitimately
// differs, so it is the only part that is a parameter.
func resolveClauseSet(rs spec.Rules, children []spec.Child, lookup func(string) *Field) []Clause {
	byGate := map[string][]Rule{}
	for _, r := range rs.List {
		rule := Rule{
			ID: r.ID, Kind: r.Kind, Operator: r.Operator,
			Min: r.Min, Max: r.Max, Notification: r.Notification,
			AttachTo: r.AttachTo, EchoValue: r.Echoes(), Description: r.Description,
			Transitions: r.Transitions, GroupBy: r.GroupBy, Cap: r.Cap,
			SkipWhen: r.SkipWhen, FactName: r.Fact, Guard: r.Guard,
		}
		// The two set-wide kinds name a COLLECTION where the others name fields:
		// they ask what the aggregate holds, not what one record says.
		if r.Kind == "childDuplicate" || r.Kind == "groupCap" {
			if len(r.Fields) > 0 {
				rule.Collection = canonicalCollection(children, r.Fields[0])
			}
		}
		if r.Only != nil {
			rule.OnlyFieldName, rule.OnlyEquals = r.Only.Field, r.Only.Equals
		}
		for _, fn := range r.Fields {
			if f := lookup(fn); f != nil {
				rule.Fields = append(rule.Fields, *f)
			}
		}
		if r.Other != "" {
			rule.Other = lookup(r.Other)
		}
		if r.AdminField != "" {
			rule.AdminField = lookup(r.AdminField)
		}
		if r.OwnerField != "" {
			rule.OwnerField = lookup(r.OwnerField)
		}
		if rule.AttachTo == "" && len(rule.Fields) > 0 {
			rule.AttachTo = rule.Fields[0].Name
		}
		for _, sc := range r.Scope {
			g := gateOf(sc)
			byGate[g] = append(byGate[g], rule)
		}
	}

	var out []Clause
	for gate, rules := range byGate {
		out = append(out, Clause{Gate: gate, Rules: rules})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return gateOrder[out[i].Gate] < gateOrder[out[j].Gate]
	})
	return out
}

func resolveManualRules(rs spec.Rules) []ManualRule {
	var out []ManualRule
	for _, m := range rs.Manual {
		var gates []string
		for _, sc := range m.Scope {
			gates = append(gates, gateOf(sc))
		}
		out = append(out, ManualRule{
			ID: m.ID, Description: strings.TrimSpace(m.Description), Gates: gates,
			Notification: m.Notification, AttachTo: m.AttachTo,
		})
	}
	return out
}

// LangOrder is the framework's catalog set, re-exported from the spec package
// so the emitters — which consume the IR and deliberately do not import the
// spec — read the same list instead of keeping a third copy of it.
var LangOrder = spec.CatalogCodes

func resolveNotifications(s *spec.Spec) []Notification {
	var out []Notification
	for _, n := range s.Notifications {
		pkg := n.Package
		if pkg == "" {
			pkg = "domain"
		}
		texts := n.Text.Map()
		var missing []string
		for _, code := range LangOrder {
			if strings.TrimSpace(texts[code]) == "" {
				missing = append(missing, code)
				// The placeholder is deliberately loud: an untranslated string
				// that looks finished is worse than one that does not.
				texts[code] = "TODO(" + strings.ToUpper(code) + "): " + n.Name
			}
		}
		out = append(out, Notification{
			Name: n.Name, Package: pkg, Semantic: n.Semantic,
			TVars: n.TVars, Text: texts, Missing: missing,
		})
	}
	return out
}

func resolveRead(s *spec.Spec, m *Model) (ReadModel, error) {
	r := ReadModel{
		Enabled: s.Read.ByID || s.Read.ByParams != nil,
		Backing: s.Read.Backing, ViewName: s.Read.View.Name,
		Version: s.Read.View.Version, MaxLimit: s.Read.View.MaxLimit,
		DeleteOnArchive: s.Read.View.DeleteOnArchive,
		TTLSeconds:      s.Read.View.TTLSeconds,
		ByID:            s.Read.ByID, ByParams: s.Read.ByParams != nil,
	}
	for _, idx := range s.Read.Indexes {
		ri := Index{
			Name: idx.Name, Unique: idx.Unique, Text: idx.Text,
			Sparse: idx.Sparse, Order: idx.Order, Partial: idx.Partial,
		}
		for _, fn := range idx.Fields {
			ri.Columns = append(ri.Columns, readColumn(s, m, fn))
		}
		r.Indexes = append(r.Indexes, ri)
	}
	for _, fr := range s.Read.FieldRestrict {
		// The Go field path, not the column: the criteria is addressed by field.
		r.FieldRestrict = append(r.FieldRestrict, FieldRestrict{
			Field: fr.Field, Column: readColumn(s, m, fr.Field), Permission: fr.Permission,
		})
	}
	if !r.Enabled {
		return r, nil
	}
	r.QueryByID = "Find" + m.Entity.Pascal + "ByIDQuery"
	r.QueryList = "Find" + m.Entity.PluralPascal + "ByParamsQuery"
	r.ResultByID = "Find" + m.Entity.Pascal + "ByIDResult"
	r.ResultList = "Find" + m.Entity.PluralPascal + "ByParamsResult"
	for _, c := range s.Read.Computed {
		base := goTypeOf(c.Type)
		r.Computed = append(r.Computed, ComputedField{
			Name: c.Name, GoType: base, BaseGoType: base,
			JSONName: naming.Camel(c.Name), LabelKey: c.LabelKey, Text: c.Text.Map(),
			Example: c.Example, Description: c.Description,
			Sources: append([]string(nil), c.From...),
		})
	}
	for _, n := range s.Read.Managed {
		r.Managed = append(r.Managed, managedReadField(s, m, n))
	}
	// A ROOT join's fields are served by a relational read model — it reads
	// through the very loader that declares them. A Mongo one is composed from
	// the TableSchema, which a join deliberately never touches, so the same
	// fields would be a zero value on every document.
	if r.Backing == "relational" {
		r.JoinFields = m.RootJoinFields()
	}
	if s.Read.ByParams != nil {
		r.Controls = s.Read.ByParams.Controls
		for _, f := range s.Read.ByParams.Filters {
			// The identity is not in m.Fields and never will be — nothing declares
			// it — so it is resolved before the declared set, exactly as the
			// validator resolves it before the declared set on its own side.
			if fld := identityReadField(m, f.Field); fld != nil {
				r.Filters = append(r.Filters, Filter{Field: *fld, Ops: f.Ops})
				continue
			}
			// The managed set is consulted from the READ being built, not through
			// lookupField: m.Read is still the previous (empty) value at this
			// point, so a filter on CreatedAt resolved to nothing and the query
			// parameter was silently never emitted.
			if fld := joinNamed(r.JoinFields, f.Field); fld != nil {
				r.Filters = append(r.Filters, Filter{Field: *fld, Ops: f.Ops})
				continue
			}
			if fld := managedNamed(r.Managed, f.Field); fld != nil {
				r.Filters = append(r.Filters, Filter{Field: *fld, Ops: f.Ops})
				continue
			}
			if fld := lookupField(m, f.Field); fld != nil {
				r.Filters = append(r.Filters, Filter{Field: *fld, Ops: f.Ops})
			}
		}
		for _, name := range s.Read.ByParams.Sort {
			if fld := identityReadField(m, name); fld != nil {
				r.Sortable = append(r.Sortable, *fld)
				continue
			}
			if fld := joinNamed(r.JoinFields, name); fld != nil {
				r.Sortable = append(r.Sortable, *fld)
				continue
			}
			if fld := managedNamed(r.Managed, name); fld != nil {
				r.Sortable = append(r.Sortable, *fld)
				continue
			}
			if fld := lookupField(m, name); fld != nil {
				r.Sortable = append(r.Sortable, *fld)
			}
		}
	}
	// LAST, and not with the declarations above: a derivation may read a
	// framework-stamped column or a root join's field, and neither exists on the
	// read model until this point.
	if err := bindComputedSources(&r, m.AllOwnerFields()); err != nil {
		return r, err
	}
	return r, nil
}

// bindComputedSources resolves every root derivation's `from:` against the read
// model that will actually be served, and refuses a name that answers to
// nothing.
//
// The refusal is the point. The set a derivation may read from is the ONE set
// the Result carries — the entity's own fields and its root-attached facets, the
// framework-stamped columns the read declared, and a root join's fields — and
// the validator blesses exactly that. If a name still fails here, the two halves
// have drifted, and the honest outcome is a generator that stops: the alternative
// is a signature short one parameter, which compiles, ships, and renders a field
// that is empty forever.
func bindComputedSources(r *ReadModel, owner []Field) error {
	for i := range r.Computed {
		c := &r.Computed[i]
		c.SourceFields = nil
		for _, name := range c.Sources {
			f, ok := readSourceField(r, owner, name)
			if !ok {
				return fmt.Errorf("read.computed (%s): %q names no field this read serves — "+
					"the derivation reads the entity's own fields and its root-attached "+
					"facets, the framework-stamped columns under read.managed, and a root "+
					"join's fields", c.Name, name)
			}
			c.SourceFields = append(c.SourceFields, f)
		}
	}
	return nil
}

// bindChildComputedSources is bindComputedSources for the per-entry scope: same
// rule, one level down, and the same refusal to carry on with a source that
// answered to nothing.
//
// The scope is the ENTRY — its own fields, the facets folded into it, and what a
// join declared `inChild` brought onto it — because that is what the derivation
// is handed and what the framework will push down under the collection's own
// segment. A root field is not in it, deliberately.
func bindChildComputedSources(m *Model) error {
	for i := range m.Children {
		c := &m.Children[i]
		for j := range c.Computed {
			cc := &c.Computed[j]
			cc.SourceFields = nil
			for _, name := range cc.Sources {
				f, ok := entrySourceField(c, name)
				if !ok {
					return fmt.Errorf("children (%s).computed (%s): %q names no field this "+
						"entry serves — a per-entry derivation reads the entry's own fields, "+
						"the facets folded into it, and what a join declared inChild brought "+
						"onto it", c.Plural, cc.Name, name)
				}
				cc.SourceFields = append(cc.SourceFields, f)
			}
		}
	}
	return nil
}

func entrySourceField(c *Child, name string) (Field, bool) {
	for _, f := range c.Fields {
		if f.Name == name {
			return f, true
		}
	}
	for _, f := range c.JoinFields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// readSourceField resolves ONE derivation source against the read model.
//
// It is not the entity's field list: a framework-stamped column the read
// exposes and a root join's field are both legitimate sources, and neither
// lives there. A collection's field is deliberately absent — the root's Result
// holds a slice of entries, not one entry, so a root derivation has nothing to
// be handed. That question is `children[].computed`, which is answered per
// entry.
func readSourceField(r *ReadModel, owner []Field, name string) (Field, bool) {
	for _, f := range owner {
		if f.Name == name {
			return f, true
		}
	}
	for _, f := range r.Managed {
		if f.Name == name {
			return f, true
		}
	}
	for _, f := range r.JoinFields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

func resolveOps(s *spec.Spec, m *Model) []Operation {
	e := m.Entity.Pascal
	has := func(mode string) bool {
		for _, x := range s.Modes {
			if x == mode {
				return true
			}
		}
		return false
	}
	perm := func(k string) string { return s.Authz.Permissions[k] }

	var ops []Operation

	if has("insert") {
		// A role's insert is an UPSERT: the identity may already exist because
		// another role created it. That is why it maps through ApplyTo (which
		// the handler may run twice, so it must stay pure) instead of building a
		// fresh entity, and why it rides its own handler.
		inputMethod, handler := "ToEntity", "handlers.InsertCommandHandler"
		if s.Storage.Kind == "sharedbase-role" {
			inputMethod, handler = "ApplyTo", "handlers.SharedBaseInsertCommandHandler"
		}
		ops = append(ops, Operation{
			Verb: "insert", Method: "fiber.MethodPost", Path: "/", Permission: perm("insert"),
			Status: "fiber.StatusCreated", Write: true,
			CommandType: "Insert" + e + "Command", ResultType: "Insert" + e + "Result",
			CommandBase: "pipeline.CommandWithBodyBase", InputMethod: inputMethod,
			HandlerType: handler,
			RequestType: "Insert" + e + "Request", ResponseType: "Insert" + e + "Response",
			Summary: "Create " + article(e) + " " + strings.ToLower(e),
		})
	}
	if has("update") {
		switch s.Update.Shape {
		case "put", "both":
			ops = append(ops, putOp(e, perm("update")))
		}
		switch s.Update.Shape {
		case "patch", "both":
			ops = append(ops, patchOp(e, perm("patch")))
		}
	}
	if has("delete") {
		ops = append(ops, byIDWriteOp("delete", e, "fiber.MethodDelete", "/:id",
			perm("delete"), "handlers.DeleteCommandHandler",
			"Permanently delete "+article(e)+" "+strings.ToLower(e)))
	}
	if has("archive") {
		// "(reversible)" is a claim about ANOTHER endpoint, so it may only be made
		// when that endpoint exists. Archiving without unarchive is a legitimate
		// model — the row survives as history and nothing brings it back — and
		// promising an undo the spec never asked for documented a route the
		// generator did not write.
		summary := "Archive " + article(e) + " " + strings.ToLower(e)
		if has("unarchive") {
			summary += " (reversible)"
		}
		ops = append(ops, byIDWriteOp("archive", e, "fiber.MethodPatch", "/:id/archive",
			perm("archive"), "handlers.ArchiveCommandHandler", summary))
	}
	if has("unarchive") {
		ops = append(ops, byIDWriteOp("unarchive", e, "fiber.MethodPatch", "/:id/unarchive",
			perm("unarchive"), "handlers.UnarchiveCommandHandler",
			"Restore an archived "+strings.ToLower(e)))
	}
	if m.Read.ByParams {
		ops = append(ops, Operation{
			Verb: "byParams", Method: "fiber.MethodGet", Path: "/", Permission: perm("read"),
			RequestType:  "Find" + m.Entity.PluralPascal + "Request",
			ResponseType: "Find" + m.Entity.PluralPascal + "Response",
			HandlerType:  "handlers.FindByParamsQueryHandler",
			Summary:      "List " + strings.ToLower(m.Entity.PluralPascal),
		})
	}
	if m.Read.ByID {
		ops = append(ops, Operation{
			Verb: "byId", Method: "fiber.MethodGet", Path: "/:id", Permission: perm("read"),
			RequestType:  "Find" + e + "ByIDRequest",
			ResponseType: "Find" + e + "ByIDResponse",
			HandlerType:  "handlers.FindByIDQueryHandler",
			Summary:      "Get " + article(e) + " " + strings.ToLower(e) + " by id",
		})
	}
	return ops
}

func putOp(e, perm string) Operation {
	return Operation{
		Verb: "update", Method: "fiber.MethodPut", Path: "/:id", Permission: perm,
		Status: "fiber.StatusOK", Write: true,
		CommandType: "Update" + e + "Command", ResultType: "Update" + e + "Result",
		CommandBase: "pipeline.CommandWithBodyIDBase", InputMethod: "ApplyTo",
		HandlerType: "handlers.UpdateCommandHandler",
		RequestType: "Update" + e + "Request", ResponseType: "Update" + e + "Response",
		Summary: "Replace " + article(e) + " " + strings.ToLower(e) + " (full body)",
	}
}

func patchOp(e, perm string) Operation {
	return Operation{
		Verb: "patch", Method: "fiber.MethodPatch", Path: "/:id", Permission: perm,
		Status: "fiber.StatusOK", Write: true,
		CommandType: "Patch" + e + "Command", ResultType: "Patch" + e + "Result",
		CommandBase: "pipeline.CommandWithBodyIDBase", InputMethod: "ApplyPartiallyTo",
		HandlerType: "handlers.PartialUpdateCommandHandler",
		RequestType: "Patch" + e + "Request", ResponseType: "Patch" + e + "Response",
		Summary: "Update " + article(e) + " " + strings.ToLower(e) + " (partial)",
	}
}

// byIDWriteOp covers the three bodyless verbs. They answer 204: there is
// nothing to return, and returning 200 with an empty body only invites callers
// to look for content that is never there.
func byIDWriteOp(verb, e, method, path, perm, handler, summary string) Operation {
	return Operation{
		Verb: verb, Method: method, Path: path, Permission: perm,
		Status: "fiber.StatusNoContent", Write: true, Bodyless: true,
		CommandType: naming.Pascal(verb) + e + "Command", ResultType: "fwresults.None",
		CommandBase: "pipeline.CommandByIDBase",
		HandlerType: handler,
		Summary:     summary,
	}
}

func resolveConstraints(s *spec.Spec, m *Model) []Constraint {
	out := []Constraint{{
		Kind: "primary-key", Table: m.Table, Columns: []string{"id"},
		Notification: "domain.EntityAlreadyAddedNotification{}", Field: "id",
	}}
	for i, f := range m.Fields {
		if f.Unique == nil {
			continue
		}
		// An ordinary field constrains its own column; a composite constrains the
		// TUPLE, so the constraint gathers the whole run of parts. The key the
		// violation is reported under, and the notification's field, then name the
		// value object rather than whichever part the database happened to
		// mention.
		cols := []string{f.Column}
		field := f.JSONName
		if f.Composite != nil {
			cols = compositeRunColumns(m.Fields, i)
			field = naming.Camel(f.Composite.Owner)
		}
		// `within` scopes the uniqueness — "unique per tenant" — so its columns
		// lead the index. They lead rather than trail because that is also the
		// prefix every scoped lookup of this table uses, and an index the
		// scope's own queries can ride is free.
		//
		// Validation holds these columns and the pre-check fact's filters to the
		// same list, in both directions: the two used to be able to disagree,
		// and what that shipped was a global index behind a per-tenant check.
		scope := make([]string, 0, len(f.Unique.Within))
		for _, name := range f.Unique.Within {
			if within := lookupField(m, name); within != nil {
				scope = append(scope, within.Column)
			}
		}
		out = append(out, Constraint{
			Kind: "unique", Table: m.Table, Columns: append(scope, cols...),
			Notification: f.Unique.Notification + "{}", Field: field,
			Scope: f.Unique.Scope, Within: f.Unique.Within,
			Archived: m.Managed.ArchivedAt,
		})
	}
	return append(out, childConstraints(m)...)
}

// childConstraints is the uniqueness of a COLLECTION ENTRY.
//
// The index always leads with the parent column, and that is not a default the
// author can change: an entry has no identity outside its owner. "This role
// cannot grant the same permission twice" is a statement about one role, and the
// same permission under a different role is a different, entirely legitimate
// row. It is also what makes this the backstop for businessIdentity, which is
// defined per owner too — and businessIdentity is an in-process check over one
// write, so it cannot see the concurrent write that this index refuses.
func childConstraints(m *Model) []Constraint {
	var out []Constraint
	for _, c := range m.Children {
		if c.Mounted {
			// The collection belongs to the spec that DECLARES it; this run only
			// mounts a surface over it. Emitting its index here would create the
			// same constraint twice, from two migrations.
			continue
		}
		for _, f := range c.Fields {
			if f.Unique == nil {
				continue
			}
			cols := []string{c.ParentColumn}
			for _, name := range f.Unique.Within {
				for _, other := range c.Fields {
					if other.Name == name {
						cols = append(cols, other.Column)
					}
				}
			}
			out = append(out, Constraint{
				Kind: "unique", Table: c.Table, Columns: append(cols, f.Column),
				Notification: f.Unique.Notification + "{}",
				// Reported under the COLLECTION, in its WIRE spelling: this
				// value goes straight into the error envelope's field path, and
				// the caller posted a list under that name. The root's binding
				// carries JSONName for the same reason — the Go field name is
				// what the DOMAIN's own AddNotification takes, one layer down.
				Field:    c.Segment,
				Scope:    f.Unique.Scope,
				Within:   f.Unique.Within,
				Archived: c.ArchivedAt,
				// Not the root's archive column: an entry is freed for reuse by
				// being soft-removed itself.
				Collection: c.Plural,
			})
		}
	}
	return out
}

// compositeRunColumns lists the columns of the composite whose run starts at
// fields[start], in declaration order — the order the constraint, the index name
// and the SQLite violation key must all agree on.
func compositeRunColumns(fields []Field, start int) []string {
	owner := fields[start].Composite.Owner
	var out []string
	for _, f := range fields[start:] {
		if f.Composite == nil || f.Composite.Owner != owner {
			break
		}
		out = append(out, f.Column)
	}
	return out
}

// lookupFactFilter resolves the name a fact narrows by, in the two forms the
// language admits: a root field, and `<Collection>.<Field>` for a question about
// ONE ENTRY of a collection.
//
// The second return is the collection, empty for the first form. It exists so
// the port's documentation can say which entry the answer is about; nothing
// downstream queries by it, because a per-entry filter is only ever on a manual
// fact, whose body is the author's.
func lookupFactFilter(s *spec.Spec, m *Model, name string) (*Field, string) {
	if coll, _, dotted := spec.ChildFactField(s, name); dotted {
		if coll == nil {
			return nil, ""
		}
		for i := range m.Children {
			if m.Children[i].Plural != coll.Plural {
				continue
			}
			for j := range m.Children[i].Fields {
				if m.Children[i].Fields[j].Name == strings.TrimPrefix(name, coll.Plural+".") {
					return &m.Children[i].Fields[j], coll.Plural
				}
			}
		}
		return nil, ""
	}
	return lookupField(m, name), ""
}

func lookupField(m *Model, name string) *Field {
	for i := range m.Fields {
		if m.Fields[i].Name == name {
			return &m.Fields[i]
		}
	}
	for i := range m.Runtime {
		if m.Runtime[i].Name == name {
			return &m.Runtime[i]
		}
	}
	// A facet's fields are the entity's fields living in another table. The rule
	// emitters never learn the difference — they write against the same receiver
	// — so the lookup must not either.
	for i := range m.Siblings {
		if m.Siblings[i].OwnerChild != "" {
			continue // that one belongs to a child, and a child looks it up itself
		}
		for j := range m.Siblings[i].Fields {
			if m.Siblings[i].Fields[j].Name == name {
				return &m.Siblings[i].Fields[j]
			}
		}
	}
	for i := range m.Read.Managed {
		if m.Read.Managed[i].Name == name {
			return &m.Read.Managed[i]
		}
	}
	return compositeWhole(m, name)
}

// managedNamed finds a framework-stamped read field by its logical name.
func managedNamed(managed []Field, name string) *Field {
	for i := range managed {
		if managed[i].Name == name {
			return &managed[i]
		}
	}
	return nil
}

// identityReadField renders the aggregate id as the query leaf a listing filters
// or orders by.
//
// Every type here is the WIRE type, string, and none of them is the aggregate's:
// there is no Go field on the entity for the id — the framework's managed
// carrier holds it — and the only emitter that reads this leaf is the one
// writing the request struct, where the framework binds `?id=` and `?orderBy=id`
// out of the query string. That is the same shape a hand-written service
// declares, `ID *string ` + "`" + `query:"id" sort:"asc,desc"` + "`" + `, and the framework resolves
// the name against the root's TableSchema.ID for both questions.
func identityReadField(m *Model, name string) *Field {
	if name != spec.IdentityName {
		return nil
	}
	return &Field{
		Name: spec.IdentityName, Column: "id", SpecType: "id",
		GoType: "string", BaseGoType: "string",
		EntityType: "string", BaseEntityType: "string",
		JSONName: "id", LabelKey: m.Entity.Pascal + "IDField",
		Example:     "7b3c1f10-3c7e-4a8d-9f0e-9d2a8e6d4b51",
		Description: "The row's identity, minted by the framework.",
	}
}

// managedReadField renders a framework-stamped column as the field every read
// emitter already knows how to write.
//
// The framework resolves these names itself — its schema maps CreatedAt,
// UpdatedAt and DeletedAt to their columns on the read path — so a filter or a
// projection naming one needs nothing beyond the Go field being there under the
// same name. DeletedAt is the only optional one: a row that was never archived
// has none.
func managedReadField(s *spec.Spec, m *Model, name string) Field {
	nullable := name == "DeletedAt"
	goType := "time.Time"
	if nullable {
		goType = "*time.Time"
	}
	return Field{
		Name: name, Column: spec.ManagedColumn(s, name), SpecType: "time",
		GoType: goType, BaseGoType: "time.Time",
		EntityType: goType, BaseEntityType: "time.Time",
		Nullable: nullable, JSONName: naming.Camel(name),
		LabelKey: m.Entity.Pascal + name + "Field",
		Example:  "2026-02-01T09:00:00Z",
		Description: "Stamped by the framework: " + map[string]string{
			"CreatedAt": "when the row was inserted.",
			"UpdatedAt": "when the row was last written.",
			"DeletedAt": "when the row was archived, when it was.",
		}[name],
	}
}

// compositeWhole answers for a composite value object addressed AS A WHOLE.
//
// m.Fields holds the expanded parts, because that is what the table, the wire
// and every read consumer see. The one thing that does not is the aggregate: it
// carries the concept, one field of the value object's type. A rule that
// compares the VALUE — immutability is the only one the language allows there —
// needs that field and finds nothing under its name, so it is synthesised here
// from what the parts already record about their owner.
//
// Validation decides WHICH rules may reach this: anything that would emit an
// operator no struct answers to (a range, a length) is refused before it gets
// here, and refused with the reason.
func compositeWhole(m *Model, name string) *Field {
	for _, f := range m.AllOwnerFields() {
		if f.Composite == nil || f.Composite.Owner != name {
			continue
		}
		c := f.Composite
		whole := Field{
			Name: c.Owner, GoType: c.VOType, BaseGoType: c.VOType,
			EntityType: c.VOType, BaseEntityType: c.VOType,
			Nullable: c.OwnerNullable, LabelKey: c.OwnerLabelKey, Text: c.OwnerText,
			JSONName: naming.Camel(c.Owner), Description: c.OwnerDescription,
			LivesOn: f.LivesOn,
		}
		return &whole
	}
	return nil
}

// article picks a/an so generated OpenAPI summaries read like English rather
// than like a template.
func article(word string) string {
	if word == "" {
		return "a"
	}
	switch strings.ToLower(word)[0] {
	case 'a', 'e', 'i', 'o', 'u':
		return "an"
	}
	return "a"
}

// Op finds a mounted operation by verb.
func (m *Model) Op(verb string) *Operation {
	for i := range m.Ops {
		if m.Ops[i].Verb == verb {
			return &m.Ops[i]
		}
	}
	return nil
}

// WriteOps returns the mounted write operations, in mount order.
func (m *Model) WriteOps() []Operation {
	var out []Operation
	for _, o := range m.Ops {
		if o.Write {
			out = append(out, o)
		}
	}
	return out
}

// ImportPath builds a package path inside the consumer's module.
func (m *Model) ImportPath(rel string) string {
	return fmt.Sprintf("%s/%s", m.Module, rel)
}

// ValueObject is a resolved value object, ready to emit.
type ValueObject struct {
	Name         string
	Kind         string // raw | enum
	Backing      string // string | int
	GoBacking    string
	Description  string
	Regex        string
	MinLength    int
	MaxLength    int
	Min, Max     *float64
	Notification string

	Members             []EnumMember
	UnknownName         string
	UnknownValue        string
	UnknownNotification string

	// Parts and Rules are the composite half: what the value object is made of,
	// and the invariants its own IsValid checks over them.
	Parts []VOPart
	Rules []Rule

	// Written is whose file the type is: "" or "generated" for the generator's,
	// "manual" for the author's. It is carried rather than folded into Kind
	// because a composite keeps its whole declared shape either way — only the
	// file moves.
	Written string
}

// GeneratedValueObjects counts the value objects this run actually writes a file
// for. It is the number the test emitter asks about: a model whose value objects
// are all hand-written has nothing for a generated test to assert.
func (m *Model) GeneratedValueObjects() int {
	n := 0
	for _, vo := range m.ValueObjects {
		if !vo.HandWritten() {
			n++
		}
	}
	return n
}

// HandWritten reports that the AUTHOR writes this type, not the generator —
// either because its shape is beyond the language (kind: manual) or because its
// shape is declared and only the file was handed over (a composite with
// written: manual). Everything downstream asks the same question of both: emit
// no file, assert nothing about a rule it does not know, and ask for the type in
// the report.
//
// It is derived rather than stored so the two spellings cannot disagree with a
// flag somebody forgot to set.
func (v ValueObject) HandWritten() bool {
	return v.Kind == "manual" || (v.IsComposite() && v.Written == "manual")
}

// IsComposite reports whether the value object's value spans several fields —
// the kind that declares no Value() and is decomposed by the schema.
func (v ValueObject) IsComposite() bool { return v.Kind == "composite" }

// EnumMember is one declared value of an enum value object.
type EnumMember struct {
	ConstName string
	Literal   string
	Name      string
	// DescriptionKey is the catalog key the FRAMEWORK derives for this member,
	// and the reason the member's text has anywhere to go:
	// domain.EnumDescriptionKey reflects over the value and answers
	// "<TypeName>.<value>" — "NivelContrato.1", "SituacaoCurso.aberto". So the
	// key is built from the VALUE, never from the member's Go name, and a
	// catalog entry filed under anything else is one Translator.EnumDescription
	// will never find.
	DescriptionKey string
	// Text is the member's human-facing text per catalog, and Missing names the
	// catalogs the spec left empty.
	//
	// This is a LABEL, with the same discipline as a field's: a couple of words,
	// and an empty catalog falls back to the member's own name spaced out rather
	// than to a loud TODO. A notification's text is a sentence nobody can guess,
	// which is why that one gets a placeholder; "Aberto" is a heading, and a
	// placeholder in its place is what the end user reads.
	Text    map[string]string
	Missing []string
}

func resolveValueObjects(s *spec.Spec) []ValueObject {
	var out []ValueObject
	for _, vo := range s.ValueObjects {
		backing := "string"
		if vo.Backing == "int" {
			backing = "int"
		}
		v := ValueObject{
			Name: vo.Name, Kind: vo.Kind, Backing: vo.Backing, GoBacking: backing,
			Description: vo.Description, Regex: vo.Regex,
			MinLength: vo.MinLength, MaxLength: vo.MaxLength,
			Min: vo.Min, Max: vo.Max,
			// QUALIFIED here, not at the emission site. A notification the
			// service declares is generated INTO the vos package, so it is
			// referenced bare; a FRAMEWORK one lives in the framework's own
			// domain package, which the generated file already imports. Emitting
			// the bare name for a framework notification produced a file that
			// referenced an identifier nothing in its package declares — and the
			// generator's own refusal message offers exactly those names as a
			// legitimate choice, so following the advice produced a tree that
			// did not compile.
			Notification:        qualifyVONotification(vo.Notification),
			UnknownNotification: qualifyVONotification(vo.UnknownNotification),
			Written:             vo.Written,
		}
		if vo.Kind == "composite" {
			resolveCompositeDeclaration(s, vo, &v)
		}
		if vo.Kind == "enum" {
			v.UnknownName = vo.Name + "Unknown"
			// The zero value IS the unknown sentinel: an out-of-set value lands
			// here rather than silently passing as a member.
			if backing == "int" {
				v.UnknownValue = "0"
			} else {
				v.UnknownValue = `""`
			}
			for _, mem := range vo.Members {
				texts := mem.Text.Map()
				var missing []string
				for _, code := range LangOrder {
					if strings.TrimSpace(texts[code]) == "" {
						missing = append(missing, code)
					}
				}
				v.Members = append(v.Members, EnumMember{
					ConstName: vo.Name + mem.Name,
					Literal:   enumLiteral(mem.Value, backing),
					Name:      mem.Name,
					// The framework builds this key by reflection over the
					// value, so it is built here the same way — from the value.
					DescriptionKey: fmt.Sprintf("%s.%v", vo.Name, mem.Value),
					Text:           texts,
					Missing:        missing,
				})
			}
		}
		out = append(out, v)
	}
	return out
}

// enumLiteral renders a member's value in the backing type. Quoting an int, or
// leaving a string bare, produces code that does not compile.
func enumLiteral(v any, backing string) string {
	if backing == "int" {
		return fmt.Sprint(v)
	}
	return fmt.Sprintf("%q", fmt.Sprint(v))
}

// ServiceModel is the resolved domain-service port.
type ServiceModel struct {
	Impl  string
	Facts []Fact
}

// Fact is one question the port answers.
type Fact struct {
	Name string
	// Manual marks a fact this generator will NOT answer: the port declares it,
	// the body is a stub in a file regeneration never touches, and the compiler
	// refuses to build until a human writes it. That is deliberate — a missing
	// method fails loudly, and a query against the wrong store compiles, returns
	// and means nothing.
	Manual bool
	Kind   string
	Field  string
	// ReturnType is the VALUE the fact answers with. It is decided by the kind
	// and the aggregated field together, never by the field alone: an average
	// over an integer column is fractional, and a sum over any integer width is
	// exact int64.
	ReturnType string
	// ReturnsFound makes the port answer (value, found) instead of value alone.
	//
	// It is on for min, max and avg, and off for sum, count and exists, because
	// that is where the empty set is ambiguous. SQL answers NULL over no rows,
	// which the framework's carriers surface as Found=false with a zero Value:
	// for a sum, zero IS the empty sum; for a minimum, a maximum or an average,
	// zero is indistinguishable from a real one. Returning it alone hands a rule
	// a number nobody computed — quietly, which is why this is in the signature
	// rather than in a comment.
	ReturnsFound bool
	// GroupKeys turns the fact into a per-group one. Non-empty means the answer
	// is a slice of GroupType, one entry per distinct key combination, computed
	// by the database in a single GROUP BY.
	GroupKeys []FactParam
	// GroupType is the generated struct one group comes back as. It lives in the
	// domain package beside the port, because the port cannot speak in the
	// framework's own *read.Group without dragging infra into the domain.
	GroupType   string
	ActiveOnly  bool
	Description string
	Params      []FactParam
}

// Grouped reports whether this fact answers per group.
func (f Fact) Grouped() bool { return len(f.GroupKeys) > 0 }

// FactParam is one argument the rule passes in.
type FactParam struct {
	Name   string
	GoType string
	Field  string
	Role   string // "" | exclude-self
	// PerEntry names the COLLECTION this parameter's field belongs to, empty
	// for the root's own fields. It is what lets the port's doc comment say
	// which entry the question is about — the whole point of a per-entry fact
	// is that it is asked once per entry, not once per write.
	PerEntry string
}

func resolveService(s *spec.Spec, m *Model) *ServiceModel {
	if s.Service == nil || !s.Service.Required {
		return nil
	}
	sm := &ServiceModel{
		Impl: m.Entity.Pascal + "ServiceImpl",
	}
	for _, f := range s.Service.Facts {
		fact := Fact{
			Name: f.Name, Kind: f.Kind, Field: f.Field,
			Manual:     f.Kind == "manual",
			ActiveOnly: f.ActiveOnly, Description: f.Description,
			ReturnType:   factReturnType(f, m),
			ReturnsFound: factReturnsFound(f.Kind),
		}
		for _, g := range f.GroupBy {
			fld := lookupField(m, g)
			if fld == nil {
				continue
			}
			// The key is carried as TEXT, always. The framework normalises a key
			// to a driver-neutral Go value and hands it over as `any`; rendering
			// it is the one reading that cannot fail on either backend, and a
			// group key is read to be compared or reported, not to be summed.
			fact.GroupKeys = append(fact.GroupKeys, FactParam{
				Name: fld.Name, GoType: "string", Field: fld.Name,
			})
		}
		if fact.Grouped() {
			fact.GroupType = m.Entity.Pascal + f.Name + "Group"
			// Every group EXISTS because a row matched, so there is nothing
			// ambiguous left for Found to report: an empty set is zero groups.
			fact.ReturnsFound = false
		}
		for _, filter := range f.Filters {
			fld, collection := lookupFactFilter(s, m, filter)
			if fld == nil {
				// Validation resolves every filter and refuses the ones that
				// resolve to nothing, so reaching here is generator
				// inconsistency. It used to be a silent `continue`, and what
				// that shipped was a port method with no parameter — a question
				// the rule could not ask, discovered three steps downstream.
				panic("service fact " + f.Name + ": the filter " + filter +
					" resolves to no field — validation should have refused this spec")
			}
			fact.Params = append(fact.Params, FactParam{
				Name: naming.Camel(fld.Name), GoType: fld.BaseGoType, Field: fld.Name,
				PerEntry: collection,
			})
		}
		if f.ExcludeSelf {
			// The row being updated must not count against itself, or every
			// update of a unique field would report a duplicate of itself.
			fact.Params = append(fact.Params, FactParam{
				Name: "selfID", GoType: "domain.ID", Role: "exclude-self",
			})
		}
		sm.Facts = append(sm.Facts, fact)
	}
	return sm
}

func factReturnType(f spec.Fact, m *Model) string {
	if f.Kind == "manual" {
		return f.Returns
	}
	switch f.Kind {
	case "exists":
		return "bool"
	case "count":
		return "int64"
	case "avg":
		// An average is fractional even over an integer column, which is what
		// the framework offers: Avg returns float64 and there is no AvgInt. The
		// field's own width says nothing about the answer's.
		return "float64"
	default:
		// sum, min, max: EXACT over any integer width, fractional otherwise.
		// int and int64 both land on the Int carriers, whose Value is int64 —
		// the sum of ints does not fit an int by rights, and narrowing the
		// answer to the column's width is how a total silently wraps.
		if fld := lookupField(m, f.Field); fld != nil {
			switch fld.BaseGoType {
			case "int", "int64":
				return "int64"
			}
		}
		return "float64"
	}
}

// factReturnsFound reports whether the empty set is ambiguous for this kind.
//
// exists and count answer for themselves (false, 0). A sum over nothing is 0 by
// definition. A minimum, a maximum or an average over nothing is NOT zero — SQL
// says NULL — so those three carry the second return.
func factReturnsFound(kind string) bool {
	switch kind {
	case "min", "max", "avg":
		return true
	}
	return false
}

// appendUniqueClauses turns a unique field with a service pre-check into a real
// rule.
//
// It matters for the USER, not for correctness of storage: the database unique
// index already refuses a duplicate, but it does so alone and only after every
// other error is fixed. The pre-check lets the duplicate be reported TOGETHER
// with the rest, in one response. The index stays as the backstop for the race
// between the check and the commit.
func appendUniqueClauses(m *Model) []Clause {
	if m.Service == nil {
		return m.Clauses
	}
	var extra []Rule
	for _, f := range m.Fields {
		if f.Unique == nil || f.Unique.Enforce != "service-precheck+constraint" {
			continue
		}
		fact := factFor(m, f.Name)
		if fact == nil {
			continue
		}
		extra = append(extra, Rule{
			ID:   "unique-" + f.Name,
			Kind: "uniquePrecheck",
			// The synthesised rules have no spec entry to carry echoValue, so the
			// default rules.list gets from an absent key is applied here instead.
			// This one earns it more than most: "that handle is taken" is a
			// different message from "administrator is taken", and the caller
			// picked the word.
			EchoValue:    true,
			Fields:       []Field{f},
			Notification: f.Unique.Notification,
			AttachTo:     f.Name,
			Fact:         fact,
		})
	}
	if len(extra) == 0 {
		return m.Clauses
	}
	// The check runs on insert AND update: a rename can collide just as a
	// creation can.
	for i := range m.Clauses {
		if m.Clauses[i].Gate == "IfInsertOrUpdate" {
			m.Clauses[i].Rules = append(m.Clauses[i].Rules, extra...)
			return m.Clauses
		}
	}
	return append(m.Clauses, Clause{Gate: "IfInsertOrUpdate", Rules: extra})
}

// bindFacts resolves the fact a factRange names, now that the port exists.
//
// A rule whose fact cannot be found is dropped rather than emitted half-way:
// validation refuses that spec, so reaching here with an unknown name means the
// model is already being reported as invalid, and emitting a call to a method
// nobody declared would replace a readable blocker with a compile error.
func bindFacts(m *Model) {
	if m.Service == nil {
		return
	}
	for ci := range m.Clauses {
		for ri := range m.Clauses[ci].Rules {
			r := &m.Clauses[ci].Rules[ri]
			if r.Kind != "factRange" || r.FactName == "" {
				continue
			}
			for i := range m.Service.Facts {
				if m.Service.Facts[i].Name == r.FactName {
					r.Fact = &m.Service.Facts[i]
					break
				}
			}
		}
	}
}

// factFor finds the existence probe that answers for a field.
func factFor(m *Model, field string) *Fact {
	for i := range m.Service.Facts {
		f := &m.Service.Facts[i]
		if f.Kind != "exists" {
			continue
		}
		for _, p := range f.Params {
			if p.Field == field {
				return f
			}
		}
	}
	return nil
}

// Child is a 1:N collection the aggregate owns.
//
// It is an AGGREGATE VALUE OBJECT, not an entity: it has no life of its own and
// is only ever reached through the root. The root does NOT declare a slice for
// it — the framework keeps children in its own collection, and a slice field
// would stay empty on read and be ignored on write.
type Child struct {
	Name string
	// ParentColumn is the foreign key back to the owner, as declared.
	ParentColumn string
	// Plural is the declared COLLECTION NAME — what the AVO returns from
	// CollectionName(). It is one name with three consumers, and they are
	// spelled differently: the document stores it verbatim (DocSegment), the
	// wire lower-camels it (Segment), and the Go field IS it (GoPlural).
	Plural string
	// GoPlural is the Go field name of the collection on the read DTO. It must
	// equal the declared name exactly: the framework resolves the document
	// segment from CollectionName and matches it against this field, so a field
	// named anything else maps to a key the document does not have.
	GoPlural    string
	Table       string
	Description string
	OwnedBy     string
	Fields      []Field
	// JoinFields are what a read join declared IN this collection brings back:
	// ordinary fields of the entry, filled on every load, served inside the
	// entry — and never filterable or sortable, because narrowing the root by a
	// field of a 1:N collection is a pushdown one root SELECT cannot express.
	JoinFields []Field
	// Computed are the entry's DERIVED read fields — the per-entry twin of
	// ReadModel.Computed, filled once per entry by a hook the author owns.
	//
	// They live on the collection rather than on the read model because that is
	// where their scope is: the derivation is handed ONE entry, so its sources
	// are the entry's own fields and nothing above them. A root derivation
	// cannot stand in — what the root holds for a collection is a slice.
	Computed   []ComputedField
	Identity   []Field // the business-identity subset
	ArchivedAt string
	InputType  string
	AddMethod  string
	// PerChild says the collection is edited one entry at a time: its own
	// endpoints, its own commands, and a 404 when the entry is not there. The
	// alternative — atomic-replace — has no entry to address, because the root's
	// update swaps the whole collection.
	PerChild bool
	// MountsAdd, MountsChange and MountsRemove are WHICH of the per-entry verbs
	// this collection mounts — children[].operations, defaulted to all three.
	//
	// They are three fields rather than a list because every consumer asks about
	// one verb at a time: the routes, the commands, the wire types, the domain
	// methods and the generated tests each emit per verb, and a verb nobody
	// mounts must leave no trace in any of them.
	MountsAdd    bool
	MountsChange bool
	MountsRemove bool
	// Permissions is what each MOUNTED per-entry verb requires, keyed add,
	// change, remove — already resolved, so no consumer repeats the fallback.
	//
	// The value is the collection's own children[].permissions entry when it
	// declares one, and the root's update permission otherwise. Resolving it
	// here rather than at the route is what keeps the report and the emitted
	// guard from ever disagreeing about which permission a route actually got:
	// the reviewer's copy and the running one come from the same map.
	Permissions map[string]string
	// Declared is the subset of Permissions the SPEC asked for, as opposed to
	// inherited. The emitters do not care — a permission is a string either way
	// — but the report does: "inherited from the root's update" and "gated on
	// its own" are the two answers a reviewer is checking between, and a
	// resolved map alone cannot tell them apart, since a collection may
	// deliberately declare the very value it would have inherited.
	Declared     map[string]bool
	ChangeMethod string
	RemoveMethod string
	// DuplicateNotification is what an ADD raises when the entry is already
	// there by business identity. It only exists per-child: an atomic replace
	// has nothing to collide with.
	// Mounted says this collection belongs to a shared identity that ANOTHER
	// role already declared: this spec exposes it, it does not create it. The
	// table, the schema, the entry type and its input DTO all exist already —
	// what is missing on this side is the surface, which is what a role that
	// cannot reach the identity's collection is really missing.
	Mounted bool
	// OpBase names the types THIS spec generates for the collection's per-entry
	// operations. It is the collection's own name when the entity owns it, and
	// the entity's name in front of it when the collection is mounted: two roles
	// over one identity generate two sets of commands into one Go package, and
	// AddPhotoCommand can only mean one of them.
	//
	// The shapes that do NOT depend on the owner — the entry type, its input DTO,
	// its result and its wire rows — keep the collection's own name and are
	// generated once, by the role that declares it.
	OpBase string
	// Projector is the function that reads the collection back off THIS entity.
	// It takes the owner's type, so unlike the entry shapes it cannot be shared
	// between two roles over one identity — it is qualified for the same reason
	// OpBase is.
	Projector string

	DuplicateNotification string
	Clauses               []Clause
	// ManualRules are the invariants declared on THIS collection that the
	// language could not express. They were parsed and then dropped on the
	// floor: no hook, no report line, no trace. Now they reach a hook of the
	// child's own, called from its BuildRules.
	ManualRules []ManualRule
	HasHookFile bool
	Segment     string // the WIRE name of the collection
	DocSegment  string // the key the projection actually stores it under
}

// Sibling is a 1:1 facet stored in its own table, sharing the owner's key.
//
// There is no Go type for it: its fields live on the OWNER as pointers, and the
// split is purely physical. An all-nil facet means no row.
type Sibling struct {
	Name string
	// Description is what the facet holds, in one line. It reaches the facet
	// table's own comment in the database.
	Description string
	Table       string
	AttachTo    string
	// OwnerChild is "" when the facet hangs off the entity's own table, or the
	// child's NAME when it lives inside that child. It is AttachTo resolved to
	// the node whose schema must declare it — the framework panics if a facet is
	// declared over a type other than its owner's.
	OwnerChild string
	Fields     []Field
}

// UpdatePermission is what editing the aggregate requires.
//
// It is the permission a per-entry verb INHERITS when its collection declares
// none, and the one the GraphQL facet-clear mutation requires outright: both
// are the root being edited through a narrower door, so both ask for what
// replacing the whole of it asks for.
//
// The order is PUT, then PATCH, then insert. A spec serving both shapes gives
// them the same permission anyway, and insert is last because a write-only
// entity has no update to borrow from.
func (m *Model) UpdatePermission() string {
	for _, verb := range []string{"update", "patch", "insert"} {
		if op := m.Op(verb); op != nil && op.Permission != "" {
			return op.Permission
		}
	}
	return ""
}

// resolveChildPermissions decides, once, what each mounted per-entry verb
// requires.
//
// The default is inheritance and stays inheritance: a collection that declares
// nothing keeps requiring the root's update permission, which is what every
// per-child collection has required since the verbs existed. Re-gating those
// routes behind something new would refuse callers holding exactly what they
// were granted, on a regeneration that changed no key.
//
// What children[].permissions buys is the case where the collection edge is a
// different job from editing the root — a role assignment, a grant — and one
// permission for both is how an administrator ends up able to widen their own.
func resolveChildPermissions(s *spec.Spec, m *Model) {
	inherited := m.UpdatePermission()
	for i := range m.Children {
		if !m.Children[i].PerChild {
			continue
		}
		perms := map[string]string{}
		declared := map[string]bool{}
		for _, op := range spec.PerChildOperations(s.Children[i]) {
			if own := spec.PerChildPermission(s.Children[i], op); own != "" {
				perms[op] = own
				declared[op] = true
				continue
			}
			perms[op] = inherited
		}
		m.Children[i].Permissions = perms
		m.Children[i].Declared = declared
	}
}

func resolveChildren(s *spec.Spec, m *Model) []Child {
	var out []Child
	for _, c := range s.Children {
		ch := Child{
			Name: c.Name, Plural: c.Plural, GoPlural: c.Plural, Table: c.Table,
			ParentColumn: c.ParentColumn,
			Description:  c.Description, OwnedBy: c.OwnedBy,
			ArchivedAt: c.ArchivedAt,
			InputType:  c.Name + "Input", AddMethod: "Add" + c.Name,
			Mounted:               c.OwnedBy == "base" && s.Storage.Base != nil && s.Storage.Base.Reuse,
			OpBase:                c.Name,
			PerChild:              c.EditStrategy == "per-child",
			MountsAdd:             spec.MountsPerChildOp(c, "add"),
			MountsChange:          spec.MountsPerChildOp(c, "change"),
			MountsRemove:          spec.MountsPerChildOp(c, "remove"),
			ChangeMethod:          "Change" + c.Name + "ByID",
			RemoveMethod:          "Remove" + c.Name + "ByID",
			DuplicateNotification: c.DuplicateNotification,
			// One name, three consumers: the document segment IS the declared
			// collection name, the wire path is its lower-camel, and the read
			// DTO's field must be the name itself.
			Segment:    naming.Camel(c.Plural),
			DocSegment: c.Plural,
		}
		// Qualified by the ENTITY, always. Every entity's commands land in one
		// Go package, so a projector named from the plural alone is a name two
		// entities can both claim — and in an RBAC service they do: Group has
		// Roles and User has Roles, which is the domain rather than a naming
		// accident, and the second one generated is `projectRoles redeclared in
		// this block`. The per-entry projector beside it was already qualified
		// (projectOneUserRole), so the collision was the collection projector
		// alone, and only until a second entity reused a plural.
		ch.Projector = "project" + m.Entity.Pascal + ch.GoPlural
		if ch.Mounted {
			ch.OpBase = m.Entity.Pascal + c.Name
		}
		for _, f := range c.Fields {
			if spec.IsComposite(f) {
				ch.Fields = append(ch.Fields, expandComposite(s, c.Name, f)...)
				continue
			}
			ch.Fields = append(ch.Fields, resolveField(c.Name, f))
		}
		// The SHAPE only: the sources are bound later, once the joins have hung
		// their fields on the entry, because a join declared inChild is a
		// legitimate source and does not exist yet at this point.
		for _, cc := range c.Computed {
			base := goTypeOf(cc.Type)
			ch.Computed = append(ch.Computed, ComputedField{
				Name: cc.Name, GoType: base, BaseGoType: base,
				JSONName: naming.Camel(cc.Name), LabelKey: cc.LabelKey, Text: cc.Text.Map(),
				Example: cc.Example, Description: cc.Description,
				Sources: append([]string(nil), cc.From...),
			})
		}
		for _, id := range c.BusinessIdentity {
			for _, f := range ch.Fields {
				if f.Name == id {
					ch.Identity = append(ch.Identity, f)
				}
			}
		}
		out = append(out, ch)
	}
	return out
}

func resolveSiblings(s *spec.Spec, m *Model) []Sibling {
	var out []Sibling
	for _, sib := range s.Siblings {
		r := Sibling{Name: sib.Name, Description: sib.Description, Table: sib.Table, AttachTo: sib.AttachTo}
		if rest, ok := strings.CutPrefix(sib.AttachTo, "child:"); ok {
			r.OwnerChild = canonicalCollection(s.Children, rest)
		}
		for _, f := range sib.Fields {
			if spec.IsComposite(f) {
				r.Fields = append(r.Fields, expandComposite(s, m.Entity.Pascal, f)...)
				continue
			}
			r.Fields = append(r.Fields, resolveField(m.Entity.Pascal, f))
		}
		out = append(out, r)
	}
	return out
}

// resolveClausesFor compiles a rule set against an arbitrary field scope, so a
// child's rules are built exactly like the root's.
// needsPreviousVersion are the kinds that compare a record against the way it
// was before this write.
//
// They cannot run where the author declares them. A collection's BuildRules is
// handed ONE entry, and an entry has no previous version: domain.Old is defined
// over Entity, and an aggregate child is not one — the framework exposes the
// prior entries from the ROOT instead, via the aggregate root the ghost carries.
//
// So the rule stays declared where it belongs, on the field it is about, and
// the generator emits it where it can work: at the root, pairing the entries
// that survived with the ones that were there. That is the same treatment a
// notification gets when the package it was declared in cannot hold it.
var needsPreviousVersion = map[string]string{
	"transition": "childTransition",
	"immutable":  "childImmutable",
}

// splitByScope separates what a single entry can decide from what only the
// aggregate can.
func splitByScope(rs spec.Rules) (entry spec.Rules, up spec.Rules) {
	entry.Manual = rs.Manual
	for _, r := range rs.List {
		if _, hoist := needsPreviousVersion[r.Kind]; hoist {
			up.List = append(up.List, r)
			continue
		}
		entry.List = append(entry.List, r)
	}
	return entry, up
}

// hoistToRoot rewrites a collection's rule as a root clause over that
// collection, keeping the field it names so the check still reads the entry.
func hoistToRoot(rs spec.Rules, c *Child) []Clause {
	if len(rs.List) == 0 {
		return nil
	}
	clauses := resolveClausesFor(rs, c.Fields)
	for i := range clauses {
		for j := range clauses[i].Rules {
			r := &clauses[i].Rules[j]
			r.Kind = needsPreviousVersion[r.Kind]
			r.Collection = c.Name
			r.Hoisted = true
			// The notification is addressed to the collection, because that is
			// the path the caller sees: the entry's own field name would resolve
			// against the root, where no such field exists.
			r.AttachTo = c.GoPlural
		}
	}
	return clauses
}

// bindValueObjectRules answers, for every rule that validates a value object IN
// PLACE, the one question the emitters cannot: which of the two calls to write.
//
// A raw value object (a composite and a hand-written one among them, and a plain
// id too — domain.ID writes IsValid, which is how the automatic pass finds it)
// answers for itself. An enum does not write IsValid at all; the framework
// checks its membership, and the call is domain.ValidateEnum. A field declared
// `vo.kind: reuse` says neither — the type lives in the project and this spec
// never described it — so the answer comes from the inventory the discoverer
// read out of internal/domain/vos.
//
// It runs as a pass over the resolved clauses rather than inside the clause
// resolver because that resolver is shared with collections and knows nothing
// about the project; here both halves are in hand, and the scope each rule was
// VALIDATED against is asked for by the same helpers the validator used, so the
// two can never drift into disagreeing about which field a rule names.
func bindValueObjectRules(s *spec.Spec, p *discover.Project, m *Model) {
	var kinds map[string]string
	if p != nil {
		kinds = p.VOKind
	}
	bind := func(clauses []Clause, scope []spec.Field) {
		for i := range clauses {
			for j := range clauses[i].Rules {
				r := &clauses[i].Rules[j]
				if r.Kind != "valueObject" {
					continue
				}
				r.VOEnum = map[string]bool{}
				for _, f := range r.Fields {
					for _, sf := range scope {
						if sf.Name != f.Name {
							continue
						}
						r.VOEnum[f.Name] = spec.VOValidationKind(s, sf, kinds) == "enum"
						break
					}
				}
			}
		}
	}
	bind(m.Clauses, spec.RuleScopeOfRoot(s))
	for i := range m.Children {
		bind(m.Children[i].Clauses, spec.RuleScopeOfChild(s, s.Children[i]))
	}
}

// bindOnlyFields resolves a set-wide rule's restriction against the COLLECTION
// it counts, which is the only scope where that field exists — the root's own
// lookup would find nothing and the restriction would quietly not apply.
func bindOnlyFields(m *Model) {
	for i := range m.Clauses {
		for j := range m.Clauses[i].Rules {
			r := &m.Clauses[i].Rules[j]
			if r.OnlyFieldName == "" || r.Collection == "" {
				continue
			}
			for ci := range m.Children {
				if m.Children[ci].Name != r.Collection {
					continue
				}
				for fi := range m.Children[ci].Fields {
					if m.Children[ci].Fields[fi].Name == r.OnlyFieldName {
						r.OnlyField = &m.Children[ci].Fields[fi]
					}
				}
			}
		}
	}
}

// mergeClauses folds hoisted clauses into the root's, by gate: two clauses on
// the same verb would emit two blocks that run in an order nobody declared.
func mergeClauses(into, extra []Clause) []Clause {
	for _, e := range extra {
		merged := false
		for i := range into {
			if into[i].Gate == e.Gate {
				into[i].Rules = append(into[i].Rules, e.Rules...)
				merged = true
				break
			}
		}
		if !merged {
			into = append(into, e)
		}
	}
	sort.SliceStable(into, func(i, j int) bool {
		return gateOrder[into[i].Gate] < gateOrder[into[j].Gate]
	})
	return into
}

// resolveClausesFor is the collection's entry into the shared resolver: the
// scope is the child's own fields plus any facet declared inside it.
// The nil children are not an omission: the two kinds that name a collection
// ask what the AGGREGATE holds, and validateAggregateWideKinds refuses both
// inside children[] — an entry has no collection in scope to name.
func resolveClausesFor(rs spec.Rules, scope []Field) []Clause {
	return resolveClauseSet(rs, nil, func(n string) *Field {
		for i := range scope {
			if scope[i].Name == n {
				return &scope[i]
			}
		}
		return nil
	})
}

// HasNamedClaimFields reports whether the identity feed reads a claim the AUTHOR
// named — a `source: claim` field, and only that. It is the one case where "the
// claim name comes from the spec" is true, and the emitted comment says so.
//
// Every other identity source asks a framework accessor that owns the claim name
// it consults, whether the field was declared or synthesised for the row scope,
// so none of them is this question.
func (m *Model) HasNamedClaimFields() bool {
	for _, f := range m.ClaimRuntimeFields() {
		if f.IdentitySource == "" {
			return true
		}
	}
	return false
}

// HasPerChild reports whether any collection is edited one entry at a time.
func (m *Model) HasPerChild() bool {
	for _, c := range m.Children {
		if c.PerChild {
			return true
		}
	}
	return false
}

// AllOwnerFields is the root's own columns plus every sibling facet: together
// they form ONE Go struct, split across tables only physically.
func (m *Model) AllOwnerFields() []Field {
	out := append([]Field{}, m.Fields...)
	for _, sib := range m.SiblingsOn("") {
		out = append(out, sib.Fields...)
	}
	return out
}

// ResponseFields are the fields a caller RECEIVES: every owned field except the
// ones declared hidden.
//
// It is deliberately narrower than AllOwnerFields and used in exactly three
// places — the write response, the by-id read response and the listing row.
// Everything else keeps the whole set: the column is still written, the criteria
// still filter and sort on it, the Result the query fills still carries it, and a
// computed read field may still derive from it. Narrowing any of those would turn
// "you do not receive this" into "this does not exist", which is a different
// feature and one the spec has other keys for.
func (m *Model) ResponseFields() []Field {
	all := m.AllOwnerFields()
	out := make([]Field, 0, len(all))
	for _, f := range all {
		if f.Hidden {
			continue
		}
		out = append(out, f)
	}
	return out
}

// Base is the shared identity a role hangs off.
//
// It is deduplicated by a natural key, from which the framework derives the
// identity's primary key deterministically — no read-back, and the same key
// always resolves to the same identity.
type Base struct {
	Table         string
	Description   string
	Reuse         bool
	NaturalKey    string
	Link          string
	RowUniqueness string
	OrphanPolicy  string
	FuncName      string
	LinkColumn    string
	Fields        []Field
	Children      []Child
}

func resolveBase(s *spec.Spec) *Base {
	if s.Storage.Kind != "sharedbase-role" || s.Storage.Base == nil {
		return nil
	}
	b := s.Storage.Base
	orphan := b.OrphanPolicy
	if orphan == "" {
		orphan = "keep"
	}
	return &Base{
		Table: b.Table, Description: b.Description, Reuse: b.Reuse,
		NaturalKey: b.NaturalKey, Link: b.Link, RowUniqueness: b.RowUniqueness,
		OrphanPolicy: orphan,
		FuncName:     b.SchemaFunc,
		LinkColumn:   b.LinkColumn,
	}
}

// IsRole reports whether this entity is a role over a shared identity.
func (m *Model) IsRole() bool { return m.Base != nil }

// BaseFields are the fields stored on the shared identity rather than on the
// role's own table.
func (m *Model) BaseFields() []Field {
	var out []Field
	for _, f := range m.Fields {
		if f.LivesOn == "base" {
			out = append(out, f)
		}
	}
	return out
}

// BaseChildren are the collections the shared IDENTITY owns, not the role.
//
// The distinction is the whole point of declaring it: an address of the person
// survives the role being archived and is seen by every other role over the
// same identity, while a role-owned collection dies with the role. The
// framework expresses that by which schema declares the Child(...) and by which
// table the foreign key points at, so both have to follow the declaration.
func (m *Model) BaseChildren() []Child {
	var out []Child
	for _, c := range m.Children {
		if c.OwnedBy == "base" {
			out = append(out, c)
		}
	}
	return out
}

// RoleChildren are the collections this role owns — the default.
func (m *Model) RoleChildren() []Child {
	var out []Child
	for _, c := range m.Children {
		if c.OwnedBy != "base" {
			out = append(out, c)
		}
	}
	return out
}

// SiblingsOn returns the 1:1 facets attached to a node: "" for the entity's own
// table, or a child's NAME for a facet that lives inside that child.
//
// A facet is a slice of ONE row of its owner, so the framework requires its
// schema to be built over the owner's type — which makes where it is declared
// part of its meaning rather than a formatting choice.
func (m *Model) SiblingsOn(node string) []Sibling {
	var out []Sibling
	for _, s := range m.Siblings {
		if s.OwnerChild == node {
			out = append(out, s)
		}
	}
	return out
}

// RoleFields are the fields private to this role.
func (m *Model) RoleFields() []Field {
	var out []Field
	for _, f := range m.Fields {
		if f.LivesOn != "base" {
			out = append(out, f)
		}
	}
	return out
}

// Index is one index on the read projection.
type Index struct {
	Columns []string
	Name    string
	Unique  bool
	Text    bool
	Sparse  bool
	Order   string
	Partial string
}

// FieldRestrict hides a field from callers who lack a permission.
type FieldRestrict struct {
	Field      string
	Column     string
	Permission string
}

// readColumn maps a spec field name to the key it carries in the read document.
//
// The read side is addressed by COLUMN, not by Go field: a projection is built
// from the schema's column names, so indexing or filtering by the Go name would
// silently match nothing.
func readColumn(sp *spec.Spec, m *Model, name string) string {
	if i := strings.Index(name, "."); i > 0 {
		// The collection through the canonical resolver, not by Name alone:
		// read.indexes and read.fieldRestrict address a collection like every
		// other key, so they take either of its two names — and a head matched
		// only against the singular fell through to `return name`, declaring an
		// index over the literal string "Permissoes.PermissaoID" instead of the
		// document path. Silently: the spec validated, the view was built, and
		// nothing indexed anything.
		head := canonicalCollection(sp.Children, name[:i])
		for _, c := range m.Children {
			if c.Name != head {
				continue
			}
			for _, f := range c.Fields {
				if f.Name == name[i+1:] {
					// The DOCUMENT key, not the wire name: an index or filter addressed by
					// the wire name would match nothing in the projection.
					return c.DocSegment + "." + f.Column
				}
			}
		}
		return name
	}
	for _, f := range m.Fields {
		if f.Name == name {
			return f.Column
		}
	}
	for _, sib := range m.Siblings {
		for _, f := range sib.Fields {
			if f.Name == name {
				return f.Column
			}
		}
	}
	return naming.Snake(name)
}

// Surfaces is what the entity exposes beyond REST.
type Surfaces struct {
	REST          bool
	GraphQL       bool
	GQLMutations  map[string]bool
	GQLConnection bool
	CSV           bool
	CSVDelimiter  string
	XLSX          bool
	XLSXSheet     string
}

func resolveSurfaces(s *spec.Spec) Surfaces {
	out := Surfaces{REST: s.Surfaces.REST, GQLMutations: map[string]bool{}}
	if g := s.Surfaces.GraphQL; g != nil && g.Enabled {
		out.GraphQL = true
		out.GQLConnection = g.Connection
		for _, mu := range g.Mutations {
			out.GQLMutations[mu] = true
		}
	}
	if e := s.Surfaces.Exports; e != nil {
		if e.CSV != nil {
			out.CSV = true
			out.CSVDelimiter = e.CSV.Delimiter
		}
		if e.XLSX != nil {
			out.XLSX = true
			out.XLSXSheet = e.XLSX.Sheet
		}
	}
	return out
}

// PatchableFields are the fields a partial update may set.
//
// Excluding one is not cosmetic: leaving it in means a field the author put
// off-limits can still be changed by sending it, which is the kind of quiet
// permission nobody notices until someone uses it.
func (m *Model) PatchableFields() []Field {
	var out []Field
	for _, f := range m.WritableFields() {
		if m.PatchExcludes[f.Name] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// WritableFields are the ones a CLIENT may send.
//
// A field the server assigns from the caller's identity is not one of them, and
// it is left OUT of the request rather than accepted and ignored: a request
// type that carries a field the server overwrites tells the caller they have a
// say in it. They do not — and the alternative was writing the assignment by
// hand in the insert mapper and then fixing the generated test that asserted
// the field came from the body.
func (m *Model) WritableFields() []Field {
	var out []Field
	for _, f := range m.AllOwnerFields() {
		if f.AssignedFrom != "" {
			continue
		}
		out = append(out, f)
	}
	// A body-sourced runtime field is written by the client and stored by
	// nobody. It belongs here — the write DTO, the command and the mapper are
	// exactly the surfaces it crosses — and nowhere the word "owner" reaches:
	// AllOwnerFields is what the table, the migration and every response are
	// built from, and this field is in none of them.
	return append(out, m.BodyRuntimeFields()...)
}

// BodyRuntimeFields are the runtime fields the CALLER supplies: they cross the
// write DTO, the command and the entity so a rule can check them, and stop
// there. A password confirmation is the case they exist for.
func (m *Model) BodyRuntimeFields() []Field {
	var out []Field
	for _, f := range m.Runtime {
		if f.Source == "body" {
			out = append(out, f)
		}
	}
	return out
}

// DeclaredIdentityFields are the runtime fields an AUTHOR declared with one of
// the framework's own questions about the caller — subject, tenant, a permission,
// the super-admin grant, presence.
//
// The row scope's synthesised fields are excluded: they are reported where the
// row scope itself is, next to the guard they feed, and listing them twice would
// read as two policies.
func (m *Model) DeclaredIdentityFields() []Field {
	var out []Field
	for _, f := range m.Runtime {
		if f.IdentitySource != "" && !f.Synthesised {
			out = append(out, f)
		}
	}
	return out
}

// ManualRuntimeFields are the runtime fields the generator declares on the
// aggregate and fills from nowhere: no write DTO, no command, no mapper and no
// OpenAPI schema mentions them, because no generated verb has anything to put
// there. Hand-written code does.
func (m *Model) ManualRuntimeFields() []Field {
	var out []Field
	for _, f := range m.Runtime {
		if f.Source == "manual" {
			out = append(out, f)
		}
	}
	return out
}

// ClaimRuntimeFields are the runtime fields the IDENTITY supplies — the ones the
// command mapper's identity feed fills, whether the author declared them or the
// row scope synthesised them.
//
// Every emitter that used to read m.Runtime for "what does this mapper read off
// the token" reads this instead. The distinction is not cosmetic there: a feed
// written for a field the token does not carry opens `if id := ctx.Identity()`
// over an empty body, and an unused variable is a build failure, not a warning.
func (m *Model) ClaimRuntimeFields() []Field {
	var out []Field
	for _, f := range m.Runtime {
		// Selected by exclusion, and the exclusions are BOTH of the sources the
		// identity feed must not touch: a body field is read off the command, and
		// a manual one is written by code this generator does not emit. Naming
		// only `body` here was exhaustive until a third source existed, and a feed
		// written for a field the token does not carry assigns nothing.
		if f.Source != "body" && f.Source != "manual" {
			out = append(out, f)
		}
	}
	return out
}

// CommandFields are the fields one write verb's body carries, in spec order.
//
// It is the per-verb narrowing of WritableFields: a persisted field is on every
// write, a body-sourced runtime field only on the verbs it declared. The patch
// additionally drops what update.patchExcludes put off-limits.
func (m *Model) CommandFields(verb string) []Field {
	base := m.WritableFields()
	if verb == "patch" {
		base = m.PatchableFields()
	}
	out := make([]Field, 0, len(base))
	for _, f := range base {
		if f.CarriedBy(verb) {
			out = append(out, f)
		}
	}
	// The scope's subject joins the INSERT body when the bypass may state it,
	// and only there: a row does not change tenant by being updated, and the
	// update mappers deliberately leave every server-assigned field alone.
	if verb == "insert" {
		if f := m.BypassSettableField(); f != nil {
			stated := *f
			stated.WireOptional = true
			out = append(out, stated)
		}
	}
	return out
}

// BypassSettableField is the row scope's subject when the caller who crosses
// that scope may state it, and nil otherwise.
//
// It is resolved from the AUTHZ side rather than by scanning the fields,
// because the two have to agree: what makes the value safe to accept is the
// row-scope guard comparing this exact field, and the guard is built from
// Authz.ScopeSubject(). Reading the flag off some other field would produce a
// request key nothing checks.
func (m *Model) BypassSettableField() *Field {
	subject := m.Authz.ScopeSubject()
	if subject == nil || !subject.BypassMaySet || m.Authz.BypassField == nil {
		return nil
	}
	return subject
}

// Mappable drops the fields a flat `e.X = c.X` must NOT be written for.
//
// A server-assigned field is in the command only when the row-scope bypass may
// state it, and then its assignment is ordered — the identity first, the
// caller's word second — which is a thing the mapper's own assigned-fields
// block writes. An unconditional copy here would run BEFORE that block and
// dereference a nil.
func Mappable(fields []Field) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if f.AssignedFrom != "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// DropBodyRuntime removes the fields that reach the entity and stop there.
//
// It is what the RESULT half of a write is built from: the command carries a
// password confirmation, the entity carries it, and the result type has no such
// member — there is no column behind it, so there is nothing to project. A test
// or a mapper that walked the command's fields into the result would name a
// field that does not exist, which is a build failure rather than a wrong
// answer, but only once someone writes the spec that produces it.
func DropBodyRuntime(fields []Field) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if f.Source == "body" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// CarriedBy reports whether a given write verb's BODY carries this field.
//
// Only a body-sourced runtime field ever answers no: everything else on the
// write surface is on every write verb the entity mounts.
func (f Field) CarriedBy(verb string) bool {
	switch f.Source {
	case "manual":
		// No generated verb carries it — that is the whole declaration. It is what
		// keeps the field off every write DTO and command, and what makes the
		// automatic pass exclude its value object under EVERY gate rather than
		// under the ones a body field happens to skip.
		return false
	case "body":
		for _, mode := range f.Modes {
			if mode == GateModeOf(verb) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// GateModeOf folds a write verb onto the granularity the rule gates have. A
// PATCH is dispatched into IfUpdate exactly as a PUT is, so the domain cannot
// tell them apart and neither does the spec key that rides on it.
func GateModeOf(verb string) string {
	if verb == "patch" {
		return "update"
	}
	return verb
}

// bodyFieldModes materialises the default: a field that names no verb is
// carried by every write verb the entity has.
//
// The intersection is deliberate in BOTH branches. Declared, it has already
// been validated against the entity's own modes, so it passes through. Omitted,
// "every write verb" has to mean the ones that EXIST — an insert-only entity
// must not grow an update DTO because a field defaulted into one.
func bodyFieldModes(declared, entityModes []string) []string {
	if len(declared) > 0 {
		return append([]string{}, declared...)
	}
	var out []string
	for _, want := range []string{"insert", "update"} {
		for _, have := range entityModes {
			if have == want {
				out = append(out, want)
				break
			}
		}
	}
	return out
}

// AssignedFields are the ones the server fills rather than the client, in spec
// order — whatever the source. They are what leaves the write surface.
func (m *Model) AssignedFields() []Field {
	var out []Field
	for _, f := range m.AllOwnerFields() {
		if f.AssignedFrom != "" {
			out = append(out, f)
		}
	}
	return out
}

// IdentityAssignedFields are the subset the GENERATOR writes the assignment
// for: the ones read from the caller's token. Only an insert writes them.
//
// The split matters to the mapper emitters and to nothing else: a `derived`
// field leaves the write surface exactly like these, but what fills it is a
// hand-written rule, so emitting an identity block for it would produce a
// block that assigns nothing — and, when it is the only assigned field, one
// that does not compile.
func (m *Model) IdentityAssignedFields() []Field {
	var out []Field
	for _, f := range m.AssignedFields() {
		if f.AssignedFrom == "identity-subject" || f.AssignedFrom == "identity-claim" {
			out = append(out, f)
		}
	}
	return out
}

// DerivedFields are the server-assigned fields computed from the entity's own
// state by a hand-written rule.
func (m *Model) DerivedFields() []Field {
	var out []Field
	for _, f := range m.AssignedFields() {
		if f.AssignedFrom == "derived" {
			out = append(out, f)
		}
	}
	return out
}

// attachChildFacets folds a facet declared inside a child into that child's
// field list.
//
// The split is physical, exactly as it is for a facet on the root: one Go type,
// two tables, one shared key. Leaving the columns out of the child's type would
// give the schema a Field(...) for something the struct does not have, which the
// framework rejects at boot — and giving the facet a type of its own would make
// it a second child, which is a different relationship.
func attachChildFacets(m *Model) {
	for i := range m.Children {
		for _, sib := range m.SiblingsOn(m.Children[i].Name) {
			for _, f := range sib.Fields {
				f.Facet = sib.Name
				m.Children[i].Fields = append(m.Children[i].Fields, f)
			}
		}
	}
}

// UsesVOsInChildren reports whether any collection has a value-object field.
// The child test file imports the vos package only when one does.
func (m *Model) UsesVOsInChildren() bool {
	for _, c := range m.Children {
		for _, f := range c.Fields {
			// A COMPOSITE part counts even when it is a plain scalar inside the
			// value object: the fixture builds the value object itself, and that
			// type lives in vos whatever its parts are made of.
			if f.VOKind != "" || f.Composite != nil {
				return true
			}
		}
	}
	return false
}

// UsesVOs reports whether any field of the entity itself carries a value
// object. A test that builds an entity literal needs the vos package exactly
// then, and importing it otherwise does not compile.
func (m *Model) UsesVOs() bool {
	for _, f := range m.AllOwnerFields() {
		if f.VOKind != "" || f.Composite != nil {
			return true
		}
	}
	return false
}

// HasNotificationsIn reports whether any notification is declared in a package
// other than the domain's own — which decides whether a generated test has to
// import it.
func (m *Model) HasNotificationsIn(pkg string) bool {
	for _, n := range m.Notifications {
		if n.Package == pkg {
			return true
		}
	}
	return false
}

// placeNotifications puts each notification in the package that can actually
// reference it.
//
// This is not a naming choice, so it is derived rather than asked for: a
// notification raised by a CHILD's rule has to live in aggregatevos, because
// the child's type does, and `domain` cannot hold it — domain imports
// aggregatevos, so the reference would be an import cycle. Emitting it in
// domain produced a tree that did not compile at all, with the author's only
// clue being "undefined" in a file they did not write.
//
// A notification raised from BOTH sides lives in aggregatevos too, and the
// root's reference is qualified: that direction of import exists, the other
// does not.
//
// A value object raises its own, and the same reasoning puts those in vos: the
// type that names it is declared there, and vos imports neither of the other
// two. Left in domain, it produced the identical failure — "undefined" inside a
// generated file — for the identical reason.
func placeNotifications(s *spec.Spec, m *Model) {
	raisedByChild := map[string]bool{}
	for _, c := range s.Children {
		for _, r := range c.Rules.List {
			if r.Notification != "" {
				raisedByChild[r.Notification] = true
			}
		}
		for _, r := range c.Rules.Manual {
			if r.Notification != "" {
				raisedByChild[r.Notification] = true
			}
		}
	}
	raisedByVO := map[string]bool{}
	for _, vo := range s.ValueObjects {
		for _, n := range []string{vo.Notification, vo.UnknownNotification} {
			if n != "" {
				raisedByVO[n] = true
			}
		}
	}
	for i := range m.Notifications {
		if m.Notifications[i].Package != "domain" {
			continue
		}
		switch {
		case raisedByVO[m.Notifications[i].Name]:
			m.Notifications[i].Package = "vos"
			m.Notifications[i].Moved = true
		case raisedByChild[m.Notifications[i].Name]:
			m.Notifications[i].Package = "aggregatevos"
			m.Notifications[i].Moved = true
		}
	}
}

// HasOwnedChildren reports whether any collection's entry type belongs to THIS
// spec. A role that only MOUNTS the identity's collection has children in the
// model and writes none of their types, so anything keyed off "has children"
// alone lands in the wrong file — or, worse, in the right file twice.
func (m *Model) HasOwnedChildren() bool {
	for _, c := range m.Children {
		if !c.Mounted {
			return true
		}
	}
	return false
}

// HasPerChildOps reports whether any collection is edited one entry at a time,
// which is what mounts the Add/Change/Remove trio and its wire types.
func (m *Model) HasPerChildOps() bool {
	for _, c := range m.Children {
		if c.PerChild {
			return true
		}
	}
	return false
}

// ArchiveWhen is the resolved "this update retires the row" decision: the
// expressions the emitter compares and assigns, rather than the spec's words.
type ArchiveWhen struct {
	// Field is the deciding field, as the entity declares it.
	Field Field
	// Equals is the trigger value, and Becomes the value the row is archived
	// holding. Becomes is empty when the spec left the trigger value in place.
	Equals  string
	Becomes string
	// Description is the spec's line on why, for the generated comment.
	Description string
}

func resolveArchiveWhen(s *spec.Spec, m *Model) *ArchiveWhen {
	aw := s.Delete.ArchiveWhen
	if aw == nil {
		return nil
	}
	f := lookupField(m, aw.Field)
	if f == nil {
		// Validation refuses an unknown field, so this is unreachable in a
		// generated run; returning nil rather than panicking keeps a `check`
		// that already has blockers from dying before it prints them.
		return nil
	}
	return &ArchiveWhen{
		Field: *f, Equals: aw.Equals, Becomes: aw.Becomes, Description: aw.Description,
	}
}

// qualifyVONotification renders a value object's notification as the reference
// the generated vos package must write.
//
// The two live in different packages and only one of them needs saying: the
// service's own notifications are generated into internal/domain/vos beside the
// value objects that raise them, while the framework's live in its domain
// package — already imported by every generated value-object file, because the
// RequiredFieldNotification path writes it unconditionally.
func qualifyVONotification(name string) string {
	if name == "" {
		return ""
	}
	if spec.IsFrameworkNotification(name) {
		return "domain." + name
	}
	return name
}
