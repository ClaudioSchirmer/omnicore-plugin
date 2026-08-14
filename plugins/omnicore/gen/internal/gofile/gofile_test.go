package gofile

import (
	"strings"
	"testing"
)

// TestPrunesUnusedImport pins the defect this package exists to kill: an
// emitter listing an import that a particular spec subset never uses.
func TestPrunesUnusedImport(t *testing.T) {
	src := `package x

import (
	"fmt"
	"strings"
)

func Hello() string { return fmt.Sprint("hi") }
`
	out, err := Finalize([]byte(src))
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	got := string(out)
	if strings.Contains(got, `"strings"`) {
		t.Errorf("the unused import survived:\n%s", got)
	}
	if !strings.Contains(got, `"fmt"`) {
		t.Errorf("the used import was dropped:\n%s", got)
	}
}

func TestKeepsBlankAndDotImports(t *testing.T) {
	src := `package x

import (
	_ "embed"
	"fmt"
)

func A() string { return fmt.Sprint(1) }
`
	out, err := Finalize([]byte(src))
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if !strings.Contains(string(out), `_ "embed"`) {
		t.Errorf("a blank import is there for its side effect and must survive:\n%s", out)
	}
}

func TestKeepsAliasedImport(t *testing.T) {
	src := `package x

import (
	fwdomain "github.com/ClaudioSchirmer/omnicore/domain"
	unused "github.com/ClaudioSchirmer/omnicore/web"
)

type T struct{ fwdomain.BaseEntity }
`
	out, err := Finalize([]byte(src))
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "fwdomain") {
		t.Errorf("the used alias was dropped:\n%s", got)
	}
	if strings.Contains(got, "unused") {
		t.Errorf("the unused alias survived:\n%s", got)
	}
}

// TestDropsEmptyImportBlock guards the cosmetic failure of leaving `import ()`
// behind, which gofmt tolerates but reads as a bug.
func TestDropsEmptyImportBlock(t *testing.T) {
	src := `package x

import "strings"

func A() int { return 1 }
`
	out, err := Finalize([]byte(src))
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if strings.Contains(string(out), "import") {
		t.Errorf("the import block should be gone entirely:\n%s", out)
	}
}

func TestOutputIsGofmtClean(t *testing.T) {
	src := "package x\nimport \"fmt\"\nfunc  A( )  string{return fmt.Sprint( 1 )}\n"
	out, err := Finalize([]byte(src))
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	again, err := Finalize(out)
	if err != nil {
		t.Fatalf("re-running Finalize: %v", err)
	}
	if string(again) != string(out) {
		t.Errorf("Finalize is not idempotent:\nfirst:\n%s\nsecond:\n%s", out, again)
	}
}

func TestParseFailureNamesTheLine(t *testing.T) {
	_, err := Finalize([]byte("package x\nfunc A( {\n"))
	if err == nil {
		t.Fatal("broken source must fail")
	}
	if !strings.Contains(err.Error(), "does not parse") {
		t.Errorf("the error should say the emitted file does not parse, got: %v", err)
	}
}
