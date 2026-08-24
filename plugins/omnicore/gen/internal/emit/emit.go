// Package emit turns the resolved model into files.
//
// Emitters read the model and nothing else. Every file they produce goes
// through gofile.Finalize, which prunes imports and formats — so an emitter may
// list every import it might need without reasoning about which ones a
// particular spec actually uses. That is deliberate: making each emitter track
// its own imports is exactly how generators end up emitting code that does not
// compile.
package emit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/gofile"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// Result is everything one generation produced.
type Result struct {
	Files []fsplan.File
	// MissingTranslations names every catalog entry filled with a marked
	// placeholder, so the report can list them instead of letting an
	// untranslated string look finished.
	MissingTranslations []string
	// UntranslatedEnumValues names every enum member whose text the spec left
	// out, per catalog. It is separate from MissingTranslations because the two
	// have different fallbacks and different costs: a notification with no text
	// is emitted as a loud placeholder, an enum member falls back to its own
	// name and is merely in the wrong language.
	UntranslatedEnumValues []string
	// StaleRegistrations names what a shared registration file already declares
	// with content that no longer matches this spec. The generator does not
	// rewrite those files — they carry other entities' declarations and edits
	// that are not its to discard — so the only honest thing left is to say so.
	StaleRegistrations []string
	// Registrations is what this run wrote into the shared files, hashed per
	// declaration, for the lock to record. It is how the NEXT run tells its own
	// text apart from a hand edit.
	Registrations map[string]map[string]string
	// TargetTables carries the shape the model now requires. It is always
	// resolved, because the reader who needs it is not the one creating the
	// tables — it is the one holding a migration that was written on an EARLIER
	// run and has to decide whether the shape still matches.
	TargetTables []TargetTable
}

// TargetTable is the shape one table must have for the generated code to work.
type TargetTable struct {
	Name    string
	Purpose string
	Columns []TargetColumn
	// Indexes are the constraints the generated CODE expects to exist. They
	// belong in the hand-off as much as the columns do: a uniqueness whose SCOPE
	// changed adds no column at all, so a target shape that lists only columns
	// describes a table that looks already correct while the constraint the
	// domain relies on is still the old one.
	Indexes []TargetIndex
}

// TargetIndex is one index the code depends on, dialect-neutral.
type TargetIndex struct {
	Name    string
	Columns []string
	Unique  bool
	// ActiveOnly says the index covers only the rows that are not archived —
	// the difference between a value that is never free again and one that comes
	// back when the row is archived.
	ActiveOnly bool
	Note       string
}

// TargetColumn is deliberately dialect-neutral: what an ALTER has to add is a
// column with a type and a nullability, and the per-dialect spelling follows the
// same mapping the original CREATE used.
type TargetColumn struct {
	Name     string
	Type     string
	Length   int
	Nullable bool
	Note     string
}

// TargetShape describes every table the model needs.
//
// It must say EXACTLY what the migration writers create — it is the hand-off an
// author writes an ALTER from when `--migrations=no` or the entity already
// exists, so any drift here becomes someone's wrong schema. Each block below
// mirrors its writer in migrations.go by name; change them together.
func TargetShape(m *ir.Model) []TargetTable {
	var out []TargetTable

	if m.IsRole() && !m.Base.Reuse {
		// Mirrors writeBaseTable, natural-key unique index included.
		out = append(out, TargetTable{
			Name: m.Base.Table, Purpose: "the shared identity",
			Columns: columnsOf(m.BaseFields(), m, true),
			Indexes: []TargetIndex{{
				Name:    m.Base.Table + "_" + naturalColumn(m) + "_key",
				Columns: []string{naturalColumn(m)}, Unique: true,
				Note: "the natural key — the identity's own key derives from it, " +
					"so it is UNIQUE and NOT NULL",
			}},
		})
	}
	// Mirrors upSQL's root table, link column included for a separate-fk role.
	root := TargetTable{
		Name: m.Table, Purpose: "the aggregate root",
		Columns: columnsOf(roleFieldsOf(m), m, true),
		Indexes: indexesOf(m),
	}
	if m.IsRole() && m.Base.Link == "separate-fk" {
		root.Columns = append(root.Columns[:1], append([]TargetColumn{{
			Name: baseLinkColumn(m), Type: "id", Note: "link to the shared identity",
		}}, root.Columns[1:]...)...)
	}
	out = append(out, root)
	for _, sib := range m.Siblings {
		// Mirrors writeSiblingTable: every column nullable, NO lifecycle columns
		// — the facet's row exists only while at least one value does.
		out = append(out, TargetTable{
			Name: sib.Table, Purpose: "the " + sib.Name + " facet (1:1, shares the owner key)",
			Columns: siblingColumnsOf(sib),
		})
	}
	for _, c := range m.Children {
		if c.Mounted {
			// The identity's collection: another spec creates it; listing it here
			// would tell an author to CREATE a table that already exists.
			continue
		}
		out = append(out, TargetTable{
			Name: c.Table, Purpose: "the " + c.Segment + " collection (1:N)",
			Columns: childColumnsOf(m, c),
		})
	}
	return out
}

// siblingColumnsOf mirrors writeSiblingTable: shared key, every declared column
// nullable, nothing managed.
func siblingColumnsOf(sib ir.Sibling) []TargetColumn {
	out := []TargetColumn{{Name: "id", Type: "id", Note: "primary key, shared with the owner row"}}
	for _, f := range sib.Fields {
		out = append(out, TargetColumn{
			Name: f.Column, Type: f.SpecType, Length: f.Length, Nullable: true,
		})
	}
	return out
}

// childColumnsOf mirrors writeChildTable: facet-owned fields live on the
// facet's table, the archive stamp is the CHILD's own, and there is no
// revision — an entry rides the aggregate's.
func childColumnsOf(m *ir.Model, c ir.Child) []TargetColumn {
	out := []TargetColumn{
		{Name: "id", Type: "id", Note: "primary key"},
		{Name: parentColumn(c), Type: "id", Note: "foreign key to " + childOwnerTable(m, c)},
	}
	for _, f := range c.Fields {
		if f.Facet != "" {
			continue
		}
		out = append(out, TargetColumn{
			Name: f.Column, Type: f.SpecType, Length: f.Length, Nullable: f.Nullable,
		})
	}
	if c.ArchivedAt != "" {
		out = append(out, TargetColumn{Name: c.ArchivedAt, Type: "time",
			Nullable: true, Note: "archive stamp"})
	}
	if m.Managed.CreatedAt != "" {
		out = append(out, TargetColumn{Name: m.Managed.CreatedAt, Type: "time"})
	}
	if m.Managed.UpdatedAt != "" {
		out = append(out, TargetColumn{Name: m.Managed.UpdatedAt, Type: "time"})
	}
	return out
}

func roleFieldsOf(m *ir.Model) []ir.Field {
	if m.IsRole() {
		return m.RoleFields()
	}
	return m.Fields
}

func columnsOf(fields []ir.Field, m *ir.Model, managed bool) []TargetColumn {
	out := []TargetColumn{{Name: "id", Type: "id", Note: "primary key"}}
	for _, f := range fields {
		out = append(out, TargetColumn{
			Name: f.Column, Type: f.SpecType, Length: f.Length, Nullable: f.Nullable,
		})
	}
	if managed {
		out = append(out, TargetColumn{Name: m.Managed.Revision, Type: "int64",
			Note: "optimistic concurrency, maintained by the framework"})
	}
	if m.Managed.CreatedAt != "" {
		out = append(out, TargetColumn{Name: m.Managed.CreatedAt, Type: "time"})
	}
	if m.Managed.UpdatedAt != "" {
		out = append(out, TargetColumn{Name: m.Managed.UpdatedAt, Type: "time"})
	}
	if m.Managed.ArchivedAt != "" {
		out = append(out, TargetColumn{Name: m.Managed.ArchivedAt, Type: "time",
			Nullable: true, Note: "archive stamp"})
	}
	return out
}

// All produces every file for the entity, in dependency order (domain first, so
// a reader following the list reads the model before its wiring).
//
// root is needed because the registration files are MERGED with what the
// project already has, rather than written from scratch.
func All(m *ir.Model, root string, meta FileMeta) (*Result, error) {
	res := &Result{}
	steps := []func(*ir.Model) ([]fsplan.File, error){
		emitValueObjects,
		emitChildren,
		emitDomain,
		emitService,
		emitApplication,
		emitFacetClearCommands,
		emitWeb,
		emitInfra,
		emitBootstrap,
		emitTests,
	}
	for _, step := range steps {
		fs, err := step(m)
		if err != nil {
			return nil, err
		}
		res.Files = append(res.Files, fs...)
	}

	// Migrations are planned like everything else and then never rewritten —
	// the hook class is what enforces "created once". They take the project root
	// because the pair they must not duplicate is the one already on disk.
	mig, err := emitMigrations(m, root)
	if err != nil {
		return nil, err
	}
	res.Files = append(res.Files, mig...)
	res.TargetTables = TargetShape(m)

	regs, missing, stale, written, err := emitRegistrations(m, root, meta.PriorRegistrations, meta.ForeignRegistrations)
	if err != nil {
		return nil, err
	}
	res.Files = append(res.Files, regs...)
	res.MissingTranslations = missing
	res.UntranslatedEnumValues = UntranslatedEnumValues(m)
	res.StaleRegistrations = stale
	res.Registrations = written

	sealFiles(res.Files, root, meta)
	return res, nil
}

// FileMeta is what every header states about the run.
type FileMeta struct {
	Spec   string
	Entity string
	Date   string
	// PriorRegistrations is what THIS entity last wrote into the shared files,
	// from the lock: path → declaration → hash. It is what lets a merge replace
	// its own text and keep its hands off everything else.
	PriorRegistrations map[string]map[string]string
	// ForeignRegistrations is what the project's OTHER entities recorded for the
	// same shared files. A declaration two specs both raise — the notification
	// of a collection two roles expose — is written by whichever ran first and
	// recorded under ITS name; without this the second reads text it never wrote
	// and reports a hand edit that never happened.
	ForeignRegistrations map[string]map[string]string
}

// sealFiles is the single place a header is attached.
//
// It runs at the end, over EVERY file, for the same reason import pruning does:
// asking each emitter to remember produces the one that forgets, and a file
// without a checksum is a file the generator cannot tell apart from something a
// human wrote.
//
// Registration files are the exception, and deliberately so — wire.go, the
// notification files and the catalogs belong to the whole service. Stamping
// them DO NOT EDIT would be false, and sealing content the generator only
// inserts into would report drift the first time anyone touched their own file.
func sealFiles(files []fsplan.File, root string, meta FileMeta) {
	for i := range files {
		f := &files[i]
		if f.Class == fsplan.Registration {
			continue
		}
		previous, _ := os.ReadFile(filepath.Join(root, f.Path))
		comment := gofile.GoComment
		if strings.HasSuffix(f.Path, ".sql") {
			comment = gofile.SQLComment
		}
		f.Content = gofile.ApplyHeaderWith(comment, f.Content, gofile.Meta{
			Describes:   describeFor(f),
			Spec:        meta.Spec,
			Entity:      meta.Entity,
			Date:        meta.Date,
			Hook:        f.Class == fsplan.Hook,
			Consequence: f.Consequence,
		}, previous)
	}
}

// describeFor prefers the file's own one-line purpose, which is the same text
// the report shows — so a reader who opens the file and a reader who reads the
// report are told the same thing.
func describeFor(f *fsplan.File) string {
	if f.Describes != "" {
		return strings.ToUpper(f.Describes[:1]) + f.Describes[1:] + "."
	}
	return "Generated from the entity spec."
}

// src is a small buffer with the conveniences the emitters keep needing.
type src struct {
	bytes.Buffer
}

func (s *src) L(format string, args ...any) {
	if len(args) == 0 {
		s.WriteString(format)
	} else {
		fmt.Fprintf(s, format, args...)
	}
	s.WriteByte('\n')
}

func (s *src) Blank() { s.WriteByte('\n') }

// Doc writes a comment block, wrapping at a readable width so generated
// documentation does not arrive as one endless line.
func (s *src) Doc(lines ...string) {
	for _, line := range lines {
		if line == "" {
			s.L("//")
			continue
		}
		for _, wrapped := range wrap(line, 76) {
			s.L("// %s", wrapped)
		}
	}
}

func wrap(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			out = append(out, line)
			line = w
			continue
		}
		line += " " + w
	}
	return append(out, line)
}

// goFile finalises a buffer into a planned file.
func goFile(path string, class fsplan.Class, describes string, s *src) (fsplan.File, error) {
	out, err := gofile.Finalize(s.Bytes())
	if err != nil {
		return fsplan.File{}, fmt.Errorf("%s: %w", path, err)
	}
	return fsplan.File{Path: path, Class: class, Content: out, Describes: describes}, nil
}

// header is the banner every generated file carries. It names the spec so a
// reader who finds a surprising line knows where to change it — and states the
// ownership rule, so the file itself warns against hand edits.
// quote renders a Go string literal.
func quote(s string) string {
	return fmt.Sprintf("%q", s)
}

// zeroCheck renders the emptiness test for a type.
//
// "Required" means something different per type, and getting it wrong is
// silent: comparing a time to "" does not compile, but comparing a number to
// its zero silently rejects a legitimate 0.
func zeroCheck(f ir.Field, receiver string) string {
	ref := receiver + "." + f.Name
	if f.Nullable {
		switch f.SpecType {
		case "string":
			return fmt.Sprintf("%s == nil || *%s == \"\"", ref, ref)
		case "time":
			return fmt.Sprintf("%s == nil || %s.IsZero()", ref, ref)
		default:
			return fmt.Sprintf("%s == nil", ref)
		}
	}
	switch f.SpecType {
	case "string":
		return fmt.Sprintf("%s == \"\"", ref)
	case "time":
		return fmt.Sprintf("%s.IsZero()", ref)
	case "id":
		// domain.ID answers IsEmpty, not IsZero — the latter compiled against
		// nothing and surfaced only when a required rule landed on an id field.
		return fmt.Sprintf("%s.IsEmpty()", ref)
	case "bool":
		// A false boolean is a value, not an absence; "required" cannot mean
		// anything else for it.
		return "false"
	default:
		return fmt.Sprintf("%s == 0", ref)
	}
}

// deref yields an expression of the field's base type, dereferencing a pointer.
func deref(f ir.Field, receiver string) string {
	if f.Nullable {
		return "*" + receiver + "." + f.Name
	}
	return receiver + "." + f.Name
}

// wireValue renders an entity field AS ITS UNDERLYING SCALAR, unwrapping a
// value object. This is what the write result projects, so the wire never
// leaks a domain type.
func wireValue(f ir.Field, receiver string) string {
	ref := receiver + "." + f.Name
	if f.VOKind == "" {
		return ref
	}
	if f.Nullable {
		// A pointer cast is nil-safe by construction; a nil check here would be
		// noise that also fails to compile for the non-pointer case.
		return fmt.Sprintf("(*%s)(%s)", f.BaseGoType, ref)
	}
	return ref + ".Value()"
}

// factArgValue renders ONE argument of a service-fact call, read off the entity.
//
// It is wireValue plus the one address wireValue cannot form: a composite's
// part. The store knows that part as an ordinary column under its exposed name,
// and the fact's query filters on exactly that — but the ENTITY never carries a
// field by that name. It carries the value object whole, so the argument is
// e.<Owner>.<Part>, and the part's own optionality decides the shape, not the
// field's (every part of an optional composite is a nullable COLUMN while
// staying a plain value inside the type).
//
// A part of an optional composite never reaches here: a rule that would pass one
// is refused in validation, because an absent value object has no value to pass.
func factArgValue(f ir.Field, receiver string) string {
	c := f.Composite
	if c == nil {
		return wireValue(f, receiver)
	}
	ref := receiver + "." + c.Owner + "." + c.PartName
	switch {
	case f.VOKind == "":
		return ref
	case c.PartNullable:
		return fmt.Sprintf("(*%s)(%s)", f.BaseGoType, ref)
	default:
		return ref + ".Value()"
	}
}

// entityValue is the inverse: a wire scalar becomes the entity's field type.
// It is a plain conversion, never a constructor — the framework validates the
// value object itself, so a per-field validate step here would be duplication.
func entityValue(f ir.Field, expr string) string {
	if f.VOKind == "" {
		return expr
	}
	if f.Nullable {
		return fmt.Sprintf("(*%s)(%s)", f.BaseEntityType, expr)
	}
	return fmt.Sprintf("%s(%s)", f.BaseEntityType, expr)
}

// identityValue is entityValue for a value the SERVER reads off the identity,
// which always arrives as text.
//
// An `id` field is the case the plain one cannot serve: the claim is a string
// and the column is the engine's own id type, so the assignment has to parse.
// Without this, `assignedFrom: identity-claim` was refused on an id outright,
// and the author had to choose between an honest UUID column that could carry a
// foreign key and a declarative rule that could read it.
func identityValue(f ir.Field, expr string) string {
	if f.SpecType == "id" {
		return fmt.Sprintf("domain.NewID(%s)", expr)
	}
	return entityValue(f, expr)
}

func fwImport(path string) string {
	return "github.com/ClaudioSchirmer/omnicore/" + path
}

// indexesOf lists the constraints the generated code counts on.
//
// A uniqueness that changed SCOPE is the case this exists for: it adds no
// column, so a hand-off describing only columns shows a table that already
// matches while the constraint still holds the old rule — and the mismatch
// surfaces as a duplicate the API accepts and the database refuses.
func indexesOf(m *ir.Model) []TargetIndex {
	var out []TargetIndex
	for _, c := range m.Constraints {
		if c.Kind != "unique" {
			continue
		}
		note := "a duplicate is reported as " + strings.TrimSuffix(c.Notification, "{}")
		out = append(out, TargetIndex{
			Name: uniqueName(c), Columns: c.Columns, Unique: true,
			ActiveOnly: c.Scope == "active-only" && m.Managed.ArchivedAt != "",
			Note:       note,
		})
	}
	if m.IsRole() && m.Base.Link == "separate-fk" {
		out = append(out, TargetIndex{
			Name: m.Table + "_identity_key", Columns: []string{baseLinkColumn(m)}, Unique: true,
			ActiveOnly: m.Base.RowUniqueness == "active-only",
			Note:       "one role row per identity",
		})
	}
	return out
}

// ViewShape renders what the read side PROJECTS, and nothing else.
//
// It is a fingerprint, not a document: the point is that it changes when the
// projected shape changes — a field added or removed, a collection renamed, a
// facet folded in — and does not change when anything else in the spec moves.
// Comparing it across runs is what lets the generator catch a shape that
// changed while read.view.version stayed put, which the framework answers by
// refusing to boot.
func ViewShape(m *ir.Model) string {
	if !m.Read.Enabled {
		return ""
	}
	// A relational read model has no materialisation, so it has no stored shape
	// to grow stale against: nothing is versioned, nothing is rebuilt, and the
	// framework has no projection to refuse to boot against. An empty
	// fingerprint is what turns the version guard off for it — the honest answer
	// rather than a hash nobody compares.
	if m.Read.Backing == "relational" {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.Read.ViewName + "\n")
	for _, f := range m.AllOwnerFields() {
		fmt.Fprintf(&b, "%s:%s\n", f.Name, f.GoType)
	}
	// A framework-stamped column the read exposes is part of the projected shape
	// like any other field: adding one to an existing view is exactly the change
	// this hash exists to catch, and the framework refuses to boot against a
	// projection built before it.
	for _, f := range m.Read.Managed {
		fmt.Fprintf(&b, "%s:%s\n", f.Name, f.GoType)
	}
	for _, c := range m.Children {
		fmt.Fprintf(&b, "%s[\n", c.GoPlural)
		for _, f := range c.Fields {
			fmt.Fprintf(&b, "  %s:%s\n", f.Name, f.GoType)
		}
		b.WriteString("]\n")
	}
	return b.String()
}
