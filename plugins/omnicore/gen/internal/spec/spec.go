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
	// as pt-BR. Every description, example and label below is read in it, and the
	// generation report says so at the top — a reviewer checking a Portuguese
	// domain against English wording is checking the wrong thing.
	//
	// It does NOT decide any catalog: a field's label is declared per catalog
	// under fields[].text, and one left out falls back to the field's own name.
	Language string `yaml:"language"`

	// Storage says where the entity's rows live: its own table (flat), or a role
	// over an identity other roles may share (sharedbase-role).
	Storage Storage `yaml:"storage"`
	// Fields are the entity's own scalar fields — each one a column, or a
	// runtime-only value the rules read from the caller's token.
	Fields []Field `yaml:"fields"`
	// ValueObjects declares the value-object types fields wrap themselves in via
	// vo: a validated raw shape, a closed enum, a composite spanning several
	// columns, or a manual one this language cannot express and you write.
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
	// Revision is the optimistic-concurrency column: bumped on every write, and
	// the value every ROOT update is guarded on — a write built on a stale read
	// matches no row and is refused rather than reverting what another writer
	// changed in the meantime. It is MANDATORY on an entity or base schema.
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
	// declared under valueObjects (raw, enum or composite).
	VO *FieldVO `yaml:"vo"`
	// Parts maps each part of a COMPOSITE value object onto its own column. It
	// is required for — and only for — a field whose vo.kind is composite: such
	// a value object declares no Value(), so its value spans several columns and
	// the single column key has nothing to hold.
	//
	// The declaration under valueObjects says what the value object IS (its
	// parts, their types, its rules); this list says where THIS entity stores
	// them. That is the same division the framework draws: the domain never
	// learns it is decomposed, and the schema is the only place that knows.
	Parts []FieldPart `yaml:"parts"`
	// Unique declares that the field's value must not repeat, and how that is
	// enforced and answered.
	Unique *Unique `yaml:"unique"`
	// Example is a plausible sample value in the field's wire format; it is
	// what the generated OpenAPI examples ("try it out") show.
	Example string `yaml:"example"`
	// Description is one line on what the field means. It becomes the column
	// comment in the migration — what a DBA and a BI tool read. It is NOT the
	// label: see text.
	Description string `yaml:"description"`
	// LabelKey is the translation-catalog key for the field's label — what a
	// CSV/XLSX column header resolves through. Derived when omitted.
	LabelKey string `yaml:"labelKey"`
	// Text is the field's LABEL — its short human name — per language catalog.
	// It is what a validation payload puts in `fieldLabel` and what a CSV/XLSX
	// export puts in a column header, so it is a couple of words, never a
	// sentence: the description explains the field, the label names it.
	//
	// Unlike a notification's text it is optional and may be partial: a catalog
	// left out falls back to the field's own name, spaced out (Workspace,
	// TenantID → "Tenant ID"), which is a placeholder a translator can find.
	Text Texts `yaml:"text"`
	// Hidden keeps a PERSISTED field out of every response body: the by-id read,
	// each row of the listing, the write results, and the CSV/XLSX exports that
	// render the listing. Everything else is unchanged — the column exists, the
	// filters, sort and indexes reach it, a write may set it, the rules read it,
	// and a computed read field may derive FROM it.
	//
	// It is the answer to "callers query by this, and receive something else":
	// three columns narrow the search, and what comes back is a description and
	// a derived value. Without it the only way to say that was read.fieldRestrict,
	// which answers something different — 403 to a caller who lacks a permission,
	// and the field to everyone who has it. This one is not about who is asking.
	//
	// Refused on a runtime-only field, which is in no response to begin with, and
	// on a field read.fieldRestrict also names: a field nobody receives cannot be
	// the one some callers may.
	Hidden bool `yaml:"hidden"`
	// Runtime marks the field as runtime-only: never persisted, fed from the
	// caller's token (see claim), existing only for the rules to read.
	Runtime bool `yaml:"runtime"`
	// Claim names the JWT claim a runtime-only field is fed from. It is required
	// for such a field: the framework deliberately does not opine on which custom
	// claims a token carries, so any convention here would be a guess.
	Claim string `yaml:"claim"`
	// AssignedFrom says the SERVER fills this persisted field, so the client
	// never sends it: it is absent from every write request, every command and
	// the OpenAPI request schema.
	//
	//   - identity-subject / identity-claim — the value comes from the caller's
	//     token, and the generator writes the assignment. Written on insert and
	//     left alone afterwards, which is what makes "who created this row" a
	//     fact rather than a claim.
	//   - derived — the value comes from the entity's OWN fields, like a public
	//     key computed from an immutable handle. The generator writes no
	//     assignment for it: what computes it is a rules.manual entry scoped to
	//     insert, and the report lists that as owed. Declared here so the field
	//     stops being advertised as writable while the server silently
	//     overwrites whatever a caller sent.
	AssignedFrom string `yaml:"assignedFrom"` // identity-subject | identity-claim | derived
}

type FieldVO struct {
	// Kind is how the field is wrapped: none | reuse (an existing VO type) |
	// raw | enum | composite.
	Kind string `yaml:"kind"`
	// Ref is the value-object type name — an entry of valueObjects, or, for
	// reuse, a type that already exists in the project.
	Ref string `yaml:"ref"`
}

// FieldPart places ONE part of a composite value object into a column. It is
// the spec's form of the framework's Field("<part>", "<column>").As("<name>")
// chain, and it exists per FIELD rather than per value object because the
// columns belong to this entity's table while the value object belongs to the
// domain.
type FieldPart struct {
	// Part is the part's name INSIDE the value object — an entry of the
	// declaration's parts list.
	Part string `yaml:"part"`
	// Column is the physical column this part is stored in. Each part gets its
	// own; the map must be a bijection.
	Column string `yaml:"column"`
	// As is the logical name the part is EXPOSED under — to criteria, the audit
	// timeline, the projected document, the read DTO, filters, orderBy,
	// ?fields=, OpenAPI, GraphQL and the exports, none of which ever learn a
	// composite exists.
	//
	// It defaults to the part's own name, which reads right when the value
	// object is specific (Address{Street, City} → street, city) and wrong when
	// it is generic: Money{Amount, Currency} on a salary field would expose
	// ?amount=, and an entity carrying both a Money and a Discount would
	// collide on it with no way out — a part's name belongs to the value object,
	// not to the consumer.
	As string `yaml:"as"`
	// Type is the part's persistable type, from the same closed set a field
	// draws from. It is required when the value object is REUSED from elsewhere
	// in the project (the declaration is not in this file to read); when the
	// value object is declared here it may be omitted, and is cross-checked
	// against the declaration when given.
	Type string `yaml:"type"`
	// Nullable says this part's column may hold NULL. Like type, it is required
	// only for a reused value object and cross-checked otherwise. A part of an
	// OPTIONAL composite (the field itself is nullable) is stored NULL-able
	// regardless: "every part column NULL" is how absence is written and read.
	Nullable bool `yaml:"nullable"`
	// Length is the column length, required for a string part.
	Length int `yaml:"length"`
	// Example is a plausible sample value in the part's wire format; it is what
	// the generated OpenAPI examples ("try it out") show.
	Example string `yaml:"example"`
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
	// Within names the fields the uniqueness is scoped BY — "unique per tenant",
	// "unique per workspace". Empty means unique across the whole table.
	//
	// It exists because the two halves of `service-precheck+constraint` used to
	// be able to disagree, silently and in the worst direction. The pre-check
	// came from the fact's filters (`[TenantID, Key]` → per tenant) and the
	// index came from the FIELD ALONE (`role_key` → global), so the domain
	// accepted a handle the database then refused, and the binding turned the
	// violation into "this handle is taken" for a tenant where it was not. Every
	// multi-tenant entity with a per-tenant natural key landed there, on exactly
	// the handles two customers both pick — administrator, owner, viewer.
	//
	// So the scope is DECLARED rather than inferred from a block that reads as
	// being about the service port, and the pre-check fact must filter by
	// exactly `within` + this field: check refuses either half disagreeing with
	// the other, in both directions.
	//
	// Each name is a persisted, non-nullable field of the root — a scope column
	// that can be NULL scopes nothing, because NULLs do not collide.
	Within []string `yaml:"within"`
}

// ---------------------------------------------------------------- value objects

type ValueObject struct {
	// Name is the value-object type name that fields reference via vo.ref.
	Name string `yaml:"name"`
	// Kind is what the value object is: raw = a validated shape; enum = a
	// closed set of members; composite = a value that spans SEVERAL fields;
	// manual = one this language cannot express, which YOU write.
	//
	// `manual` is the escape hatch, and it is deliberately a declaration rather
	// than silence: the field is typed as this value object and the mappers
	// convert to and from its backing, so the type has to exist — the report
	// asks for it by name, with the exact shape, and the package does not
	// compile until it is there. Use it when the rule needs something no raw or
	// enum can say; it is not a way to skip writing a regex.
	Kind string `yaml:"kind"`
	// Written says WHO writes the type: generated (the default) or manual —
	// you do. It is declared separately from kind because the two questions are
	// separate: kind says what the value object IS, written says whose file it
	// is, and a COMPOSITE is the case where the answers must be independent.
	//
	// A composite's parts are not decoration: they are what the schema
	// decomposes into columns, what the mappers fold and unfold, what the
	// migration sizes and what the catalogs translate. So "I write this one" and
	// "the generator does not know its shape" cannot be the same statement —
	// which is exactly what `kind: manual` says, and why it is a scalar answer
	// only. With `written: manual` the shape stays declared and the FILE becomes
	// yours: the generator emits no vos/<type>.go and no test for it, and the
	// report asks for it by name with the exact struct.
	//
	// That is the escape hatch a composite needs, because everything a composite
	// cannot express in this language — a regex over one part, "if Resource is
	// *, Action must be *", a String() that renders the concept — is ordinary Go
	// inside an IsValid you own. The rules block is refused for the same reason:
	// there is no file left for the generator to write them into.
	//
	// Refused on the scalar kinds: a raw or an enum you write yourself is
	// `kind: manual`, which already says it with one key instead of two.
	Written string `yaml:"written"`
	// Backing is the underlying representation: string or int. A composite has
	// none — its value is its parts, not a scalar — so the key is refused there.
	// For a `manual` value object it is a CONTRACT, not decoration: the emitted
	// mappers convert with vos.Name(x) and read back with .Value(), so the type
	// you write has to have exactly this underlying type.
	Backing string `yaml:"backing"`
	// Description is one line on what the value object means. Required for a
	// `manual` one: it is what the report asks the implementer for, and an
	// unnamed escape hatch degenerates into an empty TODO.
	Description string `yaml:"description"`

	// composite

	// Parts are the fields a COMPOSITE value object is made of — the reason its
	// value cannot be one column. Money{Amount, Currency}, Period{From, To},
	// Address{Street, City, ZipCode}: neither half means anything alone, which
	// is what makes the concept one value object rather than two fields.
	//
	// The parts are the value object's own shape and are declared ONCE here, for
	// every entity that uses it. Where each part is STORED is per entity, and is
	// declared on the field (fields[].parts).
	Parts []VOPart `yaml:"parts"`
	// Rules are the composite's own invariants — the ones checked inside its
	// IsValid, over its parts. This is where a CROSS-FIELD rule lives, the thing
	// a single-scalar value object cannot express and the reason composites
	// exist: "the end date may not precede the start" is a rule about the value
	// object, not about the entity carrying it.
	//
	// The kinds allowed here are the ones a value object can answer from its own
	// parts: required, length, range, comparison and requiredIf. Anything that
	// needs the rest of the entity, the old state or a service belongs to the
	// entity's own rules (or to rules.manual); a composite that knew about them
	// would not be a value object.
	Rules Rules `yaml:"rules"`

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

// VOPart is one field of a composite value object: its name, its type, and the
// vocabulary it carries. It is the DOMAIN half of a composite — the column half
// is FieldPart, on the entity that stores it.
type VOPart struct {
	// Name is the part's exported Go field name inside the value object,
	// PascalCase.
	Name string `yaml:"name"`
	// Type is the part's persistable type, from the same closed set a field
	// draws from: string | int | int64 | float64 | bool | time | id.
	Type string `yaml:"type"`
	// Nullable makes the part a POINTER inside the value object — optional even
	// when the value object itself is present. Period{From, To *time.Time} is
	// the shape: an open-ended period has a start and no end.
	Nullable bool `yaml:"nullable"`
	// VO wraps the part in a value object of its own: reuse of an existing type,
	// or one declared under valueObjects. A part may be a raw or an enum value
	// object (Money's Currency is the canonical case) but never another
	// composite — a value object nested two deep is an entity in disguise.
	VO *FieldVO `yaml:"vo"`
	// LabelKey is the translation-catalog key for the part's label — what a
	// CSV/XLSX column header and a part-level notification resolve through. It
	// is declared on the VALUE OBJECT rather than on the entity because the
	// value object owns its vocabulary for every entity that uses it. Derived
	// when omitted.
	LabelKey string `yaml:"labelKey"`
	// Description is one line on what the part means. It becomes the column
	// comment in the migration. It is NOT the label: see text.
	Description string `yaml:"description"`
	// Text is the part's LABEL, per language catalog — same rules as a field's:
	// a couple of words, optional, and a catalog left out falls back to the
	// part's own name. It is declared here, on the value object, because the
	// value object owns its vocabulary for every entity that uses it.
	Text Texts `yaml:"text"`
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
	// Operations selects WHICH per-entry verbs the collection mounts — any
	// non-empty subset of add, change, remove. Absent means all three, which is
	// what per-child meant before this key existed. Per-child only: an
	// atomic-replace collection mounts no per-entry verb to select from.
	//
	// It exists because the trio is sometimes a PAIR. A collection whose every
	// field is its business identity — a grant holding one catalog id and
	// nothing else — has no meaningful change: replacing that entry's only value
	// revokes one thing and grants another while KEEPING the first entry's row
	// id, so an audit trail reads one grant becoming another instead of two
	// events. Dropping `change` there is the honest API, and the two ways out
	// before this key existed were both worse: atomic-replace, which makes every
	// partial client a silent mass-revoker, or inventing a mutable field the
	// model does not have so that the change verb has something to change.
	//
	// What a dropped verb costs is nothing else: its route, its command, its
	// wire types and its generated tests are simply not written. The collection
	// is still stored, still projected and still validated the same way, and the
	// root's own verbs still carry the whole of it.
	Operations []string `yaml:"operations"`
	// Permissions gates the PER-ENTRY verbs on their own, keyed by the same
	// names operations uses: add, change, remove. Per-child only, and every key
	// must name a verb the collection actually mounts.
	//
	// Absent — and absent per verb, since the map may be partial — the entry
	// verbs keep requiring what the root's update requires. That is the
	// inheritance every per-child collection has always had, and it stays the
	// default: re-gating a mounted route behind a permission the deployment does
	// not grant would start refusing callers who hold exactly what they were
	// told to hold, and the 403 would not say why.
	//
	// It exists because the collection edge is sometimes a DIFFERENT job from
	// editing the root. On an RBAC entity, "may rename the group" and "may
	// change what the group confers" are the same permission only by accident:
	// the second is the one that lets an administrator hand themselves power
	// they were not given. The large platforms separate them for that reason —
	// Entra ID guards a role-assignable group behind Privileged Role
	// Administrator rather than Groups Administrator, and IAM spells
	// AttachGroupPolicy as its own action, not as part of UpdateGroup.
	//
	// It is per COLLECTION and not per entity because an entity with two
	// collections has two edges, and gating both because one of them needed it
	// is how a permission stops meaning anything.
	Permissions map[string]string `yaml:"permissions"`
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

// PerChildOperations is the effective set of per-entry verbs a collection
// mounts: what `operations` declares, or the whole trio when it declares
// nothing.
//
// The default is all three because that is what per-child meant before the key
// existed — a spec written against the older language has to keep generating
// what it generated then, and silently mounting fewer verbs on a regeneration
// would take routes away from a running service.
func PerChildOperations(c Child) []string {
	if c.EditStrategy != "per-child" {
		return nil
	}
	if len(c.Operations) == 0 {
		return ChildOperations.List()
	}
	return append([]string(nil), c.Operations...)
}

// MountsPerChildOp answers the same question for one verb.
func MountsPerChildOp(c Child, op string) bool {
	for _, have := range PerChildOperations(c) {
		if have == op {
			return true
		}
	}
	return false
}

// PerChildPermission is what the collection DECLARES for one per-entry verb,
// or "" when it declares nothing for it.
//
// It answers only half the question on purpose: "" is not "no permission", it
// is "inherit", and what is inherited is the root's — which this package cannot
// see from a Child alone. The resolution happens once, in the IR, so every
// consumer reads the same answer.
func PerChildPermission(c Child, op string) string {
	if c.EditStrategy != "per-child" {
		return ""
	}
	return c.Permissions[op]
}

// InheritedChildPermission is what a per-entry verb falls back to: the
// permission the root's own update requires.
//
// The order mirrors the emitter's, and the emitter's order is the aggregate's:
// editing one entry is editing the aggregate, so it asks for what replacing the
// whole of it asks for. PUT before PATCH because a spec serving both gives them
// the same permission anyway, and insert last because a write-only entity has
// no update to borrow from.
//
// It is "" when the spec serves none of the three — a real shape (a collection
// on a display-and-archive entity), and the one case where inheritance has
// nothing to inherit. validateChildPermissions refuses it rather than letting
// the emitter write a route no permission can satisfy.
func InheritedChildPermission(s *Spec) string {
	ops := mountedOperations(s)
	for _, verb := range []string{"update", "patch", "insert"} {
		if !ops[verb] {
			continue
		}
		if p := s.Authz.Permissions[verb]; p != "" {
			return p
		}
	}
	return ""
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
	// ArchiveWhen makes an ORDINARY UPDATE retire the row: when the field
	// reaches the value named here, the domain finishes that write as an
	// archive. It is a lifecycle decision, which is why it lives here and not
	// among the rules — a rule refuses a write, this one changes what the write
	// IS.
	ArchiveWhen *ArchiveWhen `yaml:"archiveWhen"`
}

// ArchiveWhen is "a tenant moving to closing is really being archived": the
// caller sends a plain PUT/PATCH, and the DOMAIN — not the transport, not the
// client — decides the row should not be left active.
//
// It becomes one condition at the end of the generated IfUpdate clause, calling
// the framework's CompleteAsArchive(). The framework then runs the whole
// archive: the archive stamp, the child cascade, the ARCHIVED event the read
// side routes on, and an archive audit entry.
type ArchiveWhen struct {
	// Field is the entity field that decides — one field, the state. It must be
	// a persisted field of the root.
	Field string `yaml:"field"`
	// Equals is the value that means "retire this row".
	Equals string `yaml:"equals"`
	// Becomes is the value the field is left at, persisted with the archive.
	// Optional, and worth thinking about: the archive rules do NOT re-fire on
	// this path (the write is still an update as far as the rule set is
	// concerned), so without it the row is archived holding the trigger value —
	// which is right when that value is a real resting state ("cancelled") and
	// wrong when it is a request ("closing").
	Becomes string `yaml:"becomes"`
	// Description is one line on WHY this update retires the row. It becomes the
	// comment above the condition, where the next reader of the entity meets a
	// write that quietly changes verb.
	Description string `yaml:"description"`
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
	//
	// It DEFAULTS TO TRUE, and is a pointer so that `echoValue: false` can say
	// otherwise. The framework carries the value as NotificationMessage.
	// FieldValue and has since the beginning; leaving it out was the generator's
	// omission, not the framework's limit, and it costs the caller the only half
	// of the message they can act on — "at most 4 guardians" tells them the rule,
	// "you sent 6" tells them what to change.
	//
	// Turn it off for a value that should not travel back in a 422: a secret, a
	// document number, anything the response is not already allowed to carry.
	// The generator cannot tell — nothing in the language marks a field as
	// sensitive — so that judgement is the spec author's.
	EchoValue *bool `yaml:"echoValue"`
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
	// Fact names the service fact a factRange rule reads — an entry of
	// service.facts, by name. It is what turns a declared query into an enforced
	// invariant: the fact answers the number, min/max here say what the number
	// may be, and the generator writes the call, the comparison and the
	// notification. Any argument the fact takes is filled from the entity's own
	// field of the same name, exactly as the unique precheck does.
	Fact string `yaml:"fact"`
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

// Echoes answers whether this rule sends the rejected value back, applying the
// default an absent key means. It is the only reader of the pointer, so nothing
// downstream has to know that "unset" and "true" are the same answer.
func (r Rule) Echoes() bool { return r.EchoValue == nil || *r.EchoValue }

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
	//
	// A variable is filled by the RULE that raises the notification, from the
	// bound it already declares: `min`/`max` from a range or a length, and `max`
	// or `cap` from a groupCap's `cap:`. A name outside that vocabulary has
	// nothing to source it, so the message reaches the end user with a hole
	// where the value belongs — write the bound into the sentence only when no
	// rule owns it.
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

// CatalogCodes is the framework's catalog set, in the order every listing of it
// uses. All seven, always — a subset leaves some users reading a raw key.
//
// It is the ORDER; Map below is the MAPPING. Both are needed (a map has no
// order, and a list carries no yaml key), and both are written HERE, once:
// TestCatalogSetIsWrittenOnce fails if they drift, which is how a catalog added
// to one and not the other surfaces as a failing build rather than as a
// language that quietly renders nothing.
var CatalogCodes = []string{"ptbr", "eng", "esp", "fra", "deu", "ita", "nld"}

// Map renders the seven catalogs keyed by the framework's catalog codes, which
// is the shape everything downstream reads them in. It exists so the mapping
// between a yaml key and a catalog code is written ONCE: it used to be spelled
// out at each consumer, and a catalog added there would have been silently
// dropped here.
func (t Texts) Map() map[string]string {
	return map[string]string{
		"ptbr": t.PTBR, "eng": t.ENG, "esp": t.ESP, "fra": t.FRA,
		"deu": t.DEU, "ita": t.ITA, "nld": t.NLD,
	}
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
	// GroupBy turns the fact into a PER-GROUP one: the same aggregate, computed
	// by the database in a single GROUP BY, answered as one entry per distinct
	// key. Allowed on count, sum, avg, min and max.
	//
	// It is the difference between "how many rows match" and "how many rows
	// match, per category" — and it exists so a rule about a distribution is not
	// written by loading the rows and bucketing them in Go, which is the shape
	// this key is here to kill. Contrast with rules.list[].groupCap, which caps a
	// COLLECTION THIS WRITE carries: that count cannot come from the database,
	// because the entries being written are not in it yet.
	//
	// Each key field must be persisted and non-nullable: an entry with no value
	// belongs to no group, and counting the nulls together, apart, or not at all
	// are three different rules the spec has not chosen between.
	GroupBy []string `yaml:"groupBy"`
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
	// Managed exposes the framework-stamped columns on the READ side, by their
	// fixed logical names: CreatedAt, UpdatedAt, DeletedAt. Each one listed is
	// projected into the view, returned by the by-id read and by every listing
	// row, carried into the CSV/XLSX export, and may be named under
	// byParams.filters like any other field — "created between these dates" is a
	// question every listing eventually gets asked.
	//
	// It is DECLARED rather than automatic for the same reason a control is: a
	// field that appears in the reads without anyone asking changes the view's
	// shape, and the framework refuses to boot against a projection built to an
	// older one. Listing a column the storage does not declare is refused; the
	// stamped values themselves are the framework's business either way.
	Managed []string `yaml:"managed"`
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
	// Computed declares read fields that have no column: the value is DERIVED
	// per document, after the store answered, and the derivation is yours to
	// write in a hook file the generator never rewrites.
	Computed []Computed `yaml:"computed"`
	// IdentityView is whether this role creates the shared identity's own view,
	// joins it, or skips it. Refused by this build — the identity's own view is
	// not generated yet.
	IdentityView string `yaml:"identityView"`
}

// Computed is one derived read field — a value the reads return that no column
// holds. It is the read side's twin of a `manual` fact: the language declares
// the SHAPE (its name, its type, the fields it is derived from) and hands the
// body to a human, in a file the generator writes once and never touches again.
//
// What the declaration buys, beyond the field existing:
//   - `?fields=<name>` pushes the SOURCES down to the store instead of the
//     computed name, which has no column to resolve;
//   - `?orderBy=<name>` is refused with a typed 400 on every surface, because
//     ordering happens in the store and the keyset cursor is built from stored
//     values;
//   - the tabular exports keep the column, headed by its labelKey.
//
// It is therefore NOT filterable and NOT sortable — declaring it under
// byParams.filters is refused, for the same reason: a filter is evaluated in
// the store, and there is nothing there to evaluate.
type Computed struct {
	// Name is the field's name, in the same PascalCase spelling a persisted
	// field uses; the wire name is derived from it like any other field's.
	Name string `yaml:"name"`
	// Type is the derived value's type, from the same closed set a persisted
	// field draws from.
	Type string `yaml:"type"`
	// From names the persisted fields the derivation reads. They are what the
	// framework pushes to the store when the caller selects this field, so the
	// list has to be complete — a source left out is a source that arrives nil.
	From []string `yaml:"from"`
	// LabelKey is the translation-catalog key the tabular exports render as
	// this column's header; absent, the header is the wire name.
	LabelKey string `yaml:"labelKey"`
	// Text is the computed field's LABEL, per language catalog — same rules as
	// a persisted field's: a couple of words, optional, and a catalog left out
	// falls back to the field's own name.
	Text Texts `yaml:"text"`
	// Example feeds the OpenAPI example, exactly as on a persisted field.
	Example string `yaml:"example"`
	// Description is one line on what the value MEANS. It reaches the hook the
	// author has to implement, which is the one place it is genuinely needed.
	Description string `yaml:"description"`
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
	// Sort is the ordering VOCABULARY: the paths `?orderBy=` accepts. Nothing is
	// orderable until it is listed here — an unindexed sort is a blocking sort
	// whose cost grows with the matching set, so "every field the response
	// happens to render" is the wrong default and is no longer what the framework
	// does.
	//
	// It travels with controls.orderBy, and neither half is legal alone: the
	// control is the SWITCH that decides whether the endpoint takes the parameter
	// at all, this is WHICH paths it admits. A switch with no vocabulary would
	// accept `?orderBy=` and refuse every token it could be given; a vocabulary
	// with no switch tags paths that reach no wire. The framework fails the boot
	// on either, so both are refused here, where the spec can still say which
	// half is missing.
	//
	// A path may be any stored field the listing can resolve — a filter of its
	// own is not required, and one that is not filtered becomes a leaf that is
	// orderable and carries no value on the wire. A computed field is refused:
	// ordering happens in the store and there is no column to order by.
	//
	// `ID` is orderable on every entity and is declared nowhere: the aggregate
	// id is on the root's row whatever the spec says, the projector writes it as
	// the document's _id, and it is the one path that is indexed before anything
	// asks for it. It is also the tie-break the cursor already appends to every
	// key, so ordering by it is the cheapest total order a listing can offer.
	Sort []string `yaml:"sort"`
	// Controls turns on the framework's reserved query controls (pagination,
	// orderBy, fields, search, onlyTotal, includeArchived).
	Controls Controls `yaml:"controls"`
}

type Filter struct {
	// Field is the entity field the filter applies to. `ID` is always one of
	// them, without being declared anywhere: the aggregate id lives on the
	// root's row and the framework resolves the name itself.
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

// SuperAdminClaim is the framework's super-admin wildcard, as it appears in a
// permissions claim: the one claim that answers true to every concrete
// permission question.
//
// It is a constant rather than a literal because two halves of the build have
// to agree on it — the validator, which accepts it as an authz.bypass and
// refuses every other wildcard, and the emitters, which must never pass it to
// HasPermission (that panics) and call Identity.IsSuperAdmin instead.
const SuperAdminClaim = "*:*"

type Authz struct {
	// Resource is the permission resource name, usually the entity in lower
	// case: student, professor.
	Resource string `yaml:"resource"`
	// Permissions maps each mounted operation (insert, update, patch, delete,
	// archive, unarchive, read) to the permission it requires. Cross-checked
	// both ways against what is actually mounted.
	//
	// The house taxonomy is `<resource>:<action>` with the action spelling the
	// operation — insert:<r>:insert, update and patch both :update, delete
	// :delete, archive and unarchive both :archive, read :read. Two operations
	// share an action on purpose (PUT and PATCH are one update; whoever may
	// archive may put it back), and spelling insert as `create` or `write`
	// instead is how one project ends up granting three words for one verb.
	// A project with its own taxonomy keeps it: this is the default, not a rule.
	Permissions map[string]string `yaml:"permissions"`
	// DataAccess is who may reach which rows: anyone-with-permission = every
	// holder sees every row; owner-only = only their own; tenant = only their
	// tenant's.
	//
	// owner-only and tenant scope BOTH SIDES. The read side narrows what a
	// caller sees; the write side refuses a caller who creates, edits or
	// archives a row outside their scope, with the framework's
	// TenantMismatchNotification (403). Both halves are generated together on
	// purpose: for a while only the read half was, and the result looked
	// complete — a reviewer read tenant isolation on the listings and
	// reasonably concluded the posture was in place, while a caller with the
	// ordinary permissions could create a row inside another tenant, edit one
	// and archive one. The asymmetry was what made it dangerous: they could not
	// read back the row they had just archived.
	DataAccess string `yaml:"dataAccess"`
	// OwnerField is the persisted field that records the row's owner, for
	// owner-only access — declare it with assignedFrom: identity-subject.
	OwnerField string `yaml:"ownerField"`
	// TenantField is the persisted field the tenant claim is matched against,
	// for tenant access — declare it with assignedFrom: identity-claim.
	TenantField string `yaml:"tenantField"`
	// Bypass says WHO crosses the row scope — the platform operator supporting
	// a customer, who must read and repair rows that are not theirs. Without it
	// even a `*:*` holder is filtered to their own tenant like anybody else.
	//
	// Two spellings, answering two different policies:
	//
	//   bypass: platform:cross-tenant   a CONCRETE permission, grantable like
	//                                   any other. Everyone holding it crosses —
	//                                   which includes a `*:*` superadmin AND a
	//                                   holder of `platform:*`, because that is
	//                                   what holding a permission means.
	//   bypass: "*:*"                   the superadmin wildcard itself, and
	//                                   nothing narrower. Nothing new becomes
	//                                   grantable: what crosses is the claim a
	//                                   superadmin already carries.
	//
	// Take the wildcard when the policy is "a superadmin crosses" and minting a
	// permission would widen it — a concrete string can be handed to somebody
	// who is not a superadmin, which is not the policy that was approved. Take
	// the concrete permission when crossing is meant to be delegable on its own.
	//
	// A wildcard cannot be asked of HasPermission: it panics on one (the claim
	// wildcards; the question does not), which is why `bypass: role:*` is still
	// refused — there is nothing to ask it with. `*:*` is the exception because
	// the framework gives that one question its own method: the emitted guard
	// calls Identity.IsSuperAdmin(), which reports the grant directly.
	//
	// Refused unless dataAccess scopes the rows at all.
	Bypass string `yaml:"bypass"`
	// NoIdentity decides what an ABSENT identity means, which is a policy rather
	// than an accident.
	//
	// stand-down is the DEFAULT: the scope applies to every authenticated
	// caller and steps aside only where there is nobody to scope to. That is
	// what every other identity-derived rule this generator writes already does
	// — an ownerCheck tolerates an absent principal, and has to, since with
	// auth.mode disabled no request carries one — so a row scope that alone
	// failed closed would be the odd one out, and the surprise would land on the
	// bench where the entity is first run, as a service that serves nothing and
	// accepts nothing.
	//
	// It is safe because the generated guard asks whether an identity was
	// PRESENT, never whether the scope came out empty. Those are two different
	// facts arriving as one value, and only the first is confined to a bench:
	// the middleware is bypassable solely with auth.mode disabled, which the
	// framework's own boot guard allows under APP_PROFILE=dev alone, while a
	// real token that simply carries no such claim is an ordinary production
	// request — and is still refused.
	//
	// refuse is the opt-in for a service that wants the scope enforced even with
	// authentication off: no rows, and no scoped write, on a bench included.
	//
	// Refused unless dataAccess scopes the rows at all.
	NoIdentity string `yaml:"noIdentity"`
}

// ValueObjectsNamed is every value-object type a spec depends on: the ones it
// declares AND the ones its fields reuse from elsewhere in the project.
//
// The distinction that matters to a caller is that there is none — a reused
// value object is a type this spec's code will not compile without, exactly
// like one it declares. Anything deciding whether a type is still needed has to
// count both.
func ValueObjectsNamed(s *Spec) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, vo := range s.ValueObjects {
		add(vo.Name)
		// A composite's part may itself be a value object, and a part that
		// REUSES one is a dependency exactly like a field that does: the
		// generated composite does not compile without it.
		for _, p := range vo.Parts {
			if p.VO != nil {
				add(p.VO.Ref)
			}
		}
	}
	fields := append([]Field{}, s.Fields...)
	for _, c := range s.Children {
		fields = append(fields, c.Fields...)
	}
	for _, sib := range s.Siblings {
		fields = append(fields, sib.Fields...)
	}
	for _, f := range fields {
		if f.VO != nil {
			add(f.VO.Ref)
		}
	}
	return out
}
