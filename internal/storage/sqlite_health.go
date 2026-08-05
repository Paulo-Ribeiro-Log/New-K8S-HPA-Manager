package storage

import (
	"database/sql"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// CheckSQLiteDriverWorks tenta abrir um banco em memória e fazer um Ping — detecta cedo, com uma
// mensagem clara, o caso do binário ter sido compilado com CGO_ENABLED=0 (cross-compilation pra
// macOS sem toolchain C real, ex: `make release`/`make build-all` rodado num host Linux/WSL2 sem
// CGO cross pra darwin). Nesse caso o driver "sqlite3" ainda REGISTRA e o binário COMPILA
// normalmente (vendor/github.com/mattn/go-sqlite3/static_mock.go, ativado por `//go:build !cgo`),
// mas toda operação real de banco falha em runtime com uma mensagem de stub.
//
// Sem essa checagem centralizada, isso se manifestava como dezenas de sintomas desconexos e
// confusos, cada um dos ~14 stores SQLite independentes (notes_store, user_ai_tokens/Dynatrace,
// github_tokens, predictions, health check, etc.) falhando silenciosamente por conta própria: aba
// Notas "carrega e depois falha" sem salvar nada, "tokens store não configurado" ao salvar o
// token do Dynatrace, "API not found" ao salvar o PAT do GitHub — sem nenhum diagnóstico unificado
// apontando pra causa real.
func CheckSQLiteDriverWorks() error {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return err
	}
	defer db.Close()
	return db.Ping()
}

// IsCGOStubError detecta especificamente o erro do driver stub `!cgo` do mattn/go-sqlite3 —
// permite uma mensagem de diagnóstico específica em vez de um "SQLite indisponível" genérico
// que não aponta pra causa raiz (binário compilado sem CGO).
func IsCGOStubError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "requires cgo to work")
}
