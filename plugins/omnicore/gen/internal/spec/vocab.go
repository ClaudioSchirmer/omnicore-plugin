package spec

// The closed vocabularies of the spec language. Every enum-valued key in the
// language resolves against one of these sets; a value outside the set is a
// refusal with the whole set printed, never a silent pass-through.

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
	// identifier; a claim is anything else the token carries.
	AssignedFrom = set("identity-subject", "identity-claim")

	VOKinds            = set("none", "reuse", "raw", "enum")
	VOBackings         = set("string", "int")
	UniqueEnforcements = set("service-precheck+constraint", "constraint-only")
	UniqueScopes       = set("all", "active-only")

	ChildOwners    = set("root", "base", "role")
	EditStrategies = set("atomic-replace", "per-child")

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

	// AuthzOperations is the key set accepted under authz.permissions. It is
	// cross-checked against the operations actually mounted, so a permission for
	// an operation that does not exist is a refusal, not dead configuration.
	AuthzOperations = set("insert", "update", "patch", "delete", "archive", "unarchive", "read")

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
			"enum when the valid values are finite and known; raw when it is a shape."},
		{"fields[].assignedFrom", AssignedFrom,
			"the server fills it from the caller's identity, so no write request carries it."},
		{"fields[].unique.enforce", UniqueEnforcements,
			"whether a Service pre-check answers before the database constraint does."},
		{"fields[].unique.scope", UniqueScopes,
			"all = an archived row keeps holding the value forever; active-only = it frees it."},
		{"valueObjects[].backing", VOBackings,
			"what the value object stores underneath."},
		{"children[].ownedBy", ChildOwners,
			"base = the collection belongs to the shared identity and outlives this role."},
		{"children[].editStrategy", EditStrategies,
			"atomic-replace = the root's update swaps the whole collection; per-child = its own endpoints."},
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
		{"read.backing", ReadBackings,
			"relational = read straight from the tables; mongo = from a projection updated shortly after."},
		{"read.identityView", IdentityViews,
			"whether this role creates the shared identity's own view, joins it, or skips it."},
		{"read.indexes[].order", IndexOrders,
			"the direction of an index key."},
		{"read.byParams.filters[].ops", FilterOps,
			"the operators this field is filterable by; an undeclared one is a typed 400."},
		{"authz.dataAccess", DataAccess,
			"whether every permission holder sees every row, or only their own / their tenant's."},
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
func RefusedKeys() map[string]string {
	return map[string]string{
		"read.byParams.sort": "declared sort allowlists are not generated; controls.orderBy " +
			"decides whether ?orderBy= is served at all",
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
		"children[].fields[].unique": "uniqueness of a collection entry is not generated — " +
			"declare it on a root field, or use businessIdentity for same-entry detection",
		"siblings[].fields[].unique": "uniqueness of a facet's field is not generated — " +
			"declare it on a root field",
	}
}
