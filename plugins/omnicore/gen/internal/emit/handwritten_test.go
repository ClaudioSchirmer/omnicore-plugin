package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The two escape hatches this build owns — a value object it cannot write and a
// field it cannot compute — are proven HERE and not in the coverage matrix, on
// purpose: the matrix generates, builds and boots every case, and both of these
// deliberately emit a tree that does not compile until a human finishes it.
// That is the feature. A matrix case would be asserting the opposite.

const handwrittenSpec = `
specVersion: 1
entity: Anotacao
plural: Anotacoes
language: pt-BR
storage:
  kind: flat
  table: anotacoes
  description: Anotações.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - {name: Texto, type: string, column: texto, length: 200, livesOn: root, example: oi, description: O texto.}
  - name: Documento
    type: string
    column: documento
    length: 14
    livesOn: root
    vo: {kind: manual, ref: Documento}
    example: "529.982.247-25"
    description: Documento do autor.
  - {name: Codigo, type: string, column: codigo, length: 40, livesOn: root, assignedFrom: derived, example: a-1, description: Código público.}
valueObjects:
  - name: Documento
    kind: manual
    backing: string
    description: Documento válido pelos próprios dígitos verificadores.
modes: [display, insert, update]
update: {shape: patch}
rules:
  manual:
    - {id: codigo-derivado, description: Calcular Codigo a partir de Texto no insert., scope: [insert]}
read:
  backing: relational
  view: {name: anotacoes, version: 1}
  byId: true
surfaces: {rest: true}
authz:
  resource: anotacao
  dataAccess: anyone-with-permission
  permissions: {insert: "anotacao:escrever", patch: "anotacao:escrever", read: "anotacao:ler"}
`

func handwrittenModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(handwrittenSpec), "handwritten.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if ps := spec.Validate(s, spec.Options{}); ps.HasBlockers() {
		t.Fatalf("the fixture does not validate:\n%v", ps.Error())
	}
	m, err := ir.Resolve(s, &discover.Project{
		ModulePath: "example.test/svc", Dialects: []string{"sqlite"}, Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	return m
}

// TestManualValueObjectIsDeclaredAndNotWritten pins both halves of the deal: the
// generator does not write the type, and it does not pretend the field is
// anything else either.
//
// The half that matters is the second. `kind: reuse` demands a type the project
// ALREADY has, so a value object the generator cannot express had no legal
// declaration at all — the author reached for reuse, got told the type does not
// exist, and worked around the language. `manual` is that missing answer, and it
// is only worth having if the emitted code really does type the field as the
// value object; a kind that quietly degraded to a plain string would validate,
// generate and lose the concept.
func TestManualValueObjectIsDeclaredAndNotWritten(t *testing.T) {
	m := handwrittenModel(t)
	files := emitAll(t, m)

	for _, f := range files {
		if strings.HasSuffix(f.Path, "vos/documento.go") {
			t.Errorf("%s was written: a hand-written value object must emit NO file — a "+
				"stub that validates nothing passes every check the framework runs, "+
				"silently", f.Path)
		}
	}

	entity := ""
	for path, body := range goSources(files) {
		if strings.HasSuffix(path, "internal/domain/anotacao.go") {
			entity = body
		}
	}
	if entity == "" {
		t.Fatal("the entity file was not emitted")
	}
	if !strings.Contains(entity, "vos.Documento") {
		t.Error("the field lost its value object: the whole point of declaring one the " +
			"generator cannot write is that the field is still typed as it")
	}
}

// TestDerivedFieldLeavesTheWriteSurfaceAndIsAssignedByNobody is the pair of
// promises `assignedFrom: derived` makes.
//
// The first is what the author asked for: the field stops being advertised as
// writable while the server silently overwrites whatever a caller sent. The
// second is what keeps the generator honest — it writes no assignment, because
// it does not know the derivation; the report is where that debt is recorded.
func TestDerivedFieldLeavesTheWriteSurfaceAndIsAssignedByNobody(t *testing.T) {
	m := handwrittenModel(t)
	src := goSources(emitAll(t, m))

	// The INPUT types only. Each of these files also declares the result or
	// response beside the input, and those are supposed to carry the field: it
	// left the write surface, not the read one.
	for _, typeName := range []string{
		"InsertAnotacaoRequest", "PatchAnotacaoRequest",
		"InsertAnotacaoCommand", "PatchAnotacaoCommand",
	} {
		body, found := structBody(src, typeName)
		if !found {
			t.Fatalf("%s was not emitted", typeName)
		}
		if strings.Contains(body, "Codigo") {
			t.Errorf("%s carries Codigo: a derived field is absent from every write request "+
				"and command, or the caller is told they have a say in a value the server "+
				"owns:\n%s", typeName, body)
		}
	}

	for path, body := range src {
		if strings.Contains(path, "/commands/") && strings.Contains(body, "e.Codigo =") {
			t.Errorf("%s assigns Codigo: the generator does not know the derivation, so "+
				"writing one would be inventing it", path)
		}
	}

	// And the field is still PERSISTED — it left the API, not the entity.
	entity := ""
	for path, body := range src {
		if strings.HasSuffix(path, "internal/domain/anotacao.go") {
			entity = body
		}
	}
	if !strings.Contains(entity, "Codigo") {
		t.Error("the derived field vanished from the entity: it is absent from the write " +
			"SURFACE, not from the row")
	}
}

// structBody returns the field block of one emitted type.
func structBody(src map[string]string, typeName string) (string, bool) {
	for _, body := range src {
		i := strings.Index(body, "type "+typeName+" struct {")
		if i < 0 {
			continue
		}
		rest := body[i:]
		if j := strings.Index(rest, "\n}"); j >= 0 {
			return rest[:j], true
		}
		return rest, true
	}
	return "", false
}
