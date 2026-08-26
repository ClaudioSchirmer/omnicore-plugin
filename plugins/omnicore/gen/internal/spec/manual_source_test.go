package spec

import "testing"

// source: manual is the field-level ELSE: the generator declares the shape and
// hand-written code puts the value there. What it refuses is the set of keys
// that describe a value ARRIVING, because no generated verb brings this one.

// TestManualSourceIsAccepted is the case the spelling exists for: a field on the
// aggregate that no generated write carries.
func TestManualSourceIsAccepted(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: SenhaAtual, type: string, runtime: true, source: manual, livesOn: root, example: "s3nh4", description: A senha atual.}`)
	if ps.HasBlockers() {
		t.Errorf("source: manual was refused:\n%v", ps.items)
	}
}

// TestManualSourceTakesAValueObject. Unlike the identity sources, this value is
// the caller's own — it arrives through a hand-written DTO — so the domain has
// every business judging it. The per-gate exclusions that keep the automatic
// pass off it are the emitter's job, not a refusal here.
func TestManualSourceTakesAValueObject(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: SenhaAtual, type: string, runtime: true, source: manual, vo: {kind: reuse, ref: Senha}, livesOn: root, example: "s3nh4", description: A senha atual.}`)
	if says(ps, Blocker, "vo") {
		t.Errorf("a value object on a source: manual field was refused:\n%v", ps.items)
	}
}

// TestManualSourceRefusesACompositeValueObject. A composite's parts are what the
// SCHEMA decomposes, and a runtime field has no columns to decompose into —
// whatever feeds it. The refusal is the composite pass's, not the manual
// source's: validateOneField returns early for a composite, so a second copy of
// this rule next to the source-specific ones would be unreachable.
//
// It needs a spec of its own rather than the shared template, because the
// composite has to be DECLARED for the refusal under test to be the one that
// fires — over an undeclared ref the loader answers "no value object by that
// name" first, and a test asserting on that message would pass without ever
// reaching the rule it is named after.
func TestManualSourceRefusesACompositeValueObject(t *testing.T) {
	raw := `
specVersion: 1
entity: Cadastro
plural: Cadastros
language: pt-BR
storage:
  kind: flat
  table: cadastros
  description: Cadastros.
  managed: {revision: revision}
valueObjects:
  - name: Par
    kind: composite
    description: Um par de valores.
    parts:
      - {name: Esquerda, type: int64, description: A esquerda., labelKey: ParEsquerdaField}
      - {name: Direita, type: int64, description: A direita., labelKey: ParDireitaField}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Ana, description: O nome.}
  - {name: Composto, type: string, runtime: true, source: manual, vo: {kind: composite, ref: Par}, livesOn: root, example: x, description: Um composto.}
modes: [display, insert]
read:
  backing: relational
  view: {name: cadastros}
  byId: true
surfaces: {rest: true}
authz:
  resource: cadastro
  dataAccess: anyone-with-permission
  permissions: {insert: "cadastro:escrever", read: "cadastro:ler"}
`
	sp, err := Parse([]byte(raw), "manual-composite.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v", err)
	}
	mustBlock(t, Validate(sp, Options{}), "carries ONE value, whatever feeds it")
}

// TestManualSourceRefusesTheArrivalKeys: a claim, a permission and a set of write
// verbs each say the generator brings the value, and the declaration says it does
// not.
func TestManualSourceRefusesTheArrivalKeys(t *testing.T) {
	for _, c := range []struct{ name, decl, want string }{
		{"claim", `claim: sub`, "naming a claim says"},
		{"permission", `permission: "cadastro:administrar"`, "this one is source: manual"},
		{"modes", `modes: [insert]`, "no generated verb carries a source: manual one"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ps := bodyRuntimeProblems(t, `  - name: SenhaAtual
    type: string
    runtime: true
    source: manual
    `+c.decl+`
    livesOn: root
    example: "s3nh4"
    description: A senha atual.`)
			mustBlock(t, ps, c.want)
		})
	}
}

// TestAnEmptyModesListIsRefused is the silent no-op this closes. `modes: []`
// decoded to a list nobody declared anything in, fell into the same branch as an
// absent key, and put the field on EVERY write verb — the opposite of what it
// says, with `check` answering that the spec could be generated and the output
// byte-for-byte identical to writing no modes at all.
func TestAnEmptyModesListIsRefused(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: SenhaAtual
    type: string
    runtime: true
    source: body
    modes: []
    livesOn: root
    example: "s3nh4"
    description: A senha atual.`)
	mustBlock(t, ps, "an empty modes list")
}

// TestAnAbsentModesListStillMeansEveryVerb. The refusal above must not catch the
// default: an absent key is nil, a written `[]` is not, and only the second is an
// author saying something the generator cannot do.
func TestAnAbsentModesListStillMeansEveryVerb(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: SenhaAtual, type: string, runtime: true, source: body, livesOn: root, example: "s3nh4", description: A senha atual.}`)
	if says(ps, Blocker, "modes") {
		t.Errorf("omitting modes was refused as though it had been written empty:\n%v", ps.items)
	}
}
