package spec

import "strings"

// Identifiers are emitted UNQUOTED in the Go schema and quoted in the DDL, so a
// name that is reserved on one engine produces a migration that applies on some
// dialects and is rejected on others. That is the worst shape of failure: it
// passes locally and breaks on the engine nobody develops against.
//
// The list is deliberately the UNION across the five engines rather than a
// per-dialect check, so a name is refused everywhere or nowhere. A project that
// targets one engine today may add another tomorrow, and a column cannot be
// renamed once there is data in it.
var reservedWords = map[string]string{
	// widely reserved
	"select": "SQL", "from": "SQL", "where": "SQL", "table": "SQL", "order": "SQL",
	"group": "SQL", "by": "SQL", "having": "SQL", "join": "SQL", "union": "SQL",
	"insert": "SQL", "update": "SQL", "delete": "SQL", "create": "SQL", "drop": "SQL",
	"alter": "SQL", "index": "SQL", "view": "SQL", "into": "SQL", "values": "SQL",
	"and": "SQL", "or": "SQL", "not": "SQL", "null": "SQL", "is": "SQL", "in": "SQL",
	"like": "SQL", "between": "SQL", "exists": "SQL", "case": "SQL", "when": "SQL",
	"then": "SQL", "else": "SQL", "end": "SQL", "as": "SQL", "on": "SQL",
	"primary": "SQL", "foreign": "SQL", "key": "SQL", "unique": "SQL", "check": "SQL",
	"default": "SQL", "constraint": "SQL", "references": "SQL", "grant": "SQL",
	"user": "SQL", "session": "SQL", "all": "SQL", "any": "SQL", "distinct": "SQL",

	// Oracle
	"number": "oracle", "date": "oracle", "level": "oracle", "rowid": "oracle",
	"rownum": "oracle", "access": "oracle", "audit": "oracle", "cluster": "oracle",
	"comment": "oracle", "compress": "oracle", "current": "oracle", "file": "oracle",
	"increment": "oracle", "initial": "oracle", "lock": "oracle", "long": "oracle",
	"mode": "oracle", "offline": "oracle", "online": "oracle", "pctfree": "oracle",
	"raw": "oracle", "resource": "oracle", "row": "oracle", "share": "oracle",
	"size": "oracle", "start": "oracle", "successful": "oracle", "synonym": "oracle",
	"sysdate": "oracle", "uid": "oracle", "validate": "oracle", "varchar": "oracle",

	// MySQL
	"rank": "mysql", "system": "mysql", "usage": "mysql", "read": "mysql",
	"write": "mysql", "range": "mysql", "partition": "mysql", "lead": "mysql",
	"lag": "mysql", "first": "mysql", "last": "mysql", "over": "mysql",

	// SQL Server
	"identity": "sqlserver", "percent": "sqlserver", "top": "sqlserver",
	"backup": "sqlserver", "restore": "sqlserver", "public": "sqlserver",
	"schema": "sqlserver", "function": "sqlserver", "procedure": "sqlserver",
	"trigger": "sqlserver", "transaction": "sqlserver", "rollback": "sqlserver",
	"commit": "sqlserver", "print": "sqlserver", "raiserror": "sqlserver",

	// PostgreSQL
	"analyse": "postgres", "analyze": "postgres", "asymmetric": "postgres",
	"both": "postgres", "leading": "postgres", "trailing": "postgres",
	"placing": "postgres", "returning": "postgres", "window": "postgres",
	"limit": "postgres", "offset": "postgres",
}

// ReservedWord reports the engine a name collides with, or an empty string.
func ReservedWord(name string) string {
	return reservedWords[lowerASCII(name)]
}

func lowerASCII(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'A' && r <= 'Z' {
			out[i] = r + ('a' - 'A')
		}
	}
	return string(out)
}

// ReservedViewSuffix reports why a read-model name is refused, or "".
//
// `__0` and `__1` are the blue-green SLOT suffixes the framework addresses a
// view's two physical collections by. A view named `users__0` would own a bare
// collection byte-identical to view `users`'s FIRST SLOT — and every
// consequence of that collision is silent: the DB-per-service guard whitelists
// all three physical names per view and reads the overlap as legitimate on both
// sides, a rebuild of `users` provisions into `users__0` and drops what is
// already there, and the orphan-collection diagnostic names the wrong registry
// row.
//
// The framework refuses it at boot in EVERY read-model family — plain views,
// shared-base views, composed views and relational views alike, because all four
// share one namespace. This is the generator's copy of that rule, answered while
// the author is still in the file (query.ReservedNameSuffixProblem is the
// framework's own, exported for exactly this).
//
// The test is HasSuffix, exactly as the framework's is. A length guard here
// would let the degenerate name `__0` through the generator and into a boot the
// framework aborts — and a copy of a rule that answers differently at the edge
// is worse than no copy at all, because the author trusts it.
func ReservedViewSuffix(name string) string {
	for _, slot := range []string{"__0", "__1"} {
		if strings.HasSuffix(name, slot) {
			return "ends in " + slot + ", which is a blue-green slot suffix the " +
				"framework reserves for a view's physical collections"
		}
	}
	return ""
}
