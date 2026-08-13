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
	UsesVOs       bool

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
	LivesOn        string
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
	Fact         *Fact
}

type ManualRule struct {
	ID           string
	Description  string
	Gates        []string
	Notification string
	AttachTo     string
}

type Notification struct {
	Name     string
	Package  string
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
	Sort            []string
	Controls        spec.Controls
	QueryByID       string
	QueryList       string
}

type Filter struct {
	Field Field
	Ops   []string
}

type Authz struct {
	Resource     string
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
		Authz:            Authz{Resource: s.Authz.Resource, DataAccess: s.Authz.DataAccess},
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
	m.Notifications = resolveNotifications(s)
	m.ValueObjects = resolveValueObjects(s)
	m.Service = resolveService(s, m)
	for _, f := range m.Fields {
		if f.VOKind != "" {
			m.UsesVOs = true
			break
		}
	}
	m.Clauses = resolveClauses(s, m)
	m.Clauses = appendUniqueClauses(m)
	m.ManualRules = resolveManualRules(s)
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

func resolveNames(entity, override string) Names {
	plural := naming.Plural(entity)
	if override != "" {
		plural = override
	}
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
		LivesOn: f.LivesOn,
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
	byGate := map[string][]Rule{}
	for _, r := range s.Rules.List {
		rule := Rule{
			ID: r.ID, Kind: r.Kind, Operator: r.Operator,
			Min: r.Min, Max: r.Max, Notification: r.Notification,
			AttachTo: r.AttachTo, EchoValue: r.EchoValue, Description: r.Description,
		}
		for _, fn := range r.Fields {
			if f := lookupField(m, fn); f != nil {
				rule.Fields = append(rule.Fields, *f)
			}
		}
		if r.Other != "" {
			rule.Other = lookupField(m, r.Other)
		}
		if r.OwnerField != "" {
			rule.OwnerField = lookupField(m, r.OwnerField)
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

func resolveManualRules(s *spec.Spec) []ManualRule {
	var out []ManualRule
	for _, m := range s.Rules.Manual {
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
		r.Sort = s.Read.ByParams.Sort
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
	Interface string
	Impl      string
	Facts     []Fact
}

// Fact is one question the port answers.
type Fact struct {
	Name string
	// Manual marks a fact this generator will NOT answer: the port declares it,
	// the body is a stub in a file regeneration never touches, and the compiler
	// refuses to build until a human writes it. That is deliberate — a missing
	// method fails loudly, and a query against the wrong store compiles, returns
	// and means nothing.
	Manual      bool
	Kind        string
	Field       string
	ReturnType  string
	ActiveOnly  bool
	Description string
	Params      []FactParam
}

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
		Interface: m.Entity.Pascal + "Service",
		Impl:      m.Entity.Pascal + "ServiceImpl",
	}
	for _, f := range s.Service.Facts {
		fact := Fact{
			Name: f.Name, Kind: f.Kind, Field: f.Field,
			Manual:     f.Kind == "manual",
			ActiveOnly: f.ActiveOnly, Description: f.Description,
			ReturnType: factReturnType(f, m),
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
	default:
		if fld := lookupField(m, f.Field); fld != nil {
			return fld.BaseGoType
		}
		return "float64"
	}
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
	Name        string
	Plural      string
	Table       string
	Description string
	OwnedBy     string
	Strategy    string
	Fields      []Field
	Identity    []Field // the business-identity subset
	SoftRemove  bool
	ArchivedAt  string
	InputType   string
	AddMethod   string
	Clauses     []Clause
	Segment     string // the key this child projects under in the read document
}

// Sibling is a 1:1 facet stored in its own table, sharing the owner's key.
//
// There is no Go type for it: its fields live on the OWNER as pointers, and the
// split is purely physical. An all-nil facet means no row.
type Sibling struct {
	Name     string
	Table    string
	AttachTo string
	Fields   []Field
}

func resolveChildren(s *spec.Spec, m *Model) []Child {
	var out []Child
	for _, c := range s.Children {
		plural := naming.Plural(c.Name)
		if c.Plural != "" {
			plural = c.Plural
		}
		ch := Child{
			Name: c.Name, Plural: plural, Table: c.Table,
			Description: c.Description, OwnedBy: c.OwnedBy, Strategy: c.EditStrategy,
			SoftRemove: c.SoftRemove, ArchivedAt: c.ArchivedAt,
			InputType: c.Name + "Input", AddMethod: "Add" + c.Name,
			Segment: naming.Camel(plural),
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
		ch.Clauses = resolveClausesFor(c.Rules, ch.Fields)
		out = append(out, ch)
	}
	return out
}

func resolveSiblings(s *spec.Spec, m *Model) []Sibling {
	var out []Sibling
	for _, sib := range s.Siblings {
		r := Sibling{Name: sib.Name, Table: sib.Table, AttachTo: sib.AttachTo}
		for _, f := range sib.Fields {
			r.Fields = append(r.Fields, resolveField(m.Entity.Pascal, f))
		}
		out = append(out, r)
	}
	return out
}

// resolveClausesFor compiles a rule set against an arbitrary field scope, so a
// child's rules are built exactly like the root's.
func resolveClausesFor(rs spec.Rules, scope []Field) []Clause {
	byGate := map[string][]Rule{}
	for _, r := range rs.List {
		rule := Rule{
			ID: r.ID, Kind: r.Kind, Operator: r.Operator,
			Min: r.Min, Max: r.Max, Notification: r.Notification,
			AttachTo: r.AttachTo, EchoValue: r.EchoValue, Description: r.Description,
		}
		for _, fn := range r.Fields {
			for _, f := range scope {
				if f.Name == fn {
					rule.Fields = append(rule.Fields, f)
				}
			}
		}
		if r.Other != "" {
			for i := range scope {
				if scope[i].Name == r.Other {
					rule.Other = &scope[i]
				}
			}
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

// AllOwnerFields is the root's own columns plus every sibling facet: together
// they form ONE Go struct, split across tables only physically.
func (m *Model) AllOwnerFields() []Field {
	out := append([]Field{}, m.Fields...)
	for _, sib := range m.Siblings {
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
	NaturalColumn string
	Link          string
	RowUniqueness string
	OrphanPolicy  string
	FuncName      string
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
		FuncName:     naming.Pascal(naming.Singular(b.Table)) + "Base",
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
					return c.Segment + "." + f.Column
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
	for _, f := range m.AllOwnerFields() {
		if m.PatchExcludes[f.Name] {
			continue
		}
		out = append(out, f)
	}
	return out
}
