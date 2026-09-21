package storage

import (
	"database/sql"
	"fmt"
)

// finopsDataVersion identifica a semântica dos dados FinOps persistidos (relatório em cache e
// snapshots de Rightsizing). Incrementar quando a MEDIDA gravada mudar de forma incompatível.
//
//	1 (atual): request/limit de workload são POR POD, e a demanda por node pool = valor por pod × pods.
//	0 (antes): request/limit eram a SOMA de todos os pods, comparados com o uso de UM pod — o
//	           desperdício e a utilização por pool saíam inflados/deflacionados ~N× (bug do frete-hub).
const finopsDataVersion = 1

// purgeFinOpsDataIfStale apaga as linhas das tabelas informadas quando o banco foi gravado com uma
// semântica anterior a `finopsDataVersion` (controle via PRAGMA user_version, sem tabela extra).
// São dados DERIVADOS e regeneráveis (um novo "Analisar" refaz tudo), então descartar é melhor que
// continuar exibindo números calculados com a fórmula errada. Roda uma única vez por banco: depois
// que user_version alcança a versão atual, não faz mais nada. Os nomes de tabela vêm só de
// constantes do pacote (nunca de entrada externa), por isso a interpolação direta no SQL.
func purgeFinOpsDataIfStale(db *sql.DB, tables ...string) error {
	var current int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("ler user_version: %w", err)
	}
	if current >= finopsDataVersion {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, t := range tables {
		if _, err := tx.Exec(`DELETE FROM ` + t); err != nil {
			return fmt.Errorf("limpar %s: %w", t, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// PRAGMA não aceita parâmetro; o valor é uma constante inteira.
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, finopsDataVersion)); err != nil {
		return fmt.Errorf("gravar user_version: %w", err)
	}
	return nil
}
