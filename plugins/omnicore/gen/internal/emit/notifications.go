package emit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/gofile"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// emitRegistrations produces the shared files: the notification declarations
// and the seven translation catalogs.
//
// These belong to the whole service, not to this entity, so they are MERGED:
// what is already there survives untouched, including wording a translator
// improved by hand.
func emitRegistrations(m *ir.Model, root string, prior, foreign map[string]map[string]string) ([]fsplan.File, []string, []string, map[string]map[string]string, error) {
	var files []fsplan.File
	var missingTranslations []string
	var staleRegistrations []string
	written := map[string]map[string]string{}
	priorFor := func(rel string) map[string]string {
		if prior == nil {
			return nil
		}
		return prior[rel]
	}
	foreignFor := func(rel string) map[string]string {
		if foreign == nil {
			return nil
		}
		return foreign[rel]
	}
	record := func(rel string, w map[string]string) {
		if len(w) > 0 {
			written[rel] = w
		}
	}

	// One registration file per package, by necessity rather than taste: the
	// domain package imports vos, so a notification a value object raises cannot
	// live in domain without creating an import cycle.
	for _, pkg := range []struct{ name, dir string }{
		{"domain", "internal/domain"},
		{"vos", "internal/domain/vos"},
		{"aggregatevos", "internal/domain/aggregatevos"},
	} {
		rel := pkg.dir + "/notifications.go"
		f, stale, w, err := mergeNotifications(m, root, pkg.name, pkg.dir, priorFor(rel), foreignFor(rel))
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if f != nil {
			files = append(files, *f)
		}
		staleRegistrations = append(staleRegistrations, stale...)
		record(rel, w)
	}

	entries := catalogEntries(m)
	for _, c := range catalogs {
		rel := "internal/application/translations/" + c.Code + ".go"
		f, _, stale, w, err := mergeCatalog(m, root, c.Code, c.Type, c.Ctor, c.LangConst,
			entries[c.Code], priorFor(rel))
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if f != nil {
			files = append(files, *f)
		}
		staleRegistrations = append(staleRegistrations, stale...)
		record(rel, w)
	}

	if f, err := mergeWire(m, root); err != nil {
		return nil, nil, nil, nil, err
	} else if f != nil {
		files = append(files, *f)
	}

	for _, n := range m.Notifications {
		for _, lang := range n.Missing {
			missingTranslations = append(missingTranslations,
				fmt.Sprintf("%s / %s", n.Name, strings.ToUpper(lang)))
		}
	}
	sort.Strings(missingTranslations)
	sort.Strings(staleRegistrations)
	return files, missingTranslations, staleRegistrations, written, nil
}

func mergeNotifications(m *ir.Model, root, pkg, dir string, prior, foreign map[string]string) (*fsplan.File, []string, map[string]string, error) {
	var decls []TypeDecl
	for _, n := range m.Notifications {
		if n.Package != pkg {
			continue
		}
		decls = append(decls, TypeDecl{
			Name: n.Name,
			Doc:  notificationDoc(n),
			Body: notificationBody(n),
		})
	}
	if len(decls) == 0 {
		return nil, nil, nil, nil
	}

	rel := dir + "/notifications.go"
	existing, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, nil, err
		}
		existing = []byte(notificationsSkeleton(pkg))
	}

	merged, changed, stale, written := MergeTypeDeclsWith(string(existing), decls, prior, foreign)
	// The stale list travels even when NOTHING was written: a declaration that
	// drifted is exactly the case where there is nothing to add, and returning
	// early on "no change" is how it would go unsaid.
	if !changed && err == nil {
		return nil, staleIn(rel, stale), written, nil
	}
	out, ferr := gofile.Finalize([]byte(merged))
	if ferr != nil {
		return nil, nil, nil, fmt.Errorf("%s: %w", rel, ferr)
	}
	return &fsplan.File{
		Path: rel, Class: fsplan.Registration, Content: out,
		Describes: fmt.Sprintf("%d notification declaration(s)", len(decls)),
	}, staleIn(rel, stale), written, nil
}

// catalogDescribes says what actually happened to a catalog. Counting only the
// INSERTED keys described a file that was rewritten to update a message as "0
// translation key(s)" — a line that reads like nothing happened, in the report
// whose job is saying what did.
func catalogDescribes(code string, res CatalogMerge) string {
	lang := strings.ToUpper(code)
	added, updated := len(res.Added), len(res.Updated)
	switch {
	case added > 0 && updated > 0:
		return fmt.Sprintf("%d new and %d updated %s translation key(s)", added, updated, lang)
	case added > 0:
		return fmt.Sprintf("%d %s translation key(s)", added, lang)
	case updated > 0:
		return fmt.Sprintf("%d updated %s translation key(s)", updated, lang)
	}
	return lang + " translation keys"
}

// staleIn labels each drifted name with the file it sits in, because the reader
// of the report has to open one of several shared files to fix it.
func staleIn(rel string, names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, n+" — "+rel)
	}
	return out
}

func notificationDoc(n ir.Notification) string {
	status := map[string]string{
		"validation":     "422",
		"conflict":       "409 (already exists)",
		"state-conflict": "409 (wrong state)",
		"forbidden":      "403",
		"not-found":      "404",
	}[n.Semantic]
	doc := fmt.Sprintf("%s reaches the caller as %s. The struct NAME is the translation key, "+
		"so renaming it here without renaming it in the seven catalogs leaves the message "+
		"untranslated.", n.Name, status)
	if len(n.TVars) > 0 {
		doc += fmt.Sprintf(" It interpolates %s into the message.", strings.Join(n.TVars, ", "))
	}
	return doc
}

func notificationBody(n ir.Notification) string {
	if len(n.TVars) == 0 && n.Semantic == "validation" {
		return fmt.Sprintf("type %s struct{ domain.DomainNotificationBase }", n.Name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "type %s struct {\n", n.Name)
	b.WriteString("\tdomain.DomainNotificationBase\n")
	for _, tv := range n.TVars {
		fmt.Fprintf(&b, "\t%s string `tvar:%q`\n", exported(tv), tv)
	}
	b.WriteString("}")
	if n.Semantic != "validation" {
		fmt.Fprintf(&b, "\n\nfunc (%s) Semantic() domain.NotificationSemantic { return %s }",
			n.Name, semanticConst(n.Semantic))
	}
	return b.String()
}

func semanticConst(s string) string {
	switch s {
	case "conflict":
		return "domain.SemanticConflict"
	case "state-conflict":
		return "domain.SemanticStateConflict"
	case "forbidden":
		return "domain.SemanticForbidden"
	case "not-found":
		return "domain.SemanticNotFound"
	default:
		return "domain.SemanticValidation"
	}
}

func exported(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func notificationsSkeleton(pkg string) string {
	var s src
	s.Doc(
		"Notifications raised by this package's types.",
		"",
		"This file is a registration site: omnicore-gen appends the declarations an "+
			"entity needs, and maintains the ones IT wrote — it records a hash of each, "+
			"so a declaration you changed is recognised as yours and left alone (the "+
			"report names it instead). Nothing here is ever rewritten wholesale: this "+
			"file belongs to every entity in the project.",
	)
	s.Doc("",
		"It lives beside the types that raise it: the domain package imports vos, so "+
			"a notification raised by a value object cannot live in domain.")
	s.Blank()
	s.L("package %s", pkg)
	s.Blank()
	s.L("import %s", quote(fwImport("domain")))
	return s.String()
}

// catalogEntries builds the per-language key/value set this entity contributes:
// one entry per notification, plus the entity's own context label, plus a label
// for every field.
// langOrder is the framework's catalog set. All seven, always — a subset would
// leave some users reading a raw translation key. It comes from the IR rather
// than being restated: this file emits the catalogs, and a list that disagreed
// with the one the notifications were resolved under would emit a file for a
// language nothing filled.
var langOrder = ir.LangOrder

func catalogEntries(m *ir.Model) map[string][]MapEntry {
	out := map[string][]MapEntry{}
	for _, lang := range langOrder {
		var entries []MapEntry
		for _, n := range m.Notifications {
			entries = append(entries, MapEntry{Key: n.Name, Value: n.Text[lang]})
		}
		// The entity name doubles as the context label of every error envelope;
		// without this entry the raw Go type name leaks to the end user.
		entries = append(entries, MapEntry{Key: m.Entity.Pascal, Value: m.Entity.Pascal})
		// EVERY field that carries a labelKey, not just the root's columns.
		//
		// The tag is emitted on the fields of a facet and of a collection too,
		// and the catalog only ever received the root's — so those keys resolved
		// to nothing and the raw Go identifier reached the end user:
		// `ProposalProponentDocumentField` as a CSV column header. A label that
		// is declared and not translated is worse than one that does not exist,
		// because nothing reports it: the export succeeds, the data is right,
		// and the heading is an internal name.
		for _, f := range labelledFields(m) {
			entries = append(entries, MapEntry{Key: f.LabelKey, Value: labelText(f, lang)})
		}
		// EVERY member of every enum this spec declares. The spec has always
		// accepted a text per language on them and nothing ever emitted one, so
		// the seven translations were parsed, validated, and dropped — the
		// author got "this spec can be generated" for a key no build consumed.
		entries = append(entries, enumDescriptions(m, lang)...)
		out[lang] = entries
	}
	return out
}

// enumDescriptions is the catalog contribution of the enum value objects: one
// entry per MEMBER, in one language.
//
// The key is the framework's, not ours. domain.EnumDescriptionKey reflects over
// an enum value and answers "<TypeName>.<value>", and Translator.EnumDescription
// resolves exactly that key with the request language — falling back to the key
// itself when the catalog has none. Which is what a project got until now: the
// spec carried "Aberto"/"Open"/"Ouvert" and the screen read `SituacaoCurso.aberto`
// in all seven languages.
//
// A value object written by hand (written: manual) contributes too. The TYPE is
// the author's; the member set is still the spec's, and the framework derives
// the key by reflection over the type name either way — so the entry is just as
// resolvable and leaving it out would translate the generated enums and no
// other.
//
// Note what this does NOT do: the wire still carries the raw value. The
// framework is deliberate about that ("standardized value in, standardized value
// out") — EnumDescription is a per-request helper for showing a label, never a
// step in persistence, audit, or the response DTO. What the entry buys is that
// the helper has something to find.
func enumDescriptions(m *ir.Model, lang string) []MapEntry {
	var out []MapEntry
	for _, vo := range m.ValueObjects {
		for _, mem := range vo.Members {
			if mem.DescriptionKey == "" {
				continue
			}
			out = append(out, MapEntry{Key: mem.DescriptionKey, Value: memberText(mem, lang)})
		}
	}
	return out
}

// memberText is the member's text in one catalog: what the spec declared under
// members[].text, or the member's own name spaced out.
//
// The fallback is the LABEL fallback, deliberately — see ir.EnumMember.Text.
// "Aberto" is a heading; a TODO placeholder in its place is what the end user
// reads, whereas the spaced member name is at worst an untranslated word.
func memberText(mem ir.EnumMember, lang string) string {
	if t := strings.TrimSpace(mem.Text[lang]); t != "" {
		return t
	}
	return spaceOut(mem.Name)
}

// UntranslatedEnumValues names every enum member the spec left a catalog empty
// for, as "<Type>.<value> / LANG".
//
// It is NOT folded into MissingTranslations: that list is about entries emitted
// as marked placeholders, and these are emitted as a real (if untranslated)
// label. The distinction is the point — a notification with no text is broken
// output, an enum member with no text is a heading in the wrong language.
func UntranslatedEnumValues(m *ir.Model) []string {
	var out []string
	for _, vo := range m.ValueObjects {
		for _, mem := range vo.Members {
			if mem.DescriptionKey == "" {
				continue
			}
			for _, lang := range mem.Missing {
				out = append(out, fmt.Sprintf("%s / %s", mem.DescriptionKey, strings.ToUpper(lang)))
			}
		}
	}
	sort.Strings(out)
	return out
}

// labelledFields is every field this entity declares a labelKey on, in a stable
// order and without duplicates.
//
// A MOUNTED collection is included deliberately: the entry type belongs to the
// other role, but the catalog is per-entity and a reader of THIS role's export
// needs the heading translated just the same. The merge into the catalog is
// key-wise, so both roles contributing the same key is a no-op rather than a
// conflict.
func labelledFields(m *ir.Model) []ir.Field {
	var out []ir.Field
	seen := map[string]bool{}
	add := func(fs []ir.Field) {
		for _, f := range fs {
			if f.LabelKey == "" || seen[f.LabelKey] {
				continue
			}
			seen[f.LabelKey] = true
			out = append(out, f)
		}
	}
	add(m.AllOwnerFields()) // the root's own columns and every facet on it
	add(m.Runtime)          // never persisted, but a rule can attach a notification to one
	for _, c := range m.Children {
		add(c.Fields) // already carries the fields of a facet declared inside it
	}
	// A composite's PARTS came in above — they are ordinary fields by then, each
	// carrying the label the value object declared for it. What is missing is the
	// composite AS A WHOLE: the aggregate declares a field for it, a rule can
	// attach a notification to that field, and its key belongs to no part.
	add(compositeOwnerLabels(m))
	// A COMPUTED read field has no column, so it came through none of the sets
	// above — but it is a column of the tabular export like any other, and its
	// header is looked up by exactly the same key. Leaving it out is the same
	// defect the collections had: the export succeeds and the heading is an
	// internal name.
	add(computedLabels(m))
	// The framework-stamped columns a read exposes. They are columns of the
	// tabular export like any other and their headers resolve through the same
	// key, so leaving them out heads a column with an internal name — the defect
	// the computed fields above were added here to close.
	add(m.Read.Managed)
	// A JOINED field is a column of the tabular export like any other, and its
	// header resolves through the same key. It reaches no other set — it is
	// nowhere in the TableSchema by design — so leaving it out heads a column
	// with an internal name, which is the defect the two sets above were added
	// here to close. Every join's fields are collected, not only the served
	// ones: a rule may attach a notification to a joined field whatever the
	// read side does with it.
	for _, j := range m.Joins {
		add(j.Fields)
	}
	return out
}

// computedLabels renders each computed read field as the labelled field the
// catalog needs. Only the attributes the catalog reads are set — nothing below
// this line has a column to speak of.
func computedLabels(m *ir.Model) []ir.Field {
	var out []ir.Field
	for _, c := range m.Read.Computed {
		out = append(out, ir.Field{
			Name: c.Name, LabelKey: c.LabelKey, Text: c.Text, Description: c.Description,
		})
	}
	return out
}

// compositeOwnerLabels renders each composite this entity carries as the single
// field the aggregate declares for it, so its own labelKey reaches the catalogs.
func compositeOwnerLabels(m *ir.Model) []ir.Field {
	var out []ir.Field
	sets := [][]ir.Field{m.AllOwnerFields()}
	for _, c := range m.Children {
		sets = append(sets, c.Fields)
	}
	for _, set := range sets {
		for _, g := range ir.Composites(set) {
			out = append(out, ir.Field{
				Name:        g.Head.Owner,
				LabelKey:    g.Head.OwnerLabelKey,
				Text:        g.Head.OwnerText,
				Description: g.Head.OwnerDescription,
			})
		}
	}
	return out
}

// labelText is the field's LABEL in one catalog: what the spec declared under
// fields[].text, or the field's own name spaced out.
//
// It deliberately does NOT read the field's description. It used to, in the one
// catalog matching the spec's declared language, and that is a category error
// with a visible cost: a description is a sentence explaining what the field
// means — it is what the column COMMENT wants — while a label is the field's
// short human name, which is what a validation payload puts in `fieldLabel` and
// what a CSV/XLSX export puts in a column header. Seeding one from the other
// put a whole paragraph in a 422 payload, in exactly one language, so the only
// catalog that got special treatment was the one that came out wrong.
//
// The fallback is the field name, spaced out — a placeholder a translator can
// find, which is what the other six catalogs always did and which is right.
func labelText(f ir.Field, lang string) string {
	if t := strings.TrimSpace(f.Text[lang]); t != "" {
		return t
	}
	return spaceOut(f.Name)
}

// spaceOut turns a Go field name into the placeholder label — the fallback every
// catalog the spec left no text for now uses, so it is read by end users.
//
// A run of capitals stays together: TenantID is "Tenant ID" and CPFNumber is
// "CPF Number". Splitting on every capital gave "Tenant I D", which was
// tolerable while one catalog was filled from elsewhere and is not now that all
// seven land here.
func spaceOut(name string) string {
	rs := []rune(name)
	var b strings.Builder
	for i, r := range rs {
		if i > 0 && isUpper(r) {
			prevLower := !isUpper(rs[i-1])
			nextLower := i+1 < len(rs) && !isUpper(rs[i+1])
			if prevLower || nextLower {
				b.WriteByte(' ')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }

func mergeCatalog(m *ir.Model, root, code, typeName, ctor, langConst string, entries []MapEntry, prior map[string]string) (*fsplan.File, []string, []string, map[string]string, error) {
	rel := "internal/application/translations/" + code + ".go"
	existing, err := os.ReadFile(filepath.Join(root, rel))
	created := false
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, nil, nil, err
		}
		existing = []byte(catalogSkeleton(code, typeName, ctor, langConst))
		created = true
	}

	merged, res := MergeMapEntries(string(existing), entries, prior)
	if !res.Changed && !created {
		return nil, nil, staleIn(rel, res.Stale), res.Written, nil
	}
	out, ferr := gofile.Finalize([]byte(merged))
	if ferr != nil {
		return nil, nil, nil, nil, fmt.Errorf("%s: %w", rel, ferr)
	}
	return &fsplan.File{
		Path: rel, Class: fsplan.Registration, Content: out,
		Describes: catalogDescribes(code, res),
	}, res.Added, staleIn(rel, res.Stale), res.Written, nil
}
