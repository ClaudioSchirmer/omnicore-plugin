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
	// identifier; a claim is anything else the token carries; `derived` is the
	// ELSE — the value comes from the entity itself, and the generator only
	// promises to keep the field out of the write surface.
	AssignedFrom = set("identity-subject", "identity-claim", "derived")

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

	Modes        = set("display", "insert", "update", "delete", "archive", "unarchive")
	UpdateShapes = set("patch", "put", "both")
	DeleteRoot   = set("soft", "hard", "both")
	DeleteChild  = set("soft", "hard")

	RuleKinds = set(
		"required", "immutable", "length", "range", "comparison", "transition",
		"requiredIf", "groupCap", "childDuplicate", "ownerCheck", "factRange",
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
	FactKinds = set("exists", "count", "sum", "avg", "min", "max", "manual")

	// FactReturns is the closed set a manual fact may declare. The domain port
	// returns pure values and never an error, so the set stays narrow on purpose.
	FactReturns = set("bool", "int64", "float64", "string")

	ReadBackings  = set("relational", "mongo")
	// JoinFieldTypes is FieldTypes minus `id`: a join field carries no domain
	// type, so an identity crosses as its canonical text instead of as domain.ID.
	JoinFieldTypes = set("string", "int", "int64", "float64", "bool", "time")
	// JoinKinds is what a joining row with no counterpart means. The choice is
	// not free: inner is legal only over a NON-NULLABLE foreign key.
	JoinKinds = set("inner", "left")
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
			"the server fills it, so no write request carries it — from the identity, or " +
				"(derived) from the entity's own fields, by a rule you write."},
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
			"what the rule checks; anything outside this set goes to rules.manual."},
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
			"how the fact is answered; manual means you write it in the hook file."},
		{"service.facts[].returns", FactReturns,
			"the Go type the fact answers with."},
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
		"read.identityView":                    "the shared identity's own view is not generated yet",
		"valueObjects[].descriptionKeys":       "per-value translation keys are not generated",
		"rules.list[].actionName":              "gate the rule by verb scope instead",
		"rules.manual[].actionName":            "describe the condition in the description instead",
		"children[].rules.list[].actionName":   "gate the rule by verb scope instead",
		"children[].rules.manual[].actionName": "describe the condition in the description instead",
		"children[].fields[].assignedFrom": "an entry of a collection has no identity of its " +
			"own to be assigned from — the field that records who acted belongs to the root",
		"siblings[].fields[].assignedFrom": "a facet's field is written with the facet — the " +
			"field that records who acted belongs to the root",
		"read.byParams.filters[].required": "a mandatory filter is not generated; the " +
			"endpoint would serve the parameter as optional",
		"delete.children": "nothing reads a blanket delete semantic for the collections — " +
			"removal is declared per child, with children[].softRemove",
		"siblings[].fields[].unique": "uniqueness of a facet's field is not generated — " +
			"declare it on a root field",
		"valueObjects[].rules.manual": "a GENERATED composite value object has no hook file " +
			"of its own; a hand-written invariant over it belongs to the entity's " +
			"rules.manual, which already sees the composite as a field — or take the whole " +
			"type with written: manual and check it inside your own IsValid",
		"valueObjects[].rules.manual[].actionName": "describe the condition in the description instead",
		"valueObjects[].rules.list[].actionName":   "a value object validates its value, not a verb",
		"valueObjects[].rules.list[].scope": "a value object's rule has no verb to gate on — " +
			"the framework validates every value-object field on every write; a rule that " +
			"must fire on one verb only belongs to the entity's rules",
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
