package spec

// The closed vocabularies of the spec language. Every enum-valued key in the
// language resolves against one of these sets; a value outside the set is a
// refusal with the whole set printed, never a silent pass-through.

import "strings"

var (
	StorageKinds   = set("flat", "sharedbase-role")
	LinkModels     = set("shared-pk", "separate-fk")
	RowUniqueness  = set("unique-fk", "active-only")
	OrphanPolicies = set("keep", "delete-when-unreferenced")

	// FieldTypes is the CLOSED persistable set. The framework fails at boot on an
	// unknown Go field type, so widening this set is a deliberate act paired with
	// a dialect column mapping, never an accident.
	FieldTypes = set("string", "int", "int64", "float64", "bool", "time", "id")

	// AssignedFrom is where the SERVER reads a persisted field's value, for the
	// fields a client is not allowed to send. The subject is the caller's own
	// identifier; a claim is anything else the token carries; `client-ip` is the
	// request's network origin, which is not in the token at all; `derived` is
	// the ELSE — the value comes from the entity itself, and the generator only
	// promises to keep the field out of the write surface.
	AssignedFrom = set("identity-subject", "identity-claim", "client-ip", "derived")

	// StampedKinds is WHAT filling a framework-owned column means. The two
	// members share the whole mechanism — the same marker asks for both, the
	// value is the server's either way, and neither is ever written from the
	// struct — and split on the shape of the value alone: `time` binds the write
	// operation's single instant, `counter` binds 1 on the insert and
	// `col = col + 1` afterwards.
	//
	// The split is a closed set rather than an inference from `type` because a
	// timestamp and a counter are two different promises, and a spec that means
	// one and gets the other says nothing about it: both compile, both migrate,
	// and the difference only shows up in production data.
	StampedKinds = set("time", "counter")

	// FieldSources is where a RUNTIME-only field is fed from. Every answer keeps
	// the field off every table: the difference is who supplies the value.
	//
	// `claim` is the caller's token read BY NAME, which is what runtime meant
	// before this key existed — so it is the default and every spec written
	// without it still says the same thing. `body` is the request itself: the
	// field crosses the write DTO, the command and the entity, a rule checks it,
	// and it stops there.
	//
	// `manual` is the ELSE, and it is the same ELSE the rest of this language
	// uses (vo.kind, facts[].kind, rules.manual, written): the generator declares
	// the FIELD and hands the filling to a human. The aggregate carries it so
	// hand-written rules can read it; no generated write DTO, command, mapper or
	// OpenAPI schema mentions it, because no generated verb puts anything there.
	//
	// It exists for the operation the spec cannot declare — a change-password
	// endpoint written by hand that dispatches the same mode a generated PATCH
	// does, and is told apart by its action name. Its `currentPassword` has to
	// reach the aggregate for a rule to prove possession, and must not appear in
	// the body of the ordinary PATCH. Before this there was no way to say that:
	// `source: body` puts the field on every verb it names, and naming none of
	// them (`modes: []`) was read as naming all of them.
	//
	// The other four are the framework's OWN questions about the caller, asked
	// through the accessors that answer them: Identity.Subject, Identity.TenantID,
	// Identity.HasPermission, Identity.IsSuperAdmin — plus `present`, the nil
	// check itself, which is a fact no VALUE can carry.
	//
	// They exist because `claim` answers a DIFFERENT question and looks like the
	// same one. `claim: is_admin` reads a boolean the token happens to carry;
	// `source: permission` asks the permission model, which resolves the
	// resource wildcard (`users:*`), honours the `*:*` grant and reads whichever
	// claim the deployment configured. The generated spec and the hand-written
	// service used to disagree on exactly that, and the generated half was the
	// weaker one.
	//
	// The domain has no ctx by design, so a fact about the session reaches a rule
	// only by riding a field on the entity. These name which fact, once, instead
	// of pushing the author into a hand-written command mapper or a manual
	// service fact that goes to the infrastructure to ask something the
	// application layer already had in hand.
	FieldSources = set("claim", "body", "manual", "subject", "tenant", "permission", "super-admin", "present")
	// FieldModes is which write verbs carry a source: body field. It is the two
	// the domain can tell apart, because the rule gates are the granularity that
	// matters: a PATCH is dispatched into IfUpdate exactly as a PUT is, so
	// `update` names both shapes and a third value would promise a distinction
	// BuildRules cannot make.
	FieldModes = set("insert", "update")

	// VOKinds is what a field's value object IS. The generated kinds split on
	// two questions: who owns the rule (raw writes it, enum gets membership for
	// free) and how many columns the value occupies — a composite declares no
	// Value(), so its value spans several and the schema decomposes it.
	//
	// The two remaining answers are about WHO WRITES THE TYPE: `reuse` for one
	// that already exists in the project, `manual` for one that does not exist
	// yet and that you will write, because its rule is outside what this
	// language can express.
	VOKinds = set("none", "reuse", "raw", "enum", "composite", "manual")
	// VODeclaredKinds is what a valueObjects[] entry may declare. `none` and
	// `reuse` are field-side answers ("no value object" / "one that already
	// exists"), so they are not declarations. `manual` IS one: the type is not
	// in the project yet, so the spec has to say what it will be.
	VODeclaredKinds = set("raw", "enum", "composite", "manual")
	VOBackings      = set("string", "int")

	// ManagedReads are the framework-stamped columns a read may expose, under the
	// fixed logical names the framework itself resolves them by. They are not
	// entity fields — nothing declares them under fields[], the aggregate carries
	// no Go field for them, and no write DTO accepts one — so the read side names
	// them here instead.
	ManagedReads = set("CreatedAt", "UpdatedAt", "DeletedAt")

	// VOWritings is WHO writes the type, asked separately from what it is. It
	// exists for the composite: its parts have to stay declared (the schema
	// decomposes them, the mappers fold them, the migration sizes them) while
	// the file itself becomes the author's, which is a combination `kind:
	// manual` — a scalar with a backing and nothing else — cannot express.
	VOWritings = set("generated", "manual")

	// CompositeRuleKinds is what a COMPOSITE value object's own rules may check.
	// The set is the rule kinds answerable from the parts alone: a value object
	// sees its own value and nothing else — no old state, no siblings, no
	// service, no collection. Anything outside it belongs to the entity's rules,
	// and a composite that reached for them would not be a value object.
	CompositeRuleKinds = set("required", "length", "range", "comparison", "requiredIf")
	// RedactKinds is the closed family of masks a redaction axis may declare —
	// one per constructor the framework offers, and no more. `plain` is not a
	// no-op declaration: with both axes mandatory it is the only way to say
	// "masked in the payload, intact in the audit trail" (or the reverse).
	// `hook` is the ELSE — the mask the family does not cover, written by you in
	// a file this generator creates once and never rewrites.
	RedactKinds = set("plain", "fixed", "keep-last", "hook")

	UniqueEnforcements = set("service-precheck+constraint", "constraint-only")
	UniqueScopes       = set("all", "active-only")

	ChildOwners    = set("root", "base", "role")
	EditStrategies = set("atomic-replace", "per-child")

	// ChildOperations is which per-entry verbs a per-child collection mounts.
	// Absent means all three — the trio is the default because it is what
	// per-child meant before the key existed.
	//
	// It is a closed set of THREE and not of two, even though only one of them
	// is ever dropped in practice: naming the verbs is what lets a collection
	// say "add and remove, no change" without the author having to reach for
	// atomic-replace, which is a different contract (an omitted entry is
	// revoked) rather than a smaller one.
	ChildOperations = set("add", "change", "remove")

	Modes = set("display", "insert", "update", "delete", "archive", "unarchive")

	// GraphQLMutations is Modes minus display: what a mutation can name. A read
	// is a query, and `display` in a mutation list is an author saying "expose
	// the reads too" — which is already what enabling the surface does.
	GraphQLMutations = set("insert", "update", "delete", "archive", "unarchive")
	UpdateShapes     = set("patch", "put", "both")
	DeleteRoot       = set("soft", "hard", "both")
	DeleteChild      = set("soft", "hard")

	RuleKinds = set(
		"required", "immutable", "length", "range", "comparison", "transition",
		"requiredIf", "groupCap", "childDuplicate", "ownerCheck", "factRange",
		"valueObject",
	)
	RuleScopes    = set("insert", "update", "insertOrUpdate", "archive", "unarchive", "delete")
	ComparisonOps = set("gte", "gt", "lte", "lt", "eq", "ne")
	SkipWhens     = set("empty", "null")

	// Semantics map to HTTP status through the framework's status-mapping table.
	// "conflict" and "state-conflict" are BOTH 409 and are NOT interchangeable:
	// duplicate/already-exists is conflict; wrong-state is state-conflict.
	Semantics = set("validation", "conflict", "state-conflict", "forbidden", "not-found")

	NotificationPackages = set("domain", "vos", "aggregatevos")

	// The last member is the ELSE of this vocabulary, and the pattern is the
	// language's general answer to "the generator does not know how": it declares
	// the shape and hands the body to a human, in a file it never rewrites.
	//
	// It covers far more than a call to another service. Anything the generator
	// cannot compute honestly goes through the same door — a third-party API, a
	// cache, a rule of thumb someone codes by hand — without the language having
	// to grow a keyword per case it failed to anticipate.
	//
	// notExists is exists negated, and it is here so a fact can be named for
	// the PROBLEM — which is what every rule that reads one is about, and what
	// keeps a freshly generated suite green: the test stub answers "nothing
	// found", and under the healthy-state naming that reads as the row being
	// gone.
	FactKinds = set("exists", "notExists", "count", "sum", "avg", "min", "max", "manual")

	// FactScopes is which rows a computed fact's question is about, on the
	// archived axis. It is criteria.Query's own three-way gate — the default
	// (active), IncludeArchived and OnlyArchived — named in the spec so the
	// third one is reachable at all.
	FactScopes = set("active", "all", "archivedOnly")

	// AggregateKinds is what ONE entry of service.facts[].aggregates computes.
	//
	// It is FactKinds minus the two answers that are not aggregates: `exists` is
	// a different question and a different call on the loader (a probe, not a
	// scalar), and `manual` is the ELSE, whose body nobody generates — there is
	// nothing to combine into a query with the others.
	AggregateKinds = set("count", "sum", "avg", "min", "max")

	// FactFilterOps is the comparison vocabulary a fact's filter may use, and it
	// is the framework's own: every criteria builder that takes a field, minus
	// the two families the language answers differently.
	//
	// like/ilike are out because they take a RAW pattern — the caller supplies
	// the % and the escaping, and a spec that admitted them would be handing an
	// author a footgun the framework already wrapped: contains, startswith and
	// endswith are those builders with the escaping done, which is why they are
	// the ones exposed here.
	//
	// between is out because it IS gte + lte, and writing it as one key would
	// mint two parameters nobody named. Two leaves under the same fact say the
	// same thing, in the author's own words (`as: minAge`, `as: maxAge`).
	FactFilterOps = set(
		"eq", "ne", "gt", "gte", "lt", "lte",
		"in", "nin", "isnull", "notnull",
		"contains", "startswith", "endswith",
	)

	// FactReturns is the closed set a manual fact may declare. The domain port
	// returns pure values and never an error, so the set stays narrow on purpose.
	FactReturns = set("bool", "int64", "float64", "string")

	ReadBackings = set("relational", "mongo")
	// JoinFieldTypes is FieldTypes minus `id`: a join field carries no domain
	// type, so an identity crosses as its canonical text instead of as domain.ID.
	JoinFieldTypes = set("string", "int", "int64", "float64", "bool", "time")
	// JoinKinds is what a joining row with no counterpart means. The choice is
	// not free: inner is legal only over a NON-NULLABLE foreign key.
	JoinKinds     = set("inner", "left")
	IdentityViews = set("create", "add-role", "skip")
	IndexOrders   = set("asc", "desc")

	// FilterOps is the framework's closed operator set. A value outside it is
	// rejected at boot when the wrapper is constructed, so it is rejected here.
	FilterOps = set(
		"eq", "ne", "in", "nin", "gte", "lte", "gt", "lt",
		"startswith", "contains",
		"ieq", "ine", "iin", "inin", "istartswith", "icontains",
	)

	DataAccess = set("anyone-with-permission", "owner-only", "tenant")

	// NoIdentityPolicies is what an ABSENT identity means under a scoped
	// dataAccess. It is a closed set because it is a policy with exactly two
	// defensible answers, and it used to be a hardcoded branch: no identity
	// scoped the read to the empty value, so a generated tenant entity answered
	// every listing empty on the dev bench, which is the first place it is run.
	//
	// stand-down is the default — see Authz.NoIdentity for why, and for why it
	// is safe: the guard asks about the PRESENCE of an identity, not about an
	// empty scope, so a token that merely lacks the claim is still refused.
	NoIdentityPolicies = set("refuse", "stand-down")

	// AuthzOperations is the key set accepted under authz.permissions. It is
	// cross-checked against the operations actually mounted, so a permission for
	// an operation that does not exist is a refusal, not dead configuration.
	AuthzOperations = set("insert", "update", "patch", "delete", "archive", "unarchive", "read")

	// DocOperations is the key set accepted under docs.operations, and it is
	// deliberately NOT AuthzOperations: a permission guards the two reads
	// together, so authz spells them `read`, while prose does not — a listing's
	// filter vocabulary is not what a by-id read needs explained. So the two
	// endpoints are addressable one at a time, under the names the routes are
	// generated with.
	DocOperations = set("insert", "update", "patch", "delete", "archive", "unarchive",
		"byId", "byParams")

	// canonicalActions is the house taxonomy: the ACTION half of a permission
	// spells the operation it guards, so one project speaks one vocabulary.
	//
	// It exists because the alternative writes itself differently every time —
	// insert becomes `create` in one spec and `write` in the next, and a
	// deployment ends up granting three words for one thing. Two operations
	// share an action ON PURPOSE: PUT and PATCH are both an update, and
	// unarchive is the undo of archive, so whoever may archive may put it back.
	canonicalActions = map[string]string{
		"insert": "insert", "update": "update", "patch": "update",
		"delete": "delete", "archive": "archive", "unarchive": "archive",
		"read": "read",
	}

	Dialects = set("postgres", "mysql", "sqlserver", "oracle", "sqlite")
)

// Sorted string sets with a stable printable form for error messages.

type StringSet struct {
	m     map[string]bool
	order []string
}

func set(values ...string) StringSet {
	s := StringSet{m: make(map[string]bool, len(values)), order: values}
	for _, v := range values {
		s.m[v] = true
	}
	return s
}

func (s StringSet) Has(v string) bool { return s.m[v] }

func (s StringSet) List() []string { return append([]string(nil), s.order...) }

func (s StringSet) String() string {
	out := ""
	for i, v := range s.order {
		if i > 0 {
			out += " | "
		}
		out += v
	}
	return out
}

// Vocabulary is one closed set, at the yaml path that resolves against it.
//
// The registry below is the SINGLE place a vocabulary is declared to exist for
// documentation. `explain vocabulary` renders from it rather than from a table
// of its own, and a test asserts every set in this file is registered — because
// the hand-written table this replaces fell eight sets behind, and one of them
// was the key an author went looking for and could not find.
type Vocabulary struct {
	// Path is the yaml key, as an author writes it.
	Path string
	// Set is the closed set of values that key accepts.
	Set StringSet
	// Why is one line on what the choice decides. A list of legal strings tells
	// nobody which one they want.
	Why string
}

// Vocabularies lists every closed set of the language, in the order a spec is
// written rather than alphabetically: an author reading it top to bottom walks
// their own file.
func Vocabularies() []Vocabulary {
	return []Vocabulary{
		{"storage.kind", StorageKinds,
			"flat = its own table; sharedbase-role = a ROLE over an identity other roles may share."},
		{"storage.base.link", LinkModels,
			"shared-pk = the role's own key IS the identity's; separate-fk = its own column."},
		{"storage.base.rowUniqueness", RowUniqueness,
			"unique-fk = one role row per identity ever; active-only = an archived one frees the slot."},
		{"storage.base.orphanPolicy", OrphanPolicies,
			"what happens to the identity when its last role goes."},
		{"fields[].type", FieldTypes,
			"the persistable set; money is int64 in minor units, never a float."},
		{"fields[].vo.kind", VOKinds,
			"enum when the valid values are finite and known; raw when it is a shape; " +
				"composite when the value needs SEVERAL columns to mean anything; reuse for a " +
				"type the project ALREADY has, manual for one it does not and you will write."},
		{"fields[].assignedFrom", AssignedFrom,
			"the server fills it, so no write request carries it — from the identity, " +
				"from the request's network origin (client-ip), or (derived) from the " +
				"entity's own fields, by a rule you write. The identity sources refuse " +
				"nullable, because the server is written on every insert and always has a " +
				"subject and the claim it required; `client-ip` and `derived` accept it, " +
				"for the same reason in two shapes — a write off the inbound request path " +
				"(a consumer handler, a job) HAS no origin, and your rule may legitimately " +
				"leave a derived value unset. A nullable client-ip column records that " +
				"absence as NULL; a non-nullable one records it as the empty string."},
		{"fields[].stamped", StampedKinds,
			"the framework owns the VALUE, the domain owns the WHEN. time = a nullable " +
				"timestamp (*time.Time) bound with the write operation's own instant — the " +
				"seat createdAt/updatedAt do not cover, for a FACT that just happened " +
				"(paid, approved, cancelled) rather than for the row; counter = an int64 " +
				"the server increments under the row's lock, per ROW and never a " +
				"table-wide sequence — declare it nullable (*int64) when the column must " +
				"also be able to say NO COUNT AT ALL, which is the only shape StampNull " +
				"can land in. Both leave every write surface, exactly " +
				"as assignedFrom does, and neither is ever written from the struct. What " +
				"ASKS for the stamp is not generated: e.Stamp(\"PaidAt\") belongs to a " +
				"rules.manual entry you write, because no rule in this language knows the " +
				"moment a domain calls a fact done — and so do the two verbs that CLEAR " +
				"one, e.StampNull(\"PaidAt\") (an absence) and e.StampEmpty(\"PaidAt\") " +
				"(the declared type's zero: 0 for a counter, the zero instant for a time)."},
		{"fields[].source", FieldSources,
			"where a RUNTIME-only field is fed from. claim = the caller's token by NAME " +
				"(the default, and it needs claim: <name>); body = the request itself — the " +
				"field crosses the write DTO, the command and the entity for a rule to " +
				"check, and NO column, payload, audit event or response ever sees it " +
				"(a password confirmation is the case that exists for); manual = the " +
				"aggregate carries it and this generator fills it from nowhere — no write " +
				"DTO, command or OpenAPI schema mentions it, and YOUR code puts the value " +
				"there (a hand-written operation sharing a mode with a generated verb). " +
				"The rest are the " +
				"framework's own questions about the caller, so the domain can read them " +
				"without a ctx it does not have: subject and tenant (text), permission " +
				"(bool, and it needs permission: <resource:action>), super-admin (bool, " +
				"the *:* grant) and present (bool, whether there was a caller at all)."},
		{"fields[].modes", FieldModes,
			"which write verbs carry a source: body field. Omitted = every write verb " +
				"the entity has. `update` covers both PUT and PATCH: the rule gates cannot " +
				"tell them apart, so neither does this key."},
		{"fields[].renderIn", FieldModes,
			"which write verbs RENDER a source: manual field in their response — the " +
				"output-side counterpart of modes, for a value the server minted and the " +
				"caller receives exactly once (a machine credential whose hash is all that " +
				"is stored). Same two values, and for the same reason: `update` covers PUT " +
				"and PATCH. Omitted = no verb renders it, which is what a runtime field " +
				"does by default; there is no \"every verb\" reading, because a value " +
				"minted on insert is not a value an update has in hand."},
		{"redact.inSync.kind", RedactKinds,
			"how the field appears in the outbox payload — and so on the topic, in every " +
				"consuming service, in both failure ledgers and in the projected document; " +
				"`redact.inAudit.kind` is the same set over the audit event, and both are " +
				"mandatory. plain = the real value, said out loud; fixed = a constant; " +
				"keep-last = every rune but the last n; hook = a function you write."},
		{"redact.inAudit.kind", RedactKinds,
			"how the field appears in the audit event — the audit_events row, the slog " +
				"echo and the /audit endpoint. Applied AFTER the delta is computed, so the " +
				"trail still records THAT the field changed without recording what to."},
		{"fields[].unique.enforce", UniqueEnforcements,
			"whether a Service pre-check answers before the database constraint does."},
		{"fields[].unique.scope", UniqueScopes,
			"all = an archived row keeps holding the value forever; active-only = it frees it."},
		{"valueObjects[].backing", VOBackings,
			"what the value object stores underneath; a composite has none — its parts are its value."},
		{"valueObjects[].kind", VODeclaredKinds,
			"raw/enum occupy ONE column; composite spans several and the schema decomposes " +
				"it; manual is the one the generator does not write — you do."},
		{"read.managed", ManagedReads,
			"which framework-stamped columns the reads expose and may filter on; each " +
				"one needs its column declared under storage.managed."},
		{"valueObjects[].written", VOWritings,
			"whose file the type is; manual keeps the parts declared and hands you the " +
				"IsValid — composite only, since a scalar you write is kind: manual."},
		{"valueObjects[].parts[].type", FieldTypes,
			"a composite's part is persisted like any field, so it draws from the same closed set."},
		{"valueObjects[].rules.list[].kind", CompositeRuleKinds,
			"what a composite may check from its own parts; the rest belongs to the entity's rules."},
		{"children[].ownedBy", ChildOwners,
			"base = the collection belongs to the shared identity and outlives this role."},
		{"children[].editStrategy", EditStrategies,
			"atomic-replace = the root's update swaps the whole collection; per-child = its own endpoints."},
		{"children[].operations", ChildOperations,
			"WHICH per-entry verbs a per-child collection mounts; absent = all three. " +
				"Drop `change` where every field is the business identity — replacing " +
				"such an entry is a removal plus an addition wearing an edit's clothes."},
		{"surfaces.graphql.mutations", GraphQLMutations,
			"NARROWS the root's write side on the schema; absent = every write verb the " +
				"entity mounts. `update` covers PUT and PATCH together, and a collection's " +
				"per-entry verbs are narrowed under children[].surfaces, not here."},
		{"children[].change.shape", UpdateShapes,
			"HOW the change verb takes its body — the same three answers update.shape " +
				"gives at the root. Absent = put, which is what change meant before this " +
				"key existed."},
		{"children[].surfaces.graphql.mutations", ChildOperations,
			"NARROWS which of the collection's per-entry verbs become mutations; absent = " +
				"every verb it mounts. To take the whole collection off the schema, say " +
				"children[].surfaces.graphql.enabled: false instead."},
		{"children[].permissions keys", ChildOperations,
			"WHICH per-entry verb a permission is required for; a verb left out keeps " +
				"inheriting the root's update permission, which is what every per-child " +
				"collection required before this key existed."},
		{"delete.children", DeleteChild,
			"soft = the entry is archived and can come back; hard = the row is gone."},
		{"modes", Modes,
			"the verbs the entity has at all; an absent one is not routed."},
		{"update.shape", UpdateShapes,
			"patch cannot say \"set this to null\", which is why a clearable facet forces put."},
		{"delete.root", DeleteRoot,
			"soft = archive, reversible; hard = a permanent purge, and the HTTP verb must say so."},
		{"rules.list[].kind", RuleKinds,
			"what the rule checks — or, for valueObject, WHEN a value object is checked; " +
				"anything outside this set goes to rules.manual."},
		{"rules.list[].scope", RuleScopes,
			"the verbs the rule fires on."},
		{"rules.list[].operator", ComparisonOps,
			"the comparison a `comparison` rule makes between two fields."},
		{"rules.list[].skipWhen", SkipWhens,
			"stand down when the subject is absent — \"valid IF given\" rather than \"required\"."},
		{"notifications[].semantic", Semantics,
			"the HTTP status: conflict is 409, validation 422, forbidden 403."},
		{"notifications[].package", NotificationPackages,
			"where the type is declared; one raised by a child's rule must live in aggregatevos."},
		{"service.facts[].kind", FactKinds,
			"how the fact is answered; manual means you write it in the hook file. " +
				"notExists is exists negated, so the fact can be named for the PROBLEM " +
				"— which is what the rule reading it raises a notification about."},
		{"service.facts[].scope", FactScopes,
			"which rows the question is about, on the archived axis. Absent = all, " +
				"which is what a fact has always done; active is the same thing " +
				"activeOnly: true says, and the two together are refused. archivedOnly " +
				"asks about the archived rows and nothing else, and needs the entity to " +
				"declare an archive column — with no marker column every scope yields no " +
				"gate, so it would answer about every row instead of about none."},
		{"service.facts[].returns", FactReturns,
			"the Go type the fact answers with. Under perEntry it is the map's VALUE " +
				"— the answer for one entry — not the method's return type."},
		{"service.facts[].aggregates[].kind", AggregateKinds,
			"what ONE of the numbers a fact answers is. The list asks several of them " +
				"in ONE query, which is what the framework's aggregate loader takes; " +
				"`exists` and `manual` are absent because neither is an aggregate — ask " +
				"those as facts of their own."},
		{"service.facts[].filters[].op", FactFilterOps,
			"the comparison a fact's filter makes. Absent = eq, which is what a bare " +
				"field name means. It also decides HOW the value arrives: one value for " +
				"eq/ne/gt/gte/lt/lte, a SET for in/nin (the method takes a slice, or the " +
				"spec pins the whole list under values), and NONE for isnull/notnull, " +
				"which ask about the column being empty."},
		{"joins[].fields[].type", JoinFieldTypes,
			"the type a joined value lands in. Derived from the TARGET's own declaration " +
				"when omitted, which is the spelling to prefer; stated only for a target " +
				"this project has no spec for. `id` is absent on purpose — a join field " +
				"carries no domain type, so an identity column arrives as text."},
		{"joins[].kind", JoinKinds,
			"what a row with NO counterpart means: left keeps it and reads the joined " +
				"fields as NULL (legal over any foreign key, and the fields land in " +
				"nullable Go types); inner drops it, and is legal ONLY over a " +
				"non-nullable foreign key — the declaration reaches FindByID too, so " +
				"over a nullable one it would turn a legitimate write into a 404."},
		{"read.backing", ReadBackings,
			"relational = read straight from the tables (its own read-model KIND: no version, " +
				"no collection, no rebuild — and it inherits whatever the repository's read " +
				"joins reach); mongo = from a projection updated shortly after the write."},
		{"read.identityView", IdentityViews,
			"whether this role creates the shared identity's own view, joins it, or skips it."},
		{"read.indexes[].order", IndexOrders,
			"the direction of an index key."},
		{"read.byParams.filters[].ops", FilterOps,
			"the operators this field is filterable by; an undeclared one is a typed 400."},
		{"authz.dataAccess", DataAccess,
			"whether every permission holder sees every row, or only their own / their tenant's."},
		{"authz.noIdentity", NoIdentityPolicies,
			"what an absent identity means under a scoped dataAccess. stand-down " +
				"(the default) applies the scope to every authenticated caller and " +
				"steps aside only where there is nobody to scope to — reachable on a " +
				"dev bench alone, since auth.mode: disabled is refused outside " +
				"APP_PROFILE=dev; refuse enforces it even then, serving no rows and " +
				"accepting no scoped write."},
		{"authz.permissions keys", AuthzOperations,
			"the operations a permission can be required for."},
		{"docs.operations keys", DocOperations,
			"the operations caller-facing prose can be attached to. The two reads are " +
				"separate here (byId, byParams) because they are separate endpoints " +
				"with different things to explain, even though one permission guards both."},
		{"(discovered)", Dialects,
			"the relational engines; read from the project, never declared in the spec."},
	}
}

// RefusedKeys are the keys this build accepts in the LANGUAGE and refuses in
// practice, each with the one-line reason.
//
// They exist because the language is frozen and the emitters are not: a key
// stays in the definition so a spec written for a later build still parses,
// while `check` says plainly that this build will not act on it.
//
// The list is here rather than inside coverage.go's conditionals because two
// consumers need it: `explain keys`, which must mark them — a reference that
// lists a refused key like any other sends an author to write it, run, and get
// blocked — and the test asserting the examples cover the language, which must
// not demand that a refused key be demonstrated.
// RefusalFor answers whether a key is refused, INCLUDING as a sub-key of a
// refused block.
//
// Exact lookup is not enough: refusing `siblings[].fields[].unique` says nothing
// about `siblings[].fields[].unique.scope`, which is part of the same block and
// is just as refused. Listed unmarked, it reads as a key this build honours and
// sends an author to write it, run, and get blocked — the exact round trip the
// marking exists to save.
func RefusalFor(path string) (string, bool) {
	refused := RefusedKeys()
	if why, ok := refused[path]; ok {
		return why, true
	}
	for key, why := range refused {
		if strings.HasPrefix(path, key+".") {
			return why, true
		}
	}
	return "", false
}

func RefusedKeys() map[string]string {
	return map[string]string{
		"read.indexes[].partial": "the framework takes a document filter there, and this " +
			"language has no way to write one",
		"read.identityView": "the shared identity's own view is not generated yet",
		"valueObjects[].descriptionKeys": "the per-value catalog entries are written from " +
			"the member texts, not asked for by a flag — declare " +
			"valueObjects[].members[].text",
		"rules.list[].actionName":              "gate the rule by verb scope instead",
		"rules.manual[].actionName":            "describe the condition in the description instead",
		"children[].rules.list[].actionName":   "gate the rule by verb scope instead",
		"children[].rules.manual[].actionName": "describe the condition in the description instead",
		"children[].fields[].assignedFrom": "an entry of a collection has no identity of its " +
			"own to be assigned from — the field that records who acted belongs to the root",
		"siblings[].fields[].assignedFrom": "a facet's field is written with the facet — the " +
			"field that records who acted belongs to the root",
		"children[].fields[].stamped": "the framework stamps an aggregate child's column " +
			"just as it stamps the root's, but this build does not lower it: an entry's " +
			"fields go into the entry's input DTO whole, and there is no per-field " +
			"\"the server owns this one\" narrowing there yet (the same gap that refuses " +
			"children[].fields[].assignedFrom). Date the fact on the ROOT, or take the " +
			"collection's write path by hand",
		"siblings[].fields[].stamped": "the framework itself refuses it — a facet row is a " +
			"1:1 slice of the OWNER's row and carries no framework-owned columns of its " +
			"own; declare the stamped column on the owner",
		"children[].fields[].bypassMaySet": "the row scope narrows by a field of the ROOT, " +
			"and that field's guard is what makes a stated value safe to accept — an " +
			"entry of a collection is not the subject of any scope",
		"siblings[].fields[].bypassMaySet": "the row scope narrows by a field of the ROOT, " +
			"and that field's guard is what makes a stated value safe to accept — a " +
			"facet's field is not the subject of any scope",
		"read.byParams.filters[].required": "a mandatory filter is not generated; the " +
			"endpoint would serve the parameter as optional",
		"delete.children": "nothing reads a blanket delete semantic for the collections — " +
			"removal is declared per child, with children[].softRemove",
		"siblings[].fields[].unique": "uniqueness of a facet's field is not generated — " +
			"declare it on a root field",
		"children[].fields[].unique.echoValue": "an entry's conflict comes from the " +
			"database constraint, which never saw the value — the echo travels back only " +
			"from a service pre-check, and an entry has none in this build",
		"children[].fields[].unique.attachTo": "an entry's conflict is reported against the " +
			"COLLECTION rather than against a field — what the caller needs is which entry " +
			"of the array collided",
		"valueObjects[].rules.manual": "a GENERATED composite value object has no hook file " +
			"of its own; a hand-written invariant over it belongs to the entity's " +
			"rules.manual, which already sees the composite as a field — or take the whole " +
			"type with written: manual and check it inside your own IsValid",
		"valueObjects[].rules.manual[].actionName": "describe the condition in the description instead",
		"valueObjects[].rules.list[].actionName":   "a value object validates its value, not a verb",
		"valueObjects[].rules.list[].scope": "a value object's rule has no verb to gate on — " +
			"the framework validates every value-object field on every write; a rule that " +
			"must fire on one verb only belongs to the entity's rules",
		"valueObjects[].rules.list[].guard": "a value object checks itself inside IsValid, " +
			"which is handed a NotificationContext and no Rules — there is no validation " +
			"pass here for a barrier to end; declare the barrier on the ENTITY's rule that " +
			"reaches this value object",
	}
}

// CanonicalPermission is the permission a project should grant for one
// operation on one resource: `<resource>:<action>`, where the action spells the
// operation itself.
//
// It is a RECOMMENDATION, not a rule the build enforces — a project with its
// own taxonomy keeps it, and the validator only warns when a permission leaves
// the resource's namespace. What it prevents is the drift that comes from
// nobody having decided: `create` here, `write` there, `insert` in the third
// spec, three grants for one verb.
func CanonicalPermission(resource, op string) string {
	action, ok := canonicalActions[op]
	if !ok {
		action = op
	}
	if resource == "" {
		return action
	}
	return resource + ":" + action
}
