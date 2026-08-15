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
	SpecVersion int    `yaml:"specVersion"`
	Entity      string `yaml:"entity"`
	// Plural is REQUIRED. It reaches the route path, the feature name and the
	// listing types, and no rule can spell it: an English heuristic writes
	// "Animals" for Animal and "Pessoas" is not something it could ever reach.
	// The generator does not invent names — this one is declared.
	Plural   string `yaml:"plural"`
	Language string `yaml:"language"`

	Storage       Storage        `yaml:"storage"`
	Fields        []Field        `yaml:"fields"`
	ValueObjects  []ValueObject  `yaml:"valueObjects"`
	Children      []Child        `yaml:"children"`
	Siblings      []Sibling      `yaml:"siblings"`
	Modes         []string       `yaml:"modes"`
	Update        Update         `yaml:"update"`
	Delete        Delete         `yaml:"delete"`
	Rules         Rules          `yaml:"rules"`
	Notifications []Notification `yaml:"notifications"`
	Service       *Service       `yaml:"service"`
	Read          Read           `yaml:"read"`
	Surfaces      Surfaces       `yaml:"surfaces"`
	Authz         Authz          `yaml:"authz"`

	// SourcePath is where this spec was loaded from. Not a YAML key.
	SourcePath string `yaml:"-"`
}

// ---------------------------------------------------------------- storage

type Storage struct {
	Kind        string  `yaml:"kind"` // flat | sharedbase-role
	Table       string  `yaml:"table"`
	Description string  `yaml:"description"`
	Base        *Base   `yaml:"base"`
	Managed     Managed `yaml:"managed"`
}

type Base struct {
	Table       string `yaml:"table"`
	Description string `yaml:"description"`
	Reuse       bool   `yaml:"reuse"`
	NaturalKey  string `yaml:"naturalKey"`
	Link        string `yaml:"link"` // shared-pk | separate-fk
	// LinkColumn is the role's foreign key to the identity, REQUIRED for a
	// separate-fk link. A shared-pk link has none to declare: the role's own
	// primary key IS the identity's, which is the framework's contract rather
	// than a name anyone chooses.
	LinkColumn string `yaml:"linkColumn"`
	// SchemaFunc names the Go function that declares the base schema. Declared
	// because the generator has no way to singularise a table name.
	SchemaFunc    string `yaml:"schemaFunc"`
	RowUniqueness string `yaml:"rowUniqueness"` // unique-fk | active-only
	OrphanPolicy  string `yaml:"orphanPolicy"`  // keep | delete-when-unreferenced
}

// Managed declares the framework-managed columns BY PRESENCE: an empty name
// means the column is not declared, and the framework then never mentions it.
type Managed struct {
	CreatedAt  string `yaml:"createdAt"`
	UpdatedAt  string `yaml:"updatedAt"`
	ArchivedAt string `yaml:"archivedAt"`
	Revision   string `yaml:"revision"`
}

// ---------------------------------------------------------------- fields

type Field struct {
	Name        string   `yaml:"name"`   // Go field name (exported)
	Type        string   `yaml:"type"`   // string | int | int64 | float64 | bool | time | id
	Column      string   `yaml:"column"` // physical column
	Length      int      `yaml:"length"` // required for string
	Nullable    bool     `yaml:"nullable"`
	LivesOn     string   `yaml:"livesOn"` // root | base | role | sibling:<name>
	VO          *FieldVO `yaml:"vo"`
	Unique      *Unique  `yaml:"unique"`
	Example     string   `yaml:"example"`
	Description string   `yaml:"description"`
	LabelKey    string   `yaml:"labelKey"`
	Runtime     bool     `yaml:"runtime"` // runtime-only (authz), never persisted
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
	Kind string `yaml:"kind"` // none | reuse | raw | enum
	Ref  string `yaml:"ref"`  // for reuse: the existing VO type name
}

type Unique struct {
	Enforce      string `yaml:"enforce"`      // service-precheck+constraint | constraint-only
	Notification string `yaml:"notification"` // custom <Field>AlreadyExists…
	Scope        string `yaml:"scope"`        // all | active-only
}

// ---------------------------------------------------------------- value objects

type ValueObject struct {
	Name        string `yaml:"name"`
	Kind        string `yaml:"kind"`    // raw | enum
	Backing     string `yaml:"backing"` // string | int
	Description string `yaml:"description"`

	// raw
	Regex        string   `yaml:"regex"`
	MinLength    int      `yaml:"minLength"`
	MaxLength    int      `yaml:"maxLength"`
	Min          *float64 `yaml:"min"`
	Max          *float64 `yaml:"max"`
	Notification string   `yaml:"notification"`

	// enum
	Members             []EnumMember `yaml:"members"`
	UnknownNotification string       `yaml:"unknownNotification"`
	DescriptionKeys     bool         `yaml:"descriptionKeys"`
}

type EnumMember struct {
	Name  string `yaml:"name"`
	Value any    `yaml:"value"`
	Text  Texts  `yaml:"text"`
}

// ---------------------------------------------------------------- children / siblings

type Child struct {
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
	ParentColumn          string   `yaml:"parentColumn"`
	Table                 string   `yaml:"table"`
	Description           string   `yaml:"description"`
	OwnedBy               string   `yaml:"ownedBy"`      // root | base | role
	EditStrategy          string   `yaml:"editStrategy"` // atomic-replace | per-child
	BusinessIdentity      []string `yaml:"businessIdentity"`
	Fields                []Field  `yaml:"fields"`
	Rules                 Rules    `yaml:"rules"`
	SoftRemove            bool     `yaml:"softRemove"`
	ArchivedAt            string   `yaml:"archivedAt"`
	DuplicateNotification string   `yaml:"duplicateNotification"`
}

type Sibling struct {
	Name        string  `yaml:"name"`
	Table       string  `yaml:"table"`
	Description string  `yaml:"description"`
	AttachTo    string  `yaml:"attachTo"` // root | role | child:<name>
	Fields      []Field `yaml:"fields"`
}

// ---------------------------------------------------------------- lifecycle

type Update struct {
	Shape         string   `yaml:"shape"` // patch | put | both
	PatchExcludes []string `yaml:"patchExcludes"`
}

type Delete struct {
	Root     string `yaml:"root"`     // soft | hard | both
	Children string `yaml:"children"` // soft | hard
}

// ---------------------------------------------------------------- rules

type Rules struct {
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
	ID           string   `yaml:"id"`
	Description  string   `yaml:"description"`
	Scope        []string `yaml:"scope"`
	ActionName   string   `yaml:"actionName"`
	Notification string   `yaml:"notification"`
	AttachTo     string   `yaml:"attachTo"`
}

type Rule struct {
	ID           string   `yaml:"id"`
	Kind         string   `yaml:"kind"`  // required | immutable | length | range | comparison | transition | requiredIf | groupCap | childDuplicate | ownerCheck | manual
	Scope        []string `yaml:"scope"` // insert | update | insertOrUpdate | archive | unarchive | delete
	ActionName   string   `yaml:"actionName"`
	Fields       []string `yaml:"fields"`
	Notification string   `yaml:"notification"`
	AttachTo     string   `yaml:"attachTo"`
	EchoValue    bool     `yaml:"echoValue"`
	Description  string   `yaml:"description"`

	// kind-specific
	Min         *float64            `yaml:"min"`
	Max         *float64            `yaml:"max"`
	Operator    string              `yaml:"operator"` // gte | gt | lte | lt | eq | ne
	Other       string              `yaml:"other"`    // the second field of a comparison
	SkipWhen    string              `yaml:"skipWhen"` // empty | null
	Transitions map[string][]string `yaml:"transitions"`
	GroupBy     []string            `yaml:"groupBy"`
	Cap         int                 `yaml:"cap"`
	OwnerField  string              `yaml:"ownerField"`
	AdminField  string              `yaml:"adminField"`
}

// ---------------------------------------------------------------- notifications

type Notification struct {
	Name        string   `yaml:"name"`
	Package     string   `yaml:"package"` // domain | vos | aggregatevos
	Semantic    string   `yaml:"semantic"`
	TVars       []string `yaml:"tvars"`
	Text        Texts    `yaml:"text"`
	Description string   `yaml:"description"`
}

// Texts carries the seven catalogs the framework requires. Every key is
// mandatory; --lang-fallback marks the missing ones instead of failing.
type Texts struct {
	PTBR string `yaml:"ptbr"`
	ENG  string `yaml:"eng"`
	ESP  string `yaml:"esp"`
	FRA  string `yaml:"fra"`
	DEU  string `yaml:"deu"`
	ITA  string `yaml:"ita"`
	NLD  string `yaml:"nld"`
}

// ---------------------------------------------------------------- service

type Service struct {
	Required bool   `yaml:"required"`
	Facts    []Fact `yaml:"facts"`
}

type Fact struct {
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
	Returns     string   `yaml:"returns"` // bool | int64 | float64 | string
	Field       string   `yaml:"field"`
	Filters     []string `yaml:"filters"`
	ExcludeSelf bool     `yaml:"excludeSelf"`
	ActiveOnly  bool     `yaml:"activeOnly"`
	Description string   `yaml:"description"`
}

// ---------------------------------------------------------------- read side

type Read struct {
	Backing       string          `yaml:"backing"` // relational | mongo
	View          View            `yaml:"view"`
	Indexes       []Index         `yaml:"indexes"`
	ByID          bool            `yaml:"byId"`
	ByParams      *ByParams       `yaml:"byParams"`
	FieldRestrict []FieldRestrict `yaml:"fieldRestrict"`
	IdentityView  string          `yaml:"identityView"` // create | add-role | skip
}

type View struct {
	Name string `yaml:"name"`
	// Version is YOURS, and it is the opposite of specVersion: bump it whenever
	// the projected SHAPE changes — a field added or removed, a collection
	// renamed, a facet folded in. The framework compares it against what is
	// stored and refuses to boot rather than serve a projection built to an
	// older shape, so forgetting is a failed start; and on a Mongo backing,
	// bumping it is what triggers the rebuild.
	Version         int  `yaml:"version"`
	MaxLimit        int  `yaml:"maxLimit"`
	DeleteOnArchive bool `yaml:"deleteOnArchive"`
	TTLSeconds      int  `yaml:"ttlSeconds"`
}

type Index struct {
	Fields  []string `yaml:"fields"`
	Name    string   `yaml:"name"`
	Unique  bool     `yaml:"unique"`
	Text    bool     `yaml:"text"`
	Sparse  bool     `yaml:"sparse"`
	Order   string   `yaml:"order"` // asc | desc
	Partial string   `yaml:"partial"`
}

type ByParams struct {
	Filters  []Filter `yaml:"filters"`
	Sort     []string `yaml:"sort"`
	Controls Controls `yaml:"controls"`
}

type Filter struct {
	Field    string   `yaml:"field"`
	Ops      []string `yaml:"ops"`
	Required bool     `yaml:"required"`
}

type Controls struct {
	Pagination      bool     `yaml:"pagination"`
	OrderBy         bool     `yaml:"orderBy"`
	Fields          bool     `yaml:"fields"`
	Search          []string `yaml:"search"`
	OnlyTotal       bool     `yaml:"onlyTotal"`
	IncludeArchived bool     `yaml:"includeArchived"`
}

type FieldRestrict struct {
	Field      string `yaml:"field"`
	Permission string `yaml:"permission"`
}

// ---------------------------------------------------------------- surfaces

type Surfaces struct {
	REST    bool     `yaml:"rest"`
	GraphQL *GraphQL `yaml:"graphql"`
	Exports *Exports `yaml:"exports"`
}

type GraphQL struct {
	Enabled    bool     `yaml:"enabled"`
	Mutations  []string `yaml:"mutations"`
	Connection bool     `yaml:"connection"`
}

type Exports struct {
	CSV  *CSVExport  `yaml:"csv"`
	XLSX *XLSXExport `yaml:"xlsx"`
	// The row ceiling is deliberately NOT here: it is service-wide configuration
	// (query.maxExportRows). A per-entity copy could disagree with it, with no
	// way for a reader to tell which one the export actually obeyed.
}

type CSVExport struct {
	Delimiter string `yaml:"delimiter"`
}

type XLSXExport struct {
	Sheet string `yaml:"sheet"`
}

// ---------------------------------------------------------------- authz

type Authz struct {
	Resource    string            `yaml:"resource"`
	Permissions map[string]string `yaml:"permissions"`
	DataAccess  string            `yaml:"dataAccess"` // anyone-with-permission | owner-only | tenant
	OwnerField  string            `yaml:"ownerField"`
	TenantField string            `yaml:"tenantField"`
}
