package emit

import (
	"fmt"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// emitBootstrap writes the feature: the single place where this aggregate's
// repository and view are built, and the one-line delegation that mounts it.
//
// Route bodies never appear here. A feature that grows route registrations has
// leaked the web layer into the composition root, which is the most common
// structural mistake in a service like this.
func emitBootstrap(m *ir.Model) ([]fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package main")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote(fwImport("bootstrap")))
	s.L("\t%s", quote(fwImport("infra/db/query")))
	s.L("\tappinfra %s", quote(m.ImportPath("internal/infra")))
	s.L("\tappviews %s", quote(m.ImportPath("internal/infra/views")))
	s.L("\tappweb %s", quote(m.ImportPath("internal/web")))
	s.L("\tfwgraphql %s", quote(fwImport("web/graphql")))
	s.L("\t%s", quote("github.com/gofiber/fiber/v3"))
	s.L(")")
	s.Blank()

	feature := m.Entity.PluralPascal + "Feature"
	s.Doc(fmt.Sprintf("%s holds the %s repository and view, built once.",
		feature, m.Entity.Camel))
	s.L("type %s struct {", feature)
	s.L("\trepo *appinfra.%sRepository", m.Entity.Pascal)
	if m.Service != nil {
		s.L("\tsvc  *appinfra.%s", m.Service.Impl)
	}
	if m.Read.Enabled {
		s.L("\tview %s", viewType(m))
	}
	s.L("}")
	s.Blank()

	s.L("func New%s(d bootstrap.Deps) *%s {", feature, feature)
	s.L("\trepo := appinfra.New%sRepository(d.DB)", m.Entity.Pascal)
	if m.Service != nil {
		s.L("\tsvc := appinfra.New%s(repo)", m.Service.Impl)
	}
	if m.Read.Enabled {
		if m.Read.Backing == "relational" {
			s.L("\t// The view reads through the SAME loader as the repository. Building a")
			s.L("\t// second loader for the same aggregate works and is pure waste — and the")
			s.L("\t// loader is what carries the schema, so there is no shape to restate.")
			s.L("\treturn &%s{repo: repo,%s view: appviews.%sView(repo.Loader)}", feature, svcField(m), m.Entity.Pascal)
		} else {
			s.L("\treturn &%s{repo: repo,%s view: appviews.%sView()}", feature, svcField(m), m.Entity.Pascal)
		}
	} else {
		s.L("\treturn &%s{repo: repo,%s}", feature, svcField(m))
	}
	s.L("}")
	s.Blank()

	if m.Read.Enabled {
		if m.Read.Backing == "relational" {
			s.Doc("RelationalViews contributes this aggregate's read model through "+
				"bootstrap.RelationalReadableFeature — the sibling seam of Views().",
				"",
				"It is a different seam because it is a different KIND of read model: "+
					"nothing here is materialised, so the sync engine, the drift detector "+
					"and the rebuild have nothing to receive.")
			s.L("func (f *%s) RelationalViews() []*query.RelationalViewDefinition {", feature)
			s.L("\treturn []*query.RelationalViewDefinition{f.view}")
		} else {
			s.Doc("Views contributes this aggregate's projection to the framework's sync engine.")
			s.L("func (f *%s) Views() []*query.ViewDefinition {", feature)
			s.L("\treturn []*query.ViewDefinition{f.view}")
		}
		s.L("}")
		s.Blank()
	}

	s.Doc("Mount delegates to the web layer. It stays one line by design.")
	s.L("func (f *%s) Mount(app *fiber.App, d bootstrap.Deps) {", feature)
	if m.Read.Enabled {
		s.L("\tappweb.Mount%s(app, f.repo,%s f.view, d)", m.Entity.PluralPascal, mountSvcArg(m))
	} else {
		s.L("\tappweb.Mount%s(app, f.repo,%s nil, d)", m.Entity.PluralPascal, mountSvcArg(m))
	}
	s.L("}")

	if m.Surfaces.GraphQL {
		s.Blank()
		s.Doc(
			"MountGraphQL opts this feature into the GraphQL surface.",
			"",
			"The framework discovers the method by type assertion and builds the single "+
				"shared registry itself — nothing about GraphQL is wired in the composition "+
				"root.",
		)
		s.L("func (f *%s) MountGraphQL(reg *fwgraphql.Registry, d bootstrap.Deps) {", feature)
		if m.Read.Enabled {
			s.L("\tappweb.Mount%sGraphQL(reg, f.repo,%s f.view, d)", m.Entity.PluralPascal, mountSvcArg(m))
		} else {
			s.L("\tappweb.Mount%sGraphQL(reg, f.repo,%s nil, d)", m.Entity.PluralPascal, mountSvcArg(m))
		}
		s.L("}")
	}

	// Named for the AGGREGATE, singular. One feature per aggregate, so the file
	// is named after the thing it assembles — the plural belongs to the route
	// group and to the identity feature, which is a read surface over MANY of
	// them rather than one aggregate's wiring.
	f, err := goFile("bootstrap/"+m.Entity.Snake+"_feature.go", fsplan.Owned,
		fmt.Sprintf("the %s feature (repository + view + mount)", m.Entity.PluralCamel), s)
	if err != nil {
		return nil, err
	}
	return []fsplan.File{f}, nil
}

func svcField(m *ir.Model) string {
	if m.Service == nil {
		return ""
	}
	return " svc: svc,"
}

func mountSvcArg(m *ir.Model) string {
	if m.Service == nil {
		return ""
	}
	return " f.svc,"
}

// viewType is the Go type of the read model this backing declares.
//
// The two are unrelated types on purpose: a relational read model carries no
// version, no collection and no Mongo spec, so the projection machinery — which
// takes *query.ViewDefinition concretely — cannot be handed one by accident.
func viewType(m *ir.Model) string {
	if m.Read.Backing == "relational" {
		return "*query.RelationalViewDefinition"
	}
	return "*query.ViewDefinition"
}
