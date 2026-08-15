// Package layout says where the generator's own files live inside a service.
//
// Everything this generator reads or writes ABOUT a service — the specs, their
// reports, the lock — sits together in one directory named after the tool. That
// is the whole convention, and it exists because the alternative had the three
// scattered: specs in a generic specs/, the report beside them, the lock loose
// at the project root as a dotfile. A reader had to already know which of those
// belonged to which tool, and the one holding the state that must not be lost
// was the one hidden from `ls`.
//
// It is a package rather than a constant in whichever file needed it first
// because four call sites resolve these paths — the spec finder, the sibling
// spec inventory, `init`'s default output, and the lock — and a layout spelled
// out four times is a layout that moves three times.
package layout

import "path/filepath"

// Dir is the directory, relative to the service root, that holds all of it.
const Dir = "omnicore-gen"

// LockName is the lock, inside Dir. It is deliberately visible and deliberately
// short: it does not repeat the directory's name, and a file recording what the
// generator owns is not something a reader benefits from having hidden.
const LockName = "lock.json"

// SpecSuffix is what marks a file in Dir as a spec. The name before it is free
// text, so an entity's spec can be found without loading it.
const SpecSuffix = ".omnicore.yaml"

// ReportSuffix is what the generator writes beside a spec after a run.
const ReportSuffix = ".gen-report.md"

// DirIn resolves Dir against a service root.
func DirIn(root string) string { return filepath.Join(root, Dir) }

// LockIn resolves the lock against a service root.
func LockIn(root string) string { return filepath.Join(root, Dir, LockName) }

// SpecIn resolves an entity's spec against a service root. The name is the
// entity in snake_case, which is how every other generated artefact is named.
func SpecIn(root, entitySnake string) string {
	return filepath.Join(root, Dir, entitySnake+SpecSuffix)
}

// SpecRel is the same path, relative to the service root — what a report or a
// generated header quotes, so the text does not carry a machine's directory
// structure.
func SpecRel(entitySnake string) string {
	return filepath.Join(Dir, entitySnake+SpecSuffix)
}
