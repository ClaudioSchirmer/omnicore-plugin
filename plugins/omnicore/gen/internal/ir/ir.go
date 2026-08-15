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

	Notifications []Notification
	ValueObjects  []ValueObject
	Service       *ServiceModel

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
	Example        string
	Description    string
	Unique         *Unique
	Runtime        bool
	Claim          string
	// AssignedFrom names where the server reads this field's value when the
	// client is not allowed to send it. Empty for an ordinary field.
	AssignedFrom string
	LivesOn      string
	// Facet names the 1:1 facet a field is stored in, when it is not stored on
	// its owner's own table. The Go type carries it like any other field — the
	// split is physical — so only the two emitters that write TABLES care.
	Facet string
}

type Unique struct {
	Enforce      string
	Notification string
	Scope        string
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
	Controls        spec.Controls
	QueryByID       string
	QueryList       string
}

type Filter struct {
	Field Field
	Ops   []string
}

type Authz struct {
	DataAccess   string
	OwnerField   *Field
	TenantField  *Field
	OwnerColumn  string
	TenantColumn string
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
		rf := resolveField(m.Entity.Pascal, f)
		if f.Runtime {
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
	bindFacts(m)
	m.Clauses = appendUniqueClauses(m)
	m.ManualRules = resolveManualRules(s.Rules)
	m.HasHookFile = len(m.ManualRules) > 0
	m.PatchExcludes = map[string]bool{}
	for _, name := range s.Update.PatchExcludes {
		m.PatchExcludes[name] = true
	}
	m.Read = resolveRead(s, m)
	m.Ops = resolveOps(s, m)
	m.Constraints = resolveConstraints(s, m)
	m.Surfaces = resolveSurfaces(s)
	if f := lookupField(m, s.Authz.OwnerField); f != nil {
		m.Authz.OwnerField = f
		m.Authz.OwnerColumn = f.Column
	}
	if f := lookupField(m, s.Authz.TenantField); f != nil {
		m.Authz.TenantField = f
		m.Authz.TenantColumn = f.Column
	}
	return m, nil
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
		JSONName: naming.Camel(f.Name), LabelKey: label,
		Example: f.Example, Description: f.Description, Runtime: f.Runtime, Claim: f.Claim,
		AssignedFrom: f.AssignedFrom,
		LivesOn:      f.LivesOn,
	}
	if f.Unique != nil {
		scope := f.Unique.Scope
		if scope == "" {
			scope = "all"
		}
		out.Unique = &Unique{Enforce: f.Unique.Enforce, Notification: f.Unique.Notification, Scope: scope}
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

func resolveClauses(s *spec.Spec, m *Model) []Clause {
	return resolveClauseSet(s.Rules, func(n string) *Field { return lookupField(m, n) })
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
func resolveClauseSet(rs spec.Rules, lookup func(string) *Field) []Clause {
	byGate := map[string][]Rule{}
	for _, r := range rs.List {
		rule := Rule{
			ID: r.ID, Kind: r.Kind, Operator: r.Operator,
			Min: r.Min, Max: r.Max, Notification: r.Notification,
			AttachTo: r.AttachTo, EchoValue: r.EchoValue, Description: r.Description,
			Transitions: r.Transitions, GroupBy: r.GroupBy, Cap: r.Cap,
			SkipWhen: r.SkipWhen, FactName: r.Fact,
		}
		// The two set-wide kinds name a COLLECTION where the others name fields:
		// they ask what the aggregate holds, not what one record says.
		if r.Kind == "childDuplicate" || r.Kind == "groupCap" {
			if len(r.Fields) > 0 {
				rule.Collection = r.Fields[0]
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

// langOrder is the framework's catalog set. All seven, always.
var langOrder = []string{"ptbr", "eng", "esp", "fra", "deu", "ita", "nld"}

func resolveNotifications(s *spec.Spec) []Notification {
	var out []Notification
	for _, n := range s.Notifications {
		pkg := n.Package
		if pkg == "" {
			pkg = "domain"
		}
		texts := map[string]string{
			"ptbr": n.Text.PTBR, "eng": n.Text.ENG, "esp": n.Text.ESP, "fra": n.Text.FRA,
			"deu": n.Text.DEU, "ita": n.Text.ITA, "nld": n.Text.NLD,
		}
		var missing []string
		for _, code := range langOrder {
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

func resolveRead(s *spec.Spec, m *Model) ReadModel {
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
		return r
	}
	r.QueryByID = "Find" + m.Entity.Pascal + "ByIDQuery"
	r.QueryList = "Find" + m.Entity.PluralPascal + "ByParamsQuery"
	if s.Read.ByParams != nil {
		r.Controls = s.Read.ByParams.Controls
		for _, f := range s.Read.ByParams.Filters {
			if fld := lookupField(m, f.Field); fld != nil {
				r.Filters = append(r.Filters, Filter{Field: *fld, Ops: f.Ops})
			}
		}
	}
	return r
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
		ops = append(ops, byIDWriteOp("archive", e, "fiber.MethodPatch", "/:id/archive",
			perm("archive"), "handlers.ArchiveCommandHandler",
			"Archive "+article(e)+" "+strings.ToLower(e)+" (reversible)"))
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
	for _, f := range m.Fields {
		if f.Unique == nil {
			continue
		}
		out = append(out, Constraint{
			Kind: "unique", Table: m.Table, Columns: []string{f.Column},
			Notification: f.Unique.Notification + "{}", Field: f.JSONName,
			Scope: f.Unique.Scope,
		})
	}
	return out
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
}

// EnumMember is one declared value of an enum value object.
type EnumMember struct {
	ConstName string
	Literal   string
	Name      string
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
			Min: vo.Min, Max: vo.Max, Notification: vo.Notification,
			UnknownNotification: vo.UnknownNotification,
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
				v.Members = append(v.Members, EnumMember{
					ConstName: vo.Name + mem.Name,
					Literal:   enumLiteral(mem.Value, backing),
					Name:      mem.Name,
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
			if fld := lookupField(m, filter); fld != nil {
				fact.Params = append(fact.Params, FactParam{
					Name: naming.Camel(fld.Name), GoType: fld.BaseGoType, Field: fld.Name,
				})
			}
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
			ID:           "unique-" + f.Name,
			Kind:         "uniquePrecheck",
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
	Identity    []Field // the business-identity subset
	ArchivedAt  string
	InputType   string
	AddMethod   string
	// PerChild says the collection is edited one entry at a time: its own
	// endpoints, its own commands, and a 404 when the entry is not there. The
	// alternative — atomic-replace — has no entry to address, because the root's
	// update swaps the whole collection.
	PerChild     bool
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
	Name     string
	Table    string
	AttachTo string
	// OwnerChild is "" when the facet hangs off the entity's own table, or the
	// child's NAME when it lives inside that child. It is AttachTo resolved to
	// the node whose schema must declare it — the framework panics if a facet is
	// declared over a type other than its owner's.
	OwnerChild string
	Fields     []Field
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
			ChangeMethod:          "Change" + c.Name + "ByID",
			RemoveMethod:          "Remove" + c.Name + "ByID",
			DuplicateNotification: c.DuplicateNotification,
			// One name, three consumers: the document segment IS the declared
			// collection name, the wire path is its lower-camel, and the read
			// DTO's field must be the name itself.
			Segment:    naming.Camel(c.Plural),
			DocSegment: c.Plural,
		}
		ch.Projector = "project" + ch.GoPlural
		if ch.Mounted {
			ch.OpBase = m.Entity.Pascal + c.Name
			ch.Projector = "project" + m.Entity.Pascal + ch.GoPlural
		}
		for _, f := range c.Fields {
			ch.Fields = append(ch.Fields, resolveField(c.Name, f))
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
		r := Sibling{Name: sib.Name, Table: sib.Table, AttachTo: sib.AttachTo}
		if rest, ok := strings.CutPrefix(sib.AttachTo, "child:"); ok {
			r.OwnerChild = rest
		}
		for _, f := range sib.Fields {
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
func resolveClausesFor(rs spec.Rules, scope []Field) []Clause {
	return resolveClauseSet(rs, func(n string) *Field {
		for i := range scope {
			if scope[i].Name == n {
				return &scope[i]
			}
		}
		return nil
	})
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
		for _, c := range m.Children {
			if c.Name != name[:i] {
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
	return out
}

// AssignedFields are the ones the server fills from the identity, in spec
// order. Only an insert writes them.
func (m *Model) AssignedFields() []Field {
	var out []Field
	for _, f := range m.AllOwnerFields() {
		if f.AssignedFrom != "" {
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
			if f.VOKind != "" {
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
		if f.VOKind != "" {
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
