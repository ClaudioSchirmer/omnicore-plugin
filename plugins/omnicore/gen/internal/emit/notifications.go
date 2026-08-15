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
func emitRegistrations(m *ir.Model, root string) ([]fsplan.File, []string, error) {
	var files []fsplan.File
	var missingTranslations []string

	// One registration file per package, by necessity rather than taste: the
	// domain package imports vos, so a notification a value object raises cannot
	// live in domain without creating an import cycle.
	for _, pkg := range []struct{ name, dir string }{
		{"domain", "internal/domain"},
		{"vos", "internal/domain/vos"},
		{"aggregatevos", "internal/domain/aggregatevos"},
	} {
		f, err := mergeNotifications(m, root, pkg.name, pkg.dir)
		if err != nil {
			return nil, nil, err
		}
		if f != nil {
			files = append(files, *f)
		}
	}

	entries := catalogEntries(m)
	for _, c := range catalogs {
		f, added, err := mergeCatalog(m, root, c.Code, c.Type, c.Ctor, c.LangConst, entries[c.Code])
		if err != nil {
			return nil, nil, err
		}
		if f != nil {
			files = append(files, *f)
		}
		_ = added
	}

	if f, err := mergeWire(m, root); err != nil {
		return nil, nil, err
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
	return files, missingTranslations, nil
}

func mergeNotifications(m *ir.Model, root, pkg, dir string) (*fsplan.File, error) {
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
		return nil, nil
	}

	rel := dir + "/notifications.go"
	existing, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		existing = []byte(notificationsSkeleton(pkg))
	}

	merged, changed := MergeTypeDecls(string(existing), decls)
	if !changed && err == nil {
		return nil, nil
	}
	out, ferr := gofile.Finalize([]byte(merged))
	if ferr != nil {
		return nil, fmt.Errorf("%s: %w", rel, ferr)
	}
	return &fsplan.File{
		Path: rel, Class: fsplan.Registration, Content: out,
		Describes: fmt.Sprintf("%d notification declaration(s)", len(decls)),
	}, nil
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
			"entity needs and never rewrites what is already here.",
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
// leave some users reading a raw translation key.
var langOrder = []string{"ptbr", "eng", "esp", "fra", "deu", "ita", "nld"}

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
		out[lang] = entries
	}
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
	return out
}

// labelText uses the field's description when the spec's own language matches
// the catalog, and falls back to the field name otherwise — a placeholder a
// translator can find, never a wrong translation presented as right.
func labelText(f ir.Field, lang string) string {
	if f.Description != "" && lang == "ptbr" {
		return strings.TrimSuffix(firstLine(f.Description), ".")
	}
	return spaceOut(f.Name)
}

func spaceOut(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func mergeCatalog(m *ir.Model, root, code, typeName, ctor, langConst string, entries []MapEntry) (*fsplan.File, []string, error) {
	rel := "internal/application/translations/" + code + ".go"
	existing, err := os.ReadFile(filepath.Join(root, rel))
	created := false
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, err
		}
		existing = []byte(catalogSkeleton(code, typeName, ctor, langConst))
		created = true
	}

	merged, changed, added := MergeMapEntries(string(existing), entries)
	if !changed && !created {
		return nil, nil, nil
	}
	out, ferr := gofile.Finalize([]byte(merged))
	if ferr != nil {
		return nil, nil, fmt.Errorf("%s: %w", rel, ferr)
	}
	return &fsplan.File{
		Path: rel, Class: fsplan.Registration, Content: out,
		Describes: fmt.Sprintf("%d %s translation key(s)", len(added), strings.ToUpper(code)),
	}, added, nil
}
