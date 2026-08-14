// The DEU catalog.
//
// It ships EMPTY: the generator inserts the keys each entity needs and never
// rewrites what is already here, so the gate exercises the insert path rather
// than only the "already populated" one.
package translations

import (
	"github.com/ClaudioSchirmer/omnicore/application/configuration"
	"github.com/ClaudioSchirmer/omnicore/application/translation"
)

type deu struct{}

func DEU() translation.Module { return deu{} }

func (deu) Language() configuration.Language { return configuration.LangDE }

func (deu) Translations() map[string]string {
	return map[string]string{}
}
