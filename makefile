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
# /dev/shm (RAM) + 12 workers = pico de ~10GB → derruba a instância WSL2.
# Sobrescrever se necessário: make build BUILD_PARALLEL=8
BUILD_PARALLEL ?= 4

# Build cache no tmpfs (/dev/shm) para evitar I/O pesado no VHD WSL2.
# /dev/shm é RAM pura — zero disco. Sem isso, 2GB+ de cache no VHD trava o disco a 100%.
# SOMENTE em ambiente local: /dev/shm tem limite ~64MB em GitHub Actions, o que esgota
# durante make release (3 cross-compilações) e causa GOTMPDIR a falhar (exit code 2).
# Limite de 900MB: um build limpo gera ~768MB de cache; threshold de 900MB permite
# reutilizar o cache em builds incrementais (só compila o que mudou) e só limpa quando
# o cache acumula além disso — prevenindo OOM sem penalizar cada build.
GOCACHE_MAX_MB := 900
ifeq ($(CI),)
GOCACHE_DIR=/dev/shm/go-build-cache
GOTMPDIR_DIR=/dev/shm/go-tmp
export GOCACHE=$(GOCACHE_DIR)
export GOTMPDIR=$(GOTMPDIR_DIR)
BUILD_CACHE_DIRS=$(GOCACHE_DIR) $(GOTMPDIR_DIR)
TRIM_CACHE=if [ -d "$(GOCACHE_DIR)" ]; then \
  CACHE_MB=$$(du -sm $(GOCACHE_DIR) 2>/dev/null | awk '{print $$1}'); \
  if [ "$${CACHE_MB:-0}" -gt $(GOCACHE_MAX_MB) ]; then \
    echo "🧹 Go cache em /dev/shm: $${CACHE_MB}MB > $(GOCACHE_MAX_MB)MB — limpando..."; \
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
.PHONY: release
release:
	@echo "Creating release v${VERSION_CLEAN}..."
	@mkdir -p ${BUILD_DIR}/release $(BUILD_CACHE_DIRS)
	@GOOS=linux GOARCH=amd64 go build -p $(BUILD_PARALLEL) ${BUILD_FLAGS} ${LDFLAGS} -o ${BUILD_DIR}/release/${BINARY_NAME}-linux-amd64 ${MAIN_PACKAGE}
	@GOOS=darwin GOARCH=amd64 go build -p $(BUILD_PARALLEL) ${BUILD_FLAGS} ${LDFLAGS} -o ${BUILD_DIR}/release/${BINARY_NAME}-darwin-amd64 ${MAIN_PACKAGE}
	@GOOS=darwin GOARCH=arm64 go build -p $(BUILD_PARALLEL) ${BUILD_FLAGS} ${LDFLAGS} -o ${BUILD_DIR}/release/${BINARY_NAME}-darwin-arm64 ${MAIN_PACKAGE}
	@echo "✅ Release builds complete (v${VERSION_CLEAN})"
	@echo "📦 Plataformas: Linux amd64, macOS Intel, macOS ARM"
	@ls -lh ${BUILD_DIR}/release/