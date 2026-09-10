package handlers

import "testing"

// Fixture capturada ao vivo (`find / -xdev -maxdepth 6 ... -printf '%s\t%p\n'`) contra um pod
// Spring Boot real (imagem com Maven instalado) — confirmou que a busca sem filtro traz ~50 jars
// de dependência do Maven junto do único jar realmente relevante (/app/*.jar), motivando
// sortArchiveCandidates abaixo.
const archiveFindWithSizeFixture = "110122\t/opt/java/openjdk/lib/jrt-fs.jar\n" +
	"4467\t/usr/share/maven/lib/aopalliance-1.0.jar\n" +
	"3064793\t/usr/share/maven/lib/guava-33.6.0-jre.jar\n" +
	"82187092\t/app/tms-integration-order-tms-api.jar\n"

func TestParseArchiveCandidatesWithSize(t *testing.T) {
	got := parseArchiveCandidatesWithSize(archiveFindWithSizeFixture)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	if got[3].Path != "/app/tms-integration-order-tms-api.jar" || got[3].SizeBytes != 82187092 {
		t.Errorf("got[3] = %+v", got[3])
	}
}

func TestParseArchiveCandidatesNoSize(t *testing.T) {
	got := parseArchiveCandidatesNoSize("/app/foo.jar\n/opt/bar.war\n\n")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].SizeBytes != -1 {
		t.Errorf("SizeBytes = %d, want -1 (tamanho desconhecido, fallback BusyBox find)", got[0].SizeBytes)
	}
}

// Regressão: achado real testando ao vivo — sem essa priorização, o jar de dependência maior
// (guava, ~3MB) apareceria ANTES do jar real da aplicação (82MB) só por causa da ordem que o
// find devolveu, e o usuário teria que rolar por ~50 candidatos de sistema pra achar o certo.
func TestSortArchiveCandidates_PrioritizesApplicationJarOverSystemJars(t *testing.T) {
	candidates := parseArchiveCandidatesWithSize(archiveFindWithSizeFixture)
	sortArchiveCandidates(candidates)

	if candidates[0].Path != "/app/tms-integration-order-tms-api.jar" {
		t.Fatalf("candidates[0] = %q, want o jar da aplicação (/app/...) primeiro, mesmo não sendo o único grande", candidates[0].Path)
	}
	// Dentro do grupo "de sistema" (/opt, /usr), ainda deve ordenar por tamanho decrescente.
	for i := 1; i < len(candidates)-1; i++ {
		if candidates[i].SizeBytes < candidates[i+1].SizeBytes {
			t.Errorf("candidates[%d..%d] fora de ordem decrescente por tamanho: %d < %d",
				i, i+1, candidates[i].SizeBytes, candidates[i+1].SizeBytes)
		}
	}
}

func TestIsLikelyApplicationArchivePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/app/tms-integration-order-tms-api.jar", true},
		{"/home/app/service.jar", true},
		{"/opt/java/openjdk/lib/jrt-fs.jar", false},
		{"/usr/share/maven/lib/guava-33.6.0-jre.jar", false},
		{"/lib/foo.jar", false},
		{"/var/cache/foo.jar", false},
	}
	for _, tc := range cases {
		if got := isLikelyApplicationArchivePath(tc.path); got != tc.want {
			t.Errorf("isLikelyApplicationArchivePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// Formato Info-ZIP real de `unzip -l` (mesmo layout de colunas usado pelo unzip do BusyBox) —
// inclui cabeçalho, separadores "---", uma entrada de diretório (deve ser filtrada) e o rodapé
// "N files" (que NÃO deve virar uma entrada fantasma "files").
const unzipListFixture = `Archive:  /app/foo.jar
  Length      Date    Time    Name
---------  ---------- -----   ----
        0  2024-01-01 00:00   BOOT-INF/
      123  2024-01-01 00:00   META-INF/MANIFEST.MF
    45678  2024-01-01 00:00   BOOT-INF/classes/application.yaml
---------                     -------
    45801                     3 files
`

func TestParseUnzipListOutput(t *testing.T) {
	got := parseUnzipListOutput(unzipListFixture)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (diretório e rodapé 'N files' devem ser filtrados), got %+v", len(got), got)
	}
	if got[0].Name != "META-INF/MANIFEST.MF" || got[0].SizeBytes != 123 {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Name != "BOOT-INF/classes/application.yaml" || got[1].SizeBytes != 45678 {
		t.Errorf("got[1] = %+v", got[1])
	}
}

// Fixture real de `jar tf` (JDK), capturada ao vivo contra o mesmo jar Spring Boot — inclui
// diretórios (terminam em "/", devem ser filtrados) e o application.yaml real.
const jarTfFixture = "META-INF/\n" +
	"META-INF/MANIFEST.MF\n" +
	"org/\n" +
	"org/springframework/\n" +
	"BOOT-INF/classes/application.yaml\n" +
	"BOOT-INF/classes/br/com/grupocasasbahia/tmsintegration/TmsIntegrationApplication.class\n"

func TestParseJarTfOutput(t *testing.T) {
	got := parseJarTfOutput(jarTfFixture)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (3 entradas de arquivo, 3 diretórios filtrados), got %+v", len(got), got)
	}
	for _, e := range got {
		if e.SizeBytes != -1 {
			t.Errorf("SizeBytes = %d, want -1 (jar tf não expõe tamanho)", e.SizeBytes)
		}
	}
	if got[1].Name != "BOOT-INF/classes/application.yaml" {
		t.Errorf("got[1].Name = %q", got[1].Name)
	}
}

func TestParsePythonListOutput(t *testing.T) {
	got := parsePythonListOutput("123\tMETA-INF/MANIFEST.MF\n45678\tBOOT-INF/classes/application.yaml\n")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[1].Name != "BOOT-INF/classes/application.yaml" || got[1].SizeBytes != 45678 {
		t.Errorf("got[1] = %+v", got[1])
	}
}
