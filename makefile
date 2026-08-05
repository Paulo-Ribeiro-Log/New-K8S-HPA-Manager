# Variáveis
BINARY_NAME=new-k8s-hpa
MAIN_PACKAGE=.
BUILD_DIR=build

# Detectar versão automaticamente:
# 1. Tenta pegar git tag (ex: v1.5.0)
# 2. Se não existir tag, usa "dev-<short-commit>"
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Remover prefixo "v" da versão (v1.5.0 → 1.5.0)
VERSION_CLEAN := $(shell echo $(VERSION) | sed 's/^v//')

# LDFlags para injetar versão no binário
LDFLAGS=-ldflags "-X k8s-hpa-manager/internal/updater.Version=${VERSION_CLEAN}"

# Build flags
BUILD_FLAGS=-mod=vendor

# Limita workers paralelos do compilador Go para evitar OOM no WSL2.
# Com VS Code + Chrome + servidor rodando, cada worker usa ~400MB no pico do linker.
# 2 workers = pico ~1.5GB adicional — seguro com 7-10GB disponíveis no WSL2.
# Sobrescrever se necessário: make build BUILD_PARALLEL=4
BUILD_PARALLEL ?= 2

# GOCACHE no disco (VHD ext4), não em /dev/shm.
# /dev/shm é RAM pura — com 7.6GB de WSL2 e VS Code+Chrome+servidor já consumindo ~4.5GB,
# colocar cache+tmp lá durante o build empurra o pico para ~8GB+ → OOM → WSL crashando.
# O VHD do WSL2 tem I/O adequado para builds incrementais (apenas arquivos modificados).
# SOMENTE em ambiente local: CI usa GOCACHE padrão do runner (sem variável de ambiente).
GOCACHE_MAX_MB := 1500
ifeq ($(CI),)
GOCACHE_DIR=$(HOME)/.cache/go-build-wsl
GOTMPDIR_DIR=/tmp/go-tmp
export GOCACHE=$(GOCACHE_DIR)
export GOTMPDIR=$(GOTMPDIR_DIR)
BUILD_CACHE_DIRS=$(GOCACHE_DIR) $(GOTMPDIR_DIR)
TRIM_CACHE=if [ -d "$(GOCACHE_DIR)" ]; then \
  CACHE_MB=$$(du -sm $(GOCACHE_DIR) 2>/dev/null | awk '{print $$1}'); \
  if [ "$${CACHE_MB:-0}" -gt $(GOCACHE_MAX_MB) ]; then \
    echo "🧹 Go cache: $${CACHE_MB}MB > $(GOCACHE_MAX_MB)MB — limpando..."; \
    go clean -cache; \
  fi; \
fi
else
BUILD_CACHE_DIRS=
TRIM_CACHE=true
endif

# Comandos Go
.PHONY: build
build:
	@echo "Building ${BINARY_NAME} v${VERSION_CLEAN}..."
	@mkdir -p ${BUILD_DIR} $(BUILD_CACHE_DIRS)
	@$(TRIM_CACHE)
	@go build -p $(BUILD_PARALLEL) ${BUILD_FLAGS} ${LDFLAGS} -o ${BUILD_DIR}/${BINARY_NAME} ${MAIN_PACKAGE}
	@echo "✅ Build complete: ./${BUILD_DIR}/${BINARY_NAME} v${VERSION_CLEAN}"

.PHONY: build-all
build-all:
	@echo "Building for multiple platforms..."
	@echo "⚠️  ATENÇÃO: os binários darwin gerados por este target NÃO são seguros pra distribuir"
	@echo "   se rodado num host Linux/WSL2 — sem toolchain C pra macOS, o Go cross-compila com"
	@echo "   CGO_ENABLED=0 por padrão, e o driver SQLite (mattn/go-sqlite3) cai num stub que"
	@echo "   COMPILA normalmente mas quebra TODA a persistência em runtime (Notas, tokens de"
	@echo "   IA/Dynatrace/GitHub, predictions, etc. — ver internal/storage/sqlite_health.go)."
	@echo "   Use só pra smoke-test de compilação local. Pra release de verdade, use o workflow"
	@echo "   .github/workflows/release.yml (builda macOS em runners nativos)."
	@mkdir -p ${BUILD_DIR} $(BUILD_CACHE_DIRS)
	@GOOS=linux GOARCH=amd64 go build -p $(BUILD_PARALLEL) ${BUILD_FLAGS} ${LDFLAGS} -o ${BUILD_DIR}/${BINARY_NAME}-linux-amd64 ${MAIN_PACKAGE}
	@GOOS=darwin GOARCH=amd64 go build -p $(BUILD_PARALLEL) ${BUILD_FLAGS} ${LDFLAGS} -o ${BUILD_DIR}/${BINARY_NAME}-darwin-amd64 ${MAIN_PACKAGE}
	@GOOS=darwin GOARCH=arm64 go build -p $(BUILD_PARALLEL) ${BUILD_FLAGS} ${LDFLAGS} -o ${BUILD_DIR}/${BINARY_NAME}-darwin-arm64 ${MAIN_PACKAGE}

.PHONY: run
run: build
	@echo "Running ${BINARY_NAME}..."
	@./${BUILD_DIR}/${BINARY_NAME}

.PHONY: run-dev
run-dev:
	@echo "Running in development mode..."
	@go run ${MAIN_PACKAGE} --debug

.PHONY: test
test:
	@echo "Running tests..."
	@go test -v ./...

.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -v -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html

# ============================================================================
# Frontend Web (React/TypeScript)
# ============================================================================

.PHONY: web-install
web-install:
	@echo "Installing frontend dependencies..."
	@cd internal/web/frontend && npm install

.PHONY: web-dev
web-dev:
	@echo "Starting frontend dev server (Vite)..."
	@echo "Frontend: http://localhost:5173"
	@echo "Backend:  http://localhost:8080 (start separately)"
	@cd internal/web/frontend && npm run dev

.PHONY: web-build
web-build:
	@echo "Building frontend for production..."
	@cd internal/web/frontend && npm run build
	@echo "Cleaning old assets from internal/web/static/..."
	@rm -rf internal/web/static/assets internal/web/static/index.html
	@echo "Copying fresh build from dist to internal/web/static/..."
	@cp -r internal/web/frontend/dist/* internal/web/static/
	@echo "✅ Frontend built and copied to internal/web/static/"
	@echo ""
	@echo "📦 Assets verificados:"
	@ls -lh internal/web/static/assets/ | grep -E "\.(js|css)$$" || true
	@echo ""
	@echo "📄 Index.html references:"
	@grep -E "index-.*\.(js|css)" internal/web/static/index.html || true

.PHONY: web-clean
web-clean:
	@echo "Cleaning frontend build..."
	@rm -rf internal/web/static/*
	@touch internal/web/static/.gitkeep

# Build completo (Go + Frontend)
.PHONY: build-web
build-web: web-build build
	@echo "✅ Full build complete (Frontend + Backend)"

# ============================================================================
# Build de teste com layout unificado
# ============================================================================

.PHONY: build-test
build-test:
	@echo "Building k8s-teste (layout test)..."
	@mkdir -p ${BUILD_DIR}
	@go build -o ${BUILD_DIR}/k8s-teste ./cmd/k8s-teste

.PHONY: run-test
run-test: build-test
	@echo "Running k8s-teste..."
	@./${BUILD_DIR}/k8s-teste

.PHONY: run-test-debug
run-test-debug: build-test
	@echo "Running k8s-teste with debug..."
	@./${BUILD_DIR}/k8s-teste --debug

# Mostrar versão detectada
.PHONY: version
version:
	@echo "Versão detectada: ${VERSION_CLEAN}"
	@echo "Git tag: $(shell git describe --tags 2>/dev/null || echo 'nenhuma')"
	@echo "Commit: $(shell git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"

# Build para release (múltiplas plataformas - Linux e macOS apenas)
#
# ⚠️  ATENÇÃO — NÃO USE ESTE TARGET PARA PUBLICAR UMA RELEASE se estiver rodando num host
# Linux/WSL2 (o ambiente de desenvolvimento padrão deste projeto): os binários darwin-amd64 e
# darwin-arm64 saem com CGO_ENABLED=0 (nenhum toolchain C pra macOS disponível aqui), e o driver
# SQLite (mattn/go-sqlite3) cai num stub que COMPILA normalmente mas quebra silenciosamente TODA
# a persistência em runtime — Notas, tokens de IA/Dynatrace/GitHub, predictions, health check,
# etc. (ver internal/storage/sqlite_health.go, e a seção correspondente no CLAUDE.md). O binário
# sobe, a UI carrega, mas cada feature que depende de SQLite falha com uma mensagem desconexa
# ("tokens store não configurado", "API not found", Notas "carrega e falha") sem apontar pra
# causa real. Já aconteceu de um release real sair quebrado assim.
#
# Use este target só pra smoke-test de compilação local (confirma que o código compila pras 3
# plataformas). Para gerar binários de release de verdade, use o workflow
# .github/workflows/release.yml (Actions → Release → Run workflow) — ele builda macOS em runners
# macos-14 (Apple Silicon nativo + cross-arch pra amd64 via clang universal do Xcode; macos-13
# Intel tem fila de runner instável no GitHub Actions, evitado por esse motivo), com CGO_ENABLED=1
# de verdade.
.PHONY: release
release:
	@echo "Creating release v${VERSION_CLEAN}..."
	@echo "⚠️  Rodando localmente? Os binários darwin daqui podem sair com SQLite quebrado — veja o comentário deste target no makefile."
	@mkdir -p ${BUILD_DIR}/release $(BUILD_CACHE_DIRS)
	@GOOS=linux GOARCH=amd64 go build -p $(BUILD_PARALLEL) ${BUILD_FLAGS} ${LDFLAGS} -o ${BUILD_DIR}/release/${BINARY_NAME}-linux-amd64 ${MAIN_PACKAGE}
	@GOOS=darwin GOARCH=amd64 go build -p $(BUILD_PARALLEL) ${BUILD_FLAGS} ${LDFLAGS} -o ${BUILD_DIR}/release/${BINARY_NAME}-darwin-amd64 ${MAIN_PACKAGE}
	@GOOS=darwin GOARCH=arm64 go build -p $(BUILD_PARALLEL) ${BUILD_FLAGS} ${LDFLAGS} -o ${BUILD_DIR}/release/${BINARY_NAME}-darwin-arm64 ${MAIN_PACKAGE}
	@echo "✅ Release builds complete (v${VERSION_CLEAN})"
	@echo "📦 Plataformas: Linux amd64, macOS Intel, macOS ARM"
	@ls -lh ${BUILD_DIR}/release/

# Build de release para UMA plataforma só, lida do ambiente (GOOS/GOARCH/CGO_ENABLED já setados
# pelo chamador) — usado pelo workflow release.yml, que roda cada plataforma num runner separado
# (ubuntu-latest pra linux, macos-14 pra darwin — nativo arm64 e cross-arch amd64), garantindo
# CGO_ENABLED=1 real em vez do stub silencioso que `release`/`build-all` produzem pra darwin
# quando rodados aqui.
.PHONY: release-single
release-single:
	@if [ -z "$(GOOS)" ] || [ -z "$(GOARCH)" ]; then echo "❌ defina GOOS e GOARCH no ambiente antes de chamar este target"; exit 1; fi
	@mkdir -p ${BUILD_DIR}/release $(BUILD_CACHE_DIRS)
	@echo "Building ${BINARY_NAME}-$(GOOS)-$(GOARCH) v${VERSION_CLEAN} (CGO_ENABLED=$${CGO_ENABLED:-<padrão do Go>})..."
	@go build -p $(BUILD_PARALLEL) ${BUILD_FLAGS} ${LDFLAGS} -o ${BUILD_DIR}/release/${BINARY_NAME}-$(GOOS)-$(GOARCH) ${MAIN_PACKAGE}
	@echo "✅ ${BUILD_DIR}/release/${BINARY_NAME}-$(GOOS)-$(GOARCH)"