package emit

import (
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
)

// testFiles routes generated test code to ONE FILE PER SOURCE FILE.
//
// The layout's rule for tests is "beside the source, <file>_test.go", and it is
// the rule the generator broke most quietly: every entity used to get one
// <entity>_commands_test.go, one <entity>_requests_test.go, one
// <entity>_vos_test.go — a single file per LAYER, holding the cases for a dozen
// sources. A reader who changed insert_student_command.go had no file to look
// in, and a failing test named a package rather than the thing that broke.
//
// The emitters keep their shape: they still walk the model once, in the order
// that made them readable, writing into a buffer. What changed is which buffer
// — at(name) hands back the one belonging to the source file the case is about,
// creating it on first use with the layer's import block already written. An
// emitter that never asks for a name emits no file, which is what keeps an
// entity with no siblings from getting an empty sibling test.
type testFiles struct {
	// dir is the package directory the files land in, relative to the project
	// root, WITHOUT a trailing slash.
	dir string
	// pkg opens each new buffer: the package clause and the import block. It is
	// called once per file, so every one of them stands alone.
	pkg func(*src)
	// order preserves first-use order, so a regeneration of the same spec plans
	// the same files in the same sequence and the report reads the same twice.
	order []string
	bufs  map[string]*src
}

func newTestFiles(dir string, pkg func(*src)) *testFiles {
	return &testFiles{dir: dir, pkg: pkg, bufs: map[string]*src{}}
}

// at returns the buffer for <base>_test.go, opening it on first use.
//
// base is the source file's name without its .go — insert_student_command,
// email, student_schema — so the test file is that name plus _test.
func (t *testFiles) at(base string) *src {
	if s, ok := t.bufs[base]; ok {
		return s
	}
	s := &src{}
	s.Blank()
	t.pkg(s)
	s.Blank()
	t.bufs[base] = s
	t.order = append(t.order, base)
	return s
}

// has reports whether anything was written for a source file yet. It is what
// lets an emitter add a second case to a file only when the first one exists —
// a case that would otherwise stand alone in a file whose subject generated no
// code.
func (t *testFiles) has(base string) bool {
	_, ok := t.bufs[base]
	return ok
}

// files finalises what was written. describes takes the base name so each file
// can say what IT covers rather than repeating a layer-wide sentence.
func (t *testFiles) files(describes func(base string) string) ([]fsplan.File, error) {
	out := make([]fsplan.File, 0, len(t.order))
	for _, base := range t.order {
		f, err := goFile(t.dir+"/"+base+"_test.go", fsplan.Owned, describes(base), t.bufs[base])
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}
