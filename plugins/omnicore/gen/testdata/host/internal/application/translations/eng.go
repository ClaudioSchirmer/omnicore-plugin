// The ENG catalog.
//
// It ships EMPTY: the generator inserts the keys each entity needs and never
// rewrites what is already here, so the gate exercises the insert path rather
// than only the "already populated" one.
package translations

import (
	"github.com/ClaudioSchirmer/omnicore/application/configuration"
	"github.com/ClaudioSchirmer/omnicore/application/translation"
)

type eng struct{}

func ENG() translation.Module { return eng{} }

func (eng) Language() configuration.Language { return configuration.LangENG }

func (eng) Translations() map[string]string {
	return map[string]string{}
}
