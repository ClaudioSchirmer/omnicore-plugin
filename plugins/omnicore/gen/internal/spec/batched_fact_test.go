package spec

import (
	"fmt"
	"strings"
	"testing"
)

// A BATCHED per-entry fact: one question about a whole collection, answered per
// entry.
//
// The shape it replaces is the expensive one, and it was expensive silently.
// `filters: [<Collection>.<Field>]` alone is asked ONCE PER ENTRY — the loop
// lives in the rule, so a write carrying twenty entries pays twenty round trips
// and nothing in the spec, the signature or the report said the cost was there.
// The language could already batch the QUESTION (`op: in` takes the whole set)
// and had no way to batch the ANSWER: one bool for twenty ids cannot say WHICH
// one is the bad one, which is the difference between a 422 the caller can act
// on and one they cannot.
//
// So every case here is about a way of writing the batch down that would
// otherwise compile into a method saying something else.

// batchedFactSpec is one collection with a field of every type a key could be,
// plus a fact over it whose keys are filled in per case.
//
// Everything is asserted against ONE baseline, changing one thing at a time, so
// a case that starts passing for an unrelated reason shows up as the baseline
// failing rather than as silence here.
func batchedFactSpec(factKeys string) string {
	return `
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
modes: [display, insert, update, archive]
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
      - {name: Peso, type: float64, column: peso, example: "1.5", description: O peso.}
      - {name: ConcedidoEm, type: time, column: concedido_em, example: "2026-02-01T09:00:00Z", description: Quando.}
      - {name: Herdada, type: bool, column: herdada, example: "true", description: Se veio de um grupo.}
      - {name: Observacao, type: string, column: observacao, length: 60, nullable: true, example: Nota, description: Uma nota.}
  - name: PapelGrupo
    plural: Grupos
    table: papel_grupos
    parentColumn: papel_id
    description: Os grupos do papel.
    ownedBy: root
    editStrategy: atomic-replace
    businessIdentity: [GrupoID]
    fields:
      - {name: GrupoID, type: id, column: grupo_id, example: 7c2f5e10-9a44-4b83-8d61-3f0a7b2c9e15, description: O grupo.}
service:
  required: true
  facts:
    - name: PermissaoIndisponivel
      kind: manual
      returns: bool
` + factKeys + `
      description: Quais das permissões desta escrita não existem mais. Vive em outra tabela.
read:
  backing: relational
  view: {name: papeis}
  byId: true
surfaces: {rest: true}
authz:
  resource: papel
  dataAccess: anyone-with-permission
  permissions: {insert: "papel:escrever", update: "papel:escrever", patch: "papel:escrever", archive: "papel:arquivar", read: "papel:ler"}
`
}

// validateBatched parses and validates, failing the test on a parse error —
// which is never the thing under test here and always a broken fixture.
func validateBatched(t *testing.T, factKeys string) *Problems {
	t.Helper()
	s, err := Parse([]byte(batchedFactSpec(factKeys)), "x.omnicore.yaml")
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	return Validate(s, Options{})
}

// TestBatchedFactBaselineIsClean guards the fixture every case below bends: if
// the baseline itself were refused, each "X is refused" assertion would be
// vacuous.
func TestBatchedFactBaselineIsClean(t *testing.T) {
	ps := validateBatched(t, "      perEntry: Permissoes.PermissaoID\n      filters: [DonoID]")
	if ps.HasBlockers() {
		t.Fatalf("the baseline batched fact should validate cleanly, got:\n%v", ps.Error())
	}
}

// TestBatchedFactIsCovered pins that the capability is REPORTED. A build that
// silently generated something it does not fully support is the failure mode
// CheckCoverage exists for, and a capability nothing reports is a capability
// nobody can be refused for.
func TestBatchedFactIsCovered(t *testing.T) {
	s, err := Parse([]byte(batchedFactSpec(
		"      perEntry: Permissoes.PermissaoID\n      filters: [DonoID]")), "x.omnicore.yaml")
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	if cov := CheckCoverage(s); cov.HasBlockers() {
		t.Fatalf("this build declares the batched fact implemented, so it must not be "+
			"refused by coverage:\n%v", cov.Error())
	}
	if !Implemented(CapBatchedFact) {
		t.Error("CapBatchedFact is not marked implemented, but the emitters write it")
	}
}

// TestTheKeyMustBeFoundByAgain is the heart of the shape: an answer keyed by
// something two entries share, or that cannot be compared back to itself, is a
// map nobody can read an entry out of.
//
// Each type here fails differently and all three fail quietly: a time.Time
// compares by wall clock AND monotonic reading AND location, so two values that
// PRINT the same are two keys; a float can be NaN, which never equals itself,
// so the entry can never be looked up again; a bool has two values, which makes
// the map a pair of buckets rather than an answer per entry.
func TestTheKeyMustBeFoundByAgain(t *testing.T) {
	for _, tc := range []struct{ field, why string }{
		{"ConcedidoEm", "a time key carries a monotonic reading and a location"},
		{"Peso", "a float key can be NaN, which never equals itself"},
		{"Herdada", "a bool key is two buckets, not an answer per entry"},
	} {
		ps := validateBatched(t, "      perEntry: Permissoes."+tc.field+"\n      filters: [DonoID]")
		if !blockerAbout(ps, "cannot key an answer") {
			t.Errorf("%s must be refused as a key — %s; got:\n%v", tc.field, tc.why, ps.Error())
		}
	}
}

// TestEveryUsableKeyTypeIsAccepted is the other half: a rule that refuses is
// only as good as the set it lets through, and a key type quietly refused would
// send the author to write the fact by hand.
func TestEveryUsableKeyTypeIsAccepted(t *testing.T) {
	for _, field := range []string{"PermissaoID", "Rotulo"} {
		ps := validateBatched(t, "      perEntry: Permissoes."+field+"\n      filters: [DonoID]")
		if blockerAbout(ps, "cannot key an answer") {
			t.Errorf("%s is a usable key and was refused:\n%v", field, ps.Error())
		}
	}
}

// TestANullableKeyIsRefused: every entry without a value would answer under one
// key — the same argument groupBy already makes about a nullable grouping
// field, arriving through a different door.
func TestANullableKeyIsRefused(t *testing.T) {
	ps := validateBatched(t, "      perEntry: Permissoes.Observacao\n      filters: [DonoID]")
	if !blockerAbout(ps, "nullable") {
		t.Errorf("a nullable key must be refused — every entry without a value collapses "+
			"onto one key; got:\n%v", ps.Error())
	}
}

// TestTheKeyMustNameARealEntryField, in both halves: a head that names no
// collection, and a collection whose fields do not include the one named.
func TestTheKeyMustNameARealEntryField(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"Inexistentes.PermissaoID", "does not name a collection"},
		{"Permissoes.NaoExiste", "does not name a field of the collection"},
		{"PermissaoID", "does not name an entry's field"},
	} {
		ps := validateBatched(t, "      perEntry: "+tc.key+"\n      filters: [DonoID]")
		if !blockerAbout(ps, tc.want) {
			t.Errorf("perEntry: %s must be refused with %q, got:\n%v", tc.key, tc.want, ps.Error())
		}
	}
}

// TestOnlyAManualFactMayBatch. A computed fact is a query over THIS entity's
// own table and the entries are on the collection's — the same reason a
// collection field cannot be a computed fact's filter at all. Emitting anyway
// would need a join shape nothing else in the language can express or index.
func TestOnlyAManualFactMayBatch(t *testing.T) {
	for _, kind := range []string{"exists", "notExists", "count"} {
		body := "      perEntry: Permissoes.PermissaoID\n      filters: [DonoID]"
		raw := strings.Replace(batchedFactSpec(body),
			"      kind: manual\n      returns: bool\n", "      kind: "+kind+"\n", 1)
		s, err := Parse([]byte(raw), "x.omnicore.yaml")
		if err != nil {
			t.Fatalf("the fixture does not parse: %v", err)
		}
		ps := Validate(s, Options{})
		if !blockerAbout(ps, "query over this entity's own table") {
			t.Errorf("kind: %s must not batch — the entries are on another table; got:\n%v",
				kind, ps.Error())
		}
	}
}

// TestAggregatesMayNotBatch is the same refusal for the plural form, which
// carries no `kind` at all — so the message had to be built rather than read
// off one.
func TestAggregatesMayNotBatch(t *testing.T) {
	body := "      perEntry: Permissoes.PermissaoID\n      filters: [DonoID]"
	raw := strings.Replace(batchedFactSpec(body),
		"      kind: manual\n      returns: bool\n",
		"      aggregates: [{kind: count, as: Total}, {kind: max, field: Peso, as: MaiorPeso}]\n", 1)
	s, err := Parse([]byte(raw), "x.omnicore.yaml")
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	if ps := Validate(s, Options{}); !blockerAbout(ps, "answering several numbers") {
		t.Errorf("an aggregating fact must not batch, and the refusal must not read "+
			"\"a fact of kind \" with nothing after it; got:\n%v", ps.Error())
	}
}

// TestTheEntriesComeFromONECollection. Two collections in one batched fact
// would put fields of two different entries in one carrier, and there is no
// such thing as "the" entry to key the answer by.
func TestTheEntriesComeFromONECollection(t *testing.T) {
	ps := validateBatched(t,
		"      perEntry: Permissoes.PermissaoID\n      filters: [DonoID, Grupos.GrupoID]")
	if !blockerAbout(ps, "no shared entry to key by") {
		t.Errorf("filters reaching a second collection must be refused:\n%v", ps.Error())
	}
}

// TestTheOnceRunPerEntryFormAlsoTakesONECollection is the same rule for the
// form that predates perEntry, and it is the one that shipped: a fact filtered
// by two collections generated a port documented as "asked ONCE PER ENTRY of A
// and B" — a pair of nested loops nobody wrote and no order between them.
func TestTheOnceRunPerEntryFormAlsoTakesONECollection(t *testing.T) {
	ps := validateBatched(t, "      filters: [Permissoes.PermissaoID, Grupos.GrupoID]")
	if !blockerAbout(ps, "asked once per entry of ONE collection") {
		t.Errorf("a fact filtered by two collections must be refused:\n%v", ps.Error())
	}
}

// TestOneCollectionPerEntryStillPasses guards the refusal above from being too
// wide: the once-per-entry form over a single collection is the shape that
// existed before perEntry and stays legal forever.
func TestOneCollectionPerEntryStillPasses(t *testing.T) {
	ps := validateBatched(t, "      filters: [DonoID, Permissoes.PermissaoID]")
	if ps.HasBlockers() {
		t.Fatalf("the once-per-entry form must stay legal:\n%v", ps.Error())
	}
}

// TestASetInsideTheBatchIsRefused. The entries already arrive together, so an
// `in` on a per-entry leaf would put a slice INSIDE the carrier and claim the
// entry carries many values of that field — which no entry does.
func TestASetInsideTheBatchIsRefused(t *testing.T) {
	ps := validateBatched(t,
		"      perEntry: Permissoes.PermissaoID\n"+
			"      filters:\n        - DonoID\n        - {field: Permissoes.Rotulo, op: in}")
	if !blockerAbout(ps, "already asks about the whole collection at once") {
		t.Errorf("a set operator on a per-entry leaf of a batched fact must be refused:\n%v",
			ps.Error())
	}
}

// TestASecondEntryFieldIsAccepted is the carrier's whole reason: an entry may
// contribute more than its key, and the pair must travel together.
func TestASecondEntryFieldIsAccepted(t *testing.T) {
	ps := validateBatched(t,
		"      perEntry: Permissoes.PermissaoID\n"+
			"      filters: [DonoID, Permissoes.PermissaoID, Permissoes.Rotulo]")
	if ps.HasBlockers() {
		t.Fatalf("an entry contributing two fields must be accepted:\n%v", ps.Error())
	}
}

// TestABatchedFactIsNotReadableByFactRange. A declarative range fills a fact's
// arguments from the ROOT being written, and this one takes a collection —
// exactly the argument the set operator is already refused for.
func TestABatchedFactIsNotReadableByFactRange(t *testing.T) {
	body := "      perEntry: Permissoes.PermissaoID\n      filters: [DonoID]"
	raw := strings.Replace(batchedFactSpec(body),
		"      kind: manual\n      returns: bool\n",
		"      kind: manual\n      returns: int64\n", 1)
	raw = strings.Replace(raw, "read:\n  backing: relational", rangeRuleYAML+"read:\n  backing: relational", 1)
	s, err := Parse([]byte(raw), "x.omnicore.yaml")
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	if ps := Validate(s, Options{}); !blockerAbout(ps, "answers per entry of") {
		t.Errorf("factRange over a batched fact must be refused:\n%v", ps.Error())
	}
}

// TestAOncePerEntryFactIsNotReadableByFactRange is the same for the older form,
// and it is the one that generated a tree that did not build: the value is on
// the collection's table, so `e.<Field>` names nothing on the root.
func TestAOncePerEntryFactIsNotReadableByFactRange(t *testing.T) {
	raw := strings.Replace(batchedFactSpec("      filters: [Permissoes.PermissaoID]"),
		"      kind: manual\n      returns: bool\n",
		"      kind: manual\n      returns: int64\n", 1)
	raw = strings.Replace(raw, "read:\n  backing: relational", rangeRuleYAML+"read:\n  backing: relational", 1)
	s, err := Parse([]byte(raw), "x.omnicore.yaml")
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	if ps := Validate(s, Options{}); !blockerAbout(ps, "carries no field of that collection") {
		t.Errorf("factRange over a once-per-entry fact must be refused:\n%v", ps.Error())
	}
}

// rangeRuleYAML is a factRange over the fixture's fact, plus the notification
// it raises — a rule with no declared notification is refused for that instead,
// which would make the case above prove nothing.
const rangeRuleYAML = `rules:
  list:
    - id: teto
      kind: factRange
      fact: PermissaoIndisponivel
      max: 5
      attachTo: Nome
      notification: TetoNotification
      scope: [insert]
notifications:
  - name: TetoNotification
    semantic: validation
    package: domain
    text:
      ptbr: Limite excedido.
      eng: Limit exceeded.
      esp: Límite excedido.
      fra: Limite dépassée.
      deu: Grenze überschritten.
      ita: Limite superato.
      nld: Limiet overschreden.
`

// TestTheBatchedKeyIsNotDuplicatedWhenAlsoFiltered pins the ordering rule the
// carrier depends on: the key LEADS the entry's fields whether or not `filters`
// names it again, and naming it twice must not produce two struct fields of one
// name — which does not compile.
func TestTheBatchedKeyIsNotDuplicatedWhenAlsoFiltered(t *testing.T) {
	ps := validateBatched(t,
		"      perEntry: Permissoes.PermissaoID\n"+
			"      filters: [Permissoes.PermissaoID, Permissoes.Rotulo]")
	if ps.HasBlockers() {
		t.Fatalf("naming the key among the filters as well must be accepted:\n%v", ps.Error())
	}
}

// TestThePerEntryKeyAcceptsEitherSpellingOfTheCollection. Every key that
// addresses a collection takes both its `plural` and the entry type's `name`;
// a key that took only one would be the odd one out, and the author would find
// out from a refusal rather than from the documentation.
func TestThePerEntryKeyAcceptsEitherSpellingOfTheCollection(t *testing.T) {
	for _, spelling := range []string{"Permissoes", "PapelPermissao"} {
		ps := validateBatched(t, fmt.Sprintf(
			"      perEntry: %s.PermissaoID\n      filters: [DonoID]", spelling))
		if ps.HasBlockers() {
			t.Errorf("the collection addressed as %q must resolve:\n%v", spelling, ps.Error())
		}
	}
}
