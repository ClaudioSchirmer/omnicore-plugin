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
	// Joins are the entity's READ JOINS: read-only traversals across a foreign
	// key into ANOTHER aggregate, declared on the repository.
	//
	// They sit here rather than under read: or storage: because that is where
	// the framework puts them, and the placement is the point. A join is not
	// storage — the TableSchema is untouched, so no INSERT or UPDATE can ever
	// carry a joined column, and a Mongo projection over this entity is
	// unaffected. And it is not the read model either — it hangs off the LOADER,
	// so every read through it inherits the reach at once: FindByID (which the
	// write-side handlers load through), a hand-written service call, and a
	// relational read model declared over the same loader.
	//
	// That last consequence is the one worth knowing before writing a rule: a
	// business rule that needs a value belonging to another aggregate no longer
	// has to copy that column into this table. Declare the traversal here, and
	// the field is an ordinary field of the entity, filled on every load.
	Joins []Join `yaml:"joins"`
	// Read configures the read side: the backing store, the view, and which read
	// operations (by id, by params) are served.
	Read Read `yaml:"read"`
	// Surfaces decides where the entity is exposed: REST, GraphQL, and file
	// exports.
	Surfaces Surfaces `yaml:"surfaces"`
	// Authz names the permission resource, the permission each operation
	// requires, and who may reach which rows.
	Authz Authz `yaml:"authz"`
	// Docs is the CALLER-FACING prose that reaches the OpenAPI document —
	// multi-line markdown, written for whoever is about to call the endpoint.
	Docs Docs `yaml:"docs"`

	// SourcePath is where this spec was loaded from. Not a YAML key.
	SourcePath string `yaml:"-"`
}

// ---------------------------------------------------------------- docs

// Docs is the prose an OPERATION carries in the OpenAPI document, below the
// sentence the generator writes for its verb.
//
// It exists because every other description in this language is written for a
// DEVELOPER reading the generated tree — storage.description reaches a
// migration comment, a field's reaches a Go doc comment, a rule's reaches the
// hook file. None of them reach Swagger, and none of them should: "stored as
// two columns" is a fact about the table, not about the call.
//
// What a caller needs is different, and there was nowhere to write it. The case
// that forced the key: an entity whose composite value object is exposed as two
// separate wire fields — a resource and an action that are two halves of ONE
// value, rendered as `resource:action`. Nothing in the generated document said
// so, so the two fields read as unrelated strings and the composed form was
// discoverable only by reading the domain code.
//
// It is MARKDOWN and it is MULTI-LINE. Swagger UI renders the operation
// description as markdown — paragraphs, `code`, **bold**, lists — and the
// framework already relies on that (it appends the required permission in
// bold). Write it as a YAML block scalar and use blank lines between
// paragraphs.
type Docs struct {
	// Description is appended to EVERY operation this entity mounts — the reads
	// and the writes alike. Put here what is true of the resource however it is
	// being touched: what its fields mean together, which values are composed,
	// what the caller has to know before sending anything.
	//
	// It is APPENDED, never a replacement: the sentence the generator writes for
	// the verb ("Partial update: only the fields present in the body change…")
	// is framework behaviour the author does not own, and an entity that could
	// overwrite it would be one where a caller silently stops being told that
	// PATCH cannot set a value back to null.
	Description string `yaml:"description"`
	// Operations adds prose to ONE operation, keyed by the operation the route
	// serves: insert, update, patch, delete, archive, unarchive, byId, byParams.
	//
	// The keys are the OPERATIONS, not the modes and not the authz vocabulary —
	// `read` is two endpoints with two different things to say, and a listing's
	// filter vocabulary is rarely what a by-id read needs explained.
	//
	// Both halves compose: an operation named here receives Description first
	// and then its own paragraph, so what is true of the whole entity is not
	// restated per verb.
	Operations map[string]string `yaml:"operations"`
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
	//
	// It is about the VALUE, not about who supplies it, and the two questions
	// are independent: a field the server assigns may still have no value. It
	// combines with assignedFrom:AssignedFrom string `yaml:"assignedFrom"` // identity-subject | identity-claim | client-ip | derived for exactly that — a verification
	// timestamp no caller may set and no row has until it is verified. The one
	// pairing refused is with the identity sources, where the server always has
	// the value it writes.
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
	// Hidden keeps a PERSISTED field out of every response body — the by-id read,
	// each row of the listing, the write results, the CSV/XLSX exports that render
	// the listing, and the `?fields=` vocabulary, where naming it is a typed 400
	// rather than an empty object; it governs what a caller RECEIVES, and never
	// what the database is asked for.
	//
	// Everything else is unchanged — the column exists, the filters, sort and
	// indexes reach it, a write may set it, the rules read it, and a computed read
	// field may derive FROM it.
	//
	// On a relational read model that last clause is stronger than it reads. Such
	// a read builds no SELECT of the projected fields at all: it loads the WHOLE
	// aggregate through the write side's loader — one implementation of the
	// criteria→SQL translation, shared with the repository — turns the hydrated
	// entity into a document, and prunes that document in memory. So a hidden
	// column is still selected from the table and still sits in memory on every
	// row of every page, and `?fields=` does not narrow the query either. Hiding a
	// value is not a way to stop fetching it.
	//
	// Where that distinction matters — a credential hash — the lever is not this
	// key. It is `redact` over a MONGO-backed read, whose document IS the redacted
	// payload, so the masked value never reaches the read side at all. Over a
	// relational read there is nothing to reach for: the read is the table.
	//
	// It is the answer to "callers query by this, and receive something else":
	// three columns narrow the search, and what comes back is a description and
	// a derived value. Without it the only way to say that was read.fieldRestrict,
	// which answers something different — 403 to a caller who lacks a permission,
	// and the field to everyone who has it. This one is not about who is asking.
	//
	// Refused on a runtime-only field, and on a field read.fieldRestrict also
	// names: a field nobody receives cannot be the one some callers may.
	//
	// A runtime field is out of every response by DEFAULT — there is no row for a
	// read to render — so hiding it removes nothing. The direction that field can
	// travel is the opposite one, and it has its own key: renderIn puts a
	// source: manual value into the response of the write verb that minted it.
	Hidden bool `yaml:"hidden"`
	// Runtime marks the field as runtime-only: it reaches the entity and the
	// rules read it, and no column ever holds it. WHERE its value comes from is
	// the separate question source answers.
	Runtime bool `yaml:"runtime"`
	// Source says where a runtime-only field is fed from. It is the whole answer
	// to "how does a fact about the CALLER reach a rule", and the domain has no
	// other one: an entity is handed itself and nothing else — no ctx, no
	// Identity, no request — so a session fact reaches BuildRules only by riding
	// a field the command mapper wrote on the way in.
	//
	// `claim` (the default, and the only thing runtime used to mean) reads the
	// token BY NAME: Claims["email"], as a string, or as a bool for a yes/no.
	//
	// `body` is the state the field model had no spelling for: a value that
	// crosses the request DTO, the command and the entity — so a rule can check
	// it — and stops there. The canonical instance is a password confirmation,
	// which exists to be compared against the password and whose storage would
	// itself be the bug. Nothing about such a field reaches the TableSchema, the
	// migration, the outbox payload, the audit event or any response: there is no
	// column, so there is nothing to redact and nothing to leak — including the
	// one seat that is not a copy of the row, the refusal itself, where the
	// rule's echoValue is overridden and the value is left out.
	//
	// `manual` is the ELSE this language reaches for everywhere else — vo.kind,
	// facts[].kind, rules.manual, written — applied to a field: the generator
	// declares it on the AGGREGATE and fills it from nowhere. No write request
	// DTO, no command, no mapper and no OpenAPI schema mentions it, because no
	// generated verb has anything to put there. Your code does.
	//
	// It is for the operation this language cannot declare. A hand-written
	// change-password endpoint dispatches the same mode a generated PATCH does
	// and is told apart by its action name; its `currentPassword` has to reach
	// the aggregate for a rule to prove possession, and must not appear in the
	// body of the ordinary PATCH. Neither existing answer could say that:
	// `source: body` puts the field on every verb `modes` names, and naming none
	// of them read as naming all of them — `modes: []` fell into the "omitted"
	// branch and generated the field onto every write verb, silently. Without the
	// spelling the two ways out were a column nobody wanted, or moving the proof
	// of possession out of the aggregate and into the handler, which is where the
	// rest of the service deliberately does not keep its rules.
	//
	// A `vo:` belongs on one, and works: the automatic pass walks the STRUCT, so
	// the value object would be judged on every generated write — where nothing
	// filled the field — and the same per-verb exclusions a `source: body` field
	// already gets are written for it, unconditionally, since no generated verb
	// carries it at all.
	//
	// What it costs, and the report says so: nothing the generator writes puts a
	// value there. A field left unfilled is not an error anywhere — it reads ""
	// (or false, or zero) and every rule over it judges that.
	//
	// The remaining five are the framework's OWN questions about the caller,
	// each asked through the accessor that answers it, and each of them was
	// previously reachable only when the generator synthesised the field for a
	// row scope — never when an author wanted the same fact for a rule of their
	// own:
	//
	//   - subject      → Identity.Subject, the authenticated principal (string).
	//   - tenant       → Identity.TenantID, the configured tenant claim (string).
	//   - permission   → Identity.HasPermission(permission), the caller's grant
	//                    for one concrete resource:action (bool).
	//   - super-admin  → Identity.IsSuperAdmin, the `*:*` grant (bool). It is a
	//                    separate source because HasPermission PANICS on a
	//                    wildcard: the claim wildcards, the question does not.
	//   - present      → whether the request carried an identity AT ALL (bool),
	//                    which is a fact no value can carry: an empty subject
	//                    means "nobody" and "a real token without that claim",
	//                    and a rule that stands down for the first stands down
	//                    for the second too unless it can ask this.
	//
	// `permission` is the one worth reading twice, because `claim` LOOKS like
	// it. A `claim: is_admin` field reads a boolean the token happens to carry:
	// no resource wildcard, no `*:*`, and blind to authorization.permissionsClaim.
	// `source: permission` asks the model the rest of the service is gated by.
	// Every workaround this key replaces was worse than the field: a hand-written
	// command mapper (the file a regeneration overwrites most), or a manual
	// service fact, which puts a question about the SESSION on a port whose
	// implementations talk to the database.
	Source string `yaml:"source"` // claim | body | manual | subject | tenant | permission | super-admin | present
	// Claim names the JWT claim a runtime-only field is fed from. It is required
	// for a source: claim field — the framework deliberately does not opine on
	// which custom claims a token carries, so any convention here would be a
	// guess — and refused on every other source, none of which looks a claim up
	// by name: `body` never reads the token at all, and the identity sources ask
	// the framework's own accessors, which own the claim names they consult.
	Claim string `yaml:"claim"`
	// Permission is the concrete resource:action a source: permission field asks
	// about. Required there and refused everywhere else.
	//
	// It is its OWN key rather than a second meaning for `claim`, which already
	// means "the name of a claim" — and a key that means two things is the one
	// nobody reads correctly. The language refuses `claim` outside its source for
	// exactly that reason; this side of the pair is held to the same rule.
	//
	// A wildcard is refused, with one exception that is not written here at all:
	// `*:*` is `source: super-admin`, because the framework answers that question
	// with a different method. Anything narrower — `users:*` — has no question of
	// its own and would panic at the first request, which is the failure this
	// refusal exists to move to load time.
	Permission string `yaml:"permission"`
	// Modes are the write verbs whose BODY carries a source: body runtime field.
	// Omitted means every write verb the entity has.
	//
	// An EMPTY list is refused rather than read as "no verb". It used to be
	// neither: `modes: []` decodes to a list nobody declared anything in, fell
	// into the same branch as an absent key, and generated the field onto every
	// write verb — the exact opposite of what it says, with `check` answering
	// that the spec could be generated. "On no verb" is a real thing to want and
	// it has its own spelling: source: manual.
	//
	// The values are the two the DOMAIN can tell apart — insert and update —
	// because that is the granularity of the rule gates: a PATCH is an update
	// down there, dispatched into the same IfUpdate clause, so `update` covers
	// both write shapes and there is no third value to write.
	Modes []string `yaml:"modes"`
	// RenderIn names the write verbs whose RESPONSE renders a source: manual
	// runtime field. It is the output-side counterpart of modes, and the whole
	// answer to "the server minted this value and the caller has to receive it
	// exactly once".
	//
	// Without it the language could declare a machine credential and not hand it
	// over. A client secret is minted from crypto/rand inside the insert rules,
	// hashed, and only the hash is stored: no column, no outbox payload, no audit
	// event, by design. The insert then DISCARDED the plaintext, because the
	// Result and the Response are built from the persisted fields — so a row was
	// born with a credential nobody could ever learn, and the only way out was a
	// hand-written rotation endpoint or an `adopt` that freezes two owned files
	// forever in exchange for one field.
	//
	// What it emits, and nothing else: the write Result gains the field, and its
	// FromEntity reads it off the ENTITY after the write, next to the persisted
	// projections; the write Response gains it too, so the generic Result→Response
	// pair still lines up at boot. No column, no migration, no request schema, no
	// read DTO, no `?fields=` vocabulary, no export column, no audit event and no
	// sync payload — the value was never on the row, and this key does not put it
	// there.
	//
	// The value set is the one `modes` uses, deliberately: `update` covers PUT and
	// PATCH, because a key that spells the same axis differently on its two sides
	// is the key nobody reads correctly. A verb the entity does not declare is
	// refused — a response that never renders is not a promise this language will
	// print in a report.
	//
	// It is accepted on source: manual and REFUSED on every other source, which is
	// not a limitation but the point:
	//
	//   - `body` is a value the CALLER sent, and echoing it back hands someone
	//     their own password confirmation from a surface nobody expected to carry
	//     one. The generator has always refused that; a key that reopened it by
	//     spelling would be the same leak with permission.
	//   - the identity sources — claim, subject, tenant, permission, super-admin,
	//     present — are facts the caller already holds. Rendering them is
	//     reflecting the token back at whoever presented it.
	//
	// So it is refused on a persisted field as well: that side is already served
	// by `hidden` (nobody receives it) and `redact` (the copies carry a mask), and
	// a persisted field is in the response by default anyway.
	//
	// Two consequences the docs say out loud rather than letting a reader
	// discover:
	//
	//   - a GraphQL mutation that reuses the same Response renders the field too.
	//     One Response type serves both surfaces; there is no third shape.
	//   - nothing this generator writes PUTS a value there — that is what
	//     source: manual means. A rules.manual entry scoped to the verb has to
	//     mint it, and the report asks for it by name. Unfilled, the field renders
	//     its zero value and no error says so.
	RenderIn []string `yaml:"renderIn"`
	// AssignedFrom says the SERVER fills this persisted field, so the client
	// never sends it: it is absent from every write request, every command and
	// the OpenAPI request schema.
	//
	//   - identity-subject / identity-claim — the value comes from the caller's
	//     token, and the generator writes the assignment. Written on insert and
	//     left alone afterwards, which is what makes "who created this row" a
	//     fact rather than a claim.
	//   - client-ip — the request's network origin, as the framework resolved
	//     it (AppContext.ClientIP()). It is NOT in the token, so it is read
	//     straight off the context and is written whether or not a caller is
	//     authenticated. Written on insert and left alone afterwards, like the
	//     identity sources: it records where the row CAME FROM, not where the
	//     last edit came from. Always type: string.
	//
	//     It is the one assigned source whose value can legitimately be absent
	//     while the write succeeds: a consumer handler, a background job or a
	//     test fixture has no inbound request. Declare the field nullable to
	//     record that as NULL; leave it non-nullable to record it as the empty
	//     string. Either is honest — the choice is what your reader should see.
	//
	//     What the value is WORTH is a deployment question this spec cannot
	//     answer: behind a reverse proxy the framework reads the socket peer
	//     (the balancer) until `http.trustProxy` is declared. A network control
	//     built on this column depends on that block, not on this key.
	//   - derived — the value comes from the entity's OWN fields, like a public
	//     key computed from an immutable handle. The generator writes no
	//     assignment for it: what computes it is a rules.manual entry scoped to
	//     insert, and the report lists that as owed. Declared here so the field
	//     stops being advertised as writable while the server silently
	//     overwrites whatever a caller sent. This is the one source that
	//     combines with nullable: what fills it is code the author writes, and
	//     that code may legitimately leave the value unset — a verification
	//     timestamp is null until the thing is verified, and the alternative was
	//     a non-nullable column rendering the zero time in every response.
	//     The identity sources refuse nullable, because the server always has a
	//     subject and always has the claim it required.
	AssignedFrom string `yaml:"assignedFrom"` // identity-subject | identity-claim | client-ip | derived
	// Stamped hands the column's VALUE to the framework while leaving its WHEN
	// to the domain. It is the seat createdAt/updatedAt do not cover: those date
	// the ROW — written, last touched — on a schedule the framework fixes, while
	// a stamped column dates a FACT the business decides has just happened
	// (signed, paid, approved, cancelled) or COUNTS one (failed attempts,
	// retries).
	//
	//   time    — StampedTimeField. The column is a nullable timestamp and the
	//             Go field is *time.Time: until something stamps it, the fact
	//             has not happened, and nil says that where a zero time would
	//             report year 1. Requires type: time and nullable: true.
	//   counter — StampedCounterField. The column is an int64 the framework
	//             fills with 1 on the insert and with `col = col + 1` on every
	//             write that asks — computed by the server under the row's lock,
	//             so two racing increments cannot collapse into one. PER ROW,
	//             never a table-wide sequence. Requires type: int64; nullable is
	//             the author's call and means one thing only — a nullable
	//             counter is emitted over *int64, which is the only shape
	//             `e.StampNull(...)` can land in. The increment is the server's
	//             on both.
	//
	// Both take the field OUT of every write surface — no request DTO, no
	// command, no mapper, no OpenAPI request schema — for the same reason
	// assignedFrom does, and one it does not have: the column is never written
	// from the struct at all. Assigning it by hand does nothing, and on a write
	// that did not ask for it the column is left out of the statement entirely,
	// which is why an already-stamped row keeps its value with nobody having to
	// remember to preserve it. Everything on the READ side is ordinary — it
	// filters, sorts, projects, exports and hydrates like any field.
	//
	// WHO ASKS is not generated, and the report says so by name. The request is
	// `e.Stamp("PaidAt")` on the entity, and nothing in this language knows the
	// moment a domain calls a fact done: the rule DSL validates, it does not
	// mutate. So a stamped field is declared here and filled by a rules.manual
	// entry you write — the same division `assignedFrom: derived` already makes,
	// and the same one the framework makes when it refuses to schedule the
	// instant itself. The same entry is where a fact that UN-happens is written:
	// `e.StampNull("PaidAt")` clears the column (an ABSENCE, so it needs a field
	// that can hold one — a stamped time always can, a counter only when
	// declared nullable) and `e.StampEmpty("PaidAt")` writes the declared type's
	// ZERO (0 for a counter, the zero instant for a time), which is the only
	// reset a NOT NULL column has. Both are requests, exactly like Stamp: a
	// column nothing named is left out of the statement, so nothing is cleared
	// by omission.
	//
	// Refused on a runtime field (no column to stamp), on a facet's field (a
	// sibling row is a 1:1 slice of the OWNER's row and carries no
	// framework-owned columns of its own — declare it on the owner, which is the
	// framework's own refusal), and in combination with assignedFrom, vo, unique
	// or redact: a value the framework mints is not read from an identity, is
	// not a value object, is not a business key, and has nothing to mask.
	Stamped string `yaml:"stamped"` // time | counter
	// BypassMaySet lets the caller who crosses the ROW SCOPE state this value
	// instead of having it read off their own identity.
	//
	// It closes the hole `assignedFrom` opens on its own. The server filling the
	// tenant from the caller's claim is right for every ordinary caller and
	// wrong for exactly one: the operator holding `authz.bypass`, who may read
	// and repair another tenant's rows and — until this key — had no field in
	// which to say which tenant a NEW row belongs to. The workaround both
	// consumers who hit it reached for was to drop assignedFrom entirely and
	// put the value in the body with a rule, which makes every ordinary caller
	// send a value the server already knows.
	//
	// What it changes, and only on the INSERT: the field joins that one request
	// body as an OPTIONAL value. Absent means "mine", which is what the identity
	// already wrote. Present, it is written onto the entity whoever sent it —
	// deliberately, because the row-scope guard is then the thing that answers a
	// caller who may not state one, with the same notification a write into a
	// foreign tenant already gets. Silently dropping the value instead would
	// answer 201 and create the row somewhere else.
	//
	// Refused unless the field IS the row scope's subject (authz.ownerField or
	// authz.tenantField) and authz.bypass says somebody crosses it: on any other
	// field nothing compares what the caller sent, so the body value would be
	// taken from everyone.
	BypassMaySet bool `yaml:"bypassMaySet"`
	// Redact keeps the real value in the column and in the hydrated entity while
	// masking it in every copy the framework makes of the row — the outbox
	// payload (and so the topic, the consumers, the failure ledgers and the
	// projected document) and the audit event. Both of its axes are mandatory.
	Redact *Redact `yaml:"redact"`
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
	// Redact masks THIS PART in the copies the framework makes of the row,
	// independently of its siblings inside the value object: the currency of a
	// salary is not sensitive, the amount is.
	Redact *Redact `yaml:"redact"`
}

// Redact declares that a field's real value stays in its column and in the
// hydrated entity, and appears MASKED in every copy the framework makes of the
// row. It is the spec's form of the framework's RedactedField, and it REPLACES
// the plain field declaration rather than decorating it — the column is still
// declared once, here, with a redaction policy attached.
//
// The line it draws is WHO MAKES THE COPY. The framework copies a row by itself
// into the outbox payload (and from there the topic, every consuming service,
// both failure ledgers and the projected document) and into the audit event.
// Those copies are what a redactor governs. Exposure on a READ surface is
// yours: `hidden: true` takes the field out of every response body, and
// read.fieldRestrict decides who receives it. Nothing here refuses a read —
// filters, orderBy, ?search= and the exports keep working exactly as before.
//
// BOTH axes are mandatory. A missing one is refused rather than defaulted,
// because the two answers a default could pick are "leak" and "guess", and
// neither is a decision this generator gets to make on your behalf. Write
// `{kind: plain}` to keep the real value on an axis, out loud.
type Redact struct {
	// InSync is how the field appears in the copies the SYNC pipeline carries:
	// the outbox payload, and with it the topic, every consuming service, the
	// two failure ledgers and the projected document. The composer applies the
	// same redactor, so a rebuild cannot reintroduce what the sync excluded.
	InSync *Redactor `yaml:"inSync"`
	// InAudit is how the field appears in the AUDIT EVENT — the audit_events
	// row, the slog echo and the /audit endpoint. It is applied after the delta
	// is computed, so the trail still records THAT the field changed without
	// recording what to.
	InAudit *Redactor `yaml:"inAudit"`
}

// Redactor is one axis of a redaction: which mask, and its parameter. The
// family is closed and small on purpose — there is no "omit", because an absent
// key already means "the 1:1 facet row was removed" in the payload contract and
// an absent audit entry is the very information the delta exists to carry.
type Redactor struct {
	// Kind is the mask: plain (the real value, said out loud), fixed (a constant
	// replacement), keep-last (every rune but the last n), hook (a function you
	// write).
	Kind string `yaml:"kind"`
	// Value is the replacement a `fixed` redactor writes, in the field's own
	// wire format — "***" for a string, "0" for a number, "false" for a bool,
	// an RFC 3339 instant for a time. It must carry the column's own type, or
	// the payload breaks the type map the read side decodes through and the
	// view's $jsonSchema stops validating. Required for `fixed`, refused for
	// every other kind.
	Value string `yaml:"value"`
	// Keep is how many trailing runes a `keep-last` redactor leaves visible —
	// the partial mask for a document number, a card or a phone. A value with
	// that many runes or fewer is masked ENTIRELY, because keeping it verbatim
	// would disclose the whole value precisely for the shortest inputs.
	// Required for `keep-last`, refused for every other kind.
	Keep int `yaml:"keep"`
}

type Unique struct {
	// Enforce is how uniqueness is guaranteed: service-precheck+constraint asks
	// a Service fact before the database constraint answers; constraint-only
	// relies on the constraint alone.
	Enforce string `yaml:"enforce"`
	// Notification names the conflict answer a duplicate raises — a custom
	// <Field>AlreadyExists… notification declared under notifications.
	Notification string `yaml:"notification"`
	// AttachTo names the field the conflict is reported against; defaults to
	// this field (to the value object's own name, for a composite).
	//
	// It is the same key rules.list[] carries, and it is here for the same
	// reason: the default is the right answer only when the field a caller
	// should look at IS the field the uniqueness is declared on. A composite is
	// where that breaks first — the conflict lands on the value object's field
	// name, which may be an internal spelling (`Key`) rather than the one the
	// concept goes by everywhere else.
	//
	// It must name a field of this entity: a notification points a caller at
	// something they can change, and a free label points at nothing.
	AttachTo string `yaml:"attachTo"`
	// EchoValue passes the refused value back with the conflict, so the answer
	// says WHICH value is taken rather than only that one is — ON BY DEFAULT,
	// turned off with `echoValue: false`.
	//
	// The default and the exception are in the opening sentence for the reason
	// rules.list[].echoValue says it there: `explain keys` prints one sentence,
	// and an author who reads "passes the refused value back" and stops
	// concludes the key is opt-in.
	//
	// It earns the default more than most rules do: "that handle is taken" and
	// "administrator is taken" are different messages, and the caller picked the
	// word. Turn it OFF for a value the response is not already allowed to carry
	// — a document number, an e-mail, anything a 422 body and every log that
	// renders a notification should not repeat back. The generator cannot tell
	// which those are; nothing in this language marks a field as sensitive.
	//
	// A COMPOSITE is the case where it is off by default in effect and must be
	// asked for: what was refused is the TUPLE, and no single part stands for it
	// — echoing one half points at the wrong thing. Asking for it echoes the
	// value object as a whole, which requires the type to render itself
	// (`String()`), so it is accepted only on a `written: manual` composite: one
	// this generator writes declares no String(), and the echo would hand back a
	// formatted struct.
	EchoValue *bool `yaml:"echoValue"`
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

// Echoes reports whether the conflict carries the refused value. Absent means
// yes, which is the same reading Rule.Echoes gives the identically named key —
// one spelling, one default, in both places an author meets it.
func (u Unique) Echoes() bool { return u.EchoValue == nil || *u.EchoValue }

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
	// DescriptionKeys asks for a translation key per enum member. Refused —
	// the per-value catalog entries are not asked for by a flag: EVERY member is
	// registered under the key the framework derives for it ("<Type>.<value>",
	// what domain.EnumDescriptionKey answers), and what fills that entry is the
	// member's own text. Declare members[].text instead.
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
	// Text is the member's LABEL per language catalog, registered under the key
	// the framework derives for its VALUE ("<Type>.<value>") and resolved at the
	// boundary by translator.EnumDescription — it does NOT change the wire.
	//
	// The key is domain.EnumDescriptionKey's, which reflects over the value:
	// "SituacaoCurso.aberto" for a string backing, "NivelContrato.1" for an int
	// one. Never the member's Go name — an entry under that would be complete
	// and never found.
	//
	// It is a LABEL: a couple of words, and a catalog left out falls back to the
	// member's own name rather than to a placeholder. A catalog left out is
	// named in the report.
	//
	// It does NOT change the wire. REST, GraphQL and gRPC carry the raw value in
	// every language by the framework's own design; this is what a screen asking
	// for a human-readable label gets instead of the bare key.
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
	// Change is HOW the change verb takes its body — the entry-level twin of the
	// root's `update` block, and deliberately the same two keys in the same
	// words: shape (patch | put | both) and patchExcludes.
	//
	// It exists because operations, permissions and surfaces answer WHICH verbs,
	// WHO may call them and WHERE they answer, and nothing answered WHAT THE BODY
	// IS. So a change was a PUT and only a PUT: a full replacement of the entry,
	// business identity included. On a collection whose identity is a foreign key
	// and whose one editable value is a single field, that costs the caller a
	// round trip to re-send a value the server already holds — and buys a second
	// thing nobody asked for, since the identity arrives writable. The entry's
	// row id survives, so re-keying it reads in the history as one grant becoming
	// another instead of a revoke plus a grant, which is the exact shape
	// `operations` documents as the reason to drop `change` altogether.
	//
	// Absent means put, which is what change meant before this key existed: a
	// spec written against the older language keeps generating what it generated
	// then. `both` mounts the two, the same way update.shape: both mounts PUT and
	// PATCH at the root.
	//
	// The one thing this key does NOT do implicitly is protect the business
	// identity. A partial change that accepts an identity field is refused at
	// validation with the line to write — `patchExcludes` — rather than having
	// the exclusion applied behind the author's back: the root spells the same
	// decision out loud for its natural key, and a child that spelled it
	// invisibly would be one more rule to know about a language whose whole
	// value here is that it reads the same at both levels.
	Change *ChildChange `yaml:"change"`
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
	// Surfaces narrows WHERE the per-entry verbs are exposed, keyed the same way
	// the entity's own surfaces block is. Per-child only, and absent means the
	// collection follows the entity: every verb it mounts appears on every surface
	// the entity serves.
	//
	// Following is the default because the alternative is what this key was added
	// to end. A collection verb used to reach REST alone, with no way to say
	// otherwise, so a GraphQL-only consumer could create a role and never grant it
	// a permission — the write side of an aggregate silently halved, and nothing
	// in the spec, the generated code or the report said so.
	//
	// It sits beside operations and permissions because the three are the same
	// decision asked three ways — WHICH verbs the collection has, WHO may call
	// them, WHERE they answer — and an author who narrows one usually wants to see
	// the other two while doing it.
	Surfaces *ChildSurfaces `yaml:"surfaces"`
	// BusinessIdentity names the fields that make two entries THE SAME entry —
	// the duplicate detector, and the match key per-child operations use.
	BusinessIdentity []string `yaml:"businessIdentity"`
	// Fields are the entry's own fields.
	Fields []Field `yaml:"fields"`
	// Computed declares derived READ fields on the ENTRY — the per-entry twin of
	// read.computed, and the seat a root derivation cannot be.
	//
	// The root's derivation runs once per document and is handed values; what
	// the root holds for a collection is a SLICE, so it has no single entry to
	// be handed and no way to produce one answer per row. Declared here, the
	// derivation runs once per ENTRY and takes that entry's own fields — which
	// is the shape of every "one label per grant", "one flag per line" question,
	// and the reason the root key alone was not enough.
	//
	// `from` names the ENTRY's fields, bare and unqualified: the entry is the
	// scope, so there is no collection to name in front of them. That is also
	// what the framework expects — it records a nested field's computed sources
	// under the same segment prefix as the field itself, so
	// `?fields=<collection>.<name>` pushes `<collection>.<source>` down without
	// either side having to spell the segment out.
	//
	// READ ONLY, like its root twin, and one step more so: the write verbs
	// return an entry through its own `<Entry>Response`, which carries what was
	// STORED. A derived value there would be computed from the entity the caller
	// just sent rather than from the document the store answered with, and those
	// are not the same question.
	//
	// It reaches REST and GraphQL, and NOT the tabular export — a CSV/XLSX row is
	// flat, so no field of a collection is in one, derived or stored. The
	// labelKey is still registered in every catalog: it is what a flattening
	// export would need, and it costs nothing until there is one.
	Computed []Computed `yaml:"computed"`
	// Rules are the invariants checked on each entry of the collection.
	Rules Rules `yaml:"rules"`
	// SoftRemove keeps the row: the entry is archived (see archivedAt) instead
	// of deleted, and per-child removal mounts as an archive rather than a
	// DELETE — the verb has to say which of the two it performs.
	//
	// It does NOT make removal reversible. There is no per-entry unarchive:
	// children[].operations is closed at add|change|remove, unarchive is a ROOT
	// mode, and an archived entry is not loaded into the aggregate, so no
	// command can address it. What the archive buys is history and a row that
	// whatever references it still finds — not a way back. A collection whose
	// entries genuinely need archive⇄unarchive is an entity of its own.
	SoftRemove bool `yaml:"softRemove"`
	// ArchivedAt is the column marking an archived entry; required when
	// softRemove is on, refused when it is off.
	ArchivedAt string `yaml:"archivedAt"`
	// DuplicateNotification names the conflict answer a per-child ADD raises
	// when the entry is already there.
	DuplicateNotification string `yaml:"duplicateNotification"`
}

// ChildChange is the body contract of one collection's change verb.
//
// It is Update with a different seat, on purpose: same key names, same closed
// set, same meaning. An author who has decided what the root's update looks
// like has already decided how to say it here, and a second vocabulary for the
// same question is how a spec ends up declaring one thing for the parent and
// another for the child.
type ChildChange struct {
	// Shape is which change verbs exist: patch | put | both. PATCH cannot say
	// "set this to null" here either — an absent field and an explicit null are
	// the same thing on the wire — so a collection with a clearable field keeps
	// put among its shapes.
	Shape string `yaml:"shape"`
	// PatchExcludes names entry fields a partial change may NOT touch. The
	// business identity belongs here whenever patch is served: without it the
	// verb can re-key the entry while keeping its row id, which is a removal and
	// an addition wearing an edit's clothes.
	PatchExcludes []string `yaml:"patchExcludes"`
}

// ChildChangeShape is HOW a collection serves its change verb: put (the
// default), patch, or both — and "" when it mounts no change at all.
//
// The default is put because that is the only shape the language could express
// before children[].change existed. Reading it as anything else would take a
// running service's PUT away on a regeneration that changed no key.
func ChildChangeShape(c Child) string {
	if !MountsPerChildOp(c, "change") {
		return ""
	}
	if c.Change == nil || c.Change.Shape == "" {
		return "put"
	}
	return c.Change.Shape
}

// ChildServesPut and ChildServesPatch split that answer into the two verbs the
// emitters mount. They are asked one at a time everywhere — a route, a command,
// a wire pair and a test per verb — so the shape string is resolved once, here,
// and no consumer re-reads the default.
func ChildServesPut(c Child) bool {
	shape := ChildChangeShape(c)
	return shape == "put" || shape == "both"
}

func ChildServesPatch(c Child) bool {
	shape := ChildChangeShape(c)
	return shape == "patch" || shape == "both"
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
	// ownerCheck | valueObject. Anything outside this set goes to rules.manual.
	//
	// `valueObject` is the odd one and the only one that adds no check of its
	// own: it PULLS FORWARD the validation the framework would run anyway. Every
	// value-object field validates automatically, but that pass runs AFTER
	// BuildRules — so a value object cannot be the premise of the rules below
	// it, which is exactly what a scope field or a foreign key usually is.
	// Declaring it here validates it in place (its own IsValid, or membership
	// for an enum), excludes it from the automatic pass so the caller is not
	// told twice, and — with guard: true — lets it end the pass. It raises no
	// notification of its own: the value object owns that answer, which is why
	// notification, attachTo, echoValue and skipWhen are all refused on it.
	Kind string `yaml:"kind"`
	// Scope is the verbs the rule fires on: insert | update | insertOrUpdate |
	// archive | unarchive | delete.
	Scope []string `yaml:"scope"`
	// ActionName is refused by this build — gate the rule by verb scope instead.
	ActionName string `yaml:"actionName"`
	// Fields are the subject fields; for a collection rule (groupCap,
	// childDuplicate) the single entry is the child's NAME; for a valueObject
	// rule they are the value-object-backed fields to validate in place, and
	// naming several of them means all of them have their say before the
	// barrier — which is the point of listing them together.
	Fields []string `yaml:"fields"`
	// Notification names the answer the rule raises when it refuses.
	Notification string `yaml:"notification"`
	// AttachTo names the field the refusal is reported against; defaults to the
	// rule's first field.
	AttachTo string `yaml:"attachTo"`
	// EchoValue passes the rejected value back in the notification, so the caller
	// sees what was refused — ON BY DEFAULT, turned off with `echoValue: false`,
	// and never applied to a `runtime: true, source: body` field.
	//
	// The default and the exception are in the OPENING SENTENCE deliberately:
	// `explain keys` prints one sentence per key, and that is the whole reference
	// most authors read. An author who read "passes the rejected value back" and
	// nothing after it concluded the key was opt-in, wrote no spec that declared
	// it, and shipped one that echoed a plaintext password.
	//
	// It is a pointer so that `echoValue: false` can say otherwise rather than
	// reading as an unset bool. The framework carries the value as NotificationMessage.
	// FieldValue and has since the beginning; leaving it out was the generator's
	// omission, not the framework's limit, and it costs the caller the only half
	// of the message they can act on — "at most 4 guardians" tells them the rule,
	// "you sent 6" tells them what to change.
	//
	// Turn it off for a value that should not travel back in a 422: a secret, a
	// document number, anything the response is not already allowed to carry.
	// The generator cannot tell — nothing in the language marks a field as
	// sensitive — so that judgement is the spec author's.
	//
	// With ONE exception, which the generator can tell and therefore does not
	// leave to the author: a `runtime: true, source: body` field is never
	// echoed, on any rule, whatever this key says. Such a field exists to reach
	// no copy of anything — no column, so no payload, no topic, no audit event
	// and no response — and the canonical one is a password confirmation, whose
	// echo would put the plaintext in the 422 body and in every log that renders
	// a notification. Writing `echoValue: true` on a rule over such a field is
	// refused rather than ignored; leaving the key out is silently correct.
	EchoValue *bool `yaml:"echoValue"`
	// Description is one optional line on why the rule exists.
	Description string `yaml:"description"`
	// Guard makes this rule a BARRIER: the validation pass ends after it when
	// anything has already been rejected.
	//
	// It is positional, and deliberately so. The barrier lands on the line after
	// this rule's block, so every rule declared ABOVE it — this one included —
	// has already had its say and every notification they raised reaches the
	// caller. Four preconditions that must all be reported before the pass stops
	// are four ordinary rules with `guard: true` on the LAST of them, not four
	// barriers.
	//
	// What it stops is everything the pass has not done yet: the rules below it,
	// the automatic value-object validation of this owner, and the BuildRules
	// and value objects of every aggregate child. The framework's structural
	// gates — the verb being allowed at all, and id validity — sit outside the
	// barrier and always report.
	//
	// It can never skip validation. The framework's StopIfInvalid fires only
	// where a notification has ALREADY been emitted, so a clean write runs whole
	// and a stop happens exclusively on a write that is already rejected. What
	// changes is how much the 422 carries: what was found up to the barrier,
	// instead of that plus every field the entity would also have failed on.
	//
	// Order matters, and only within a verb gate. Rules keep the order they are
	// declared in, so the barrier falls where you put it; but the GATES are
	// emitted in a fixed order (insert, insertOrUpdate, update, archive,
	// unarchive, delete), so on an insert a guard declared under insertOrUpdate
	// sits after everything declared under insert, whatever the yaml order.
	//
	// Refused on a composite value object's rules: those are checked inside the
	// value object's own IsValid, which is handed a NotificationContext and no
	// Rules — there is nothing there to stop.
	Guard bool `yaml:"guard"`

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
	//
	// It answers ONE question. A fact that must answer several numbers over the
	// same rows declares `aggregates` instead — the two are mutually exclusive,
	// because a fact answers in one shape or the other and never in both.
	//
	// `notExists` is `exists` read the other way round, and it exists so a fact
	// can be NAMED FOR THE PROBLEM. "Is unavailable", "is not in the catalog",
	// "does not apply here" are the questions a rule actually asks, and the
	// healthy state is the one nobody raises a notification about. Spelled as
	// `exists`, such a fact has to be named for the healthy state — and then the
	// generated test stub, which answers "nothing found", reads as "the row is
	// gone" and turns a correct spec red on the day it is written.
	Kind string `yaml:"kind"` // exists | notExists | count | sum | avg | min | max | manual
	// Returns is required for a manual fact: the generator has to know the
	// signature it is declaring, and for the other kinds it follows from the kind.
	//
	// For an aggregating kind it follows the KIND, not the column: an average is
	// float64 even over an integer field, while sum/min/max over int or int64 is
	// exact int64. And min, max and avg answer `(value, bool)` — over an empty
	// set SQL says NULL, and a zero returned alone reads as a real result.
	Returns string `yaml:"returns"` // bool | int64 | float64 | string
	// Field is the field the fact aggregates — required for sum, avg, min and
	// max; refused for manual, whose body is hand-written, and refused beside
	// `aggregates`, where each entry names its own.
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
	// Aggregates asks SEVERAL numbers of the same rows, in ONE query.
	//
	// It is `kind` widened from a single answer to a named set of them, and it
	// exists because the store never had the one-at-a-time limit: the framework's
	// aggregate loader takes as many specs as you pass — `Aggregate(ctx, q, total,
	// cents)`, `AggregateBy(ctx, q, by, total, cents)` — and computes them in one
	// SELECT. Asked as one fact each, the same question costs one query per
	// number over identical criteria, and a rule that needs two of them reads
	// two answers that were never guaranteed to be about the same instant.
	//
	// The answer becomes a STRUCT: one field per entry, named by `as`, plus the
	// grouping keys when there are any. That is why `as` is required rather than
	// derived — two aggregates over one column (a min and a max of the same
	// field) have no distinct name to derive, and the fields of a generated
	// struct are what a rule reads by name.
	//
	// Declaring one entry is refused: one aggregate is what `kind` says.
	Aggregates []FactAggregate `yaml:"aggregates"`
	// Filters is what the query narrows by — the fact's WHERE, written as the
	// framework's own criteria tree rather than as a list of equalities.
	//
	// The entries are ANDed, which is what `filters` meant when it was a list of
	// field names, so every spec written before this key grew a shape still says
	// exactly what it said. A bare name is `eq` against a parameter; the block
	// form names the operator, and a group node (all/any/not) nests.
	Filters []FactFilter `yaml:"filters"`
	// PerEntry turns a question about ONE ENTRY of a collection into ONE
	// question about the WHOLE collection, answered per entry.
	//
	// Its value is the key the answer is attributed to — `<Collection>.<Field>`
	// — and it changes both halves of the signature: the entries arrive
	// together, and the answer is a `map[<key>]<returns>` instead of a scalar.
	//
	// Without it, a fact naming a collection field is asked once per entry: the
	// loop lives in the rule, the body runs N times, and a write carrying
	// twenty entries pays twenty round trips for a question one `IN` would have
	// answered. The language could already BATCH THE QUESTION — `op: in` takes
	// the whole set — and had no way to batch the ANSWER, so the only shape
	// available was one bool for twenty ids, which cannot say WHICH id is the
	// bad one. That is not cosmetic: it is the difference between a 422 the
	// caller can act on and one they cannot.
	//
	// What the map means where a key is MISSING is decided here, once, so two
	// services do not read the same silence two ways: an absent key is the fact
	// answering NOTHING for that entry. For a fact named after the problem —
	// which is the naming this language asks for — Go's zero value already
	// gives the right reading at the call site: absent is `false`, and nothing
	// is raised. A body that means "I could not resolve this one" must say so
	// by putting `true` in the map, never by leaving the key out.
	//
	// The key field must be one an entry can actually be FOUND BY again, so it
	// is non-nullable (every entry without a value would collapse onto one key,
	// the same argument groupBy already makes) and one of string, int, int64 or
	// id. A `time` key compares by wall clock AND monotonic reading AND
	// location, so two values that print the same are two different keys; a
	// `float64` key can be NaN, which never equals itself; a `bool` key makes
	// the map two buckets rather than an answer per entry.
	//
	// An entry may contribute MORE THAN ONE field: every other filter naming
	// the same collection becomes a field of a generated entry carrier, so what
	// the method takes is `[]<Entity><Fact>Entry` rather than two parallel
	// slices the caller could misalign. With the key alone, the parameter stays
	// a plain slice of it.
	//
	// Only a manual fact may declare it, for the same reason a computed fact
	// cannot be filtered by a collection field at all: a computed fact is a
	// query over THIS entity's own table, and the entries are on another one.
	PerEntry string `yaml:"perEntry"`
	// ExcludeSelf leaves the record being written out of the answer, so an
	// update never collides with itself.
	ExcludeSelf bool `yaml:"excludeSelf"`
	// ActiveOnly considers only the rows that are not archived. Refused for a
	// manual fact.
	//
	// It is `scope: active` in the spelling that shipped first, and it stays
	// legal forever. Declaring both is refused rather than reconciled — they
	// govern the same gate, and picking one silently would run a query the
	// author did not write.
	ActiveOnly bool `yaml:"activeOnly"`
	// Scope is which rows the question is about, on the ARCHIVED axis — the
	// framework's own three-way gate, said in the spec instead of assumed.
	//
	//   active       — only the rows that are not archived (`activeOnly: true`).
	//   all          — archived rows included. THE DEFAULT, and what a fact
	//                  with neither key has always done.
	//   archivedOnly — the archived rows and nothing else.
	//
	// The third is the one that was unaskable. "Is this tenant unavailable"
	// means missing OR archived, and a fact could ask about the living rows or
	// about all of them, never about the archived ones alone — so the question
	// was written by hand or not asked. It is `criteria.Query.OnlyArchived`,
	// which the framework has always offered and nothing here reached.
	//
	// Refused when the entity declares no archive column: with no marker column
	// every scope yields no gate, so `archivedOnly` would quietly answer about
	// EVERY row rather than about none.
	Scope string `yaml:"scope"`
	// Description says what the answer means. Required for a manual fact — it
	// is what the generated stub and the report tell the implementer to write.
	Description string `yaml:"description"`
}

// FactAggregate is ONE of the numbers a fact answers, when it answers several.
//
// Every entry becomes a field of the fact's answer and one aggregate spec in a
// single query. The vocabulary is `kind` minus the two answers that are not
// aggregates at all: `exists` is a different question (and a different call on
// the loader), and `manual` is the ELSE, whose body nobody generates.
type FactAggregate struct {
	// Kind is what is computed: count | sum | avg | min | max.
	Kind string `yaml:"kind"`
	// Field is the column aggregated — required for sum, avg, min and max, and
	// refused for count, which counts rows rather than values.
	Field string `yaml:"field"`
	// As is the entry's name in the answer, PascalCase: a field of the generated
	// struct, and what a factRange rule names to bound this number
	// (`fact: <Fact>.<As>`).
	//
	// Required, and deliberately not derived from the field: a min and a max of
	// one column are two entries with one field name between them, and the two
	// numbers a rule wants to tell apart would arrive under the same word.
	As string `yaml:"as"`
}

// FactFilter is ONE node of a fact's narrowing: a leaf comparison, or a
// boolean group of nodes.
//
// It used to be a bare field name and nothing else, and every name became a
// criteria.Eq inside one And. That is a fraction of what the store can be
// asked: the framework's criteria package offers the whole comparison set and
// both connectives, the generator already emits a non-eq comparison of its own
// (excludeSelf writes criteria.Ne on the identity), and the READ side has
// spoken this vocabulary since it existed — read.byParams.filters[].ops names
// the operators a listing admits. The write side was the half still limited to
// equality, so a question as ordinary as "how many rows are in one of THESE
// states" had to be asked as one fact per state and folded back together in a
// rule, with the definition of the set living outside the fact that is named
// for it.
//
// A node is either a LEAF (field, plus op/as/value/values) or a GROUP (all,
// any or not) — never both, and never neither. The bare-string spelling
// survives untouched and means `eq` against a parameter, so a spec written
// before this shape still says what it said.
type FactFilter struct {
	// Field is what the comparison is about — a field of this entity, a part of
	// a composite value object, a field a ROOT read join brings in from another
	// aggregate, `ID` for the row's own identity, or `<Collection>.<Field>` for
	// a fact about an entry of a collection.
	//
	// `ID` is the framework's fixed logical name for the aggregate id, and it is
	// addressable here for the same reason it is under read.byParams.filters:
	// the STORE resolves it, on a slot every schema types as an identity, so the
	// probe binds in the dialect's native id form. It reaches the method as
	// `id domain.ID` (a set operator as `idSet []domain.ID`), which is what
	// makes "is this row still live", "which of these ids are" and — above all —
	// a `kind: manual` body that needs the id askable at all. Without it a
	// hand-written body had to re-derive the id from a natural key, paying a
	// join to translate a value its caller was already holding.
	//
	// A rule cannot fill it: rules.list fills a fact's arguments from the entity
	// being written, and the id is not minted until after the rules have run.
	// That combination is refused where it is written, and points at
	// rules.manual — or at excludeSelf, which passes the same id under the gate
	// the generator writes for the insert case.
	//
	// The join spelling is the one that is easy to miss, and it costs nothing:
	// a root join is ALWAYS in the FROM, and the framework compiles the same
	// traversal for `Exists` and for the aggregate calls as it does for
	// `FindAll` — it even types an identity column across the join leg, so the
	// bind is the dialect's native id form rather than text that would match
	// nothing on three of the four engines. So "does an active row exist whose
	// OWNER's tenant is this one" is one query the store already knows how to
	// answer, and the generator was the half that could not name the column.
	//
	// A join declared `inChild` is NOT reachable here. It rides the
	// collection's own batched SELECT and never reaches a predicate — the same
	// boundary every other child field has.
	//
	// It is the leaf half of the node and is required there; a group node
	// (all/any/not) names no field of its own.
	Field string `yaml:"field"`
	// Op is the comparison, from the framework's criteria vocabulary. Absent
	// means eq, which is what a bare field name has always meant.
	//
	// What the operator decides, beyond the SQL: how the value reaches the
	// query. eq/ne/gt/gte/lt/lte take ONE value, in/nin take a SET (the method
	// takes a slice), and isnull/notnull take NONE — the condition is about the
	// column being empty, so there is nothing for a caller to pass.
	Op string `yaml:"op"`
	// As names the method parameter this leaf contributes, when the default
	// would not do.
	//
	// The default is the field's own name, lower-camelled, which is unambiguous
	// until one field is compared twice: a floor and a ceiling over the same
	// column are two parameters, and two parameters of one name do not compile.
	// Naming them is the author's call — `minAge`/`maxAge` says what the caller
	// is passing, and an invented suffix would not.
	As string `yaml:"as"`
	// Value pins this leaf to a CONSTANT instead of a parameter: the query
	// carries the literal and the method does not take it.
	//
	// It is what puts a definition INSIDE the fact that is named for it. A
	// bucket declared at the call site is a bucket every caller has to repeat
	// and every new member of the enum has to be added to by hand, in a rule
	// rather than in the spec — and a fact whose parameters are all pinned is
	// one a declarative rule can still read, because factRange fills arguments
	// from the entity and has nothing to fill a constant with.
	//
	// Over an enum value object the literal is a MEMBER NAME, checked against
	// the declared members; over anything else it is the value itself, in the
	// field's own type. A timestamp and an identity are refused: a date typed
	// into a spec is a query that silently ages, and an id pinned in one is a
	// row someone pasted.
	Value any `yaml:"value"`
	// Values is Value for the set operators — the whole IN list, as constants.
	Values []any `yaml:"values"`
	// All is an AND of the nodes under it. The top-level `filters` list is
	// already one, so this is for an AND nested inside an `any`.
	All []FactFilter `yaml:"all"`
	// Any is an OR of the nodes under it.
	Any []FactFilter `yaml:"any"`
	// Not negates what is under it. Several nodes are ANDed first, exactly as
	// the top-level list is, so `not: [A, B]` is "not (A and B)" — which is NOT
	// the same as "neither A nor B". For that, negate an `any`.
	//
	// Spelled out because the reading is the one an author is most likely to
	// assume backwards, and both queries are perfectly valid: nothing downstream
	// would refuse the one they did not mean.
	Not []FactFilter `yaml:"not"`
}

// ---------------------------------------------------------------- read side

type Read struct {
	// Backing is where reads are served from, and it picks between two different
	// KINDS of read model rather than flipping a switch on one.
	//
	// relational = straight from the tables (the source of record): strongly
	// consistent with the write that just happened, no CDC hop, and composed at
	// read time instead of fetched as one document. Nothing is materialised, so
	// there is no collection, no view.version, no rebuild and no drift — and the
	// keys that only make sense over a collection (view.version, indexes,
	// view.deleteOnArchive, view.ttlSeconds, controls.search) are refused rather
	// than silently discarded. It also inherits whatever the repository's READ
	// JOINS reach: a joined field is filterable, sortable and served here.
	//
	// mongo = from a projection updated shortly after the write: an O(1)
	// document read and the full read-side vocabulary, at the cost of CDC lag.
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
	// From names the STORED fields the derivation reads, in the scope where it
	// is declared: the ENTITY's own fields under read.computed, and the ENTRY's
	// own fields — bare, with no collection spelled in front of them — under
	// children[].computed.
	//
	// The scope belongs in the first sentence because this comment is ALL that
	// `explain keys` prints for the key (it shows one sentence), and it is
	// printed under both paths — one struct serves both. Written for the root
	// alone, it sent an author declaring a per-entry derivation straight at the
	// root's fields, which is the one mistake the key exists to make impossible.
	//
	// They are what the framework pushes to the store when the caller selects
	// this field, so the list has to be complete: a source left out is a source
	// that arrives nil. The per-entry form needs no prefix precisely because the
	// framework records a nested field's sources under the same segment as the
	// field itself.
	//
	// A ROOT derivation reads the entity's own fields, its root-attached facets,
	// a column listed under read.managed, and a ROOT join's fields. It may not
	// read a collection's: it runs once per document, and what the root holds
	// for a collection is a slice, so there is no single value to hand it.
	//
	// Each source becomes a PARAMETER of the derivation, under its camelCase
	// name, so two sources that camel-case to one word are refused — two
	// parameters of one name do not compile.
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
	//
	// It may not end in __0 or __1: those are the blue-green SLOT suffixes the
	// framework addresses a view's two physical collections by, so a view named
	// users__0 would own a collection byte-identical to view users's first slot,
	// and every consequence of that collision is silent. The framework refuses
	// the name at boot in every read-model family — plain, shared-base, composed
	// and relational alike, since all four share one namespace.
	Name string `yaml:"name"`
	// Version is YOURS, and it is the opposite of specVersion: bump it whenever
	// the projected SHAPE changes — a field added or removed, a collection
	// renamed, a facet folded in. The framework compares it against what is
	// stored and refuses to boot rather than serve a projection built to an
	// older shape, so forgetting is a failed start; and bumping it is what
	// triggers the rebuild.
	//
	// MONGO BACKING ONLY, and refused on a relational one: a read model with no
	// materialisation has no stored shape to grow stale against, nothing to
	// rebuild and no boot to refuse. A version there would be a number nobody
	// ever compares.
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

// Surfaces is WHERE the entity answers. The three are independent switches: any
// one of them alone is a complete answer, and any combination is legal.
//
// Independent means what it says. A GraphQL-only service is a service with no
// HTTP route but the schema; a REST-only one has no schema; a project that wants
// nothing but the spreadsheet mounts the two export paths and neither CRUD
// surface. What none of them does is change the write: the command, the rules
// and the permission are one set of objects, and a surface is a way in.
//
// Whatever the entity mounts appears on every surface that is on — the root's
// verbs AND the per-entry verbs of its collections. The narrowing keys below
// exist to take things OFF a surface, never to put them on; forgetting one
// cannot silently halve an API.
type Surfaces struct {
	// REST mounts the HTTP endpoints — the entity's own routes and the per-entry
	// routes of its per-child collections. At least one surface must be on.
	REST bool `yaml:"rest"`
	// GraphQL exposes the entity in the GraphQL schema.
	GraphQL *GraphQL `yaml:"graphql"`
	// Exports serves the listing as downloadable files. It stands on its own: it
	// needs a listing (read.byParams), not surfaces.rest.
	Exports *Exports `yaml:"exports"`
}

// GraphQL is the schema surface: enabled, plus the two keys that NARROW it.
//
// Enabled alone is the whole declaration for most entities — it exposes the
// reads the entity serves, every write verb it mounts, and every per-entry verb
// of its collections. mutations and connection subtract from that; they do not
// add to it, and neither is required.
type GraphQL struct {
	// Enabled turns the GraphQL surface on.
	Enabled bool `yaml:"enabled"`
	// Mutations NARROWS the write side to the verbs listed; each must be among
	// the entity's modes. Absent means every write verb the entity mounts.
	//
	// `update` covers both shapes, exactly as fields[].modes does: with
	// update.shape: both, the PUT-shaped and the PATCH-shaped mutations are one
	// decision here, because they are one verb in the domain.
	//
	// It governs the ROOT's verbs only. A collection's per-entry verbs are named
	// in a different vocabulary (add | change | remove) and are narrowed where
	// they are declared, under children[].surfaces.
	Mutations []string `yaml:"mutations"`
	// Connection serves the paginated connection query; it requires display
	// among the modes. Absent means served, whenever the entity has a listing.
	//
	// It governs the PLURAL query alone. The singular one follows read.byId: a
	// schema that can read one record by id is the smallest useful GraphQL
	// surface there is, and turning the listing off is not a reason to lose it.
	Connection *bool `yaml:"connection"`
}

// ChildSurfaces narrows where ONE collection's per-entry verbs are exposed.
//
// Every key is optional and every absent key means "follow the entity". What it
// cannot do is widen: a collection cannot reach a surface its entity does not
// serve, because the mount that would carry it is not written at all.
type ChildSurfaces struct {
	// REST mounts this collection's per-entry routes. Absent = whatever the
	// entity's surfaces.rest says.
	REST *bool `yaml:"rest"`
	// GraphQL exposes this collection's per-entry verbs as mutations.
	GraphQL *ChildGraphQL `yaml:"graphql"`
}

// ChildGraphQL is one collection's seat on the schema surface.
type ChildGraphQL struct {
	// Enabled exposes the collection's per-entry verbs as mutations. Absent =
	// whatever the entity's surfaces.graphql says; false takes the whole
	// collection off the schema while leaving its REST routes standing.
	Enabled *bool `yaml:"enabled"`
	// Mutations NARROWS the exposed verbs to the ones listed, in the same
	// vocabulary operations uses: add, change, remove. Absent means every verb
	// the collection mounts, and each value must be one of them.
	Mutations []string `yaml:"mutations"`
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

// ------------------------------------------------------------------- joins

// Join is one read-only traversal into another aggregate, across a foreign key
// this entity (or one of its collections) already stores.
//
// It is 1:1 and horizontal: one column of the target mapped onto one Go field
// at a time. There is no many-valued form — a 1:N traversal would multiply the
// root's rows and break the paged read — so "bring the order's items" is a
// child collection, never a join.
//
// It reaches as FAR as it is told to: `then` continues the traversal from this
// join's target to the next aggregate, and from there onward. What the
// generator emits for every hop is the target reduced to its own table — a
// traversal puts one table in the FROM, so the framework takes a Direct schema
// there.
type Join struct {
	// Kind decides what a joining row with NO counterpart means:
	//
	//   left  — the row is still returned and the traversed fields read as NULL.
	//           Legal over any foreign key, and the right default: a load exists
	//           to return THIS aggregate, and a missing relation is not a reason
	//           to stop returning it. Its fields land in NULLABLE Go fields.
	//   inner — the row is NOT returned. Legal ONLY over a non-nullable foreign
	//           key: the declaration lives on the repository, so it applies to
	//           FindByID too, and over a nullable key it would silently turn a
	//           legitimate write into a 404.
	Kind string `yaml:"kind"`
	// To is the target aggregate — the entity name, as another spec of this
	// project declares it. The generator resolves its schema and its columns
	// from that spec, which is what lets it refuse a column the target does not
	// have while the author is still in the file.
	To string `yaml:"to"`
	// On is the FOREIGN KEY column on the JOINING table: this entity's root
	// table normally, the collection's table when inChild is set. The other side
	// of the predicate is always the target's declared id column — a foreign key
	// points at a primary key, and both schemas already name it — so a traversal
	// onto a non-id column of the target is deliberately not expressible.
	On string `yaml:"on"`
	// InChild hangs the traversal off one of THIS entity's own collections
	// instead of its root: the foreign key is the collection's, and the mapped
	// Go fields land on the collection's entry.
	//
	// A child join is LOAD-ONLY. Its fields are filled on every loaded entry and
	// served inside the entry, but they are not filterable or sortable: narrowing
	// the root by a field of a 1:N collection is a pushdown a single root SELECT
	// cannot express — the same boundary every child field already has.
	InChild string `yaml:"inChild"`
	// Fields is what the traversal brings back. At least one is mandatory: a
	// join that maps no column reaches nothing.
	Fields []JoinField `yaml:"fields"`
	// Then continues the traversal FROM THIS ENTRY'S TARGET — the aggregate one
	// hop further out, and from there onward with no depth limit. Several hops
	// listed here branch off the same target.
	//
	// Two rules make the rest follow, and both are what tells a chain apart from
	// a second join:
	//
	//   - a hop's `on` is a foreign key OF THE PREVIOUS TARGET's own table, never
	//     of the entity that declared the chain;
	//   - every hop's fields land on the SAME struct the head lands on, at any
	//     depth — the entity, or the collection's entry under inChild — because a
	//     joined field carries no domain type and there is no "struct of hop 2"
	//     for one to live in.
	//
	// A hop takes no inChild of its own: only the head decides what the chain
	// hangs off. `kind` still means what it means per hop, but ABSENCE follows
	// the PATH: one `left` anywhere above makes every field below it nullable,
	// whatever the deeper hops declare, and the whole chain reports absent
	// together — a miss at any hop leaves hop one's fields nil too, because the
	// framework emits depth 2 and beyond as a NESTED join rather than a flat
	// list.
	//
	// The cost is a table per hop on EVERY read through this repository,
	// FindByID included — the load the write-side handlers go through. The
	// framework logs one advisory per chain at boot for exactly that reason;
	// where the reach is only ever read, its own answer is a Direct repository,
	// which this generator does not emit.
	Then []Join `yaml:"then"`
}

// JoinField maps ONE column of the joined table onto a Go field of the entity
// that declares the join. The joined side's spelling never surfaces above
// infrastructure, so renaming is free.
type JoinField struct {
	// Name is the Go field this entity gains, PascalCase — the name the rules,
	// the criteria, the DTOs and `?fields=` all speak. It must not collide with
	// anything the entity already answers: its own fields, a sibling's, the
	// shared base's, or another join's.
	Name string `yaml:"name"`
	// Column is the column on the TARGET's own table. A column of the target's
	// shared base or of one of its siblings is not reachable: the traversal is
	// one predicate onto one table.
	//
	// The framework-STAMPED columns count as columns of that table: whatever the
	// target registers under storage.managed.createdAt, .updatedAt and
	// .archivedAt is resolved on the read path under a fixed LOGICAL name, so a
	// traversal may reach it even though no fields[] entry ever names it. The
	// column itself is the target author's to name — read it off that spec and
	// write it here as spelled there; only the slot is the framework's. The
	// archive column crosses into a POINTER on either kind of join, because NULL
	// there is the normal state of a row that was never archived.
	//
	// The revision is the one managed column that does NOT cross. It is the
	// guard of the target's own writes — the value its update is matched on — so
	// a copy carried across a join is stale the moment that aggregate is written
	// again, and the read path does not resolve it.
	Column string `yaml:"column"`
	// Type is the Go type the value lands in. Derived from the target's own
	// declaration of that column when omitted, which is the spelling to prefer —
	// stating it again is a second place for the two to disagree.
	//
	// A join field carries NO DOMAIN TYPE: not an identity, not a value object of
	// any kind. The value belongs to ANOTHER aggregate and arrives read-only — it
	// is never written through this entity and never validated by this domain, so
	// a domain type here would be an instance no rule ever approved. `id` is
	// therefore refused: an identity column crosses as its canonical TEXT, which
	// is also the only shape correct on all four engines (three store an id as
	// raw bytes and the framework decodes it on the way out).
	//
	// The pointer is not declared here either — it follows from what can be
	// ABSENT: a left join with no counterpart, or a column the TARGET declares
	// nullable. Either one makes NULL reachable, on either kind of join.
	Type string `yaml:"type"`
	// Nullable says the column is nullable ON THE TARGET, and it exists for the
	// one case the generator cannot see: a hand-written aggregate this project
	// declares no spec for.
	//
	// It matters because the pointer does not follow from the join kind alone.
	// An INNER join proves the joined ROW exists, never that every column of it
	// is filled — so a nullable column crossing an inner join still lands in a
	// pointer on this side, and a non-pointer field there fails the framework's
	// own check at repository construction, which is a boot to find. When the
	// target IS a spec of this project the answer is read off its declaration
	// and stating it here is refused as a second place for the two to disagree;
	// a LEFT join is nullable whatever this says, because a missing counterpart
	// produces NULL on its own.
	Nullable bool `yaml:"nullable"`
	// LabelKey is the translation-catalog key for the field's label, exactly as
	// on a persisted field. Derived when omitted.
	LabelKey string `yaml:"labelKey"`
	// Text is the field's LABEL per language catalog — a couple of words, the
	// same contract a persisted field's text has.
	Text Texts `yaml:"text"`
	// Example feeds the OpenAPI example, exactly as on a persisted field.
	Example string `yaml:"example"`
	// Hidden keeps the joined value OFF THE WIRE: out of the by-id response,
	// out of every listing row, out of the CSV/XLSX exports. Everything else is
	// unchanged — the field is still on the entity, still filled on every load,
	// still readable by the rules and by a domain service, and still nameable
	// under read.byParams.filters and sort.
	//
	// This is the shape to reach for when the traversal exists FOR A RULE. A
	// service that has to decide against a value belonging to another aggregate
	// needs that value on the entity, and needs nothing at all on the wire —
	// and the two are separate questions, so the language asks them separately.
	// The same key means the same thing on a persisted field.
	Hidden bool `yaml:"hidden"`
	// Description is one line on what the value means, for the reader of the
	// generated entity.
	Description string `yaml:"description"`
}
