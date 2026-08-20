package vos

import "github.com/ClaudioSchirmer/omnicore/domain"

// ChavePermissao is the hand-written half of the spec beside this directory: a
// composite declared with written: manual, whose shape the generator knows and
// whose FILE it never writes.
type ChavePermissao struct {
	Recurso string `labelKey:"ChavePermissaoRecursoField"`
	Acao    string `labelKey:"ChavePermissaoAcaoField"`
}

// IsValid carries the invariant no key of the spec language states: one part's
// admissible values depend on the OTHER part's value.
//
// It is also the proof that the bargain holds — the framework finds this by TYPE
// with no registration, and there is deliberately no Value(): its absence is
// what tells the schema to decompose the concept across two columns.
func (k ChavePermissao) IsValid(fieldName string, ctx *domain.NotificationContext) bool {
	ok := true
	if k.Recurso == "" {
		ctx.AddNotification("Recurso", domain.RequiredFieldNotification{})
		ok = false
	}
	if k.Acao == "" {
		ctx.AddNotification("Acao", domain.RequiredFieldNotification{})
		ok = false
	}
	if k.Recurso == "*" && k.Acao != "*" {
		ctx.AddNotification("Acao", domain.SchemaViolationNotification{})
		ok = false
	}
	return ok
}

// String renders the concept under a name that is not Value(), which is the
// other half of what written: manual buys.
func (k ChavePermissao) String() string { return k.Recurso + ":" + k.Acao }
