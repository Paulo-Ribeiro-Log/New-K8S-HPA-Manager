// Package dbfile cuida das permissões dos arquivos SQLite da aplicação.
//
// Os bancos em ~/.k8s-hpa-manager guardam credenciais, tokens, manifestos e histórico. O SQLite
// os cria com a permissão padrão do sistema (0644), legível por qualquer usuário da máquina. O
// pacote não depende de nenhum outro do projeto para poder ser importado de qualquer lugar sem
// criar ciclo de imports.
package dbfile

import (
	"os"
	"strings"
)

// Restrict deixa o banco legível e gravável só pelo dono (0600). Chame antes de sql.Open: o SQLite
// cria -wal e -shm com a permissão do banco, então criar o arquivo já 0600 cobre os três. Em
// instalações existentes, o chmod corrige o que estava 0644, inclusive -wal e -shm.
//
// O dono continua lendo e gravando normalmente (é o mesmo usuário que roda o servidor). Bancos em
// memória e URIs "file:" são ignorados. Falhas de permissão não derrubam a aplicação: o banco só
// continua com a permissão que tinha.
func Restrict(dbPath string) {
	if dbPath == "" || strings.HasPrefix(dbPath, ":") || strings.HasPrefix(dbPath, "file:") || strings.Contains(dbPath, "mode=memory") {
		return
	}
	if f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_RDWR, 0600); err == nil {
		f.Close()
	}
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if _, err := os.Stat(p); err == nil {
			_ = os.Chmod(p, 0600)
		}
	}
}
