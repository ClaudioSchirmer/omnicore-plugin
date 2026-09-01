package schemas

import (
	"github.com/ClaudioSchirmer/omnicore/infra/db/core"
	appdomain "github.com/omnicore/gen-golden-host/internal/domain"
)

// SedeSchema e o que a travessia atravessa PARA DENTRO. O join precisa de duas
// coisas dele: a identidade contra a qual o predicado compara, e as colunas que
// mapeia.
func SedeSchema() *core.TableSchema {
	return core.NewTableSchema[*appdomain.Sede]("sedes").
		ID("id").
		Revision("revision").
		Field("Nome", "nome").
		Field("Codigo", "codigo").
		// A chave de onde um HOP parte: a cadeia continua da sede para a sede
		// MATRIZ dela, e a chave de um hop e coluna da tabela do alvo anterior.
		Field("MatrizID", "matriz_id")
}
