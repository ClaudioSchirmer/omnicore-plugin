package domain

import "github.com/ClaudioSchirmer/omnicore/domain"

// Sede e o ALVO de um read join, escrito a mao de proposito: e a forma que um
// projeto que adotou o gerador no meio do caminho tem por definicao, e a
// linguagem aceita o alvo invisivel na palavra do autor — cada campo mapeado
// declara o proprio tipo, e a funcao de schema segue a convencao do projeto.
type Sede struct {
	domain.BaseEntity
	Nome     string     `labelKey:"SedeNomeField"`
	Codigo   string     `labelKey:"SedeCodigoField"`
	MatrizID *domain.ID `labelKey:"SedeMatrizIDField"`
}

func (e *Sede) Modes() []domain.EntityMode {
	return []domain.EntityMode{domain.ModeDisplay}
}

func (e *Sede) BuildRules() {}
