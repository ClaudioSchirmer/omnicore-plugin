package emit

import (
	"fmt"
	"os"
	"path/filepath"
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
//
// # Why a migration is a HOOK and not an owned file
//
// This is the one output the generator writes ONCE and then never touches
// again, and the `_manual` in its name says so at a glance.
//
// The reason is that a migration is the only artefact here whose effect
// outlives the file. Every other generated file is a claim about the code, and
// rewriting it is free — the compiler checks the result. A migration is a claim
// about a DATABASE the generator cannot see. Once it has run anywhere, the
// framework's tracking table records it as applied, and rewriting the file
// changes nothing in that database while making it say something else: a
// service that boots green and fails on the first query touching the change.
//
// So the generator does not diff schemas, does not write an ALTER, and does not
// decide whether the original ran. It creates once, hands the file over, and
// gets out of the way. A later change to the shape is a NEW numbered pair,
// written by whoever knows where the first one has been.
func emitMigrations(m *ir.Model, root string) ([]fsplan.File, error) {
	var out []fsplan.File
	for _, dialect := range m.Dialects {
		d, ok := dialects[dialect]
		if !ok {
			return nil, fmt.Errorf("no column mapping for dialect %q", dialect)
		}
		base := migrationBase(m, dialect, root)

		out = append(out,
			fsplan.File{
				Path:        base + ".up.sql",
				Class:       fsplan.Hook,
				Content:     []byte(upSQL(m, d)),
				Describes:   fmt.Sprintf("the %s table on %s", m.Table, dialect),
				Consequence: migrationConsequence,
			},
			fsplan.File{
				Path:        base + ".down.sql",
				Class:       fsplan.Hook,
				Content:     []byte(downSQL(m, d)),
				Describes:   fmt.Sprintf("the rollback of %s on %s", m.Table, dialect),
				Consequence: migrationConsequence,
			},
		)
	}
	return out, nil
}

// migrationConsequence is the header a reader meets INSIDE the file, so the
// rule survives being found without the report.
const migrationConsequence = "This pair was created once and is now yours. The generator " +
	"will not rewrite it, and you should not either once it has run anywhere: the " +
	"framework records an applied migration in its tracking table, so editing the file " +
	"changes the file and not the database. To change the shape, add a NEW numbered pair " +
	"in this folder — and remember that adding a NOT NULL column to a table that already " +
	"has rows needs a default, and that a rename done as drop-then-add takes the data with it."

// migrationBase resolves the path stem, preferring a pair that is ALREADY on
// disk over the name this build would choose.
//
// It exists because the name gained its `_manual` suffix after projects had
// been generated without it. Ignoring that would not be a cosmetic mismatch:
// the older pair would go unrecognised, a second pair creating the SAME tables
// would be written beside it, and the service would run both.
func migrationBase(m *ir.Model, dialect, root string) string {
	ordinal := m.Ordinal[dialect]
	stem := fmt.Sprintf("migrations/%s/%04d_%s", dialect, ordinal, m.Entity.Snake)
	preferred := stem + "_manual"
	if exists(root, preferred+".up.sql") {
		return preferred
	}
	if exists(root, stem+".up.sql") {
		return stem
	}
	return preferred
}

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
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
	// Comment is the line-comment marker. It carries what is true about the
	// FILE — never a description of a table or a column that the engine could
	// have stored itself.
	Comment string
	// InlineColumnComment renders the clause a COLUMN DEFINITION carries where
	// the engine keeps the description inline. MySQL only; nil elsewhere.
	InlineColumnComment func(text string) string
	// TableComment and ColumnComment render the statement that puts a
	// description INSIDE the database, run right after the CREATE TABLE. nil
	// where the engine takes it inline instead (MySQL columns) or cannot store
	// one at all (SQLite).
	TableComment  func(table, text string) string
	ColumnComment func(table, column, text string) string
}

// descriptions decides WHERE a description written in the spec ends up, and
// that decision is the whole reason for asking the author to write one.
//
// A description that lives only as a `--` line in a migration file is invisible
// to everyone holding a CONNECTION rather than the repository: the DBA reading
// the catalogue, the BI tool listing columns, the next developer running \d+ or
// opening the table in a client. So every engine that can store one is given
// it — postgres and oracle with COMMENT ON after the table, mysql inline on the
// column plus an ALTER for the table, sqlserver with an MS_Description extended
// property. SQLite has nowhere to put it, and there, and only there, the text
// stays in the file.
type descriptions struct {
	d       dialect
	pending []string
}

// table returns the line to write ABOVE the CREATE TABLE — empty whenever the
// description is going into the database instead.
func (x *descriptions) table(table, text string) string {
	text = clampDescription(firstLine(text))
	if text == "" {
		return ""
	}
	if x.d.TableComment != nil {
		x.pending = append(x.pending, x.d.TableComment(table, text))
		return ""
	}
	return x.d.Comment + " " + text + "\n"
}

// column returns the clause to append to the column DEFINITION and the line to
// write above it. At most one of the two is ever non-empty.
func (x *descriptions) column(table, column, text string) (inline, above string) {
	text = clampDescription(firstLine(text))
	if text == "" {
		return "", ""
	}
	if x.d.InlineColumnComment != nil {
		return x.d.InlineColumnComment(text), ""
	}
	if x.d.ColumnComment != nil {
		x.pending = append(x.pending, x.d.ColumnComment(table, column, text))
		return "", ""
	}
	return "", "  " + x.d.Comment + " " + text
}

// flush writes the statements collected for the table just emitted. It is
// called after each CREATE TABLE rather than once at the end so the reader
// meets a table's descriptions beside it.
func (x *descriptions) flush(b *strings.Builder) {
	if len(x.pending) == 0 {
		return
	}
	for _, s := range x.pending {
		b.WriteString(s + "\n")
	}
	x.pending = nil
}

// clampDescription keeps a description inside the tightest engine limit — MySQL
// stops at 1024 characters on a column. A description longer than that is prose
// that belongs in the docs, and a DDL error at apply time is an expensive way to
// find that out.
func clampDescription(s string) string {
	const max = 900
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}

// sqlText quotes a description as a SQL string literal, doubling the quotes it
// contains. An apostrophe in a description is ordinary prose, and unescaped it
// ends the literal and turns the rest of the sentence into syntax.
func sqlText(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func commentOnTable(q func(string) string) func(string, string) string {
	return func(table, text string) string {
		return fmt.Sprintf("COMMENT ON TABLE %s IS %s;", q(table), sqlText(text))
	}
}

func commentOnColumn(q func(string) string) func(string, string, string) string {
	return func(table, column, text string) string {
		return fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s;", q(table), q(column), sqlText(text))
	}
}

// mysqlTableComment uses ALTER rather than a table option on the CREATE, so one
// mechanism covers every table this file writes. On a table created moments ago
// it is a metadata-only change.
func mysqlTableComment(table, text string) string {
	return fmt.Sprintf("ALTER TABLE %s COMMENT = %s;", backtickQuote(table), sqlText(text))
}

// sqlserverExtendedProperty stores the description the way T-SQL does: an
// MS_Description extended property, which is what SSMS, `sys.extended_properties`
// and every schema browser read as the object's description.
//
// The schema is taken from SCHEMA_NAME() through the @schema variable the file
// declares, not hardcoded to dbo: a service whose tables live in its own schema
// would otherwise fail every one of these on apply.
func sqlserverExtendedProperty(table, column, text string) string {
	var b strings.Builder
	b.WriteString("EXEC sp_addextendedproperty @name = N'MS_Description', @value = N")
	b.WriteString(sqlText(text))
	b.WriteString(",\n  @level0type = N'SCHEMA', @level0name = @schema,\n")
	fmt.Fprintf(&b, "  @level1type = N'TABLE',  @level1name = N%s", sqlText(table))
	if column != "" {
		fmt.Fprintf(&b, ",\n  @level2type = N'COLUMN', @level2name = N%s", sqlText(column))
	}
	b.WriteString(";")
	return b.String()
}

var dialects = map[string]dialect{
	"postgres": {
		Name: "postgres", ID: "UUID", Timestamp: "TIMESTAMPTZ",
		IfExists: true, Quote: ansiQuote, Column: postgresColumn, Comment: "--",
		TableComment: commentOnTable(ansiQuote), ColumnComment: commentOnColumn(ansiQuote),
	},
	"mysql": {
		Name: "mysql", ID: "BINARY(16)", Timestamp: "DATETIME(6)",
		IfExists: true, Quote: backtickQuote, Column: mysqlColumn, Comment: "--",
		TableComment: mysqlTableComment,
		InlineColumnComment: func(text string) string {
			return " COMMENT " + sqlText(text)
		},
	},
	"sqlserver": {
		Name: "sqlserver", ID: "BINARY(16)", Timestamp: "DATETIMEOFFSET",
		NamedDefaults: true, IfExists: true, Quote: bracketQuote,
		Column: sqlserverColumn, Comment: "--",
		TableComment: func(table, text string) string {
			return sqlserverExtendedProperty(table, "", text)
		},
		ColumnComment: sqlserverExtendedProperty,
	},
	"oracle": {
		Name: "oracle", ID: "RAW(16)", Timestamp: "TIMESTAMP WITH TIME ZONE",
		DefaultBeforeNotNull: true, IfExists: true, Quote: ansiQuote,
		Column: oracleColumn, Comment: "--",
		TableComment: commentOnTable(ansiQuote), ColumnComment: commentOnColumn(ansiQuote),
	},
	// SQLite is the only engine here with nowhere to store a description, so it
	// is the only one whose migration still carries them as `--` lines.
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
	desc := &descriptions{d: d}
	fmt.Fprintf(&b, "-- %s\n", m.Table)
	fmt.Fprintf(&b, "-- Generated by omnicore-gen from the %s spec.\n", m.Entity.Pascal)
	b.WriteString("\n")
	writeSessionPrelude(&b, d)

	// The identity comes first: the role's foreign key points at it.
	if m.IsRole() && !m.Base.Reuse {
		writeBaseTable(&b, m, d)
		b.WriteString("\n")
	}

	b.WriteString(desc.table(m.Table, m.TableDescription))
	fmt.Fprintf(&b, "CREATE TABLE %s (\n", d.Quote(m.Table))

	// Where a description stays in the file, it goes on its OWN line, ABOVE the
	// column. Trailing it after the definition would swallow the separating
	// comma into the comment and produce DDL that does not parse.
	var lines []string
	var comments []string
	add := func(comment, def string) {
		comments = append(comments, comment)
		lines = append(lines, def)
	}
	// column emits one column together with wherever its description lives on
	// this engine: inline on the definition, a statement after the table, or a
	// line above it.
	column := func(col, note, def string) {
		inline, above := desc.column(m.Table, col, note)
		add(above, def+inline)
	}

	column("id", "Row id — a UUID v7 minted by the framework, not a sequence.",
		fmt.Sprintf("  %s %s NOT NULL", d.Quote("id"), d.ID))

	if m.IsRole() && m.Base.Link == "separate-fk" {
		column(baseLinkColumn(m), "Link to the shared identity this row plays a role over.",
			fmt.Sprintf("  %s %s NOT NULL", d.Quote(baseLinkColumn(m)), d.ID))
	}
	for _, f := range roleColumns(m) {
		null := " NOT NULL"
		if f.Nullable {
			null = " NULL"
		}
		column(f.Column, f.Description,
			fmt.Sprintf("  %s %s%s", d.Quote(f.Column), d.Column(f), null))
	}

	// Revision is mandatory on an entity table: the framework guards every root
	// update on it and refuses to build the repository without it.
	column(m.Managed.Revision, "Optimistic-concurrency stamp: bumped on every write, and "+
		"the value each update is guarded on. Maintained by the framework.",
		fmt.Sprintf("  %s %s", d.Quote(m.Managed.Revision), stampTail(d, bigintOf(d), "0", false)))

	if m.Managed.CreatedAt != "" {
		column(m.Managed.CreatedAt, "When the row was created; written by the database default.",
			fmt.Sprintf("  %s %s", d.Quote(m.Managed.CreatedAt),
				stampTail(d, d.Timestamp, nowOf(d), false)))
	}
	if m.Managed.UpdatedAt != "" {
		column(m.Managed.UpdatedAt, "When the row was last written, maintained by the framework.",
			fmt.Sprintf("  %s %s", d.Quote(m.Managed.UpdatedAt),
				stampTail(d, d.Timestamp, nowOf(d), false)))
	}
	if m.Managed.ArchivedAt != "" {
		column(m.Managed.ArchivedAt, "Archive stamp; a non-null value hides the row from reads.",
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
	desc.flush(&b)

	// Siblings first, then children: both point back at their owner, so the
	// owner's table has to exist before either. A facet that lives inside a child
	// waits for that child, which is why the children go out before the facets
	// that hang off them.
	for _, sib := range m.SiblingsOn("") {
		b.WriteString("\n")
		writeSiblingTable(&b, m, sib, d)
	}
	for _, c := range m.Children {
		if c.Mounted {
			// The identity's collection is created by the spec that declares the
			// identity. Creating it again would be a second CREATE TABLE for one
			// table, and dropping it on this role's rollback would take another
			// role's rows with it.
			continue
		}
		b.WriteString("\n")
		writeChildTable(&b, m, c, d)
		for _, sib := range m.SiblingsOn(c.Name) {
			b.WriteString("\n")
			writeSiblingTable(&b, m, sib, d)
		}
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
		if len(c.Columns) > 1 {
			fmt.Fprintf(&b, "%s Over the TUPLE: this constraint belongs to a composite value object,\n", d.Comment)
			fmt.Fprintf(&b, "%s whose parts identify together and mean nothing apart.\n", d.Comment)
		}
		if c.Scope == "active-only" && m.Managed.ArchivedAt != "" {
			fmt.Fprintf(&b, "%s Scoped to the ACTIVE rows: an archived row releases the value, so it\n", d.Comment)
			fmt.Fprintf(&b, "%s can be taken again while the old row stays as history.\n", d.Comment)
			writeActiveOnlyUnique(&b, c.Table, uniqueName(c), c.Columns,
				constraintColumnTypes(m, c, d), m.Managed.ArchivedAt, d)
			continue
		}
		fmt.Fprintf(&b, "CREATE UNIQUE INDEX %s ON %s (%s);\n",
			d.Quote(uniqueName(c)), d.Quote(c.Table), quoteAll(c.Columns, d))
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
	b.WriteString("-- created and others not, so the down must tolerate what is absent.\n")
	if d.Name == "oracle" {
		// The spelling below parses from 23ai on; stating the floor here beats
		// an ORA-00933 with no pointer on an older server.
		b.WriteString("-- Oracle 23ai or newer: DROP TABLE IF EXISTS is not parsed by 19c/21c —\n")
		b.WriteString("-- on those, replace each line with the classic PL/SQL EXECUTE IMMEDIATE\n")
		b.WriteString("-- guard (catching SQLCODE -942).\n")
	}
	b.WriteString("\n")

	// The indexes are NOT dropped separately: dropping the table takes them with
	// it on every engine, and the standalone DROP INDEX spelling differs enough
	// between engines (MySQL has no IF EXISTS for it) to be a liability for no gain.
	// Reverse order: a table pointed at cannot go before the tables pointing at it.
	for _, c := range m.Children {
		if c.Mounted {
			continue // not this spec's table to drop
		}
		for _, sib := range m.SiblingsOn(c.Name) {
			fmt.Fprintf(&b, "DROP TABLE IF EXISTS %s;\n", d.Quote(sib.Table))
		}
		fmt.Fprintf(&b, "DROP TABLE IF EXISTS %s;\n", d.Quote(c.Table))
	}
	for _, sib := range m.SiblingsOn("") {
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
	owner := siblingOwnerTable(m, sib)
	desc := &descriptions{d: d}
	text := fmt.Sprintf("The %s facet of %s (1:1, key shared with the owner).", sib.Name, owner)
	if sib.Description != "" {
		text = firstLine(sib.Description) + " " + text
	}
	b.WriteString(desc.table(sib.Table, text))
	fmt.Fprintf(b, "CREATE TABLE %s (\n", d.Quote(sib.Table))
	inline, above := desc.column(sib.Table, "id",
		fmt.Sprintf("Row id — the same id as the %s row it belongs to.", owner))
	writeLine(b, above)
	fmt.Fprintf(b, "  %s %s NOT NULL%s,\n", d.Quote("id"), d.ID, inline)
	for _, f := range sib.Fields {
		inline, above := desc.column(sib.Table, f.Column, f.Description)
		writeLine(b, above)
		fmt.Fprintf(b, "  %s %s NULL%s,\n", d.Quote(f.Column), d.Column(f), inline)
	}
	fmt.Fprintf(b, "  CONSTRAINT %s PRIMARY KEY (%s),\n",
		d.Quote(sib.Table+"_pkey"), d.Quote("id"))
	fmt.Fprintf(b, "  CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)%s\n",
		d.Quote(sib.Table+"_owner_fk"), d.Quote("id"), d.Quote(owner), d.Quote("id"),
		cascadeClause(d))
	b.WriteString(");\n")
	desc.flush(b)
}

// writeLine writes a description that stayed in the file, and nothing at all on
// the engines that took it into the catalogue.
func writeLine(b *strings.Builder, line string) {
	if line != "" {
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
}

// writeChildTable emits a 1:N collection.
//
// The foreign key is indexed because every read of the aggregate loads the
// children by it; without the index that is a full scan per parent.
func writeChildTable(b *strings.Builder, m *ir.Model, c ir.Child, d dialect) {
	fk := parentColumn(c)
	owner := childOwnerTable(m, c)
	desc := &descriptions{d: d}
	text := fmt.Sprintf("The %s collection of %s.", c.Segment, owner)
	if c.Description != "" {
		text = firstLine(c.Description) + " " + text
	}
	b.WriteString(desc.table(c.Table, text))
	fmt.Fprintf(b, "CREATE TABLE %s (\n", d.Quote(c.Table))
	inline, above := desc.column(c.Table, "id",
		"Row id — a UUID v7 minted by the framework, not a sequence.")
	writeLine(b, above)
	fmt.Fprintf(b, "  %s %s NOT NULL%s,\n", d.Quote("id"), d.ID, inline)
	inline, above = desc.column(c.Table, fk, fmt.Sprintf("The %s row this entry belongs to.", owner))
	writeLine(b, above)
	fmt.Fprintf(b, "  %s %s NOT NULL%s,\n", d.Quote(fk), d.ID, inline)
	for _, f := range c.Fields {
		if f.Facet != "" {
			continue // it is a column of the facet's own table
		}
		null := " NOT NULL"
		if f.Nullable {
			null = " NULL"
		}
		inline, above := desc.column(c.Table, f.Column, f.Description)
		writeLine(b, above)
		fmt.Fprintf(b, "  %s %s%s%s,\n", d.Quote(f.Column), d.Column(f), null, inline)
	}
	if c.ArchivedAt != "" {
		inline, above := desc.column(c.Table, c.ArchivedAt,
			"Archive stamp; a non-null value hides the entry from reads.")
		writeLine(b, above)
		fmt.Fprintf(b, "  %s %s NULL%s,\n", d.Quote(c.ArchivedAt), d.Timestamp, inline)
	}
	if m.Managed.CreatedAt != "" {
		inline, above := desc.column(c.Table, m.Managed.CreatedAt,
			"When the entry was created; written by the database default.")
		writeLine(b, above)
		fmt.Fprintf(b, "  %s %s%s,\n", d.Quote(m.Managed.CreatedAt),
			stampTail(d, d.Timestamp, nowOf(d), false), inline)
	}
	if m.Managed.UpdatedAt != "" {
		inline, above := desc.column(c.Table, m.Managed.UpdatedAt,
			"When the entry was last written, maintained by the framework.")
		writeLine(b, above)
		fmt.Fprintf(b, "  %s %s%s,\n", d.Quote(m.Managed.UpdatedAt),
			stampTail(d, d.Timestamp, nowOf(d), false), inline)
	}
	fmt.Fprintf(b, "  CONSTRAINT %s PRIMARY KEY (%s),\n",
		d.Quote(c.Table+"_pkey"), d.Quote("id"))
	fmt.Fprintf(b, "  CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)%s\n",
		d.Quote(c.Table+"_parent_fk"), d.Quote(fk), d.Quote(owner), d.Quote("id"),
		cascadeClause(d))
	b.WriteString(");\n")
	desc.flush(b)
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
	desc := &descriptions{d: d}
	text := fmt.Sprintf("The shared identity %s plays a role over.", m.Table)
	if m.Base.Description != "" {
		text = firstLine(m.Base.Description) + " " + text
	}
	b.WriteString(desc.table(m.Base.Table, text))
	fmt.Fprintf(b, "CREATE TABLE %s (\n", d.Quote(m.Base.Table))
	inline, above := desc.column(m.Base.Table, "id",
		"Identity id — derived from the natural key, so the same key resolves to this same row.")
	writeLine(b, above)
	fmt.Fprintf(b, "  %s %s NOT NULL%s,\n", d.Quote("id"), d.ID, inline)
	for _, f := range m.BaseFields() {
		null := " NOT NULL"
		if f.Nullable {
			null = " NULL"
		}
		inline, above := desc.column(m.Base.Table, f.Column, f.Description)
		writeLine(b, above)
		fmt.Fprintf(b, "  %s %s%s%s,\n", d.Quote(f.Column), d.Column(f), null, inline)
	}
	inline, above = desc.column(m.Base.Table, m.Managed.Revision,
		"Optimistic-concurrency stamp: bumped on every write. Maintained by the framework.")
	writeLine(b, above)
	fmt.Fprintf(b, "  %s %s%s,\n", d.Quote(m.Managed.Revision),
		stampTail(d, bigintOf(d), "0", false), inline)
	if m.Managed.CreatedAt != "" {
		inline, above := desc.column(m.Base.Table, m.Managed.CreatedAt,
			"When the identity was created; written by the database default.")
		writeLine(b, above)
		fmt.Fprintf(b, "  %s %s%s,\n", d.Quote(m.Managed.CreatedAt),
			stampTail(d, d.Timestamp, nowOf(d), false), inline)
	}
	if m.Managed.UpdatedAt != "" {
		inline, above := desc.column(m.Base.Table, m.Managed.UpdatedAt,
			"When the identity was last written, maintained by the framework.")
		writeLine(b, above)
		fmt.Fprintf(b, "  %s %s%s,\n", d.Quote(m.Managed.UpdatedAt),
			stampTail(d, d.Timestamp, nowOf(d), false), inline)
	}
	if m.Managed.ArchivedAt != "" {
		inline, above := desc.column(m.Base.Table, m.Managed.ArchivedAt,
			"Archive stamp; a non-null value hides the identity from reads.")
		writeLine(b, above)
		fmt.Fprintf(b, "  %s %s NULL%s,\n", d.Quote(m.Managed.ArchivedAt), d.Timestamp, inline)
	}
	fmt.Fprintf(b, "  CONSTRAINT %s PRIMARY KEY (%s)\n",
		d.Quote(m.Base.Table+"_pkey"), d.Quote("id"))
	b.WriteString(");\n")
	desc.flush(b)
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
	writeActiveOnlyUnique(b, m.Table, name, []string{col}, []string{d.ID}, m.Managed.ArchivedAt, d)
}

// constraintColumnType answers the engine type of the column a unique
// constraint covers, so MySQL's generated shadow column can mirror it exactly.
func constraintColumnTypes(m *ir.Model, c ir.Constraint, d dialect) []string {
	out := make([]string, 0, len(c.Columns))
	for _, col := range c.Columns {
		out = append(out, constraintColumnType(m, col, d))
	}
	return out
}

func constraintColumnType(m *ir.Model, column string, d dialect) string {
	for _, f := range m.Fields {
		if f.Column == column {
			return d.Column(f)
		}
	}
	// The constraint was resolved from m.Fields, so this is unreachable — but
	// an id-typed fallback beats a panic inside a migration writer.
	return d.ID
}

// quoteAll renders a column list for an index, each name quoted in the engine's
// own way. One column reads exactly as it did before this was a list.
func quoteAll(cols []string, d dialect) string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, d.Quote(c))
	}
	return strings.Join(out, ", ")
}

// writeActiveOnlyUnique spells "unique among the rows that are not archived" in
// each engine's own terms.
//
// It is one behaviour with four spellings, and getting the spelling wrong does
// not fail loudly — it produces an index that is either too strict (an archived
// row keeps blocking the value forever) or absent. So the four live together,
// where they can be compared.
//
// colTypes are the SQL types of the constrained columns ON THIS ENGINE, in the
// same order. Only MySQL reads them — its spelling materialises the condition as
// generated columns, and each must carry its own value's type. It used to be
// hard-wired to the id type, which applied cleanly and then rejected any active
// VARCHAR value longer than 16 bytes at INSERT.
//
// The list is a list because a composite value object is unique as a TUPLE. Two
// of the four spellings do not generalise for free: MySQL needs one generated
// column PER part (their types differ, so one concatenated column would be a
// different constraint), and Oracle needs one CASE per part — it omits an index
// entry only when every indexed expression is NULL, which is exactly what an
// archived row produces.
func writeActiveOnlyUnique(b *strings.Builder, table, name string, cols, colTypes []string, archived string, d dialect) {
	switch d.Name {
	case "postgres", "sqlite", "sqlserver":
		fmt.Fprintf(b, "CREATE UNIQUE INDEX %s ON %s (%s) WHERE %s IS NULL;\n",
			d.Quote(name), d.Quote(table), quoteAll(cols, d), d.Quote(archived))
	case "oracle":
		// Oracle has no partial index, but it does ignore all-NULL entries: a
		// function-based index that yields NULL for an archived row leaves those
		// rows out of the constraint entirely.
		fmt.Fprintf(b, "%s Oracle has no partial index; an all-NULL entry is simply not indexed,\n", d.Comment)
		fmt.Fprintf(b, "%s so the expression yields NULL for archived rows and they drop out.\n", d.Comment)
		exprs := make([]string, 0, len(cols))
		for _, col := range cols {
			exprs = append(exprs, fmt.Sprintf("CASE WHEN %s IS NULL THEN %s END",
				d.Quote(archived), d.Quote(col)))
		}
		fmt.Fprintf(b, "CREATE UNIQUE INDEX %s ON %s (%s);\n",
			d.Quote(name), d.Quote(table), strings.Join(exprs, ", "))
	case "mysql":
		// MySQL has neither partial nor function-based indexes here, so the
		// condition is materialised as a generated column: it equals the value
		// while the row is active and NULL once archived, and MySQL does not
		// enforce uniqueness across NULLs.
		fmt.Fprintf(b, "%s MySQL has no partial index, so the condition becomes a column: it\n", d.Comment)
		fmt.Fprintf(b, "%s mirrors the value while active and turns NULL once archived, and NULLs\n", d.Comment)
		fmt.Fprintf(b, "%s do not collide with each other.\n", d.Comment)
		mirrors := make([]string, 0, len(cols))
		for i, col := range cols {
			mirror := "active_" + col
			mirrors = append(mirrors, mirror)
			fmt.Fprintf(b, "ALTER TABLE %s ADD COLUMN %s %s GENERATED ALWAYS AS (CASE WHEN %s IS NULL THEN %s END) STORED;\n",
				d.Quote(table), d.Quote(mirror), colTypes[i], d.Quote(archived), d.Quote(col))
		}
		fmt.Fprintf(b, "CREATE UNIQUE INDEX %s ON %s (%s);\n",
			d.Quote(name), d.Quote(table), quoteAll(mirrors, d))
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
	b.WriteString("-- The schema the descriptions below are attached to. Read from the session\n")
	b.WriteString("-- rather than hardcoded to dbo, so a service whose tables live in its own\n")
	b.WriteString("-- schema stores them on ITS objects instead of failing on every one.\n")
	b.WriteString("DECLARE @schema sysname = SCHEMA_NAME();\n\n")
}

// childOwnerTable is the table a collection's foreign key points at.
//
// A base-owned collection belongs to the shared IDENTITY: it outlives this role
// being archived and every other role over the same identity sees it. Pointing
// its key at the role's table instead would quietly make it role-private and
// delete it with the role — the same rows, a different meaning.
func childOwnerTable(m *ir.Model, c ir.Child) string {
	if c.OwnedBy == "base" && m.IsRole() {
		return m.Base.Table
	}
	return m.Table
}

// siblingOwnerTable is the table a 1:1 facet borrows its primary key from.
func siblingOwnerTable(m *ir.Model, sib ir.Sibling) string {
	if sib.OwnerChild == "" {
		return m.Table
	}
	for _, c := range m.Children {
		if c.Name == sib.OwnerChild {
			return c.Table
		}
	}
	return m.Table
}
