package emit

import (
	"fmt"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
)

// Value objects are where a field's own rule lives.
//
// The framework discovers VO-typed fields by reflection and validates them on
// every write, automatically. That is why BuildRules never repeats a format,
// length, range or membership check: doing so would report the same problem
// twice, and the rule would then live in two places that can disagree.
func emitValueObjects(m *ir.Model) ([]fsplan.File, error) {
	if len(m.ValueObjects) == 0 {
		return nil, nil
	}
	var out []fsplan.File
	for _, vo := range m.ValueObjects {
		var (
			f   fsplan.File
			err error
		)
		if vo.Kind == "enum" {
			f, err = emitEnumVO(m, vo)
		} else {
			f, err = emitRawVO(m, vo)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}

	doc, err := emitVOPackageDoc(m)
	if err != nil {
		return nil, err
	}
	return append(out, doc), nil
}

func emitRawVO(m *ir.Model, vo ir.ValueObject) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package vos")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("regexp"))
	s.L("\t%s", quote(fwImport("domain")))
	s.L(")")
	s.Blank()

	rx := naming.Camel(vo.Name) + "Pattern"
	if vo.Regex != "" {
		s.Doc("Compiled once at package level: compiling a pattern per call would " +
			"pay for it on every write.")
		s.L("var %s = regexp.MustCompile(%s)", rx, backquote(vo.Regex))
		s.Blank()
	}

	doc := []string{fmt.Sprintf("%s is a %s constrained by its own rule.", vo.Name, vo.GoBacking)}
	if vo.Description != "" {
		doc = append(doc, "", vo.Description)
	}
	doc = append(doc, "",
		"The framework finds every field of this type and validates it on each write, "+
			"so no rule elsewhere needs to repeat the check.")
	s.Doc(doc...)
	s.L("type %s %s", vo.Name, vo.GoBacking)
	s.Blank()

	s.Doc("Value unwraps to the underlying type, which is what the wire and the database see.")
	s.L("func (v %s) Value() %s { return %s(v) }", vo.Name, vo.GoBacking, vo.GoBacking)
	s.Blank()

	s.Doc("IsValid is the framework's entry point. It reports every problem it finds " +
		"through the context rather than returning one, so a caller sees all of them at once.")
	s.L("func (v %s) IsValid(fieldName string, ctx *domain.NotificationContext) bool {", vo.Name)
	emitRawChecks(s, vo)
	s.L("\treturn true")
	s.L("}")

	return goFile("internal/domain/vos/"+naming.Snake(vo.Name)+".go", fsplan.Owned,
		fmt.Sprintf("the %s value object", vo.Name), s)
}

// emitRawChecks writes the checks a raw value object performs on itself.
//
// The rejected value is echoed as `v` — the value object itself, NOT a
// conversion back to its backing type. AddNotification renders whatever it is
// given through fmt.Sprint, and a value object is a named type over string,
// int or float64 that declares no String(), so it already prints as the value
// it carries. `string(v)` and `int(v)` produced the identical text and only
// read as if the framework needed the help.
func emitRawChecks(s *src, vo ir.ValueObject) {
	notif := vo.Notification + "{}"

	if vo.GoBacking == "string" {
		s.L("\tif v == \"\" {")
		s.L("\t\tctx.AddNotification(fieldName, domain.RequiredFieldNotification{})")
		s.L("\t\treturn false")
		s.L("\t}")
	}
	if vo.MinLength > 0 || vo.MaxLength > 0 {
		var conds []string
		if vo.MinLength > 0 {
			conds = append(conds, fmt.Sprintf("len(v) < %d", vo.MinLength))
		}
		if vo.MaxLength > 0 {
			conds = append(conds, fmt.Sprintf("len(v) > %d", vo.MaxLength))
		}
		s.L("\tif %s {", strings.Join(conds, " || "))
		s.L("\t\tctx.AddNotification(fieldName, %s, v)", notif)
		s.L("\t\treturn false")
		s.L("\t}")
	}
	if vo.Regex != "" {
		s.L("\tif !%sPattern.MatchString(string(v)) {", naming.Camel(vo.Name))
		s.L("\t\tctx.AddNotification(fieldName, %s, v)", notif)
		s.L("\t\treturn false")
		s.L("\t}")
	}
	if vo.Min != nil || vo.Max != nil {
		var conds []string
		if vo.Min != nil {
			conds = append(conds, fmt.Sprintf("v < %s", numberIn(*vo.Min, vo.GoBacking)))
		}
		if vo.Max != nil {
			conds = append(conds, fmt.Sprintf("v > %s", numberIn(*vo.Max, vo.GoBacking)))
		}
		s.L("\tif %s {", strings.Join(conds, " || "))
		s.L("\t\tctx.AddNotification(fieldName, %s, v)", notif)
		s.L("\t\treturn false")
		s.L("\t}")
	}
}

func numberIn(v float64, backing string) string {
	if backing == "int" {
		return fmt.Sprintf("%d", int64(v))
	}
	return strings.TrimSuffix(fmt.Sprintf("%g", v), ".0")
}

func emitEnumVO(m *ir.Model, vo ir.ValueObject) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package vos")
	s.Blank()
	s.L("import %s", quote(fwImport("domain")))
	s.Blank()

	doc := []string{fmt.Sprintf("%s is a closed set of values.", vo.Name)}
	if vo.Description != "" {
		doc = append(doc, "", vo.Description)
	}
	doc = append(doc, "",
		"Go has no enum keyword, so this is the framework's stand-in: a named type "+
			"plus a declared member list, which the framework checks on every write. "+
			"There is no IsValid here — membership is not a rule this type writes.")
	s.Doc(doc...)
	s.L("type %s %s", vo.Name, vo.GoBacking)
	s.Blank()

	s.Doc("The values are EXPLICIT, never an implicit sequence: inserting a member into " +
		"a generated sequence would silently renumber the ones after it, and the stored " +
		"data would no longer mean what it used to.")
	s.L("const (")
	s.L("\t// %s is the zero value: an out-of-set input lands here rather than", vo.UnknownName)
	s.L("\t// passing as a member.")
	s.L("\t%s %s = %s", vo.UnknownName, vo.Name, vo.UnknownValue)
	for _, mem := range vo.Members {
		s.L("\t%s %s = %s", mem.ConstName, vo.Name, mem.Literal)
	}
	s.L(")")
	s.Blank()

	list := naming.Camel(vo.Name) + "Members"
	s.L("var %s = []%s{", list, vo.Name)
	for _, mem := range vo.Members {
		s.L("\t%s,", mem.ConstName)
	}
	s.L("}")
	s.Blank()

	s.L("func (v %s) Value() %s { return %s(v) }", vo.Name, vo.GoBacking, vo.GoBacking)
	s.Blank()
	s.Doc("Values is what the framework checks membership against. The unknown sentinel " +
		"is deliberately absent from it.")
	s.L("func (v %s) Values() []%s { return %s }", vo.Name, vo.Name, list)
	s.Blank()
	s.L("func (v %s) UnknownNotification() domain.Notification { return %s{} }",
		vo.Name, vo.UnknownNotification)

	return goFile("internal/domain/vos/"+naming.Snake(vo.Name)+".go", fsplan.Owned,
		fmt.Sprintf("the %s enumeration (%d members)", vo.Name, len(vo.Members)), s)
}

func emitVOPackageDoc(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Doc(
		"Package vos holds the value objects: the types that carry a field's own rule.",
		"",
		"The framework discovers a field of one of these types and validates it on "+
			"every write, with no registration. That is why an aggregate's BuildRules "+
			"never repeats a format, length, range or membership check — the rule has "+
			"exactly one home, here.",
		"",
		"This package is a leaf: it imports nothing from the rest of the domain.",
	)
	s.L("package vos")
	return goFile("internal/domain/vos/doc.go", fsplan.Owned, "the vos package documentation", s)
}

// backquote prefers a raw string literal so a regex's backslashes survive
// unescaped, falling back when the pattern itself contains a backquote.
func backquote(s string) string {
	if !strings.Contains(s, "`") {
		return "`" + s + "`"
	}
	return fmt.Sprintf("%q", s)
}
