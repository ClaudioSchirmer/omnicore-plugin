package spec

// The two spellings of a fact filter, and the walk everything else reads it by.
//
// `filters: [CampusID, RegistrationNumber]` is the shape every spec written
// before the operator vocabulary existed uses, and it stays legal forever: a
// bare name is an `eq` leaf whose value arrives as a parameter. The block form
// is the same node with the rest of the vocabulary available. Keeping both is
// not politeness — a language that renames what it already had makes every
// existing spec wrong on the day the feature ships.

import (
	"fmt"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
	"gopkg.in/yaml.v3"
)

// factFilterKeys is the node's own key set, kept here because a type with an
// UnmarshalYAML of its own is invisible to the decoder's KnownFields: yaml.v3
// hands the node over and stops checking. Without this, `feild: X` inside a
// filter would decode to an empty node and generate a method with one parameter
// missing — the precise silent-drop that strict decoding exists to prevent.
var factFilterKeys = map[string]bool{
	"field": true, "op": true, "as": true, "value": true, "values": true,
	"all": true, "any": true, "not": true,
}

// UnmarshalYAML accepts either spelling of a node.
func (f *FactFilter) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var name string
		if err := node.Decode(&name); err != nil {
			return err
		}
		*f = FactFilter{Field: name}
		return nil
	case yaml.MappingNode:
		var unknown []string
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if !factFilterKeys[key.Value] {
				unknown = append(unknown, fmt.Sprintf(
					"line %d: field %s not found in type spec.FactFilter", key.Line, key.Value))
			}
		}
		if len(unknown) > 0 {
			return &yaml.TypeError{Errors: unknown}
		}
		// The alias sheds the method, so this decodes the block instead of
		// calling back into here forever.
		type node2 FactFilter
		var raw node2
		if err := node.Decode(&raw); err != nil {
			return err
		}
		*f = FactFilter(raw)
		return nil
	default:
		return &yaml.TypeError{Errors: []string{fmt.Sprintf(
			"line %d: cannot unmarshal !!%s into spec.FactFilter", node.Line, shortTag(node.Tag))}}
	}
}

// shortTag renders yaml's own tag the way the decoder's other messages do, so
// translateDecodeError recognises it and restates it in the author's language.
func shortTag(tag string) string {
	return strings.TrimPrefix(tag, "!!")
}

// Group reports whether the node combines other nodes instead of comparing a
// field, and which connective it is.
func (f FactFilter) Group() (op string, nodes []FactFilter, ok bool) {
	switch {
	case len(f.All) > 0:
		return "all", f.All, true
	case len(f.Any) > 0:
		return "any", f.Any, true
	case len(f.Not) > 0:
		return "not", f.Not, true
	}
	return "", nil, false
}

// DeclaredGroups counts the group keys the node carries, so "exactly one of
// them" can be said as a refusal rather than resolved by precedence. An author
// who wrote `all` and `any` on one node meant something, and picking the first
// one silently would generate a query they did not ask for.
func (f FactFilter) DeclaredGroups() []string {
	var out []string
	if f.All != nil {
		out = append(out, "all")
	}
	if f.Any != nil {
		out = append(out, "any")
	}
	if f.Not != nil {
		out = append(out, "not")
	}
	return out
}

// Operator is the leaf's comparison, with the default filled in. A leaf with no
// op is an equality — what a bare field name has always meant.
func (f FactFilter) Operator() string {
	if f.Op == "" {
		return "eq"
	}
	return f.Op
}

// Pinned reports whether the leaf carries a constant instead of a parameter.
func (f FactFilter) Pinned() bool { return f.Value != nil || f.Values != nil }

// ParamName is what the leaf's value is called in the generated signature.
func (f FactFilter) ParamName() string {
	if f.As != "" {
		return f.As
	}
	// A per-entry filter names the collection first, and the collection is not
	// part of the question the parameter carries: the entry's field is.
	name := f.Field
	if _, after, dotted := strings.Cut(name, "."); dotted {
		name = after
	}
	return naming.Camel(name)
}

// TakesValue reports whether the operator compares against anything at all.
// isnull and notnull are about the column being empty, so they take neither a
// parameter nor a constant.
func TakesValue(op string) bool { return op != "isnull" && op != "notnull" }

// TakesSet reports whether the operator compares against MANY values — the
// operators whose parameter is a slice and whose constant form is `values`.
func TakesSet(op string) bool { return op == "in" || op == "nin" }

// WalkFactFilters visits every node of a fact's tree, leaves and groups alike,
// in the order they are written — which is also the order the parameters land
// in the generated signature.
//
// `at` is the yaml path of the node, built as the walk descends, because a
// refusal that says `service.facts[2].filters[1].any[0]` points at the line and
// one that says "a filter" sends the author to read the whole block.
func WalkFactFilters(nodes []FactFilter, at string, visit func(f FactFilter, at string)) {
	for i, n := range nodes {
		where := fmt.Sprintf("%s[%d]", at, i)
		visit(n, where)
		if op, kids, ok := n.Group(); ok {
			WalkFactFilters(kids, where+"."+op, visit)
		}
	}
}

// FactFilterFields lists the fields a fact's tree names, in tree order and with
// duplicates kept: it is what the checks that ask "does this fact reach that
// column" read, and one field compared twice is two leaves.
func FactFilterFields(nodes []FactFilter) []string {
	var out []string
	WalkFactFilters(nodes, "", func(f FactFilter, _ string) {
		if _, _, isGroup := f.Group(); !isGroup && f.Field != "" {
			out = append(out, f.Field)
		}
	})
	return out
}

// PlainEqFilters is the tree read as the flat list of equalities it used to be
// — the shape the unique pre-check must have — or false when it is anything
// more.
//
// The pre-check is not one query among others: it is the domain's half of a
// uniqueness whose other half is a database index, and the two are held to the
// same columns compared the same way. An OR, a range, a pinned constant or a
// set makes the domain ask a DIFFERENT question than the index answers, which
// is the failure the exact-match rule already exists to prevent — it would just
// arrive wearing an operator instead of a missing column.
func PlainEqFilters(nodes []FactFilter) ([]string, bool) {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if _, _, isGroup := n.Group(); isGroup {
			return nil, false
		}
		if n.Operator() != "eq" || n.Pinned() || n.Field == "" {
			return nil, false
		}
		out = append(out, n.Field)
	}
	return out, true
}
