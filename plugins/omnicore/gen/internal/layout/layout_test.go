package layout

import (
	"path/filepath"
	"testing"
)

// This package is four one-line functions and a handful of constants, and it is
// tested because of what they ARE: the answer to "where does this project's
// generator state live". Change one of them and every existing project's specs,
// reports and lock stop being found — the tool reports a first run on a tree it
// has generated a hundred times, and the next generate treats every file it
// owns as somebody else's.

func TestTheConventionIsSpecsOmnicoreGen(t *testing.T) {
	root := filepath.FromSlash("/srv/app")
	for _, tc := range []struct{ got, want string }{
		{Dir, filepath.ToSlash("specs/omnicore-gen")},
		{DirIn(root), filepath.Join(root, "specs", "omnicore-gen")},
		{LockIn(root), filepath.Join(root, "specs", "omnicore-gen", "lock.json")},
		{SpecIn(root, "papel"), filepath.Join(root, "specs", "omnicore-gen", "papel.omnicore.yaml")},
		{SpecRel("papel"), filepath.Join("specs", "omnicore-gen", "papel.omnicore.yaml")},
	} {
		if tc.got != tc.want {
			t.Errorf("want %q, got %q", tc.want, tc.got)
		}
	}
}

// TestARelativePathCarriesNoMachine. SpecRel is what a generated header and a
// report quote, and every one of those lines is committed: an absolute path
// there prints one developer's directory structure into the whole team's diff.
func TestARelativePathCarriesNoMachine(t *testing.T) {
	if filepath.IsAbs(SpecRel("papel")) {
		t.Errorf("SpecRel must stay relative to the service root, got %q", SpecRel("papel"))
	}
}

// TestTheSpecSuffixIsWhatFindsASpec. The finder lists a directory and decides
// from the NAME alone, so a suffix that stopped matching what SpecIn writes
// makes the generator create specs it can never find again.
func TestTheSpecSuffixIsWhatFindsASpec(t *testing.T) {
	name := filepath.Base(SpecIn("/srv/app", "papel"))
	if len(name) <= len(SpecSuffix) || name[len(name)-len(SpecSuffix):] != SpecSuffix {
		t.Fatalf("what SpecIn writes must be what the finder recognises: %q vs %q", name, SpecSuffix)
	}
	if name[:len(name)-len(SpecSuffix)] != "papel" {
		t.Errorf("the entity's snake_case name must be readable from the file name, got %q", name)
	}
}
