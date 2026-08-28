package spec

import "testing"

// renderIn is the output side of source: manual — the value the author's own
// rule minted, handed to the caller by the verb that minted it. What it refuses
// is every OTHER way a value can reach the entity, because each of those is
// already the caller's or already the token's, and answering with it is a leak
// rather than a delivery.

// TestRenderInIsAcceptedOnAManualField is the case the key exists for: a machine
// credential whose hash is all the row keeps, so this response is the only place
// the plaintext can ever be.
func TestRenderInIsAcceptedOnAManualField(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: SenhaProvisoria
    type: string
    runtime: true
    source: manual
    renderIn: [insert]
    livesOn: root
    example: "s3nh4"
    description: A senha provisória sorteada na criação.`)
	if ps.HasBlockers() {
		t.Errorf("renderIn on a source: manual field was refused:\n%v", ps.items)
	}
}

// TestRenderInRefusesTheValueTheCallerSent. A source: body field is an INPUT —
// a password confirmation is the case it exists for — and echoing it back hands
// someone their own credential from a response nobody expected to carry one.
// The generator has refused that since the source existed; a key that reopened
// it by spelling would be the same leak with permission.
func TestRenderInRefusesTheValueTheCallerSent(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: ConfirmacaoSenha
    type: string
    runtime: true
    source: body
    renderIn: [insert]
    livesOn: root
    example: "s3nh4"
    description: A senha digitada outra vez.`)
	mustBlock(t, ps, "hands them their own credential")
	// And it names where the key does belong, or the author's next move is to
	// find the nearest thing that is not refused.
	mustBlock(t, ps, "source: manual")
}

// TestRenderInRefusesAFactAboutTheCaller. The identity sources answer questions
// the caller already knows the answer to: rendering one reflects the token back
// at whoever presented it. Each source is asserted, because the refusal reads
// the source by name and a branch that missed one would leak exactly the fact
// that source carries.
func TestRenderInRefusesAFactAboutTheCaller(t *testing.T) {
	for _, c := range []struct{ name, decl string }{
		{"claim", "source: claim\n    claim: email"},
		{"subject", "source: subject"},
		{"tenant", "source: tenant"},
		{"permission", `source: permission
    permission: "cadastro:administrar"`},
		{"super-admin", "source: super-admin"},
		{"present", "source: present"},
	} {
		t.Run(c.name, func(t *testing.T) {
			typ := "string"
			switch c.name {
			case "permission", "super-admin", "present":
				typ = "bool"
			}
			ps := bodyRuntimeProblems(t, `  - name: FatoDoChamador
    type: `+typ+`
    runtime: true
    `+c.decl+`
    renderIn: [insert]
    livesOn: root
    example: "x"
    description: Um fato sobre quem chamou.`)
			mustBlock(t, ps, "reflects the token back")
		})
	}
}

// TestRenderInRefusesAPersistedField. That side of the question is already
// answered — a persisted field is in every response by default, `hidden` takes
// it out and `redact` masks the copies — and a third key saying "put it in"
// would be a second spelling for the default.
func TestRenderInRefusesAPersistedField(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: Apelido
    type: string
    column: apelido
    length: 60
    livesOn: root
    renderIn: [insert]
    example: ana
    description: O apelido.`)
	mustBlock(t, ps, "this field has a column")
	mustBlock(t, ps, "hidden: true")
}

// TestRenderInRefusesAVerbTheEntityDoesNotHave. A response that never happens is
// not a promise worth printing in a report — and the failure it replaces is the
// worst kind: the author believes the credential is being delivered.
func TestRenderInRefusesAVerbTheEntityDoesNotHave(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: SenhaProvisoria
    type: string
    runtime: true
    source: manual
    renderIn: [update]
    livesOn: root
    example: "s3nh4"
    description: A senha provisória.`, "[display, insert]")
	mustBlock(t, ps, "no response would ever render the field")
}

// TestRenderInRefusesAValueOutsideTheVocabulary keeps the two sides of the axis
// spelled the same way. `patch` is a real verb and still refused here, because
// the domain dispatches it into the same IfUpdate a PUT goes to — the same
// reason `modes` takes two values and not three.
func TestRenderInRefusesAValueOutsideTheVocabulary(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: SenhaProvisoria
    type: string
    runtime: true
    source: manual
    renderIn: [patch]
    livesOn: root
    example: "s3nh4"
    description: A senha provisória.`)
	mustBlock(t, ps, "is not a write verb whose response can render a field")
}

// TestAnEmptyRenderInListIsRefused. Written-out brackets say "no verb renders
// it", which is what leaving the key out already says — so the author meant
// something, and the generator that silently agreed with the default is the one
// that ships a credential nobody receives. It is the same refusal `modes: []`
// gets, for the same reason.
func TestAnEmptyRenderInListIsRefused(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: SenhaProvisoria
    type: string
    runtime: true
    source: manual
    renderIn: []
    livesOn: root
    example: "s3nh4"
    description: A senha provisória.`)
	mustBlock(t, ps, "an empty renderIn list")
}

// TestAnAbsentRenderInIsTheDefault. The refusal above must not catch the field
// that simply does not ask: a manual runtime field with no renderIn is the
// original declaration — on the aggregate, and on nothing else.
func TestAnAbsentRenderInIsTheDefault(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: SenhaAtual, type: string, runtime: true, source: manual, livesOn: root, example: "s3nh4", description: A senha atual.}`)
	if says(ps, Blocker, "renderIn") {
		t.Errorf("omitting renderIn was refused as though it had been written empty:\n%v", ps.items)
	}
}
