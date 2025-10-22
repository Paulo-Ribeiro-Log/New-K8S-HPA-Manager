# Variáveis
BINARY_NAME=k8s-hpa-manager
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

# Comandos Go
.PHONY: build
build:
	@echo "Building ${BINARY_NAME} v${VERSION_CLEAN}..."
	@mkdir -p ${BUILD_DIR}
	@go build ${LDFLAGS} -o ${BUILD_DIR}/${BINARY_NAME} ${MAIN_PACKAGE}
	@echo "✅ Build complete: ./${BUILD_DIR}/${BINARY_NAME} v${VERSION_CLEAN}"

.PHONY: build-all
build-all:
	@echo "Building for multiple platforms..."
	@mkdir -p ${BUILD_DIR}
	@GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o ${BUILD_DIR}/${BINARY_NAME}-linux-amd64 ${MAIN_PACKAGE}
	@GOOS=darwin GOARCH=amd64 go build ${LDFLAGS} -o ${BUILD_DIR}/${BINARY_NAME}-darwin-amd64 ${MAIN_PACKAGE}
	@GOOS=darwin GOARCH=arm64 go build ${LDFLAGS} -o ${BUILD_DIR}/${BINARY_NAME}-darwin-arm64 ${MAIN_PACKAGE}
	@GOOS=windows GOARCH=amd64 go build ${LDFLAGS} -o ${BUILD_DIR}/${BINARY_NAME}-windows-amd64.exe ${MAIN_PACKAGE}

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
	@echo "✅ Frontend built to internal/web/static/"

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

# Build para release (múltiplas plataformas)
.PHONY: release
release:
	@echo "Creating release v${VERSION_CLEAN}..."
	@mkdir -p ${BUILD_DIR}/release
	@GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o ${BUILD_DIR}/release/${BINARY_NAME}-linux-amd64 ${MAIN_PACKAGE}
	@GOOS=darwin GOARCH=amd64 go build ${LDFLAGS} -o ${BUILD_DIR}/release/${BINARY_NAME}-darwin-amd64 ${MAIN_PACKAGE}
	@GOOS=darwin GOARCH=arm64 go build ${LDFLAGS} -o ${BUILD_DIR}/release/${BINARY_NAME}-darwin-arm64 ${MAIN_PACKAGE}
	@GOOS=windows GOARCH=amd64 go build ${LDFLAGS} -o ${BUILD_DIR}/release/${BINARY_NAME}-windows-amd64.exe ${MAIN_PACKAGE}
	@echo "✅ Release builds complete (v${VERSION_CLEAN})"
	@ls -lh ${BUILD_DIR}/release/