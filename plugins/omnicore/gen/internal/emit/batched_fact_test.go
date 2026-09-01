package emit

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// What the generator WRITES for the four questions a fact could not ask.
//
// The spec package proves each of them is accepted or refused; this proves the
// emitted Go says the same thing. They are separate failures: a key can
// validate and reach the port as a scalar, a scope can validate and emit no
// gate at all, and both compile.

const batchedEmitSpec = `
specVersion: 1
entity: Papel
plural: Papeis
language: pt-BR
storage:
  kind: flat
  table: papeis
  description: Papéis.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Admin, description: O nome.}
  - {name: DonoID, type: id, column: dono_id, livesOn: root, example: 9f14b0a2-6d38-4c5e-b7a1-2e0c5d81f4a3, description: O dono.}
  - {name: Peso, type: float64, column: peso, livesOn: root, example: "1.5", description: O peso.}
modes: [display, insert, update, archive, unarchive]
update: {shape: both}
delete: {root: soft}
children:
  - name: PapelPermissao
    plural: Permissoes
    table: papel_permissoes
    parentColumn: papel_id
    description: As permissões do papel.
    ownedBy: root
    editStrategy: atomic-replace
    businessIdentity: [PermissaoID]
    fields:
      - {name: PermissaoID, type: id, column: permissao_id, example: 3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31, description: A permissão.}
      - {name: Rotulo, type: string, column: rotulo, length: 60, example: Ler, description: O rótulo.}
service:
  required: true
  facts:
    # Batched, one entry field: a plain slice of the key, no carrier.
    - name: PermissaoIndisponivel
      kind: manual
      returns: bool
      perEntry: Permissoes.PermissaoID
      filters: [DonoID]
      description: Quais permissões desta escrita não existem mais.
    # Batched, two entry fields: the generated carrier.
    - name: RotuloNaoConfere
      kind: manual
      returns: bool
      perEntry: Permissoes.PermissaoID
      filters: [Permissoes.PermissaoID, Permissoes.Rotulo]
      description: Quais entradas trazem um rótulo que não é o do catálogo.
    # The once-per-entry form, kept legal: the loop lives in the rule.
    - name: PermissaoVetada
      kind: manual
      returns: bool
      filters: [Permissoes.PermissaoID]
      description: Se esta permissão é vetada para o chamador.
    # The set-valued form: batched question, scalar answer.
    - name: AlgumaPermissaoVetada
      kind: manual
      returns: bool
      filters:
        - {field: Permissoes.PermissaoID, op: in}
      description: Se alguma das permissões desta escrita é vetada.
    # No description on purpose: the sentence the generator writes for notExists
    # is itself under test, and a description would replace it.
    - name: SemHomonimo
      kind: notExists
      filters: [Nome]
    - name: NomeJaArquivado
      kind: count
      filters: [Nome]
      scope: archivedOnly
      description: Quantos papéis arquivados já usaram este nome.
    - name: PapeisVivos
      kind: count
      filters: [DonoID]
      scope: active
      description: Quantos papéis vivos este dono tem.
    - name: SomaDosPesos
      kind: sum
      field: Peso
      filters: [DonoID]
      description: A soma dos pesos deste dono.
    - name: PesoPorDono
      kind: count
      groupBy: [Nome]
      description: Quantos papéis por nome.
read:
  backing: relational
  view: {name: papeis}
  byId: true
surfaces: {rest: true}
authz:
  resource: papel
  dataAccess: anyone-with-permission
  permissions: {insert: "papel:escrever", update: "papel:escrever", patch: "papel:escrever", archive: "papel:arquivar", unarchive: "papel:arquivar", read: "papel:ler"}
`

func batchedEmitModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(batchedEmitSpec), "papel.omnicore.yaml")
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

// TestABatchedFactAnswersAMapKeyedByTheEntry is the shape, on the port. One
// bool for twenty ids cannot say WHICH one is the problem; the map can, which
// is the whole reason the key exists.
func TestABatchedFactAnswersAMapKeyedByTheEntry(t *testing.T) {
	got := fileNamed(t, batchedEmitModel(t), "internal/domain/papel_service.go")
	want := "PermissaoIndisponivel(donoID domain.ID, permissaoIDSet []domain.ID) map[domain.ID]bool"
	if !strings.Contains(got, want) {
		t.Errorf("the port does not declare %s:\n%s", want, got)
	}
}

// TestOneEntryFieldTravelsWithoutACarrier. With the key alone a struct would be
// pure ceremony, so the parameter is a plain slice of it — and the type must be
// the KEY's, not a carrier nobody generated.
func TestOneEntryFieldTravelsWithoutACarrier(t *testing.T) {
	got := fileNamed(t, batchedEmitModel(t), "internal/domain/papel_service.go")
	if strings.Contains(got, "PapelPermissaoIndisponivelEntry") {
		t.Errorf("a carrier was generated for an entry that contributes only its key:\n%s", got)
	}
}

// TestTwoEntryFieldsTravelAsOneCarrier is what the carrier is FOR: two parallel
// slices are two things a caller can put out of step, and the answer would then
// be about a different entry than the one whose values were sent.
func TestTwoEntryFieldsTravelAsOneCarrier(t *testing.T) {
	// Whitespace-collapsed: gofmt aligns a struct's field types, so an exact
	// match here would break the day a longer field name is added and prove
	// nothing about the shape.
	m := batchedEmitModel(t)
	// The carrier is one more domain type, so it has a file of its own; the
	// port names it from the entity.
	got := squeeze(fileNamed(t, m, "internal/domain/papel_rotulo_nao_confere_entry.go") +
		fileNamed(t, m, "internal/domain/papel_service.go"))
	for _, want := range []string{
		"type PapelRotuloNaoConfereEntry struct {",
		"PermissaoID domain.ID",
		"Rotulo string",
		"RotuloNaoConfere(entries []PapelRotuloNaoConfereEntry) map[domain.ID]bool",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the carrier is not as declared, missing %q:\n%s", want, got)
		}
	}
}

// TestTheKeyLeadsTheCarrier. The answer is keyed by it, so a reader must find
// it first — and naming it among the filters as well must not produce two
// struct fields of one name, which does not compile.
func TestTheKeyLeadsTheCarrier(t *testing.T) {
	got := fileNamed(t, batchedEmitModel(t), "internal/domain/papel_rotulo_nao_confere_entry.go")
	body := between(t, got, "type PapelRotuloNaoConfereEntry struct {", "}")
	if strings.Count(body, "PermissaoID") != 1 {
		t.Errorf("the key must appear exactly once in the carrier, got:\n%s", body)
	}
	if strings.Index(body, "PermissaoID") > strings.Index(body, "Rotulo") {
		t.Errorf("the key must LEAD the carrier's fields, got:\n%s", body)
	}
}

// TestTheBatchedDocSettlesTheMissingKey is the one thing a map's type cannot
// say, and the one two services would otherwise read two ways: an absent key is
// the fact answering nothing, and Go's zero value settles what that means.
func TestTheBatchedDocSettlesTheMissingKey(t *testing.T) {
	got := docText(fileNamed(t, batchedEmitModel(t), "internal/domain/papel_service.go"))
	for _, want := range []string{
		"Asked ONCE for the WHOLE of Permissoes",
		"keyed by PermissaoID",
		"absent reads as false",
		"silence rather than a verdict",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the port's documentation does not say %q:\n%s", want, got)
		}
	}
}

// TestTheOncePerEntryDocPointsAtTheBatch. The older form stays legal and stays
// expensive, so the note that describes the cost now also names the key that
// removes it — a capability nobody is told about is one people work around.
func TestTheOncePerEntryDocPointsAtTheBatch(t *testing.T) {
	// Anchored on the fact's own DESCRIPTION, not its name: the name first
	// appears at the signature, and the note sits above it — so a name anchor
	// reads the next fact's documentation, or nothing at all.
	got := docText(fileNamed(t, batchedEmitModel(t), "internal/domain/papel_service.go"))
	doc := between(t, got, "Se esta permissão é vetada para o chamador.", "PermissaoVetada(")
	if !strings.Contains(doc, "Asked ONCE PER ENTRY of Permissoes") {
		t.Errorf("the once-per-entry note is gone:\n%s", doc)
	}
	if !strings.Contains(doc, "declare perEntry: <collection>.<field>") {
		t.Errorf("the note must name the key that batches it:\n%s", doc)
	}
}

// TestASetValuedPerEntryFactIsNotDocumentedAsPerEntry is the false sentence the
// report caught: `op: in` already asks about the whole collection, so "asked
// once per entry … multiplied by the size of the collection" was untrue of the
// very signature it sat on — the parameter is a slice.
func TestASetValuedPerEntryFactIsNotDocumentedAsPerEntry(t *testing.T) {
	got := docText(fileNamed(t, batchedEmitModel(t), "internal/domain/papel_service.go"))
	doc := between(t, got, "Se alguma das permissões desta escrita é vetada.", "AlgumaPermissaoVetada(")
	if strings.Contains(doc, "ONCE PER ENTRY") {
		t.Errorf("a set-valued filter is not asked once per entry:\n%s", doc)
	}
	if !strings.Contains(doc, "asked ONCE about the whole collection") {
		t.Errorf("the note must say the question is already batched:\n%s", doc)
	}
	if !strings.Contains(doc, "cannot say WHICH entry") {
		t.Errorf("the note must say what a scalar answer costs:\n%s", doc)
	}
}

// TestTheHookStubQualifiesTheCarrier. The carrier is declared beside the PORT,
// in the domain, and the hook file is in infra — so the stub has to name it
// through the import, and the import has to be there.
func TestTheHookStubQualifiesTheCarrier(t *testing.T) {
	got := fileNamed(t, batchedEmitModel(t), "internal/infra/papel_service_manual.go")
	for _, want := range []string{
		`appdomain "example.test/svc/internal/domain"`,
		"RotuloNaoConfere(entries []appdomain.PapelRotuloNaoConfereEntry) map[domain.ID]bool",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the hook stub is missing %q:\n%s", want, got)
		}
	}
}

// TestTheGeneratedTestStubAnswersAnEmptyMap. "Nothing found" for every entry at
// once, so a freshly generated suite passes on the day it is written — the same
// contract the port's own documentation states.
func TestTheGeneratedTestStubAnswersAnEmptyMap(t *testing.T) {
	// gofmt keeps a body on its own line, so the assertion is about the
	// signature and the answer separately rather than about the one-liner the
	// emitter writes before formatting.
	got := squeeze(fileNamed(t, batchedEmitModel(t), "internal/domain/papel_test.go"))
	for _, want := range []string{
		"PermissaoIndisponivel(_ domain.ID, _ []domain.ID) map[domain.ID]bool {\n return nil\n}",
		"RotuloNaoConfere(_ []PapelRotuloNaoConfereEntry) map[domain.ID]bool {\n return nil\n}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the test stub does not answer an empty map for %q:\n%s", want, got)
		}
	}
}

// TestNotExistsIsDocumentedAsTheInvertedReading. Without a description of its
// own, the generated sentence has to say WHY the fact is spelled this way —
// otherwise "notExists" reads as a typo for "exists" to the next person.
func TestNotExistsIsDocumentedAsTheInvertedReading(t *testing.T) {
	got := docText(fileNamed(t, batchedEmitModel(t), "internal/domain/papel_service.go"))
	for _, want := range []string{
		"reports whether NO matching row exists",
		"read the other way round",
		"named for the problem the rule raises",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the port does not explain notExists (%q):\n%s", want, got)
		}
	}
}

// TestNotExistsInvertsTheProbeOnce. The fact is named for the PROBLEM, so the
// reading is inverted in the body rather than at every call site.
func TestNotExistsInvertsTheProbeOnce(t *testing.T) {
	got := fileNamed(t, batchedEmitModel(t), "internal/infra/papel_service.go")
	body := between(t, got, "func (s *PapelServiceImpl) SemHomonimo(", "\n}")
	if !strings.Contains(body, "s.repo.Loader.Exists(") {
		t.Errorf("notExists must use the SAME probe as exists:\n%s", body)
	}
	if !strings.Contains(body, "return !found") {
		t.Errorf("notExists must invert the probe's answer:\n%s", body)
	}
}

// TestEachScopeReachesItsFrameworkCall. The gate is the framework's own
// three-way one; emitting the wrong call is a query that runs and answers about
// the wrong rows.
func TestEachScopeReachesItsFrameworkCall(t *testing.T) {
	got := fileNamed(t, batchedEmitModel(t), "internal/infra/papel_service.go")

	archived := between(t, got, "func (s *PapelServiceImpl) NomeJaArquivado(", "\n}")
	if !strings.Contains(archived, "q = q.OnlyArchived()") {
		t.Errorf("scope: archivedOnly must reach OnlyArchived:\n%s", archived)
	}

	active := between(t, got, "func (s *PapelServiceImpl) PapeisVivos(", "\n}")
	if strings.Contains(active, "q = q.IncludeArchived()") || strings.Contains(active, "OnlyArchived") {
		t.Errorf("scope: active is the query default and must add nothing:\n%s", active)
	}

	// The default is `all`, not `active`: a fact with no scope key has always
	// included the archived rows, and narrowing that would change what every
	// spec written before the key asks.
	all := between(t, got, "func (s *PapelServiceImpl) SomaDosPesos(", "\n}")
	if !strings.Contains(all, "q = q.IncludeArchived()") {
		t.Errorf("a fact with no scope must keep including the archived rows:\n%s", all)
	}
}

// TestTheCostNoteFitsTheQuestion is the sentence that was true of one kind and
// printed on all of them: "the probe exists precisely so a YES/NO QUESTION does
// not pay for full hydration", on top of sums, averages and grouped counts.
func TestTheCostNoteFitsTheQuestion(t *testing.T) {
	got := docText(fileNamed(t, batchedEmitModel(t), "internal/infra/papel_service.go"))

	probe := between(t, got, "SemHomonimo ", "func (s *PapelServiceImpl) SemHomonimo(")
	if !strings.Contains(probe, "yes/no question") {
		t.Errorf("a probe SHOULD carry the yes/no sentence:\n%s", probe)
	}
	for _, tc := range []struct{ fact, want string }{
		{"SomaDosPesos", "only the number travels back"},
		{"PesoPorDono", "one GROUP BY answers every key at once"},
	} {
		doc := between(t, got, tc.fact+" ", "func (s *PapelServiceImpl) "+tc.fact+"(")
		if strings.Contains(doc, "yes/no question") {
			t.Errorf("%s is not a yes/no question and carries that sentence:\n%s", tc.fact, doc)
		}
		if !strings.Contains(doc, tc.want) {
			t.Errorf("%s should say %q:\n%s", tc.fact, tc.want, doc)
		}
	}
}

// TestAGroupedCountDoesNotMentionFound. A count never carries Found — it cannot
// — so naming it in a file the developer is meant to read sends them looking
// for a field that is not in the struct above.
func TestAGroupedCountDoesNotMentionFound(t *testing.T) {
	got := docText(fileNamed(t, batchedEmitModel(t), "internal/infra/papel_service.go"))
	body := between(t, got, "func (s *PapelServiceImpl) PesoPorDono(", "\n}")
	if strings.Contains(body, "Found says so") {
		t.Errorf("a grouped count has no Found to speak of:\n%s", body)
	}
	if !strings.Contains(body, "One entry per distinct key") {
		t.Errorf("the rest of the note must survive:\n%s", body)
	}
}

// TestANonBoolBatchedFactNamesItsOwnZero. The missing-key sentence has to be
// written in the FACT's return type: "absent reads as false" is the wrong word
// for a fact that answers a number, and a sentence that is wrong about the type
// is one a reader stops trusting about the semantics.
func TestANonBoolBatchedFactNamesItsOwnZero(t *testing.T) {
	f := ir.Fact{
		Name: "PesoDaPermissao", Kind: "manual", Manual: true, ReturnType: "int64",
		Description: "Quanto pesa cada permissão.",
		PerEntry: &ir.FactPerEntry{
			Collection: "Permissoes", Param: "permissaoIDSet",
			Key:    ir.FactParam{Name: "permissaoID", GoType: "domain.ID", Field: "PermissaoID"},
			Fields: []ir.FactParam{{Name: "permissaoID", GoType: "domain.ID", Field: "PermissaoID"}},
		},
	}
	doc := factDoc(f)
	if !strings.Contains(doc, "absent reads as the zero int64") {
		t.Errorf("the missing-key sentence must be in the fact's own type:\n%s", doc)
	}
	if strings.Contains(doc, "absent reads as false") {
		t.Errorf("a fact answering int64 was told its absent key reads as false:\n%s", doc)
	}
}

// TestFactSignatureIsWhatThePortDeclares. The REPORT hands this line to whoever
// writes the body, and it used to build one of its own from the parameter list
// and the return TYPE — which stopped being the whole answer the moment a fact
// could answer a map. One function, one truth.
func TestFactSignatureIsWhatThePortDeclares(t *testing.T) {
	m := batchedEmitModel(t)
	port := fileNamed(t, m, "internal/domain/papel_service.go")
	for _, f := range m.Service.Facts {
		if !strings.Contains(port, FactSignature(f)) {
			t.Errorf("FactSignature says %q, which the port does not declare:\n%s",
				FactSignature(f), port)
		}
	}
}

// squeeze collapses runs of spaces and tabs to a single space, so an assertion
// is about what the code SAYS rather than about how gofmt aligned it.
func squeeze(s string) string {
	return spaceRunRe.ReplaceAllString(s, " ")
}

var spaceRunRe = regexp.MustCompile(`[ \t]+`)

// docText reads a comment back as the sentence it is.
//
// The emitter WRAPS at 72 columns, so any phrase long enough to be worth
// asserting about is split across lines by `//` — and an assertion written
// against the unwrapped sentence would pass or fail on where the wrap happened
// to land, which is not what any of these tests are about.
func docText(s string) string {
	return squeeze(commentPrefixRe.ReplaceAllString(s, " "))
}

var commentPrefixRe = regexp.MustCompile(`\n\s*//\s?`)

// between is the slice of s from the first occurrence of open to the next
// occurrence of close after it, so a test can assert about ONE emitted method
// rather than about the whole file — where a sentence belonging to a different
// fact would satisfy it.
//
// A missing anchor is FATAL rather than an empty string. Returning "" quietly
// is what makes every `if strings.Contains(doc, x) { t.Error }` in this file
// pass for the wrong reason: an assertion that something is ABSENT is trivially
// satisfied by looking at nothing.
func between(t *testing.T, s, open, close string) string {
	t.Helper()
	i := strings.Index(s, open)
	if i < 0 {
		t.Fatalf("the anchor %q is not in the emitted file — this test is asserting "+
			"about nothing:\n%s", open, s)
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		t.Fatalf("the closing anchor %q never follows %q, so the slice would run to the "+
			"end of the file and cover other declarations", close, open)
	}
	return rest[:j]
}
