// Package spec defines the omnicore-gen spec language (v1) and its loader.
//
// The types in this file ARE the definition of the language: there is no
// separate JSON-Schema artifact to drift against. Decoding is strict
// (KnownFields), so an unknown key is an error, and every field declared here
// must be consumed by an emitter or explicitly refused by the coverage gate
// (see coverage.go). That pairing is what enforces INV-1 — "nothing generated
// half-way": a key that no emitter reads cannot silently exist.
package spec

// Spec is one entity, the unit the generator consumes.
type Spec struct {
	// SpecVersion is the version of THIS LANGUAGE, not of your file. It is 1, it
	// stays 1, and editing your spec never changes it — a different value is
	// refused, because it would mean the file speaks a dialect this build does
	// not. Do not confuse it with read.view.version, which is yours and which you
	// DO bump.
	SpecVersion int `yaml:"specVersion"`
	// Entity is the aggregate's singular name, PascalCase, usable as a Go
	// identifier: it names the Go types, the routes and the generated files.
	Entity string `yaml:"entity"`
	// Plural is REQUIRED. It reaches the route path, the feature name and the
	// listing types, and no rule can spell it: an English heuristic writes
	// "Animals" for Animal and "Pessoas" is not something it could ever reach.
	// The generator does not invent names — this one is declared.
	Plural string `yaml:"plural"`
	// Language is the language the spec's human-facing text is written in, such
	// as pt-BR: descriptions written in it seed the matching label catalog, and
	// the other catalogs fall back to the field name.
	Language string `yaml:"language"`

	// Storage says where the entity's rows live: its own table (flat), or a role
	// over an identity other roles may share (sharedbase-role).
	Storage Storage `yaml:"storage"`
	// Fields are the entity's own scalar fields — each one a column, or a
	// runtime-only value the rules read from the caller's token.
	Fields []Field `yaml:"fields"`
	// ValueObjects declares the value-object types fields wrap themselves in via
	// vo: a validated raw shape, or a closed enum.
	ValueObjects []ValueObject `yaml:"valueObjects"`
	// Children are the owned collections (1:N): rows that live with the root,
	// are edited through it, and are nested in its read document.
	Children []Child `yaml:"children"`
	// Siblings are the 1:1 facets: optional field groups split into their own
	// table so they can be null in bulk, yet read as fields of the same entity.
	Siblings []Sibling `yaml:"siblings"`
	// Modes lists the verbs the entity has at all (display, insert, update,
	// delete, archive, unarchive); an absent one is not routed.
	Modes []string `yaml:"modes"`
	// Update decides the shape of the update surface — PATCH, PUT or both — and
	// what a partial update may not touch.
	Update Update `yaml:"update"`
	// Delete decides what removing the entity means: archive (soft, reversible)
	// or purge (hard, permanent).
	Delete Delete `yaml:"delete"`
	// Rules are the entity's invariants: the declarative list the DSL expresses
	// in full, plus the named manual residue a human implements.
	Rules Rules `yaml:"rules"`
	// Notifications declares every business answer the rules and value objects
	// raise, each with its HTTP semantic and its seven translations.
	Notifications []Notification `yaml:"notifications"`
	// Service declares the domain service and the facts it answers — the
	// questions the rules ask that the row being written cannot answer alone.
	Service *Service `yaml:"service"`
	// Read configures the read side: the backing store, the view, and which read
	// operations (by id, by params) are served.
	Read Read `yaml:"read"`
	// Surfaces decides where the entity is exposed: REST, GraphQL, and file
	// exports.
	Surfaces Surfaces `yaml:"surfaces"`
	// Authz names the permission resource, the permission each operation
	// requires, and who may reach which rows.
	Authz Authz `yaml:"authz"`

	// SourcePath is where this spec was loaded from. Not a YAML key.
	SourcePath string `yaml:"-"`
}

// ---------------------------------------------------------------- storage

type Storage struct {
	// Kind is the storage posture: flat = the entity's own table;
	// sharedbase-role = a ROLE over an identity other roles may share.
	Kind string `yaml:"kind"`
	// Table is the physical table the entity's own rows live in (the role's
	// table under sharedbase-role).
	Table string `yaml:"table"`
	// Description is one line on what the table holds; it reaches the generated
	// schema as the table's documentation.
	Description string `yaml:"description"`
	// Base is the shared identity a sharedbase-role sits on — its table, link
	// model and lifecycle. Declared only when kind is sharedbase-role.
	Base *Base `yaml:"base"`
	// Managed declares the framework-managed columns by the presence of their
	// names; an empty name means the column does not exist.
	Managed Managed `yaml:"managed"`
}

type Base struct {
	// Table is the physical table of the shared identity.
	Table string `yaml:"table"`
	// Description is one line on what the identity holds.
	Description string `yaml:"description"`
	// Reuse says whether another role already declares the base: false = this
	// spec writes it; true = this spec only points at it.
	Reuse bool `yaml:"reuse"`
	// NaturalKey names the field the identity is deduplicated by: two roles
	// created with the same value resolve to ONE identity rather than two. It
	// must be a non-nullable field that lives on the base.
	NaturalKey string `yaml:"naturalKey"`
	// Link is how the role's row points at the identity's: shared-pk = the
	// role's own primary key IS the identity's; separate-fk = its own column,
	// named by linkColumn.
	Link string `yaml:"link"`
	// LinkColumn is the role's foreign key to the identity, REQUIRED for a
	// separate-fk link. A shared-pk link has none to declare: the role's own
	// primary key IS the identity's, which is the framework's contract rather
	// than a name anyone chooses.
	LinkColumn string `yaml:"linkColumn"`
	// SchemaFunc names the Go function that declares the base schema. Declared
	// because the generator has no way to singularise a table name.
	SchemaFunc string `yaml:"schemaFunc"`
	// RowUniqueness says how many role rows one identity may hold: unique-fk =
	// one ever; active-only = an archived one frees the slot.
	RowUniqueness string `yaml:"rowUniqueness"`
	// OrphanPolicy is what happens to the identity when its last role goes:
	// keep, or delete-when-unreferenced.
	OrphanPolicy string `yaml:"orphanPolicy"`
}

// Managed declares the framework-managed columns BY PRESENCE: an empty name
// means the column is not declared, and the framework then never mentions it.
type Managed struct {
	// CreatedAt is the column the framework stamps when the row is inserted.
	CreatedAt string `yaml:"createdAt"`
	// UpdatedAt is the column the framework stamps on every write.
	UpdatedAt string `yaml:"updatedAt"`
	// ArchivedAt is the column that marks a soft-deleted row — what soft delete,
	// unarchive and includeArchived hinge on. Empty = no archived state.
	ArchivedAt string `yaml:"archivedAt"`
	// Revision is the optimistic-concurrency counter column, bumped on every
	// write.
	Revision string `yaml:"revision"`
}

// ---------------------------------------------------------------- fields

type Field struct {
	// Name is the exported Go field name, PascalCase.
	Name string `yaml:"name"`
	// Type is the persistable type, from the closed set: string | int | int64 |
	// float64 | bool | time | id. Money is int64 in minor units, never a float.
	Type string `yaml:"type"`
	// Column is the physical column the field is stored in. A runtime-only
	// field has none.
	Column string `yaml:"column"`
	// Length is the column length, required for a string field.
	Length int `yaml:"length"`
	// Nullable says the column may hold NULL, making the field optional by
	// shape rather than required.
	Nullable bool `yaml:"nullable"`
	// LivesOn is the table the field is stored in: root for a flat entity;
	// base (shared by every role) or role (private to this one) under
	// sharedbase; sibling:<name> for a facet's field.
	LivesOn string `yaml:"livesOn"`
	// VO wraps the field in a value object: reuse of an existing type, or one
	// declared under valueObjects (raw or enum).
	VO *FieldVO `yaml:"vo"`
	// Unique declares that the field's value must not repeat, and how that is
	// enforced and answered.
	Unique *Unique `yaml:"unique"`
	// Example is a plausible sample value in the field's wire format; it is
	// what the generated OpenAPI examples ("try it out") show.
	Example string `yaml:"example"`
	// Description is one line on what the field means. It becomes the column
	// comment in the migration, and in the spec's language it seeds the label.
	Description string `yaml:"description"`
	// LabelKey is the translation-catalog key for the field's label — what a
	// CSV/XLSX column header resolves through. Derived when omitted.
	LabelKey string `yaml:"labelKey"`
	// Runtime marks the field as runtime-only: never persisted, fed from the
	// caller's token (see claim), existing only for the rules to read.
	Runtime bool `yaml:"runtime"`
	// Claim names the JWT claim a runtime-only field is fed from. It is required
	// for such a field: the framework deliberately does not opine on which custom
	// claims a token carries, so any convention here would be a guess.
	Claim string `yaml:"claim"`
	// AssignedFrom says the SERVER fills this persisted field from the caller's
	// identity, so the client never sends it: it is absent from every write
	// request and command, written on insert, and left alone afterwards. Use it
	// for the field that records who created the row — the one an owner-only
	// policy then reads.
	AssignedFrom string `yaml:"assignedFrom"` // identity-subject | identity-claim
}

type FieldVO struct {
	// Kind is how the field is wrapped: none | reuse (an existing VO type) |
	// raw | enum.
	Kind string `yaml:"kind"`
	// Ref is the value-object type name — an entry of valueObjects, or, for
	// reuse, a type that already exists in the project.
	Ref string `yaml:"ref"`
}

type Unique struct {
	// Enforce is how uniqueness is guaranteed: service-precheck+constraint asks
	// a Service fact before the database constraint answers; constraint-only
	// relies on the constraint alone.
	Enforce string `yaml:"enforce"`
	// Notification names the conflict answer a duplicate raises — a custom
	// <Field>AlreadyExists… notification declared under notifications.
	Notification string `yaml:"notification"`
	// Scope decides whether an archived row keeps holding the value: all =
	// forever; active-only = archiving frees it for reuse.
	Scope string `yaml:"scope"`
}

// ---------------------------------------------------------------- value objects

type ValueObject struct {
	// Name is the value-object type name that fields reference via vo.ref.
	Name string `yaml:"name"`
	// Kind is what the value object is: raw = a validated shape; enum = a
	// closed set of members.
	Kind string `yaml:"kind"`
	// Backing is the underlying representation: string or int.
	Backing string `yaml:"backing"`
	// Description is one line on what the value object means.
	Description string `yaml:"description"`

	// raw

	// Regex is the pattern a raw value must match.
	Regex string `yaml:"regex"`
	// MinLength is the minimum length a raw string value must have.
	MinLength int `yaml:"minLength"`
	// MaxLength is the maximum length a raw string value may have.
	MaxLength int `yaml:"maxLength"`
	// Min is the lower bound a raw numeric value must satisfy.
	Min *float64 `yaml:"min"`
	// Max is the upper bound a raw numeric value may reach.
	Max *float64 `yaml:"max"`
	// Notification names the validation answer an invalid raw value raises.
	Notification string `yaml:"notification"`

	// enum

	// Members are the enum's values — the closed set the wrapped field may hold.
	Members []EnumMember `yaml:"members"`
	// UnknownNotification names the answer raised when a value outside the
	// enum's set arrives.
	UnknownNotification string `yaml:"unknownNotification"`
	// DescriptionKeys asks for a translation key per enum member. Refused by
	// this build — per-value translation keys are not generated.
	DescriptionKeys bool `yaml:"descriptionKeys"`
}

type EnumMember struct {
	// Name is the member's Go constant name, PascalCase.
	Name string `yaml:"name"`
	// Value is the wire/storage value the member maps to.
	Value any `yaml:"value"`
	// Text carries the member's human-facing text, per language catalog.
	Text Texts `yaml:"text"`
}

// ---------------------------------------------------------------- children / siblings

type Child struct {
	// Name is the child's singular PascalCase name — it names the entry's Go
	// type.
	Name string `yaml:"name"`
	// Plural is REQUIRED and is the child's COLLECTION NAME — the single name
	// the framework uses for this collection: the document segment the
	// projection nests it under, the Go field the read DTO declares for it, and
	// (lower-camelled) the notification wire path. It is a persisted key, so a
	// guess here is a wrong document, not a cosmetic slip.
	//
	// Declare it as the domain says it, in the domain's own language, valid as
	// an exported Go field name: "Addresses", "OrderLines", "Enderecos".
	Plural string `yaml:"plural"`
	// ParentColumn is REQUIRED: the foreign key back to the owner. It is a
	// column name, so deriving it would be inventing a name that outlives the
	// decision — renaming it later is a migration.
	ParentColumn string `yaml:"parentColumn"`
	// Table is the physical table the collection's rows live in.
	Table string `yaml:"table"`
	// Description is one line on what the collection holds.
	Description string `yaml:"description"`
	// OwnedBy is whose collection it is: root; or, under sharedbase, role
	// (private to this role) or base (belonging to the shared identity, which
	// every role reads and which outlives this one).
	OwnedBy string `yaml:"ownedBy"`
	// EditStrategy is how the collection is edited: atomic-replace = the root's
	// update swaps the whole collection; per-child = each entry gets its own
	// add/update/remove endpoints.
	EditStrategy string `yaml:"editStrategy"`
	// BusinessIdentity names the fields that make two entries THE SAME entry —
	// the duplicate detector, and the match key per-child operations use.
	BusinessIdentity []string `yaml:"businessIdentity"`
	// Fields are the entry's own fields.
	Fields []Field `yaml:"fields"`
	// Rules are the invariants checked on each entry of the collection.
	Rules Rules `yaml:"rules"`
	// SoftRemove makes removal reversible: the entry is archived (see
	// archivedAt) instead of deleted, and per-child removal mounts as an
	// archive rather than a DELETE.
	SoftRemove bool `yaml:"softRemove"`
	// ArchivedAt is the column marking an archived entry; required when
	// softRemove is on, refused when it is off.
	ArchivedAt string `yaml:"archivedAt"`
	// DuplicateNotification names the conflict answer a per-child ADD raises
	// when the entry is already there.
	DuplicateNotification string `yaml:"duplicateNotification"`
}

type Sibling struct {
	// Name is the facet's PascalCase name.
	Name string `yaml:"name"`
	// Table is the physical table the facet's row lives in.
	Table string `yaml:"table"`
	// Description is one line on what the facet holds.
	Description string `yaml:"description"`
	// AttachTo is the node the 1:1 facet hangs off: root; role under
	// sharedbase; or child:<name> for a facet of a collection entry.
	AttachTo string `yaml:"attachTo"`
	// Fields are the facet's fields — fields of the same Go type, stored in the
	// facet's table so they can be null in bulk.
	Fields []Field `yaml:"fields"`
}

// ---------------------------------------------------------------- lifecycle

type Update struct {
	// Shape is which update verbs exist: patch | put | both. PATCH cannot say
	// "set this to null", which is why a clearable facet forces put.
	Shape string `yaml:"shape"`
	// PatchExcludes names fields a partial update may NOT touch — a natural
	// key, or anything whose change must go through a deliberate operation.
	PatchExcludes []string `yaml:"patchExcludes"`
}

type Delete struct {
	// Root is what deleting the entity means: soft = archive, reversible; hard
	// = a permanent purge, and the HTTP verb must say so; both = the two verbs
	// are served.
	Root string `yaml:"root"`
	// Children would declare a blanket removal semantic for the collections.
	// Refused by this build — removal is declared per child, with
	// children[].softRemove.
	Children string `yaml:"children"`
}

// ---------------------------------------------------------------- rules

type Rules struct {
	// List is the declarative rules — the invariants the DSL can express and
	// the generator writes in full.
	List []Rule `yaml:"list"`
	// Manual is the residue the DSL cannot express. It is a NAMED LIST, not a
	// flag: each entry becomes an actionable item in the generated report's
	// "what still needs implementing" section, and the hook file is written from
	// it. An entry without a description is refused — an unnamed escape hatch
	// degenerates into an empty TODO, which is what this list exists to prevent.
	Manual []ManualRule `yaml:"manual"`
}

// ManualRule is one invariant the author must hand-write in <entity>_rules_manual.go.
type ManualRule struct {
	// ID is the rule's stable identifier, kebab-case; it names the stub and the
	// report line.
	ID string `yaml:"id"`
	// Description says what the invariant IS, precisely enough to implement —
	// it is what the generated stub and the report tell the implementer.
	// Required.
	Description string `yaml:"description"`
	// Scope is the verbs the rule fires on: insert | update | insertOrUpdate |
	// archive | unarchive | delete.
	Scope []string `yaml:"scope"`
	// ActionName is refused by this build — describe the condition in the
	// description instead.
	ActionName string `yaml:"actionName"`
	// Notification names the answer the hand-written rule raises when it
	// refuses.
	Notification string `yaml:"notification"`
	// AttachTo names the field the refusal is reported against.
	AttachTo string `yaml:"attachTo"`
}

type Rule struct {
	// ID is the rule's stable identifier, kebab-case.
	ID string `yaml:"id"`
	// Kind is what the rule checks: required | immutable | length | range |
	// comparison | transition | requiredIf | groupCap | childDuplicate |
	// ownerCheck. Anything outside this set goes to rules.manual.
	Kind string `yaml:"kind"`
	// Scope is the verbs the rule fires on: insert | update | insertOrUpdate |
	// archive | unarchive | delete.
	Scope []string `yaml:"scope"`
	// ActionName is refused by this build — gate the rule by verb scope instead.
	ActionName string `yaml:"actionName"`
	// Fields are the subject fields; for a collection rule (groupCap,
	// childDuplicate) the single entry is the child's NAME.
	Fields []string `yaml:"fields"`
	// Notification names the answer the rule raises when it refuses.
	Notification string `yaml:"notification"`
	// AttachTo names the field the refusal is reported against; defaults to the
	// rule's first field.
	AttachTo string `yaml:"attachTo"`
	// EchoValue passes the rejected value back in the notification, so the
	// caller sees what was refused.
	EchoValue bool `yaml:"echoValue"`
	// Description is one optional line on why the rule exists.
	Description string `yaml:"description"`

	// kind-specific

	// Min is the lower bound of a range rule, or the minimum length of a length
	// rule.
	Min *float64 `yaml:"min"`
	// Max is the upper bound of a range rule, or the maximum length of a length
	// rule.
	Max *float64 `yaml:"max"`
	// Operator is the comparison a comparison rule makes between the two
	// fields: gte | gt | lte | lt | eq | ne.
	Operator string `yaml:"operator"`
	// Other is the second field: what a comparison compares against, or the
	// field whose presence makes a requiredIf fire.
	Other string `yaml:"other"`
	// SkipWhen stands the rule down when the subject is absent (empty | null) —
	// "valid IF given" rather than "required".
	SkipWhen string `yaml:"skipWhen"`
	// Transitions is a transition rule's state machine: each key lists the
	// values it may move to. A value that did not change is always allowed.
	Transitions map[string][]string `yaml:"transitions"`
	// GroupBy partitions a collection before a cap is applied. It is OPTIONAL:
	// with no grouping the cap is on the whole collection, which is what "at most
	// N of these" usually means.
	GroupBy []string `yaml:"groupBy"`
	// Cap is the maximum number of entries a groupCap allows — per group when
	// groupBy is set, over the whole collection otherwise.
	Cap int `yaml:"cap"`
	// Only restricts WHICH entries the cap counts. Without it a cap on a status
	// field caps every status equally — "at most 3 under review" silently became
	// "at most 3 rejected" too, which no domain asked for and which nothing
	// reports.
	Only *RuleOnly `yaml:"only"`
	// OwnerField is the runtime-only field carrying the caller's identity — the
	// value an ownerCheck compares against the subject field.
	OwnerField string `yaml:"ownerField"`
	// AdminField is the runtime-only bool that bypasses an ownerCheck — whether
	// the caller is an administrator.
	AdminField string `yaml:"adminField"`
}

// RuleOnly narrows a set-wide rule to the entries whose field carries one
// value. It is deliberately a single equality: anything richer is a query, and a
// query in a rule is the point where the language stops being readable.
type RuleOnly struct {
	// Field is a field of the collection being counted.
	Field string `yaml:"field"`
	// Equals is the value that makes an entry count.
	Equals string `yaml:"equals"`
}

// ---------------------------------------------------------------- notifications

type Notification struct {
	// Name is the notification's Go type name; rules, value objects and unique
	// declarations reference it by this name.
	Name string `yaml:"name"`
	// Package is where the type is declared: domain (the default) | vos (raised
	// by a value object) | aggregatevos (raised by a child's rule — declaring it
	// in domain would be an import cycle).
	Package string `yaml:"package"`
	// Semantic maps the answer to its HTTP status: validation 422, conflict 409
	// (duplicate), state-conflict 409 (wrong state), forbidden 403, not-found
	// 404.
	Semantic string `yaml:"semantic"`
	// TVars are the placeholder names the texts interpolate, like {min} and
	// {max}.
	TVars []string `yaml:"tvars"`
	// Text is the message in each of the seven catalogs.
	Text Texts `yaml:"text"`
	// Description is one optional line on when the answer is raised.
	Description string `yaml:"description"`
}

// Texts carries the seven catalogs the framework requires. Every key is
// mandatory; --lang-fallback marks the missing ones instead of failing.
type Texts struct {
	// PTBR is the Brazilian Portuguese text.
	PTBR string `yaml:"ptbr"`
	// ENG is the English text.
	ENG string `yaml:"eng"`
	// ESP is the Spanish text.
	ESP string `yaml:"esp"`
	// FRA is the French text.
	FRA string `yaml:"fra"`
	// DEU is the German text.
	DEU string `yaml:"deu"`
	// ITA is the Italian text.
	ITA string `yaml:"ita"`
	// NLD is the Dutch text.
	NLD string `yaml:"nld"`
}

// ---------------------------------------------------------------- service

type Service struct {
	// Required says the entity needs a domain service at all; declaring facts
	// without it is refused.
	Required bool `yaml:"required"`
	// Facts are the questions the service answers — what the rules need to know
	// that the row being written cannot tell on its own.
	Facts []Fact `yaml:"facts"`
}

type Fact struct {
	// Name is the fact's PascalCase name; it becomes a method on the service
	// port.
	Name string `yaml:"name"`
	// Kind names HOW the answer is obtained. Every kind but the last is a query
	// this generator writes against the service's own store.
	//
	// `manual` is the ELSE — see Rules.Manual for the same idea applied to rules.
	// It says: the generator does not know how to answer this. It then declares
	// the method on the port and stubs the body in a file it never regenerates,
	// so the compiler refuses to build until a human writes it. That is the whole
	// point: a missing method fails loudly, whereas a query against the wrong
	// store compiles, returns, and means nothing.
	Kind string `yaml:"kind"` // exists | count | sum | avg | min | max | manual
	// Returns is required for a manual fact: the generator has to know the
	// signature it is declaring, and for the other kinds it follows from the kind.
	//
	// For an aggregating kind it follows the KIND, not the column: an average is
	// float64 even over an integer field, while sum/min/max over int or int64 is
	// exact int64. And min, max and avg answer `(value, bool)` — over an empty
	// set SQL says NULL, and a zero returned alone reads as a real result.
	Returns string `yaml:"returns"` // bool | int64 | float64 | string
	// Field is the field the fact aggregates — required for sum, avg, min and
	// max; refused for manual, whose body is hand-written.
	//
	// It must be numeric (int, int64, float64). The database computes the
	// aggregate and the framework carries it back as an exact integer or a
	// float; there is no carrier for text, a timestamp or a boolean, so max over
	// a name or a date is refused rather than emitted as something that compiles
	// and means nothing.
	Field string `yaml:"field"`
	// Filters names the fields the query narrows by; each becomes a parameter
	// of the generated method.
	Filters []string `yaml:"filters"`
	// ExcludeSelf leaves the record being written out of the answer, so an
	// update never collides with itself.
	ExcludeSelf bool `yaml:"excludeSelf"`
	// ActiveOnly considers only the rows that are not archived. Refused for a
	// manual fact.
	ActiveOnly bool `yaml:"activeOnly"`
	// Description says what the answer means. Required for a manual fact — it
	// is what the generated stub and the report tell the implementer to write.
	Description string `yaml:"description"`
}

// ---------------------------------------------------------------- read side

type Read struct {
	// Backing is where reads are served from: relational = straight from the
	// tables; mongo = from a projection updated shortly after the write.
	Backing string `yaml:"backing"`
	// View names, versions and bounds the read view — on a mongo backing, the
	// projection's collection.
	View View `yaml:"view"`
	// Indexes are the indexes declared on the view's projected documents (mongo
	// backing only); controls.search requires a text index among them.
	Indexes []Index `yaml:"indexes"`
	// ByID serves the GET-by-id read.
	ByID bool `yaml:"byId"`
	// ByParams serves the filtered listing: its filters and reserved controls.
	ByParams *ByParams `yaml:"byParams"`
	// FieldRestrict hides a field from callers without a permission — asking
	// for it explicitly is a 403 rather than a silent omission.
	FieldRestrict []FieldRestrict `yaml:"fieldRestrict"`
	// IdentityView is whether this role creates the shared identity's own view,
	// joins it, or skips it. Refused by this build — the identity's own view is
	// not generated yet.
	IdentityView string `yaml:"identityView"`
}

type View struct {
	// Name is the view's name — on a mongo backing, the projection's collection
	// name — unique across the project's entities.
	Name string `yaml:"name"`
	// Version is YOURS, and it is the opposite of specVersion: bump it whenever
	// the projected SHAPE changes — a field added or removed, a collection
	// renamed, a facet folded in. The framework compares it against what is
	// stored and refuses to boot rather than serve a projection built to an
	// older shape, so forgetting is a failed start; and on a Mongo backing,
	// bumping it is what triggers the rebuild.
	Version int `yaml:"version"`
	// MaxLimit is the page-size ceiling a listing request may ask for.
	MaxLimit int `yaml:"maxLimit"`
	// DeleteOnArchive drops an archived row's document from the projection
	// instead of keeping it marked. Mongo backing only.
	DeleteOnArchive bool `yaml:"deleteOnArchive"`
	// TTLSeconds is a time-to-live on the projected document, in seconds; 0
	// means none. Mongo backing only.
	TTLSeconds int `yaml:"ttlSeconds"`
}

type Index struct {
	// Fields are the projected fields the index covers, in order.
	Fields []string `yaml:"fields"`
	// Name overrides the derived index name.
	Name string `yaml:"name"`
	// Unique makes the index reject duplicate values.
	Unique bool `yaml:"unique"`
	// Text makes it a text index — what controls.search requires.
	Text bool `yaml:"text"`
	// Sparse indexes only the documents that HAVE the field.
	Sparse bool `yaml:"sparse"`
	// Order is the direction of the index keys: asc | desc.
	Order string `yaml:"order"`
	// Partial would filter which documents the index covers. Refused by this
	// build — the framework takes a document filter there, and this language
	// has no way to write one.
	Partial string `yaml:"partial"`
}

type ByParams struct {
	// Filters declares each field the listing is filterable by, with its
	// allowed operators.
	Filters []Filter `yaml:"filters"`
	// Sort would declare a sort allowlist. Refused by this build —
	// controls.orderBy decides whether ?orderBy= is served at all.
	Sort []string `yaml:"sort"`
	// Controls turns on the framework's reserved query controls (pagination,
	// orderBy, fields, search, onlyTotal, includeArchived).
	Controls Controls `yaml:"controls"`
}

type Filter struct {
	// Field is the entity field the filter applies to.
	Field string `yaml:"field"`
	// Ops are the operators the field is filterable by, from the framework's
	// closed set; an undeclared one is a typed 400.
	Ops []string `yaml:"ops"`
	// Required would make the filter mandatory. Refused by this build — the
	// endpoint would serve the parameter as optional.
	Required bool `yaml:"required"`
}

type Controls struct {
	// Pagination serves cursor pagination: ?first, ?last, ?after, ?before.
	Pagination bool `yaml:"pagination"`
	// OrderBy serves ?orderBy=.
	OrderBy bool `yaml:"orderBy"`
	// Fields serves ?fields= partial projection — every listing field then
	// becomes a pointer with omitempty so it can be left out.
	Fields bool `yaml:"fields"`
	// Search serves ?search= across the named fields; it requires a text index
	// covering them.
	Search []string `yaml:"search"`
	// OnlyTotal serves ?onlyTotal=, answering just the count.
	OnlyTotal bool `yaml:"onlyTotal"`
	// IncludeArchived serves ?includeArchived=, letting archived rows into the
	// listing; it requires a managed archivedAt column.
	IncludeArchived bool `yaml:"includeArchived"`
}

type FieldRestrict struct {
	// Field is the field being hidden.
	Field string `yaml:"field"`
	// Permission is the permission a caller must hold to receive it.
	Permission string `yaml:"permission"`
}

// ---------------------------------------------------------------- surfaces

type Surfaces struct {
	// REST mounts the HTTP endpoints; at least one surface must be on.
	REST bool `yaml:"rest"`
	// GraphQL exposes the entity in the GraphQL schema.
	GraphQL *GraphQL `yaml:"graphql"`
	// Exports serves the listing as downloadable files.
	Exports *Exports `yaml:"exports"`
}

type GraphQL struct {
	// Enabled turns the GraphQL surface on.
	Enabled bool `yaml:"enabled"`
	// Mutations lists the verbs exposed as mutations; each must be among the
	// entity's modes.
	Mutations []string `yaml:"mutations"`
	// Connection serves the paginated connection query; it requires display
	// among the modes.
	Connection bool `yaml:"connection"`
}

type Exports struct {
	// CSV enables the CSV export of the listing.
	CSV *CSVExport `yaml:"csv"`
	// XLSX enables the XLSX export of the listing.
	XLSX *XLSXExport `yaml:"xlsx"`
	// The row ceiling is deliberately NOT here: it is service-wide configuration
	// (query.maxExportRows). A per-entity copy could disagree with it, with no
	// way for a reader to tell which one the export actually obeyed.
}

type CSVExport struct {
	// Delimiter is the column separator the CSV uses.
	Delimiter string `yaml:"delimiter"`
}

type XLSXExport struct {
	// Sheet is the worksheet name.
	Sheet string `yaml:"sheet"`
}

// ---------------------------------------------------------------- authz

type Authz struct {
	// Resource is the permission resource name, usually the entity in lower
	// case: student, professor.
	Resource string `yaml:"resource"`
	// Permissions maps each mounted operation (insert, update, patch, delete,
	// archive, unarchive, read) to the permission it requires. Cross-checked
	// both ways against what is actually mounted.
	Permissions map[string]string `yaml:"permissions"`
	// DataAccess is who may reach which rows: anyone-with-permission = every
	// holder sees every row; owner-only = only their own; tenant = only their
	// tenant's.
	DataAccess string `yaml:"dataAccess"`
	// OwnerField is the persisted field that records the row's owner, for
	// owner-only access — declare it with assignedFrom: identity-subject.
	OwnerField string `yaml:"ownerField"`
	// TenantField is the persisted field the tenant claim is matched against,
	// for tenant access — declare it with assignedFrom: identity-claim.
	TenantField string `yaml:"tenantField"`
}
