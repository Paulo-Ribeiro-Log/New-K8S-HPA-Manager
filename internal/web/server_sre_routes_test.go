package web

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Guarda de regressão do roteamento: lê o server.go e confere que as rotas que executam coisas
// ficam atrás de RequireSREGroup(). Hoje o filtro é um no-op (middleware/rbac.go), mas o roteamento
// precisa estar certo para quando o RBAC de grupo for reativado. Já houve rota de shell e de envio
// de mensagens sem o filtro.

func lerServerGo(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("ler server.go: %v", err)
	}
	return string(b)
}

// Rotas que alteram algo no Teams Broadcast (enviar, apagar, escanear, buscar mensagem, gravar em
// disco) precisam do filtro. As de leitura (GET) ficam de fora.
func TestTeamsBroadcastRotasDeEfeitoExigemSRE(t *testing.T) {
	re := regexp.MustCompile(`teamsGroup\.(POST|PUT|PATCH|DELETE)\("(/broadcast/[^"]*)"`)
	achadas := 0
	for _, linha := range strings.Split(lerServerGo(t), "\n") {
		m := re.FindStringSubmatch(linha)
		if m == nil {
			continue
		}
		achadas++
		if !strings.Contains(linha, "RequireSREGroup()") {
			t.Errorf("rota %s %s sem RequireSREGroup(): %s", m[1], m[2], strings.TrimSpace(linha))
		}
	}
	if achadas < 6 {
		t.Fatalf("achei só %d rotas de efeito do broadcast; o teste ficou desatualizado em relação ao server.go", achadas)
	}
}

// Todo grupo WebSocket que abre um shell (pod, debug, tshoot, node, VM por SSH ou SSM, terminal do
// Code Editor) precisa de `grupo.Use(rbacMiddleware.RequireSREGroup())`.
func TestGruposDeShellWebSocketExigemSRE(t *testing.T) {
	src := lerServerGo(t)
	re := regexp.MustCompile(`(\w+)\.GET\("[^"]*",\s*\w+\.(HandleShell|HandleDebug|HandleTshoot|HandleNodeShell|HandleTerminal|HandleSSHTerminal|HandleSSMTerminal)\)`)
	grupos := map[string][]string{}
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		grupos[m[1]] = append(grupos[m[1]], m[2])
	}
	if len(grupos) < 5 {
		t.Fatalf("achei só %d grupos de shell (%v); o teste ficou desatualizado em relação ao server.go", len(grupos), grupos)
	}
	for grupo, handlers := range grupos {
		if !strings.Contains(src, grupo+".Use(rbacMiddleware.RequireSREGroup())") {
			t.Errorf("o grupo %s abre shell (%s) mas não tem %s.Use(rbacMiddleware.RequireSREGroup())", grupo, strings.Join(handlers, ", "), grupo)
		}
	}
}
