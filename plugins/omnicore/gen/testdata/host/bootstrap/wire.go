package main

import (
	"github.com/ClaudioSchirmer/omnicore/application/translation"
	"github.com/ClaudioSchirmer/omnicore/bootstrap"
	"github.com/ClaudioSchirmer/omnicore/web/openapi"

	apptrans "github.com/omnicore/gen-golden-host/internal/application/translations"
)

// Wire is the composition root.
//
// The generator INSERTS into it: a feature line per entity it generates. The
// file starts with an empty feature list on purpose, so the gate exercises the
// insert path from scratch rather than only the "already wired" one.
func Wire(d bootstrap.Deps) bootstrap.Wiring {
	return bootstrap.Wiring{
		Translations: []translation.Module{
			apptrans.PTBR(), apptrans.ENG(), apptrans.ESP(), apptrans.FRA(),
			apptrans.DEU(), apptrans.ITA(), apptrans.NLD(),
		},
		Features: []bootstrap.Feature{},
		OpenAPI: &openapi.Config{
			Title:            "omnicore-gen golden host",
			Version:          "0.1.0",
			Description:      "Minimal service the generator is exercised against.",
			LanguageSelector: true,
		},
	}
}
