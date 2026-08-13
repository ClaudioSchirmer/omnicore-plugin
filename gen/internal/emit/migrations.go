package emit

import (
	"fmt"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// One migration PAIR per entity per dialect: every table the entity owns in a
// single up file in dependency order, and a down file that drops in reverse.
//
// The down file is not optional — the framework aborts the boot when an up has
// no twin, even a no-op one. And it must be idempotent: an up that failed
// halfway leaves some objects created and others not, so the down has to
// tolerate what is not there.
func emitMigrations(m *ir.Model) ([]fsplan.File, error) {
	var out []fsplan.File
	for _, dialect := range m.Dialects {
		d, ok := dialects[dialect]
		if !ok {
			return nil, fmt.Errorf("no column mapping for dialect %q", dialect)
		}
		ordinal := m.Ordinal[dialect]
		base := fmt.Sprintf("migrations/%s/%04d_%s", dialect, ordinal, m.Entity.Snake)

		out = append(out,
			fsplan.File{
				Path:      base + ".up.sql",
				Class:     fsplan.Owned,
				Content:   []byte(upSQL(m, d)),
				Describes: fmt.Sprintf("the %s table on %s", m.Table, dialect),
			},
			fsplan.File{
				Path:      base + ".down.sql",
				Class:     fsplan.Owned,
				Content:   []byte(downSQL(m, d)),
				Describes: fmt.Sprintf("the rollback of %s on %s", m.Table, dialect),
			},
		)
	}
	return out, nil
}

// dialect carries everything that differs per engine. Keeping it as data rather
// than as branches in the emitter is what makes adding an engine a table entry.
type dialect struct {
	Name string
	// ID is the native identifier column type. It is deliberately NOT a
	// 36-character text column: the framework's ids are time-ordered, and a
	// textual or GUID-sorted column throws that locality away.
	ID        string
	Timestamp string
	// DefaultBeforeNotNull is true where the grammar demands DEFAULT before
	// NOT NULL. Getting this backwards produces DDL the engine rejects outright.
	DefaultBeforeNotNull bool
	NamedDefaults        bool
	IfExists             bool
	Quote                func(string) string
	Column               func(f ir.Field) string
	Comment              string
}

var dialects = map[string]dialect{
	"postgres": {
		Name: "postgres", ID: "UUID", Timestamp: "TIMESTAMPTZ",
		IfExists: true, Quote: ansiQuote, Column: postgresColumn, Comment: "--",
	},
	"mysql": {
		Name: "mysql", ID: "BINARY(16)", Timestamp: "DATETIME(6)",
		IfExists: true, Quote: backtickQuote, Column: mysqlColumn, Comment: "--",
	},
	"sqlserver": {
		Name: "sqlserver", ID: "BINARY(16)", Timestamp: "DATETIMEOFFSET",
		NamedDefaults: true, IfExists: true, Quote: bracketQuote,
		Column: sqlserverColumn, Comment: "--",
	},
	"oracle": {
		Name: "oracle", ID: "RAW(16)", Timestamp: "TIMESTAMP WITH TIME ZONE",
		DefaultBeforeNotNull: true, IfExists: true, Quote: ansiQuote,
		Column: oracleColumn, Comment: "--",
	},
	"sqlite": {
		Name: "sqlite", ID: "TEXT", Timestamp: "TEXT",
		IfExists: true, Quote: ansiQuote, Column: sqliteColumn, Comment: "--",
	},
}

func ansiQuote(s string) string     { return `"` + s + `"` }
func backtickQuote(s string) string { return "`" + s + "`" }
func bracketQuote(s string) string  { return "[" + s + "]" }

func postgresColumn(f ir.Field) string {
	switch f.SpecType {
	case "string":
		return fmt.Sprintf("VARCHAR(%d)", f.Length)
	case "int":
		return "INTEGER"
	case "int64":
		return "BIGINT"
	case "float64":
		return "DOUBLE PRECISION"
	case "bool":
		return "BOOLEAN"
	case "time":
		return "TIMESTAMPTZ"
	case "id":
		return "UUID"
	}
	return "TEXT"
}

func mysqlColumn(f ir.Field) string {
	switch f.SpecType {
	case "string":
		return fmt.Sprintf("VARCHAR(%d)", f.Length)
	case "int":
		return "INT"
	case "int64":
		return "BIGINT"
	case "float64":
		return "DOUBLE"
	case "bool":
		return "TINYINT(1)"
	case "time":
		return "DATETIME(6)"
	case "id":
		return "BINARY(16)"
	}
	return "TEXT"
}

func sqlserverColumn(f ir.Field) string {
	switch f.SpecType {
	case "string":
		return fmt.Sprintf("NVARCHAR(%d)", f.Length)
	case "int":
		return "INT"
	case "int64":
		return "BIGINT"
	case "float64":
		return "FLOAT"
	case "bool":
		return "BIT"
	case "time":
		return "DATETIMEOFFSET"
	case "id":
		return "BINARY(16)"
	}
	return "NVARCHAR(MAX)"
}

func oracleColumn(f ir.Field) string {
	switch f.SpecType {
	case "string":
		return fmt.Sprintf("VARCHAR2(%d CHAR)", f.Length)
	case "int":
		return "NUMBER(10)"
	case "int64":
		return "NUMBER(19)"
	case "float64":
		return "BINARY_DOUBLE"
	case "bool":
		return "NUMBER(1)"
	case "time":
		return "TIMESTAMP WITH TIME ZONE"
	case "id":
		return "RAW(16)"
	}
	return "CLOB"
}

func sqliteColumn(f ir.Field) string {
	switch f.SpecType {
	case "string":
		return "TEXT"
	case "int", "int64", "bool":
		return "INTEGER"
	case "float64":
		return "REAL"
	case "time", "id":
		return "TEXT"
	}
	return "TEXT"
}

func upSQL(m *ir.Model, d dialect) string {
	var b strings.Builder
	fmt.Fprintf(&b, "-- %s — %s\n", m.Table, firstLine(m.TableDescription))
	fmt.Fprintf(&b, "-- Generated by omnicore-gen from the %s spec.\n", m.Entity.Pascal)
	b.WriteString("\n")
	writeSessionPrelude(&b, d)

	// The identity comes first: the role's foreign key points at it.
	if m.IsRole() && !m.Base.Reuse {
		writeBaseTable(&b, m, d)
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "CREATE TABLE %s (\n", d.Quote(m.Table))

	// Column comments go on their OWN line, ABOVE the column. Trailing them
	// after the definition would swallow the separating comma into the comment
	// and produce DDL that does not parse.
	var lines []string
	var comments []string
	add := func(comment, def string) {
		comments = append(comments, comment)
		lines = append(lines, def)
	}

	add("", fmt.Sprintf("  %s %s NOT NULL", d.Quote("id"), d.ID))

	if m.IsRole() && m.Base.Link == "separate-fk" {
		add("  "+d.Comment+" link to the shared identity",
			fmt.Sprintf("  %s %s NOT NULL", d.Quote(baseLinkColumn(m)), d.ID))
	}
	for _, f := range roleColumns(m) {
		null := " NOT NULL"
		if f.Nullable {
			null = " NULL"
		}
		comment := ""
		if f.Description != "" {
			comment = "  " + d.Comment + " " + firstLine(f.Description)
		}
		add(comment, fmt.Sprintf("  %s %s%s", d.Quote(f.Column), d.Column(f), null))
	}

	// Revision is mandatory on an entity table: the framework uses it for
	// optimistic concurrency and refuses to build the repository without it.
	add("  "+d.Comment+" optimistic-concurrency stamp, maintained by the framework",
		fmt.Sprintf("  %s %s", d.Quote(m.Managed.Revision), stampTail(d, bigintOf(d), "0", false)))

	if m.Managed.CreatedAt != "" {
		add("", fmt.Sprintf("  %s %s", d.Quote(m.Managed.CreatedAt),
			stampTail(d, d.Timestamp, nowOf(d), false)))
	}
	if m.Managed.UpdatedAt != "" {
		add("", fmt.Sprintf("  %s %s", d.Quote(m.Managed.UpdatedAt),
			stampTail(d, d.Timestamp, nowOf(d), false)))
	}
	if m.Managed.ArchivedAt != "" {
		add("  "+d.Comment+" archive stamp; a non-null value hides the row from reads",
			fmt.Sprintf("  %s %s NULL", d.Quote(m.Managed.ArchivedAt), d.Timestamp))
	}

	add("", fmt.Sprintf("  CONSTRAINT %s PRIMARY KEY (%s)",
		d.Quote(m.Table+"_pkey"), d.Quote("id")))
	if m.IsRole() {
		// RESTRICT, never CASCADE: an identity still played by a role must not
		// be removable, and the framework converges the identity itself when its
		// last role goes.
		add("  "+d.Comment+" a referencing role vetoes removing the identity",
			fmt.Sprintf("  CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)%s",
				d.Quote(m.Table+"_identity_fk"), d.Quote(baseLinkColumn(m)),
				d.Quote(m.Base.Table), d.Quote("id"), restrictClause(d)))
	}

	for i, line := range lines {
		if comments[i] != "" {
			b.WriteString(strings.TrimRight(comments[i], " ") + "\n")
		}
		b.WriteString(line)
		if i < len(lines)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(");\n")

	// Siblings first, then children: both point back at the root, so the root's
	// table has to exist before either.
	for _, sib := range m.Siblings {
		b.WriteString("\n")
		writeSiblingTable(&b, m, sib, d)
	}
	for _, c := range m.Children {
		b.WriteString("\n")
		writeChildTable(&b, m, c, d)
	}

	if m.IsRole() && m.Base.Link == "separate-fk" {
		b.WriteString("\n")
		writeRoleUniqueness(&b, m, d)
	}

	for _, c := range m.Constraints {
		if c.Kind != "unique" {
			continue
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "%s the repository binds this constraint's violation to a clean 409.\n", d.Comment)
		fmt.Fprintf(&b, "CREATE UNIQUE INDEX %s ON %s (%s);\n",
			d.Quote(uniqueName(c)), d.Quote(c.Table), d.Quote(c.Columns[0]))
	}
	return b.String()
}

// stampTail orders DEFAULT and NOT NULL per the engine's grammar.
func stampTail(d dialect, typ, def string, nullable bool) string {
	null := "NOT NULL"
	if nullable {
		null = "NULL"
	}
	if d.DefaultBeforeNotNull {
		return fmt.Sprintf("%s DEFAULT %s %s", typ, def, null)
	}
	return fmt.Sprintf("%s %s DEFAULT %s", typ, null, def)
}

func bigintOf(d dialect) string {
	switch d.Name {
	case "oracle":
		return "NUMBER(19)"
	case "sqlite":
		return "INTEGER"
	default:
		return "BIGINT"
	}
}

func nowOf(d dialect) string {
	switch d.Name {
	case "mysql":
		return "CURRENT_TIMESTAMP(6)"
	case "sqlserver":
		return "SYSDATETIMEOFFSET()"
	case "oracle":
		return "SYSTIMESTAMP"
	case "sqlite":
		return "(strftime('%Y-%m-%dT%H:%M:%fZ','now'))"
	default:
		return "NOW()"
	}
}

func downSQL(m *ir.Model, d dialect) string {
	var b strings.Builder
	fmt.Fprintf(&b, "-- Rollback of %s.\n", m.Table)
	b.WriteString("-- Idempotent on purpose: an up that failed halfway leaves some objects\n")
	b.WriteString("-- created and others not, so the down must tolerate what is absent.\n\n")

	// The indexes are NOT dropped separately: dropping the table takes them with
	// it on every engine, and the standalone DROP INDEX spelling differs enough
	// between engines (MySQL has no IF EXISTS for it) to be a liability for no gain.
	// Reverse order: a table pointed at cannot go before the tables pointing at it.
	for _, c := range m.Children {
		fmt.Fprintf(&b, "DROP TABLE IF EXISTS %s;\n", d.Quote(c.Table))
	}
	for _, sib := range m.Siblings {
		fmt.Fprintf(&b, "DROP TABLE IF EXISTS %s;\n", d.Quote(sib.Table))
	}
	fmt.Fprintf(&b, "DROP TABLE IF EXISTS %s;\n", d.Quote(m.Table))
	if m.IsRole() && !m.Base.Reuse {
		fmt.Fprintf(&b, "DROP TABLE IF EXISTS %s;\n", d.Quote(m.Base.Table))
	}
	return b.String()
}

// writeSiblingTable emits a 1:1 facet.
//
// Its primary key IS the owner's, which is what makes the relationship one to
// one structurally rather than by convention. It carries no lifecycle columns:
// the owner owns the lifecycle, and a second archive stamp here could disagree
// with it.
func writeSiblingTable(b *strings.Builder, m *ir.Model, sib ir.Sibling, d dialect) {
	fmt.Fprintf(b, "%s %s — the %s facet of %s (1:1, key shared with the owner).\n",
		d.Comment, sib.Table, sib.Name, m.Table)
	fmt.Fprintf(b, "CREATE TABLE %s (\n", d.Quote(sib.Table))
	fmt.Fprintf(b, "  %s %s NOT NULL,\n", d.Quote("id"), d.ID)
	for _, f := range sib.Fields {
		if f.Description != "" {
			fmt.Fprintf(b, "  %s %s\n", d.Comment, firstLine(f.Description))
		}
		fmt.Fprintf(b, "  %s %s NULL,\n", d.Quote(f.Column), d.Column(f))
	}
	fmt.Fprintf(b, "  CONSTRAINT %s PRIMARY KEY (%s),\n",
		d.Quote(sib.Table+"_pkey"), d.Quote("id"))
	fmt.Fprintf(b, "  CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)%s\n",
		d.Quote(sib.Table+"_owner_fk"), d.Quote("id"), d.Quote(m.Table), d.Quote("id"),
		cascadeClause(d))
	b.WriteString(");\n")
}

// writeChildTable emits a 1:N collection.
//
// The foreign key is indexed because every read of the aggregate loads the
// children by it; without the index that is a full scan per parent.
func writeChildTable(b *strings.Builder, m *ir.Model, c ir.Child, d dialect) {
	fk := parentColumn(m)
	fmt.Fprintf(b, "%s %s — the %s collection of %s.\n", d.Comment, c.Table, c.Segment, m.Table)
	if c.Description != "" {
		fmt.Fprintf(b, "%s %s\n", d.Comment, firstLine(c.Description))
	}
	fmt.Fprintf(b, "CREATE TABLE %s (\n", d.Quote(c.Table))
	fmt.Fprintf(b, "  %s %s NOT NULL,\n", d.Quote("id"), d.ID)
	fmt.Fprintf(b, "  %s %s NOT NULL,\n", d.Quote(fk), d.ID)
	for _, f := range c.Fields {
		null := " NOT NULL"
		if f.Nullable {
			null = " NULL"
		}
		if f.Description != "" {
			fmt.Fprintf(b, "  %s %s\n", d.Comment, firstLine(f.Description))
		}
		fmt.Fprintf(b, "  %s %s%s,\n", d.Quote(f.Column), d.Column(f), null)
	}
	if c.ArchivedAt != "" {
		fmt.Fprintf(b, "  %s %s NULL,\n", d.Quote(c.ArchivedAt), d.Timestamp)
	}
	if m.Managed.CreatedAt != "" {
		fmt.Fprintf(b, "  %s %s,\n", d.Quote(m.Managed.CreatedAt),
			stampTail(d, d.Timestamp, nowOf(d), false))
	}
	if m.Managed.UpdatedAt != "" {
		fmt.Fprintf(b, "  %s %s,\n", d.Quote(m.Managed.UpdatedAt),
			stampTail(d, d.Timestamp, nowOf(d), false))
	}
	fmt.Fprintf(b, "  CONSTRAINT %s PRIMARY KEY (%s),\n",
		d.Quote(c.Table+"_pkey"), d.Quote("id"))
	fmt.Fprintf(b, "  CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)%s\n",
		d.Quote(c.Table+"_parent_fk"), d.Quote(fk), d.Quote(m.Table), d.Quote("id"),
		cascadeClause(d))
	b.WriteString(");\n")
	fmt.Fprintf(b, "%s every read of the aggregate loads this collection by the key below.\n", d.Comment)
	fmt.Fprintf(b, "CREATE INDEX %s ON %s (%s);\n",
		d.Quote(c.Table+"_parent_idx"), d.Quote(c.Table), d.Quote(fk))
}

// cascadeClause deletes the dependent rows with their owner. Oracle accepts
// ON DELETE CASCADE but not the RESTRICT spelling, so only the cascade is used.
func cascadeClause(d dialect) string {
	return " ON DELETE CASCADE"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// writeBaseTable emits the shared identity.
//
// Its natural key is UNIQUE and NOT NULL, and that is not a nicety: the key
// derives the identity's primary key, so a null one would collapse every
// key-less record into a single identity — silent corruption rather than an error.
func writeBaseTable(b *strings.Builder, m *ir.Model, d dialect) {
	fmt.Fprintf(b, "%s %s — the shared identity %s plays a role over.\n",
		d.Comment, m.Base.Table, m.Table)
	if m.Base.Description != "" {
		fmt.Fprintf(b, "%s %s\n", d.Comment, firstLine(m.Base.Description))
	}
	fmt.Fprintf(b, "CREATE TABLE %s (\n", d.Quote(m.Base.Table))
	fmt.Fprintf(b, "  %s %s NOT NULL,\n", d.Quote("id"), d.ID)
	for _, f := range m.BaseFields() {
		null := " NOT NULL"
		if f.Nullable {
			null = " NULL"
		}
		if f.Description != "" {
			fmt.Fprintf(b, "  %s %s\n", d.Comment, firstLine(f.Description))
		}
		fmt.Fprintf(b, "  %s %s%s,\n", d.Quote(f.Column), d.Column(f), null)
	}
	fmt.Fprintf(b, "  %s %s,\n", d.Quote(m.Managed.Revision),
		stampTail(d, bigintOf(d), "0", false))
	if m.Managed.CreatedAt != "" {
		fmt.Fprintf(b, "  %s %s,\n", d.Quote(m.Managed.CreatedAt),
			stampTail(d, d.Timestamp, nowOf(d), false))
	}
	if m.Managed.UpdatedAt != "" {
		fmt.Fprintf(b, "  %s %s,\n", d.Quote(m.Managed.UpdatedAt),
			stampTail(d, d.Timestamp, nowOf(d), false))
	}
	if m.Managed.ArchivedAt != "" {
		fmt.Fprintf(b, "  %s %s NULL,\n", d.Quote(m.Managed.ArchivedAt), d.Timestamp)
	}
	fmt.Fprintf(b, "  CONSTRAINT %s PRIMARY KEY (%s)\n",
		d.Quote(m.Base.Table+"_pkey"), d.Quote("id"))
	b.WriteString(");\n")
	fmt.Fprintf(b, "%s the natural key: UNIQUE and NOT NULL, because the identity's own\n", d.Comment)
	fmt.Fprintf(b, "%s key is derived from it. A null here would merge unrelated records.\n", d.Comment)
	fmt.Fprintf(b, "CREATE UNIQUE INDEX %s ON %s (%s);\n",
		d.Quote(m.Base.Table+"_"+naturalColumn(m)+"_key"), d.Quote(m.Base.Table),
		d.Quote(naturalColumn(m)))
}

// restrictClause vetoes removing a referenced row. SQL Server spells it NO
// ACTION, and Oracle has no RESTRICT at all — omitting the clause there gives
// the same behaviour, which is its default.
func restrictClause(d dialect) string {
	switch d.Name {
	case "sqlserver":
		return " ON DELETE NO ACTION"
	case "oracle":
		return ""
	default:
		return " ON DELETE RESTRICT"
	}
}

// writeRoleUniqueness enforces how many role rows one identity may hold.
//
// Two shapes, and the difference is not cosmetic:
//
//   - a plain UNIQUE means one row per identity, ever. An archived remnant
//     keeps blocking a new one, so the only way back is to unarchive it.
//   - active-only means the uniqueness ignores archived rows, so an identity can
//     hold the role, lose it, and hold it again — the remnants stay as history.
//
// Every engine expresses the second differently, and one of them cannot express
// it as an index at all.
func writeRoleUniqueness(b *strings.Builder, m *ir.Model, d dialect) {
	col := baseLinkColumn(m)
	name := m.Table + "_identity_key"

	if m.Base.RowUniqueness != "active-only" {
		fmt.Fprintf(b, "%s one role row per identity, ever: an archived remnant keeps\n", d.Comment)
		fmt.Fprintf(b, "%s blocking a new one, and unarchiving is the way back.\n", d.Comment)
		fmt.Fprintf(b, "CREATE UNIQUE INDEX %s ON %s (%s);\n",
			d.Quote(name), d.Quote(m.Table), d.Quote(col))
		return
	}

	fmt.Fprintf(b, "%s one ACTIVE role row per identity. Archived rows are excluded, so the\n", d.Comment)
	fmt.Fprintf(b, "%s identity can hold this role again later while the old rows stay as history.\n", d.Comment)

	archived := m.Managed.ArchivedAt
	switch d.Name {
	case "postgres", "sqlite":
		fmt.Fprintf(b, "CREATE UNIQUE INDEX %s ON %s (%s) WHERE %s IS NULL;\n",
			d.Quote(name), d.Quote(m.Table), d.Quote(col), d.Quote(archived))
	case "sqlserver":
		fmt.Fprintf(b, "CREATE UNIQUE INDEX %s ON %s (%s) WHERE %s IS NULL;\n",
			d.Quote(name), d.Quote(m.Table), d.Quote(col), d.Quote(archived))
	case "oracle":
		// Oracle has no partial index, but it does ignore all-NULL entries: a
		// function-based index that yields NULL for an archived row leaves those
		// rows out of the constraint entirely.
		fmt.Fprintf(b, "%s Oracle has no partial index; an all-NULL entry is simply not indexed,\n", d.Comment)
		fmt.Fprintf(b, "%s so the expression yields NULL for archived rows and they drop out.\n", d.Comment)
		fmt.Fprintf(b, "CREATE UNIQUE INDEX %s ON %s (CASE WHEN %s IS NULL THEN %s END);\n",
			d.Quote(name), d.Quote(m.Table), d.Quote(archived), d.Quote(col))
	case "mysql":
		// MySQL has neither partial nor function-based indexes here, so the
		// condition is materialised as a generated column: it equals the link
		// while the row is active and NULL once archived, and MySQL does not
		// enforce uniqueness across NULLs.
		fmt.Fprintf(b, "%s MySQL has no partial index, so the condition becomes a column: it\n", d.Comment)
		fmt.Fprintf(b, "%s mirrors the link while active and turns NULL once archived, and NULLs\n", d.Comment)
		fmt.Fprintf(b, "%s do not collide with each other.\n", d.Comment)
		fmt.Fprintf(b, "ALTER TABLE %s ADD COLUMN %s %s GENERATED ALWAYS AS (CASE WHEN %s IS NULL THEN %s END) STORED;\n",
			d.Quote(m.Table), d.Quote("active_"+col), d.ID, d.Quote(archived), d.Quote(col))
		fmt.Fprintf(b, "CREATE UNIQUE INDEX %s ON %s (%s);\n",
			d.Quote(name), d.Quote(m.Table), d.Quote("active_"+col))
	}
}

// writeSessionPrelude makes the file independent of whatever session settings
// the client happens to have.
//
// SQL Server refuses to create a filtered index unless QUOTED_IDENTIFIER is on,
// and clients differ on the default. Stating it here means the migration
// applies the same way from every tool instead of only from the one it was
// tested with.
func writeSessionPrelude(b *strings.Builder, d dialect) {
	if d.Name != "sqlserver" {
		return
	}
	b.WriteString("-- Required for filtered indexes; clients differ on the default, so the\n")
	b.WriteString("-- file states it rather than depending on the caller's session.\n")
	b.WriteString("SET QUOTED_IDENTIFIER ON;\n\n")
}
