package spec

import (
	"fmt"
	"regexp"
	"strings"
)

// Options tunes validation for the few knobs that are genuinely the caller's.
type Options struct {
	// LangFallback downgrades "missing translation" from blocker to warning and
	// lets the emitter mark the gaps. Never silent: the report lists every one.
	LangFallback bool
	// ExistingVOs are the value objects the project already declares. Declaring
	// one of them again is refused: a second copy of a rule is a rule that can
	// disagree with itself, and reuse is what the spec is for.
	ExistingVOs []string
}

// Validate checks a spec against the language's closed vocabularies AND against
// the framework's boot contract.
//
// The second half is the point: every check here corresponds to a condition the
// framework would otherwise discover at BOOT (a panic) or at the first write (a
// runtime 500). Catching them statically is INV-3 — a boot trap must never cost
// a boot to find.
func Validate(s *Spec, opt Options) *Problems {
	ps := &Problems{}

	validateHeader(s, ps)
	validateStorage(s, ps)
	validateFields(s, ps, opt)
	validateValueObjects(s, ps, opt)
	validateChildren(s, ps, opt)
	validateSiblings(s, ps, opt)
	validateLifecycle(s, ps)
	validateRules(s, ps)
	validateNotifications(s, ps, opt)
	validateService(s, ps)
	validateRead(s, ps)
	validateSurfaces(s, ps)
	validateAuthz(s, ps)

	ps.Sort()
	return ps
}

var goIdentRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
var columnRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validateHeader(s *Spec, ps *Problems) {
	if s.SpecVersion != 1 {
		ps.BlockerFix("specVersion",
			fmt.Sprintf("unsupported spec version %d", s.SpecVersion),
			"this generator speaks specVersion 1")
	}
	if s.Entity == "" {
		ps.Blockerf("entity", "the entity name is required")
	} else if !goIdentRe.MatchString(s.Entity) {
		ps.BlockerFix("entity",
			fmt.Sprintf("%q is not a usable Go type name", s.Entity),
			"use PascalCase with no underscores, e.g. Student")
	}
	if s.Language == "" {
		ps.WarnFix("language",
			"no language declared for the human-facing text",
			"set language (e.g. pt-BR) so descriptions and examples are generated consistently")
	}
}

func validateStorage(s *Spec, ps *Problems) {
	st := s.Storage
	if !StorageKinds.Has(st.Kind) {
		ps.BlockerFix("storage.kind",
			fmt.Sprintf("%q is not a storage kind", st.Kind),
			"one of: "+StorageKinds.String())
	}
	if st.Table == "" {
		ps.Blockerf("storage.table", "the table name is required")
	} else if engine := ReservedWord(st.Table); engine != "" {
		ps.BlockerFix("storage.table",
			fmt.Sprintf("the table name %q is a reserved word (%s)", st.Table, engine),
			"rename it — it would apply on some engines and be rejected on others")
	} else if !columnRe.MatchString(st.Table) {
		ps.BlockerFix("storage.table",
			fmt.Sprintf("%q is not a usable table name", st.Table),
			"lowercase, digits and underscores, starting with a letter")
	}
	if st.Description == "" {
		ps.WarnFix("storage.description",
			"the table has no description",
			"it becomes the table comment in the migration — one line is enough")
	}

	// Revision is MANDATORY on an entity/base schema: the framework fails to
	// construct the repository without it. Children and siblings must NOT have
	// it (checked with their own tables).
	if st.Managed.Revision == "" {
		ps.BlockerFix("storage.managed.revision",
			"the revision column is not declared",
			"entity and shared-base schemas require it; declare e.g. revision: revision")
	}

	switch st.Kind {
	case "flat":
		if st.Base != nil {
			ps.BlockerFix("storage.base",
				"a flat entity cannot declare a shared base",
				"either drop the base block or set storage.kind: sharedbase-role")
		}
	case "sharedbase-role":
		if st.Base == nil {
			ps.BlockerFix("storage.base",
				"a shared-base role must declare its base",
				"add the base block with table, naturalKey and link")
			return
		}
		validateBase(s, ps)
	}
}

func validateBase(s *Spec, ps *Problems) {
	b := s.Storage.Base
	if b.Table == "" {
		ps.Blockerf("storage.base.table", "the base table name is required")
	}
	if !LinkModels.Has(b.Link) {
		ps.BlockerFix("storage.base.link",
			fmt.Sprintf("%q is not a link model", b.Link),
			"one of: "+LinkModels.String())
	}
	if b.OrphanPolicy != "" && !OrphanPolicies.Has(b.OrphanPolicy) {
		ps.BlockerFix("storage.base.orphanPolicy",
			fmt.Sprintf("%q is not an orphan policy", b.OrphanPolicy),
			"one of: "+OrphanPolicies.String())
	}
	if b.Link == "separate-fk" {
		if !RowUniqueness.Has(b.RowUniqueness) {
			ps.BlockerFix("storage.base.rowUniqueness",
				"a separate-fk link must state how role rows are kept unique",
				"one of: "+RowUniqueness.String())
		}
	} else if b.RowUniqueness != "" {
		ps.BlockerFix("storage.base.rowUniqueness",
			"rowUniqueness only applies to a separate-fk link",
			"a shared-pk link gets row uniqueness from the primary key; remove the key")
	}

	// The natural key derives the identity's primary key as a deterministic
	// UUIDv5. A nullable key would collapse every key-less record into ONE
	// identity — silent data corruption, so it is refused, not warned.
	if b.NaturalKey == "" {
		ps.BlockerFix("storage.base.naturalKey",
			"the natural key is required",
			"it derives the identity's primary key and is the dedup key")
		return
	}
	f := findField(s.Fields, b.NaturalKey)
	if f == nil {
		ps.BlockerFix("storage.base.naturalKey",
			fmt.Sprintf("%q does not name a field of this entity", b.NaturalKey),
			"the natural key must be one of the declared fields")
		return
	}
	if f.Nullable {
		ps.BlockerFix("storage.base.naturalKey",
			fmt.Sprintf("the natural key %q is nullable", b.NaturalKey),
			"a null key collapses every key-less record into one identity; make it required")
	}
	if f.LivesOn != "base" {
		ps.BlockerFix("storage.base.naturalKey",
			fmt.Sprintf("the natural key %q does not live on the base", b.NaturalKey),
			"set its livesOn: base — the key belongs to the shared identity, not the role")
	}
}

func validateFields(s *Spec, ps *Problems, opt Options) {
	if len(s.Fields) == 0 {
		ps.Blockerf("fields", "an entity needs at least one field")
		return
	}
	seenName := map[string]int{}
	seenCol := map[string]int{}
	for i, f := range s.Fields {
		where := fmt.Sprintf("fields[%d] (%s)", i, orUnnamed(f.Name))
		validateOneField(s, f, where, ps, false, opt)

		if j, dup := seenName[f.Name]; dup {
			ps.Blockerf(where, "the field name %q is already used by fields[%d]", f.Name, j)
		} else if f.Name != "" {
			seenName[f.Name] = i
		}
		if f.Runtime {
			continue
		}
		key := f.LivesOn + "." + f.Column
		if j, dup := seenCol[key]; dup {
			ps.Blockerf(where, "the column %q is already mapped by fields[%d] on the same table", f.Column, j)
		} else if f.Column != "" {
			seenCol[key] = i
		}
	}
}

// reservedFieldNames are owned by the framework's managed carrier. Declaring one
// as a mapped field is a boot panic; the ParentID case additionally silently
// overwrites the framework's own value.
var reservedFieldNames = map[string]string{
	"ID":       "the aggregate id comes from the framework's managed carrier",
	"ParentID": "the parent link is projected automatically as the read-only twin of ID",
	"Revision": "the revision column is declared under storage.managed, not as a field",
}

func validateOneField(s *Spec, f Field, where string, ps *Problems, isChild bool, opt Options) {
	if f.Name == "" {
		ps.Blockerf(where, "the field name is required")
	} else if !goIdentRe.MatchString(f.Name) {
		ps.BlockerFix(where,
			fmt.Sprintf("%q is not a usable Go field name", f.Name),
			"use exported PascalCase, e.g. EnrollmentNumber")
	} else if why, reserved := reservedFieldNames[f.Name]; reserved {
		ps.BlockerFix(where,
			fmt.Sprintf("%q is a reserved field name", f.Name),
			why)
	}

	if !FieldTypes.Has(f.Type) {
		ps.BlockerFix(where,
			fmt.Sprintf("%q is not a persistable type", f.Type),
			"one of: "+FieldTypes.String())
	}

	// A zero-length VARCHAR is rejected outright by Postgres, SQL Server and
	// Oracle, so a string without a length would emit DDL that cannot apply.
	if f.Type == "string" && f.Length <= 0 && !f.Runtime {
		ps.BlockerFix(where,
			"a string field needs a length",
			"set length: N — a zero-length VARCHAR is rejected by postgres, sqlserver and oracle")
	}

	if f.Runtime {
		if f.Column != "" {
			ps.BlockerFix(where,
				"a runtime-only field cannot have a column",
				"runtime fields are fed from the request identity and never persisted")
		}
		if f.Unique != nil {
			ps.Blockerf(where, "a runtime-only field cannot be unique — it is never stored")
		}
		if f.Claim == "" {
			ps.BlockerFix(where+".claim",
				"a runtime-only field does not say which claim it comes from",
				"name it, e.g. claim: email — the framework does not opine on custom "+
					"claim names, so there is no convention to fall back on")
		}
		if f.Type != "string" {
			ps.BlockerFix(where+".type",
				"a runtime-only field is read from a token claim, so it must be text",
				"set type: string")
		}
		return
	}

	if f.Column == "" {
		ps.Blockerf(where, "the column name is required")
	} else if !columnRe.MatchString(f.Column) {
		ps.BlockerFix(where,
			fmt.Sprintf("%q is not a usable column name", f.Column),
			"lowercase, digits and underscores, starting with a letter")
	}
	if engine := ReservedWord(f.Column); engine != "" {
		ps.BlockerFix(where,
			fmt.Sprintf("the column name %q is a reserved word (%s)", f.Column, engine),
			"identifiers are emitted unquoted in places the generator does not control, "+
				"so this would apply on some engines and be rejected on others; rename the "+
				"column (a column cannot be renamed once it holds data)")
	}
	// The underscore namespace is reserved by the framework; a column starting
	// with it fails at boot.
	if strings.HasPrefix(f.Column, "_") {
		ps.BlockerFix(where,
			fmt.Sprintf("the column %q starts with an underscore", f.Column),
			"the underscore prefix is reserved by the framework")
	}

	validateFieldPlacement(s, f, where, ps, isChild)

	if f.VO != nil {
		if !VOKinds.Has(f.VO.Kind) {
			ps.BlockerFix(where+".vo.kind",
				fmt.Sprintf("%q is not a value-object kind", f.VO.Kind),
				"one of: "+VOKinds.String())
		}
		if f.VO.Kind == "reuse" && f.VO.Ref == "" {
			ps.BlockerFix(where+".vo",
				"reusing a value object needs its name",
				"set vo.ref to the existing type, e.g. Email")
		}
		// No guard on the inventory being non-empty: a project with NO value
		// objects is precisely where a reuse reference is certainly wrong, and
		// skipping the check there made it vacuous for every fresh project.
		if f.VO.Kind == "reuse" && f.VO.Ref != "" {
			found := false
			for _, name := range opt.ExistingVOs {
				if name == f.VO.Ref {
					found = true
					break
				}
			}
			if !found {
				known := "none — this project declares no value objects yet"
				if len(opt.ExistingVOs) > 0 {
					known = strings.Join(opt.ExistingVOs, ", ")
				}
				ps.BlockerFix(where+".vo.ref",
					fmt.Sprintf("the project declares no value object named %q", f.VO.Ref),
					fmt.Sprintf("declare it under valueObjects (kind: raw or enum), or "+
						"correct the name — known here: %s", known))
			}
		}
		if (f.VO.Kind == "raw" || f.VO.Kind == "enum") && f.VO.Ref == "" {
			ps.BlockerFix(where+".vo",
				"a new value object needs a name",
				"set vo.ref and declare it under valueObjects")
		}
	}

	if f.Unique != nil {
		if !UniqueEnforcements.Has(f.Unique.Enforce) {
			ps.BlockerFix(where+".unique.enforce",
				fmt.Sprintf("%q is not an enforcement style", f.Unique.Enforce),
				"one of: "+UniqueEnforcements.String())
		}
		if f.Unique.Scope != "" && !UniqueScopes.Has(f.Unique.Scope) {
			ps.BlockerFix(where+".unique.scope",
				fmt.Sprintf("%q is not a uniqueness scope", f.Unique.Scope),
				"one of: "+UniqueScopes.String())
		}
		if f.Unique.Notification == "" {
			ps.BlockerFix(where+".unique",
				"a unique field needs its own conflict notification",
				"declare one under notifications and name it here — the framework's "+
					"already-added notification reports a primary-key collision, not this")
		}
		if f.Nullable {
			ps.WarnFix(where,
				"a nullable field is declared unique",
				"most engines allow many NULLs past a unique index; confirm that is intended")
		}
	}

	if f.Example == "" {
		ps.WarnFix(where,
			"no example value",
			"it is what Swagger's \"try it out\" shows; a plausible value costs nothing")
	}
	if f.Description == "" {
		ps.WarnFix(where,
			"no description",
			"it becomes the column comment in the migration")
	}
}

func validateFieldPlacement(s *Spec, f Field, where string, ps *Problems, isChild bool) {
	if isChild {
		if f.LivesOn != "" && f.LivesOn != "root" {
			ps.BlockerFix(where+".livesOn",
				"a child's fields live on the child's own table",
				"remove livesOn")
		}
		return
	}
	switch f.LivesOn {
	case "root":
		if s.Storage.Kind == "sharedbase-role" {
			ps.BlockerFix(where+".livesOn",
				"a shared-base role has no \"root\" table",
				"say base (the shared identity) or role (this role's own table)")
		}
	case "base", "role":
		if s.Storage.Kind != "sharedbase-role" {
			ps.BlockerFix(where+".livesOn",
				fmt.Sprintf("%q only exists in a shared-base model", f.LivesOn),
				"a flat entity places every field on root")
		}
	case "":
		ps.BlockerFix(where+".livesOn",
			"the field does not say which table it lives on",
			placementHint(s))
	default:
		if strings.HasPrefix(f.LivesOn, "sibling:") {
			name := strings.TrimPrefix(f.LivesOn, "sibling:")
			if findSibling(s.Siblings, name) == nil {
				ps.Blockerf(where+".livesOn", "there is no sibling named %q", name)
			}
			return
		}
		ps.BlockerFix(where+".livesOn",
			fmt.Sprintf("%q is not a placement", f.LivesOn),
			placementHint(s))
	}
}

func placementHint(s *Spec) string {
	if s.Storage.Kind == "sharedbase-role" {
		return "one of: base | role | sibling:<name>"
	}
	return "one of: root | sibling:<name>"
}

func validateValueObjects(s *Spec, ps *Problems, opt Options) {
	existing := map[string]bool{}
	for _, name := range opt.ExistingVOs {
		existing[name] = true
	}
	seen := map[string]bool{}
	for i, vo := range s.ValueObjects {
		where := fmt.Sprintf("valueObjects[%d] (%s)", i, orUnnamed(vo.Name))
		if vo.Name == "" {
			ps.Blockerf(where, "the value object needs a name")
		} else if !goIdentRe.MatchString(vo.Name) {
			ps.BlockerFix(where, fmt.Sprintf("%q is not a usable Go type name", vo.Name), "use PascalCase")
		} else if seen[vo.Name] {
			ps.Blockerf(where, "the value object %q is declared twice", vo.Name)
		} else if existing[vo.Name] {
			ps.BlockerFix(where,
				fmt.Sprintf("the project already declares a value object named %q", vo.Name),
				fmt.Sprintf("reuse it instead — on the field write vo: {kind: reuse, ref: %s}; "+
					"a second copy is a rule that can drift from the first", vo.Name))
		} else {
			seen[vo.Name] = true
		}

		if !VOBackings.Has(vo.Backing) {
			ps.BlockerFix(where+".backing",
				fmt.Sprintf("%q is not a backing type", vo.Backing),
				"one of: "+VOBackings.String())
		}

		switch vo.Kind {
		case "raw":
			if vo.Regex == "" && vo.MinLength == 0 && vo.MaxLength == 0 && vo.Min == nil && vo.Max == nil {
				ps.BlockerFix(where,
					"a raw value object with no rule is just its underlying type",
					"give it a regex, a length bound or a numeric range — or declare the field as vo.kind: none")
			}
			if vo.Regex != "" {
				if _, err := regexp.Compile(vo.Regex); err != nil {
					ps.Blockerf(where+".regex", "the pattern does not compile: %v", err)
				}
			}
			if vo.Notification == "" {
				ps.BlockerFix(where+".notification",
					"a raw value object needs the notification it raises when invalid",
					"declare one under notifications with package: vos")
			}
			if len(vo.Members) > 0 {
				ps.Blockerf(where+".members", "members belong to an enum value object, not a raw one")
			}
		case "enum":
			if len(vo.Members) == 0 {
				ps.BlockerFix(where+".members",
					"an enum value object needs its members",
					"list them with explicit values — the zero value is reserved for the unknown sentinel")
			}
			if vo.UnknownNotification == "" {
				ps.BlockerFix(where+".unknownNotification",
					"an enum value object needs the notification for an out-of-set value",
					"declare one under notifications with package: vos")
			}
			validateEnumMembers(vo, where, ps)
			if vo.Regex != "" || vo.MinLength != 0 || vo.MaxLength != 0 {
				ps.Blockerf(where, "format rules belong to a raw value object; an enum validates membership")
			}
		default:
			ps.BlockerFix(where+".kind",
				fmt.Sprintf("%q is not a value-object kind", vo.Kind),
				"raw (a format or range) | enum (a fixed set of values)")
		}
	}
}

func validateEnumMembers(vo ValueObject, where string, ps *Problems) {
	seenName := map[string]bool{}
	seenVal := map[string]bool{}
	for i, m := range vo.Members {
		mw := fmt.Sprintf("%s.members[%d] (%s)", where, i, orUnnamed(m.Name))
		if m.Name == "" || !goIdentRe.MatchString(m.Name) {
			ps.BlockerFix(mw, "the member needs a PascalCase name", "e.g. Active")
		} else if seenName[m.Name] {
			ps.Blockerf(mw, "the member %q is declared twice", m.Name)
		} else {
			seenName[m.Name] = true
		}

		if m.Value == nil {
			ps.BlockerFix(mw,
				"the member has no explicit value",
				"declare it — an implicit sequence silently re-numbers when a member is inserted")
		} else {
			// The backing type decides how the member renders in Go. A
			// mismatch here produces code that does not compile.
			switch vo.Backing {
			case "int":
				if _, ok := m.Value.(int); !ok {
					ps.BlockerFix(mw,
						fmt.Sprintf("the value %v is not a number but the backing is int", m.Value),
						"use a number, or set backing: string")
				} else if m.Value.(int) == 0 {
					ps.BlockerFix(mw,
						"zero is reserved for the unknown sentinel",
						"start the members at 1")
				}
			case "string":
				sv, ok := m.Value.(string)
				if !ok {
					ps.BlockerFix(mw,
						fmt.Sprintf("the value %v is not a string but the backing is string", m.Value),
						"quote it, or set backing: int")
				} else if sv == "" {
					ps.Blockerf(mw, "the empty string is reserved for the unknown sentinel")
				}
			}
			key := fmt.Sprint(m.Value)
			if seenVal[key] {
				ps.Blockerf(mw, "the value %v is used by more than one member", m.Value)
			}
			seenVal[key] = true
		}
	}
}

func validateChildren(s *Spec, ps *Problems, opt Options) {
	seen := map[string]bool{}
	for i, c := range s.Children {
		where := fmt.Sprintf("children[%d] (%s)", i, orUnnamed(c.Name))
		if c.Name == "" || !goIdentRe.MatchString(c.Name) {
			ps.BlockerFix(where, "the child needs a PascalCase name", "e.g. Grade")
		} else if seen[c.Name] {
			ps.Blockerf(where, "the child %q is declared twice", c.Name)
		} else {
			seen[c.Name] = true
		}
		if c.Table == "" {
			ps.Blockerf(where+".table", "the child table name is required")
		}
		if !ChildOwners.Has(c.OwnedBy) {
			ps.BlockerFix(where+".ownedBy",
				fmt.Sprintf("%q is not an owner", c.OwnedBy),
				"one of: "+ChildOwners.String())
		}
		if c.OwnedBy == "base" && s.Storage.Kind != "sharedbase-role" {
			ps.BlockerFix(where+".ownedBy",
				"only a shared-base model has a base to own children",
				"use ownedBy: root")
		}
		if c.OwnedBy == "role" && s.Storage.Kind != "sharedbase-role" {
			ps.BlockerFix(where+".ownedBy",
				"only a shared-base model has a role to own children",
				"use ownedBy: root")
		}
		if !EditStrategies.Has(c.EditStrategy) {
			ps.BlockerFix(where+".editStrategy",
				fmt.Sprintf("%q is not an edit strategy", c.EditStrategy),
				"atomic-replace (the root's update replaces the whole collection) | "+
					"per-child (add/update/archive by id)")
		}
		if len(c.Fields) == 0 {
			ps.Blockerf(where+".fields", "a child needs at least one field")
		}
		for j, f := range c.Fields {
			validateOneField(s, f, fmt.Sprintf("%s.fields[%d] (%s)", where, j, orUnnamed(f.Name)), ps, true, opt)
		}
		for _, bi := range c.BusinessIdentity {
			if findField(c.Fields, bi) == nil {
				ps.Blockerf(where+".businessIdentity",
					"%q does not name a field of this child", bi)
			}
		}
		if len(c.BusinessIdentity) == 0 {
			ps.BlockerFix(where+".businessIdentity",
				"the child does not say what makes two entries the same",
				"list the fields that identify it in business terms — the framework matches "+
					"children by that, never by comparing every field")
		}
		// A soft removal must have somewhere to record it.
		if c.SoftRemove && c.ArchivedAt == "" {
			ps.BlockerFix(where+".archivedAt",
				"the child is soft-removable but declares no archive column",
				"name the column, or set softRemove: false for a hard delete")
		}
		if !c.SoftRemove && c.ArchivedAt != "" {
			ps.BlockerFix(where+".archivedAt",
				"an archive column is declared but the child is not soft-removable",
				"set softRemove: true, or drop the column")
		}
		if c.EditStrategy == "per-child" && c.SoftRemove && c.DuplicateNotification == "" {
			ps.WarnFix(where+".duplicateNotification",
				"no duplicate notification for a per-child collection",
				"the update path can edit one entry into another's identity; naming a "+
					"notification makes the rejection specific")
		}
	}
}

func validateSiblings(s *Spec, ps *Problems, opt Options) {
	seen := map[string]bool{}
	for i, sib := range s.Siblings {
		where := fmt.Sprintf("siblings[%d] (%s)", i, orUnnamed(sib.Name))
		if sib.Name == "" || !goIdentRe.MatchString(sib.Name) {
			ps.BlockerFix(where, "the sibling needs a PascalCase name", "e.g. Scholarship")
		} else if seen[sib.Name] {
			ps.Blockerf(where, "the sibling %q is declared twice", sib.Name)
		} else {
			seen[sib.Name] = true
		}
		if sib.Table == "" {
			ps.Blockerf(where+".table", "the sibling table name is required")
		}

		switch {
		case sib.AttachTo == "root":
			if s.Storage.Kind == "sharedbase-role" {
				ps.BlockerFix(where+".attachTo",
					"a shared-base model has no \"root\" to attach a sibling to",
					"attach it to the role — a base-level 1:1 facet is nullable columns ON the base, "+
						"because a base has many roles and the framework panics on a base sibling")
			}
		case sib.AttachTo == "role":
			if s.Storage.Kind != "sharedbase-role" {
				ps.BlockerFix(where+".attachTo", "there is no role in a flat model", "use attachTo: root")
			}
		case sib.AttachTo == "base":
			ps.BlockerFix(where+".attachTo",
				"a sibling cannot attach to a shared base — the framework panics at boot",
				"a base has many roles, so the 1:1 does not hold; put the facet as nullable "+
					"columns on the base, or attach the sibling to the role")
		case strings.HasPrefix(sib.AttachTo, "child:"):
			name := strings.TrimPrefix(sib.AttachTo, "child:")
			c := findChild(s.Children, name)
			if c == nil {
				ps.Blockerf(where+".attachTo", "there is no child named %q", name)
			} else if c.OwnedBy == "base" {
				ps.BlockerFix(where+".attachTo",
					fmt.Sprintf("%q is a base child, and a sibling cannot attach to one", name),
					"the framework panics at boot; attach the facet to a role child instead")
			}
		default:
			ps.BlockerFix(where+".attachTo",
				fmt.Sprintf("%q is not an attachment node", sib.AttachTo),
				"one of: root | role | child:<name>")
		}

		if len(sib.Fields) == 0 {
			ps.Blockerf(where+".fields", "a sibling needs at least one field")
		}
		for j, f := range sib.Fields {
			fw := fmt.Sprintf("%s.fields[%d] (%s)", where, j, orUnnamed(f.Name))
			validateOneField(s, f, fw, ps, true, opt)
			// The facet is optional as a whole: an all-nil facet means "no row".
			// A non-nullable column in it could never be cleared.
			if !f.Nullable {
				ps.BlockerFix(fw,
					"a sibling's fields must be nullable",
					"the facet exists only when it has values; an absent facet means no row")
			}
		}
	}

	// PATCH cannot express "set this to null", so a clearable facet needs PUT.
	if len(s.Siblings) > 0 && s.Update.Shape == "patch" {
		ps.BlockerFix("update.shape",
			"the model has a sibling facet but the root only accepts PATCH",
			"PATCH cannot assign null, so the facet could never be cleared; use put or both")
	}
}

func validateLifecycle(s *Spec, ps *Problems) {
	if len(s.Modes) == 0 {
		ps.BlockerFix("modes", "the entity declares no modes", "at minimum: [display]")
	}
	has := map[string]bool{}
	for i, m := range s.Modes {
		if !Modes.Has(m) {
			ps.BlockerFix(fmt.Sprintf("modes[%d]", i),
				fmt.Sprintf("%q is not a mode", m), "one of: "+Modes.String())
			continue
		}
		if has[m] {
			ps.Warnf("modes", "the mode %q is listed twice", m)
		}
		has[m] = true
	}

	// Modes ⟺ the archive column: the framework cross-checks these at
	// repository construction and panics when they disagree.
	archiving := has["archive"] || has["unarchive"]
	if archiving && s.Storage.Managed.ArchivedAt == "" {
		ps.BlockerFix("storage.managed.archivedAt",
			"the entity archives but declares no archive column",
			"declare it (e.g. archivedAt: deleted_at) — the framework refuses the mismatch at boot")
	}
	if has["unarchive"] && !has["archive"] {
		ps.BlockerFix("modes",
			"unarchive without archive",
			"there is nothing to bring back; add archive or drop unarchive")
	}

	if s.Update.Shape != "" && !UpdateShapes.Has(s.Update.Shape) {
		ps.BlockerFix("update.shape",
			fmt.Sprintf("%q is not an update shape", s.Update.Shape),
			"one of: "+UpdateShapes.String())
	}
	if has["update"] && s.Update.Shape == "" {
		ps.BlockerFix("update.shape",
			"the entity updates but does not say how",
			"patch (partial, the common case) | put (full body) | both")
	}
	if !has["update"] && s.Update.Shape != "" {
		ps.BlockerFix("update.shape",
			"an update shape is declared but update is not among the modes",
			"add update to modes, or remove the shape")
	}
	for _, ex := range s.Update.PatchExcludes {
		if findField(s.Fields, ex) == nil {
			ps.Blockerf("update.patchExcludes", "%q does not name a field of this entity", ex)
		}
	}

	if s.Delete.Root != "" && !DeleteRoot.Has(s.Delete.Root) {
		ps.BlockerFix("delete.root",
			fmt.Sprintf("%q is not a delete semantic", s.Delete.Root),
			"one of: "+DeleteRoot.String())
	}
	if s.Delete.Children != "" && !DeleteChild.Has(s.Delete.Children) {
		ps.BlockerFix("delete.children",
			fmt.Sprintf("%q is not a delete semantic", s.Delete.Children),
			"one of: "+DeleteChild.String())
	}
	// The HTTP verb must tell the truth: DELETE is a purge, archive is soft.
	hard := s.Delete.Root == "hard" || s.Delete.Root == "both"
	if hard && !has["delete"] {
		ps.BlockerFix("delete.root",
			"a hard delete is declared but delete is not among the modes",
			"add delete to modes, or set delete.root: soft")
	}
	if has["delete"] && !hard {
		ps.BlockerFix("delete.root",
			"the delete mode is declared but delete.root is not hard",
			"DELETE is exclusively an irreversible purge; a reversible removal is archive")
	}
	soft := s.Delete.Root == "soft" || s.Delete.Root == "both"
	if soft && !archiving {
		ps.BlockerFix("delete.root",
			"a soft delete is declared but the entity does not archive",
			"add archive (and usually unarchive) to modes")
	}
}

// frameworkNotifications are supplied by the framework and already translated.
// Naming one costs nothing; declaring a duplicate of one is noise.
var frameworkNotifications = map[string]bool{
	"RequiredFieldNotification":      true,
	"SchemaViolationNotification":    true,
	"RecordNotFoundNotification":     true,
	"EntityAlreadyAddedNotification": true,
	"EntityDoesNotExistNotification": true,
	"ArchiveNotAllowedNotification":  true,
}

func validateRules(s *Spec, ps *Problems) {
	validateRuleSet(s, s.Rules, s.Fields, "rules", ps)
	for i, c := range s.Children {
		validateRuleSet(s, c.Rules, c.Fields, fmt.Sprintf("children[%d].rules", i), ps)
	}
}

func validateRuleSet(s *Spec, rs Rules, scopeFields []Field, where string, ps *Problems) {
	seenID := map[string]bool{}
	for i, r := range rs.List {
		w := fmt.Sprintf("%s.list[%d] (%s)", where, i, orUnnamed(r.ID))
		if r.ID == "" {
			ps.BlockerFix(w, "the rule needs an id", "a short stable slug, e.g. grade-range")
		} else if seenID[r.ID] {
			ps.Blockerf(w, "the rule id %q is used twice", r.ID)
		} else {
			seenID[r.ID] = true
		}
		if !RuleKinds.Has(r.Kind) {
			ps.BlockerFix(w+".kind",
				fmt.Sprintf("%q is not a rule kind", r.Kind),
				"one of: "+RuleKinds.String()+" — anything else goes under rules.manual")
			continue
		}
		validateRuleScope(r, w, ps)
		validateRuleFields(r, scopeFields, w, ps)
		validateRuleNotification(s, r, w, ps)
		validateRuleShape(s, r, scopeFields, w, ps)
	}

	for i, m := range rs.Manual {
		w := fmt.Sprintf("%s.manual[%d] (%s)", where, i, orUnnamed(m.ID))
		if m.ID == "" {
			ps.BlockerFix(w, "the manual rule needs an id", "it names the item in the hook file")
		} else if seenID[m.ID] {
			ps.Blockerf(w, "the rule id %q is used twice", m.ID)
		} else {
			seenID[m.ID] = true
		}
		// This is the check that keeps the escape hatch honest: an unnamed
		// residue becomes an empty TODO nobody can act on.
		if strings.TrimSpace(m.Description) == "" {
			ps.BlockerFix(w+".description",
				"a manual rule must say what it has to enforce",
				"this text is what the generated report tells the implementer to write; "+
					"without it the hook file is an empty TODO")
		}
		validateRuleScope(Rule{Scope: m.Scope}, w, ps)
		if m.Notification != "" {
			validateRuleNotification(s, Rule{Notification: m.Notification}, w, ps)
		}
	}
}

func validateRuleScope(r Rule, w string, ps *Problems) {
	if len(r.Scope) == 0 {
		ps.BlockerFix(w+".scope",
			"the rule does not say when it applies",
			"one or more of: "+RuleScopes.String())
	}
	for _, sc := range r.Scope {
		if !RuleScopes.Has(sc) {
			ps.BlockerFix(w+".scope",
				fmt.Sprintf("%q is not a scope", sc),
				"one of: "+RuleScopes.String())
		}
	}
}

func validateRuleFields(r Rule, scopeFields []Field, w string, ps *Problems) {
	if len(r.Fields) == 0 {
		ps.Blockerf(w+".fields", "the rule names no field")
		return
	}
	for _, fn := range r.Fields {
		if findField(scopeFields, fn) == nil {
			ps.Blockerf(w+".fields", "%q does not name a field in this scope", fn)
		}
	}
}

func validateRuleNotification(s *Spec, r Rule, w string, ps *Problems) {
	if r.Notification == "" {
		return
	}
	if frameworkNotifications[r.Notification] {
		return
	}
	for _, n := range s.Notifications {
		if n.Name == r.Notification {
			return
		}
	}
	ps.BlockerFix(w+".notification",
		fmt.Sprintf("%q is not declared", r.Notification),
		"declare it under notifications, or name one of the framework's: "+
			strings.Join(sortedKeys(frameworkNotifications), ", "))
}

func validateRuleShape(s *Spec, r Rule, scopeFields []Field, w string, ps *Problems) {
	switch r.Kind {
	case "comparison":
		if r.Other == "" {
			ps.BlockerFix(w+".other", "a comparison needs the field to compare against", "set other: <field>")
		} else if findField(scopeFields, r.Other) == nil {
			ps.Blockerf(w+".other", "%q does not name a field in this scope", r.Other)
		}
		if !ComparisonOps.Has(r.Operator) {
			ps.BlockerFix(w+".operator",
				fmt.Sprintf("%q is not a comparison operator", r.Operator),
				"one of: "+ComparisonOps.String())
		}
		if r.Notification == "" {
			ps.Blockerf(w+".notification", "a comparison needs the notification it raises")
		}
	case "range":
		if r.Min == nil && r.Max == nil {
			ps.BlockerFix(w, "a range needs a bound", "set min, max, or both")
		}
		if r.Notification == "" {
			ps.Blockerf(w+".notification", "a range needs the notification it raises")
		}
	case "length":
		if r.Min == nil && r.Max == nil {
			ps.BlockerFix(w, "a length rule needs a bound", "set min, max, or both")
		}
		for _, fn := range r.Fields {
			if f := findField(scopeFields, fn); f != nil && f.Type != "string" {
				ps.Blockerf(w, "%q is not a string, so it has no length", fn)
			}
		}
	case "transition":
		if len(r.Transitions) == 0 {
			ps.BlockerFix(w+".transitions",
				"a transition rule needs the allowed moves",
				"map each current value to the values it may become")
		}
		for _, fn := range r.Fields {
			f := findField(scopeFields, fn)
			if f == nil {
				continue
			}
			if f.VO == nil || f.VO.Kind != "enum" {
				ps.BlockerFix(w,
					fmt.Sprintf("%q is not an enum, so its transitions are not a closed set", fn),
					"model the field as an enum value object first")
			}
		}
		if r.Notification == "" {
			ps.Blockerf(w+".notification", "a transition rule needs the notification it raises")
		}
	case "groupCap":
		if len(r.GroupBy) == 0 {
			ps.BlockerFix(w+".groupBy", "a cap needs the key it groups by", "list the field(s)")
		}
		if r.Cap <= 0 {
			ps.BlockerFix(w+".cap", "a cap needs a positive limit", "set cap: N")
		}
		if s.Service == nil || !s.Service.Required {
			ps.BlockerFix(w,
				"a per-group cap has to count rows the entity cannot see",
				"declare service.required: true and the fact that answers the count")
		}
		if r.Notification == "" {
			ps.Blockerf(w+".notification", "a cap needs the notification it raises")
		}
	case "requiredIf":
		if r.Other == "" {
			ps.BlockerFix(w+".other", "requiredIf needs the field it depends on", "set other: <field>")
		}
		if r.SkipWhen != "" && !SkipWhens.Has(r.SkipWhen) {
			ps.BlockerFix(w+".skipWhen",
				fmt.Sprintf("%q is not a skip condition", r.SkipWhen),
				"one of: "+SkipWhens.String())
		}
	case "ownerCheck":
		if r.OwnerField == "" {
			ps.BlockerFix(w+".ownerField",
				"an owner check needs the field carrying the caller's identity",
				"declare a runtime-only field and name it here")
		} else {
			f := findField(s.Fields, r.OwnerField)
			if f == nil {
				ps.Blockerf(w+".ownerField", "%q does not name a field of this entity", r.OwnerField)
			} else if !f.Runtime {
				ps.BlockerFix(w+".ownerField",
					fmt.Sprintf("%q is a persisted field", r.OwnerField),
					"the caller's identity is runtime-only — set runtime: true on it")
			}
		}
	case "immutable":
		if r.Notification == "" {
			ps.Blockerf(w+".notification", "an immutability rule needs the notification it raises")
		}
		for _, sc := range r.Scope {
			if sc == "insert" {
				ps.BlockerFix(w+".scope",
					"immutability cannot apply on insert",
					"there is no previous value to compare against; scope it to update")
			}
		}
	case "childDuplicate":
		if len(s.Children) == 0 {
			ps.Blockerf(w, "the entity has no child collection to check for duplicates")
		}
	}
}

func validateNotifications(s *Spec, ps *Problems, opt Options) {
	seen := map[string]bool{}
	for i, n := range s.Notifications {
		where := fmt.Sprintf("notifications[%d] (%s)", i, orUnnamed(n.Name))
		if n.Name == "" || !goIdentRe.MatchString(n.Name) {
			ps.BlockerFix(where, "the notification needs a PascalCase name", "e.g. GradeOutOfRangeNotification")
		} else if seen[n.Name] {
			ps.Blockerf(where, "the notification %q is declared twice", n.Name)
		} else {
			seen[n.Name] = true
		}
		if frameworkNotifications[n.Name] {
			ps.BlockerFix(where,
				fmt.Sprintf("%q is a framework notification", n.Name),
				"use it by name; redeclaring it shadows the framework's own translations")
		}
		if !strings.HasSuffix(n.Name, "Notification") {
			ps.WarnFix(where,
				"the name does not end in Notification",
				"the struct name IS the translation key; the suffix keeps the catalogs readable")
		}
		if !Semantics.Has(n.Semantic) {
			ps.BlockerFix(where+".semantic",
				fmt.Sprintf("%q is not a semantic", n.Semantic),
				"one of: "+Semantics.String()+
					" — conflict is a duplicate, state-conflict is a wrong state; both are 409")
		}
		if n.Package != "" && !NotificationPackages.Has(n.Package) {
			ps.BlockerFix(where+".package",
				fmt.Sprintf("%q is not a package", n.Package),
				"one of: "+NotificationPackages.String())
		}
		validateTexts(n, where, ps, opt)
	}
}

func validateTexts(n Notification, where string, ps *Problems, opt Options) {
	langs := map[string]string{
		"ptbr": n.Text.PTBR, "eng": n.Text.ENG, "esp": n.Text.ESP, "fra": n.Text.FRA,
		"deu": n.Text.DEU, "ita": n.Text.ITA, "nld": n.Text.NLD,
	}
	for _, code := range []string{"ptbr", "eng", "esp", "fra", "deu", "ita", "nld"} {
		if strings.TrimSpace(langs[code]) != "" {
			continue
		}
		msg := fmt.Sprintf("no %s translation", strings.ToUpper(code))
		fix := "the framework carries seven catalogs and every key must exist in all of them"
		if opt.LangFallback {
			ps.WarnFix(where+".text."+code, msg,
				"--lang-fallback will emit a marked placeholder; replace it before shipping")
		} else {
			ps.BlockerFix(where+".text."+code, msg, fix)
		}
	}
	// A tvar the text never interpolates is dead; a placeholder with no tvar
	// renders literally as "{x}" to the end user.
	for _, tv := range n.TVars {
		found := false
		for _, txt := range langs {
			if strings.Contains(txt, "{"+tv+"}") {
				found = true
				break
			}
		}
		if !found {
			ps.BlockerFix(where+".tvars",
				fmt.Sprintf("the variable %q never appears in any translation", tv),
				fmt.Sprintf("write {%s} where the value belongs, or drop the variable", tv))
		}
	}
	placeholder := regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_]*)\}`)
	for code, txt := range langs {
		for _, m := range placeholder.FindAllStringSubmatch(txt, -1) {
			if !contains(n.TVars, m[1]) {
				ps.BlockerFix(where+".text."+code,
					fmt.Sprintf("the text interpolates {%s} but no such variable is declared", m[1]),
					fmt.Sprintf("add %q to tvars, or the end user sees the braces verbatim", m[1]))
			}
		}
	}
}

func validateService(s *Spec, ps *Problems) {
	if s.Service == nil {
		return
	}
	if !s.Service.Required {
		if len(s.Service.Facts) > 0 {
			ps.BlockerFix("service.required",
				"facts are declared but the service is not required",
				"set required: true — the framework only passes a service when the entity asks for one")
		}
		return
	}
	if len(s.Service.Facts) == 0 {
		ps.BlockerFix("service.facts",
			"the entity requires a service but names no fact for it to answer",
			"declare what the rules need to know, or set required: false")
	}
	seen := map[string]bool{}
	for i, f := range s.Service.Facts {
		where := fmt.Sprintf("service.facts[%d] (%s)", i, orUnnamed(f.Name))
		if f.Name == "" || !goIdentRe.MatchString(f.Name) {
			ps.BlockerFix(where, "the fact needs a PascalCase name", "it becomes a method on the port")
		} else if seen[f.Name] {
			ps.Blockerf(where, "the fact %q is declared twice", f.Name)
		} else {
			seen[f.Name] = true
		}
		if !FactKinds.Has(f.Kind) {
			ps.BlockerFix(where+".kind",
				fmt.Sprintf("%q is not a fact kind", f.Kind),
				"one of: "+FactKinds.String())
		}
		if f.Kind == "manual" {
			validateManualFact(f, where, ps)
			continue
		}
		if f.Returns != "" {
			ps.BlockerFix(where+".returns",
				fmt.Sprintf("a %s fact already determines what it returns", f.Kind),
				"returns is only declared for a manual fact, where the generator has "+
					"no way to infer the signature")
		}
		if f.Kind != "exists" && f.Kind != "count" && f.Field == "" {
			ps.BlockerFix(where+".field",
				fmt.Sprintf("%s needs the field it aggregates", f.Kind),
				"set field: <field>")
		}
		for _, fl := range append(append([]string{}, f.Filters...), f.Field) {
			if fl == "" {
				continue
			}
			if findField(s.Fields, fl) == nil {
				ps.Blockerf(where, "%q does not name a field of this entity", fl)
			}
		}
	}
}

// validateManualFact keeps the ELSE honest.
//
// A manual fact is a promise that a human will write the body, so the spec must
// carry the two things that promise needs: what the answer MEANS, and what its
// type is. Without the first the stub is an empty TODO; without the second the
// generator cannot even declare the method it is asking someone to implement.
func validateManualFact(f Fact, where string, ps *Problems) {
	if strings.TrimSpace(f.Description) == "" {
		ps.BlockerFix(where+".description",
			"a manual fact must say what it answers and where the answer comes from",
			"this text is what the generated stub and the report tell the implementer "+
				"to write; without it the stub is an empty TODO")
	}
	if f.Returns == "" {
		ps.BlockerFix(where+".returns",
			"a manual fact must declare its return type",
			"one of: "+FactReturns.String()+" — the generator declares the method on "+
				"the port and cannot infer the signature of a body it is not writing")
	} else if !FactReturns.Has(f.Returns) {
		ps.BlockerFix(where+".returns",
			fmt.Sprintf("%q is not a return type a fact may declare", f.Returns),
			"one of: "+FactReturns.String())
	}
	if f.Field != "" {
		ps.BlockerFix(where+".field",
			"a manual fact aggregates nothing — the body is hand-written",
			"drop it, or use one of the computed kinds")
	}
	if f.ActiveOnly {
		ps.BlockerFix(where+".activeOnly",
			"the archived scope describes a query this generator is not writing",
			"drop it — what the hand-written body considers is its own decision")
	}
}

func validateRead(s *Spec, ps *Problems) {
	r := s.Read
	display := contains(s.Modes, "display")
	reading := r.ByID || r.ByParams != nil
	if reading && !display {
		ps.BlockerFix("modes",
			"the entity serves reads but display is not among its modes",
			"add display to modes")
	}
	if !reading {
		if r.Backing != "" || r.View.Name != "" || len(r.Indexes) > 0 {
			ps.BlockerFix("read",
				"a read side is configured but no read operation is served",
				"set read.byId and/or read.byParams, or remove the read block")
		}
		return
	}

	if !ReadBackings.Has(r.Backing) {
		ps.BlockerFix("read.backing",
			fmt.Sprintf("%q is not a backing", r.Backing),
			"one of: "+ReadBackings.String())
	}
	if r.View.Name == "" {
		ps.Blockerf("read.view.name", "the view needs a name")
	}
	if r.View.Version < 1 {
		ps.BlockerFix("read.view.version",
			"the view version must start at 1",
			"the framework uses it to decide when a rebuild is due; 0 is not a version")
	}

	if r.Backing == "relational" {
		// A relational view has no Mongo collection, so these are not merely
		// useless — they would be silently discarded, which is worse.
		if len(r.Indexes) > 0 {
			ps.BlockerFix("read.indexes",
				"a relational view has no collection to index",
				"the indexes belong on the tables, in the migration; remove them here")
		}
		if r.View.DeleteOnArchive {
			ps.BlockerFix("read.view.deleteOnArchive",
				"deleteOnArchive is a Mongo projection option",
				"a relational view reads the tables directly; remove it")
		}
		if r.View.TTLSeconds > 0 {
			ps.BlockerFix("read.view.ttlSeconds",
				"a time-to-live is a Mongo collection option",
				"remove it, or set read.backing: mongo")
		}
	}

	for i, idx := range r.Indexes {
		where := fmt.Sprintf("read.indexes[%d]", i)
		if len(idx.Fields) == 0 {
			ps.Blockerf(where+".fields", "an index needs at least one field")
		}
		for _, fn := range idx.Fields {
			if !readableField(s, fn) {
				ps.Blockerf(where+".fields", "%q does not name a readable field", fn)
			}
		}
		if idx.Order != "" && !IndexOrders.Has(idx.Order) {
			ps.BlockerFix(where+".order",
				fmt.Sprintf("%q is not an order", idx.Order), "one of: "+IndexOrders.String())
		}
		if idx.Text && idx.Unique {
			ps.Blockerf(where, "a text index cannot be unique")
		}
	}

	if r.ByParams != nil {
		validateByParams(s, r, ps)
	}

	for i, fr := range r.FieldRestrict {
		where := fmt.Sprintf("read.fieldRestrict[%d]", i)
		if !readableField(s, fr.Field) {
			ps.Blockerf(where+".field", "%q does not name a readable field", fr.Field)
		}
		if fr.Permission == "" {
			ps.Blockerf(where+".permission", "a restricted field needs the permission that unlocks it")
		}
	}

	if r.IdentityView != "" {
		if !IdentityViews.Has(r.IdentityView) {
			ps.BlockerFix("read.identityView",
				fmt.Sprintf("%q is not an identity-view action", r.IdentityView),
				"one of: "+IdentityViews.String())
		}
		if s.Storage.Kind != "sharedbase-role" {
			ps.BlockerFix("read.identityView",
				"only a shared-base model has an identity view",
				"remove the key")
		}
	}
}

func validateByParams(s *Spec, r Read, ps *Problems) {
	bp := r.ByParams
	for i, f := range bp.Filters {
		where := fmt.Sprintf("read.byParams.filters[%d] (%s)", i, orUnnamed(f.Field))
		if !readableField(s, f.Field) {
			ps.Blockerf(where, "%q does not name a readable field", f.Field)
			continue
		}
		if len(f.Ops) == 0 {
			ps.Blockerf(where+".ops", "the filter declares no operator")
		}
		fld := findAnyField(s, f.Field)
		for _, op := range f.Ops {
			if !FilterOps.Has(op) {
				ps.BlockerFix(where+".ops",
					fmt.Sprintf("%q is not a filter operator", op),
					"one of: "+FilterOps.String())
				continue
			}
			if fld != nil {
				checkOpAgainstType(*fld, op, where, ps)
			}
		}
	}
	for _, sf := range bp.Sort {
		if !readableField(s, sf) {
			ps.Blockerf("read.byParams.sort", "%q does not name a readable field", sf)
		}
	}
	for _, sf := range bp.Controls.Search {
		if !readableField(s, sf) {
			ps.Blockerf("read.byParams.controls.search", "%q does not name a readable field", sf)
			continue
		}
		if f := findAnyField(s, sf); f != nil && f.Type != "string" {
			ps.BlockerFix("read.byParams.controls.search",
				fmt.Sprintf("%q is not text, so a search cannot match it", sf),
				"search covers string fields")
		}
	}
	if len(bp.Controls.Search) > 0 {
		if r.Backing == "relational" {
			ps.BlockerFix("read.byParams.controls.search",
				"a relational view has no text index to serve a search",
				"drop the search, or set read.backing: mongo")
		} else if !hasTextIndex(r.Indexes) {
			ps.BlockerFix("read.indexes",
				"a search is served but no text index backs it",
				"declare an index with text: true over the searched fields")
		}
	}
	if bp.Controls.IncludeArchived && s.Storage.Managed.ArchivedAt == "" {
		ps.BlockerFix("read.byParams.controls.includeArchived",
			"the listing offers to include archived rows but nothing is ever archived",
			"remove the control, or declare storage.managed.archivedAt")
	}
}

// checkOpAgainstType keeps an operator from being offered where it cannot mean
// anything — ordering a boolean, or prefix-matching a number.
func checkOpAgainstType(f Field, op, where string, ps *Problems) {
	textOnly := map[string]bool{
		"startswith": true, "contains": true, "istartswith": true,
		"icontains": true, "ieq": true, "ine": true, "iin": true, "inin": true,
	}
	ordered := map[string]bool{"gt": true, "gte": true, "lt": true, "lte": true}
	if textOnly[op] && f.Type != "string" {
		ps.BlockerFix(where+".ops",
			fmt.Sprintf("%q only applies to text, and %s is %s", op, f.Name, f.Type),
			"use eq/ne/in/nin for this type")
	}
	if ordered[op] && (f.Type == "bool" || f.Type == "id") {
		ps.BlockerFix(where+".ops",
			fmt.Sprintf("%q needs an ordered type, and %s is %s", op, f.Name, f.Type),
			"use eq/ne for this type")
	}
}

func validateSurfaces(s *Spec, ps *Problems) {
	su := s.Surfaces
	gqlOn := su.GraphQL != nil && su.GraphQL.Enabled
	if !su.REST && !gqlOn {
		ps.BlockerFix("surfaces",
			"the entity exposes no surface",
			"set surfaces.rest: true and/or surfaces.graphql.enabled: true")
	}
	if gqlOn {
		for _, m := range su.GraphQL.Mutations {
			if !Modes.Has(m) || m == "display" {
				ps.BlockerFix("surfaces.graphql.mutations",
					fmt.Sprintf("%q is not a mutable verb", m),
					"one of: insert | update | delete | archive | unarchive")
				continue
			}
			// A mutation for a verb the entity does not accept would reference
			// a command this run never emits.
			if !contains(s.Modes, m) {
				ps.BlockerFix("surfaces.graphql.mutations",
					fmt.Sprintf("a %s mutation is declared but %s is not among the modes", m, m),
					"add it to modes, or drop the mutation")
			}
		}
		if su.GraphQL.Connection && !contains(s.Modes, "display") {
			ps.BlockerFix("surfaces.graphql.connection",
				"a connection is a paged read but the entity has no display mode",
				"add display to modes")
		}
	}
	if su.Exports != nil {
		if s.Read.ByParams == nil {
			ps.BlockerFix("surfaces.exports",
				"an export writes out a listing, and no listing is served",
				"declare read.byParams, or remove the exports")
		}
		if su.Exports.CSV == nil && su.Exports.XLSX == nil {
			ps.BlockerFix("surfaces.exports",
				"the exports block names no format",
				"declare csv, xlsx, or both")
		}
		if su.Exports.CSV != nil {
			d := su.Exports.CSV.Delimiter
			if len([]rune(d)) != 1 {
				ps.BlockerFix("surfaces.exports.csv.delimiter",
					"the delimiter must be exactly one character",
					"e.g. \",\" or \";\"")
			}
		}
		if su.Exports.XLSX != nil && su.Exports.XLSX.Sheet == "" {
			ps.BlockerFix("surfaces.exports.xlsx.sheet",
				"the spreadsheet needs a sheet name", "e.g. Students")
		}
	}
}

func validateAuthz(s *Spec, ps *Problems) {
	a := s.Authz
	if a.Resource == "" {
		ps.BlockerFix("authz.resource",
			"the permission resource is required",
			"usually the entity name in lower case, e.g. student")
	}
	if a.DataAccess == "" {
		ps.BlockerFix("authz.dataAccess",
			"the spec does not say who may read or modify which rows",
			"anyone-with-permission is a valid answer, but it has to be stated")
	} else if !DataAccess.Has(a.DataAccess) {
		ps.BlockerFix("authz.dataAccess",
			fmt.Sprintf("%q is not a data-access model", a.DataAccess),
			"one of: "+DataAccess.String())
	}
	switch a.DataAccess {
	case "owner-only":
		if a.OwnerField == "" {
			ps.BlockerFix("authz.ownerField",
				"owner-only access needs the field that identifies the owner",
				"name a runtime-only field fed from the caller's identity")
		} else if f := findField(s.Fields, a.OwnerField); f == nil {
			ps.Blockerf("authz.ownerField", "%q does not name a field of this entity", a.OwnerField)
		}
	case "tenant":
		if a.TenantField == "" {
			ps.BlockerFix("authz.tenantField",
				"tenant access needs the field carrying the tenant",
				"name the field the tenant claim is matched against")
		} else if f := findField(s.Fields, a.TenantField); f == nil {
			ps.Blockerf("authz.tenantField", "%q does not name a field of this entity", a.TenantField)
		}
	}

	// Permissions are cross-checked BOTH ways: a permission for an operation
	// that is not mounted is dead configuration, and a mounted operation with no
	// permission is a boot panic under an enabled authorization layer.
	ops := mountedOperations(s)
	for key := range a.Permissions {
		if !AuthzOperations.Has(key) {
			ps.BlockerFix("authz.permissions",
				fmt.Sprintf("%q is not an operation", key),
				"one of: "+AuthzOperations.String())
			continue
		}
		if !ops[key] {
			ps.BlockerFix("authz.permissions",
				fmt.Sprintf("a permission is declared for %q but that operation is not served", key),
				"add the mode/surface that mounts it, or remove the permission")
		}
		if strings.TrimSpace(a.Permissions[key]) == "" {
			ps.BlockerFix("authz.permissions."+key,
				"the permission is empty",
				"an empty string registers a route that no permission can satisfy")
		}
	}
	for op := range ops {
		if _, ok := a.Permissions[op]; !ok {
			ps.BlockerFix("authz.permissions",
				fmt.Sprintf("the %s operation is served but has no permission", op),
				fmt.Sprintf("add %s: %s:<action> — with authorization enabled, "+
					"a route without one aborts boot", op, a.Resource))
		}
	}
}

// mountedOperations is the single source of truth for "what this spec actually
// serves". Both the authz cross-check and the emitters read it, so a permission
// can never disagree with a route.
func mountedOperations(s *Spec) map[string]bool {
	ops := map[string]bool{}
	for _, m := range s.Modes {
		switch m {
		case "insert":
			ops["insert"] = true
		case "update":
			switch s.Update.Shape {
			case "put":
				ops["update"] = true
			case "patch":
				ops["patch"] = true
			case "both":
				ops["update"] = true
				ops["patch"] = true
			}
		case "delete":
			ops["delete"] = true
		case "archive":
			ops["archive"] = true
		case "unarchive":
			ops["unarchive"] = true
		}
	}
	if s.Read.ByID || s.Read.ByParams != nil {
		ops["read"] = true
	}
	return ops
}

// ---------------------------------------------------------------- helpers

func findField(fs []Field, name string) *Field {
	for i := range fs {
		if fs[i].Name == name {
			return &fs[i]
		}
	}
	return nil
}

func findChild(cs []Child, name string) *Child {
	for i := range cs {
		if cs[i].Name == name {
			return &cs[i]
		}
	}
	return nil
}

func findSibling(ss []Sibling, name string) *Sibling {
	for i := range ss {
		if ss[i].Name == name {
			return &ss[i]
		}
	}
	return nil
}

// findAnyField looks through the root and its siblings — everything that lands
// on the aggregate's own struct.
func findAnyField(s *Spec, name string) *Field {
	if f := findField(s.Fields, name); f != nil {
		return f
	}
	for i := range s.Siblings {
		if f := findField(s.Siblings[i].Fields, name); f != nil {
			return f
		}
	}
	return nil
}

// readableField also accepts a child field addressed as Child.Field, which is
// how the read side names it inside the projected document.
func readableField(s *Spec, name string) bool {
	if findAnyField(s, name) != nil {
		return true
	}
	if i := strings.Index(name, "."); i > 0 {
		if c := findChild(s.Children, name[:i]); c != nil {
			return findField(c.Fields, name[i+1:]) != nil
		}
	}
	return false
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func hasTextIndex(idx []Index) bool {
	for _, i := range idx {
		if i.Text {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func orUnnamed(s string) string {
	if s == "" {
		return "unnamed"
	}
	return s
}
