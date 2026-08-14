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
		"requiredIf", "groupCap", "childDuplicate", "ownerCheck",
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
