package web

import (
	"io/fs"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/buildinfo"
)

// currentBuild é o Info deste processo, já com o bundle do frontend que este binário embute.
func currentBuild() buildinfo.Info {
	in := buildinfo.Get()
	if sub, err := fs.Sub(staticFiles, "static"); err == nil {
		in.FrontendBundle = buildinfo.FrontendBundle(sub)
	}
	return in
}

func buildInfoLabel() string {
	in := currentBuild()
	s := in.Label()
	if in.BuiltAt != "" {
		s += " · gerado em " + in.BuiltAt
	}
	return s
}

// healthPayload é a resposta de GET /health (sem auth). `version` continua no topo por
// compatibilidade; `build` diz qual build é: o frontend compara `build.frontend_bundle` com o JS que o
// navegador carregou e avisa quando a página é de um build anterior.
func healthPayload() gin.H {
	in := currentBuild()
	return gin.H{"status": "ok", "mode": "web", "version": in.Version, "build": in}
}
