package spec

import "testing"

// The identity sources are the answer to "the domain has no ctx". They are
// validated as one family, against the same base entity the body-sourced runtime
// field is, because the keys they share are the keys they disagree about: one
// needs a claim name, one needs a permission, and the rest need neither.

// TestIdentitySourcesAreAccepted is the case the whole family exists for. A
// refusal here is the feature not shipping.
func TestIdentitySourcesAreAccepted(t *testing.T) {
	for _, c := range []struct{ source, decl string }{
		{"subject", `  - {name: Solicitante, type: string, runtime: true, source: subject, livesOn: root, example: usr-1, description: Quem chamou.}`},
		{"tenant", `  - {name: SolicitanteTenant, type: string, runtime: true, source: tenant, livesOn: root, example: alfa, description: Tenant de quem chamou.}`},
		{"permission", `  - {name: PodeAdministrar, type: bool, runtime: true, source: permission, permission: "cadastro:administrar", livesOn: root, example: "false", description: Se administra.}`},
		{"super-admin", `  - {name: ESuperUsuario, type: bool, runtime: true, source: super-admin, livesOn: root, example: "false", description: Se e superusuario.}`},
		{"present", `  - {name: TemIdentidade, type: bool, runtime: true, source: present, livesOn: root, example: "false", description: Se veio identidade.}`},
	} {
		t.Run(c.source, func(t *testing.T) {
			if ps := bodyRuntimeProblems(t, c.decl); ps.HasBlockers() {
				t.Errorf("source: %s was refused:\n%v", c.source, ps.items)
			}
		})
	}
}

// TestPermissionSourceNeedsItsPermission: without one there is nothing to hand
// HasPermission, and the field would answer false for every caller — the
// safe-looking answer, and the one nothing reports.
func TestPermissionSourceNeedsItsPermission(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: PodeAdministrar, type: bool, runtime: true, source: permission, livesOn: root, example: "false", description: Se administra.}`)
	mustBlock(t, ps, "does not say WHICH permission")
}

// TestPermissionSourceRefusesAWildcard is the refusal that matters most: the
// framework PANICS on a wildcard argument, so a spec accepted here generates,
// compiles and takes the service down on the first request that reaches the
// feed.
func TestPermissionSourceRefusesAWildcard(t *testing.T) {
	for _, wildcard := range []string{"cadastro:*", "*:*"} {
		t.Run(wildcard, func(t *testing.T) {
			ps := bodyRuntimeProblems(t, `  - {name: PodeAdministrar, type: bool, runtime: true, source: permission, permission: "`+wildcard+`", livesOn: root, example: "false", description: Se administra.}`)
			mustBlock(t, ps, "cannot be asked about")
		})
	}
}

// TestPermissionSourceRefusesANonPermission: "administrar" is not a permission,
// and HasPermission panics on it exactly as it does on a wildcard.
func TestPermissionSourceRefusesANonPermission(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: PodeAdministrar, type: bool, runtime: true, source: permission, permission: administrar, livesOn: root, example: "false", description: Se administra.}`)
	mustBlock(t, ps, "is not a permission")
}

// TestIdentitySourcesRefuseAClaimName: naming a claim next to a source that asks
// the framework says two different things about where the value comes from, and
// the one the author did not mean would win silently.
func TestIdentitySourcesRefuseAClaimName(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: Solicitante, type: string, runtime: true, source: subject, claim: sub, livesOn: root, example: usr-1, description: Quem chamou.}`)
	mustBlock(t, ps, "looks no claim up by name")
}

// TestPermissionIsRefusedOutsideItsSource, on both sides of the pair: on another
// identity source, and on a persisted field.
func TestPermissionIsRefusedOutsideItsSource(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: Solicitante, type: string, runtime: true, source: subject, permission: "cadastro:administrar", livesOn: root, example: usr-1, description: Quem chamou.}`)
	mustBlock(t, ps, "this one is source: subject")

	ps = bodyRuntimeProblems(t, `  - {name: Apelido, type: string, column: apelido, length: 40, permission: "cadastro:administrar", livesOn: root, example: Ana, description: O apelido.}`)
	mustBlock(t, ps, "this field has a column")
}

// TestIdentitySourcesHoldTheirType. A permission is a yes/no and a subject is
// text; the wrong one generates a mapper that assigns a string to a bool three
// steps after the spec said yes.
func TestIdentitySourcesHoldTheirType(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: PodeAdministrar, type: string, length: 10, runtime: true, source: permission, permission: "cadastro:administrar", livesOn: root, example: "nao", description: Se administra.}`)
	mustBlock(t, ps, "holding a permission is a yes/no")

	ps = bodyRuntimeProblems(t, `  - {name: Solicitante, type: bool, runtime: true, source: subject, livesOn: root, example: "false", description: Quem chamou.}`)
	mustBlock(t, ps, "arrives as text")
}

// TestIdentitySourcesRefuseAValueObject: what the framework answers about the
// caller goes through no constructor of the domain's, so the feed has a raw
// string (or a bool) and nothing to build a value object out of.
func TestIdentitySourcesRefuseAValueObject(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: Solicitante, type: string, runtime: true, source: subject, vo: {kind: reuse, ref: Email}, livesOn: root, example: usr-1, description: Quem chamou.}`)
	mustBlock(t, ps, "goes through no constructor of yours")
}

// TestAClaimFieldRefusesAValueObject closes the hole this refusal was written
// for, and it is the silent kind. The declaration used to be ACCEPTED: the
// value-object type was generated, the aggregate declared the field as the plain
// scalar anyway, and the rule inside that type ran over nothing — a key that does
// nothing, reading exactly like a key that works.
//
// The reason it is refused rather than made to work: a claim is asserted by the
// issuer and already verified by the signature, so judging it here answers 422
// for a value the caller never sent and cannot fix.
func TestAClaimFieldRefusesAValueObject(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: SolicitanteEmail, type: string, runtime: true, claim: email, vo: {kind: reuse, ref: Email}, livesOn: root, example: ana@x.com, description: E-mail de quem chamou.}`)
	mustBlock(t, ps, "asserted by the ISSUER")
}

// TestABodyFieldStillTakesAValueObject is the other half, and the reason the two
// are refused separately: a source: body value IS the caller's own, the automatic
// pass validates it because that pass walks the struct, and a password
// confirmation is the case the whole spelling exists for.
func TestABodyFieldStillTakesAValueObject(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: ConfirmacaoSenha, type: string, runtime: true, source: body, vo: {kind: reuse, ref: Senha}, livesOn: root, example: "s3nh4", description: A confirmacao.}`)
	if says(ps, Blocker, "vo") {
		t.Errorf("a value object on a source: body field was refused:\n%v", ps.items)
	}
}

// TestModesIsRefusedOnAnIdentitySource: modes names the write verbs whose BODY
// carries a value, and a fact about the caller rides the bodyless verbs too —
// which is exactly where an archive guard reads it.
func TestModesIsRefusedOnAnIdentitySource(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: TemIdentidade
    type: bool
    runtime: true
    source: present
    modes: [insert]
    livesOn: root
    example: "false"
    description: Se veio identidade.`)
	mustBlock(t, ps, "fed from the caller's identity")
}

// TestTheRowScopesOwnNamesAreReserved. The resolver synthesises these onto the
// aggregate; an author who declared one would get two Go struct fields with one
// name, and a build failure with no line pointing back at the spec.
func TestTheRowScopesOwnNamesAreReserved(t *testing.T) {
	for _, name := range []string{
		"RequestingTenant", "RequestingSubject",
		"RequestingMayCrossScope", "RequestingIdentityPresent",
	} {
		t.Run(name, func(t *testing.T) {
			ps := bodyRuntimeProblems(t, `  - {name: `+name+`, type: string, runtime: true, source: subject, livesOn: root, example: usr-1, description: Quem chamou.}`)
			mustBlock(t, ps, "is a reserved field name")
		})
	}
}
