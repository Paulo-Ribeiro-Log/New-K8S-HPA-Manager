# Variáveis
BINARY_NAME=k8s-hpa-manager
MAIN_PACKAGE=.
BUILD_DIR=build
VERSION?=dev
LDFLAGS=-ldflags "-X main.version=${VERSION}"

# Comandos Go
.PHONY: build
build:
	@echo "Building ${BINARY_NAME}..."
	@mkdir -p ${BUILD_DIR}
	@go build ${LDFLAGS} -o ${BUILD_DIR}/${BINARY_NAME} ${MAIN_PACKAGE}

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

# Build de teste com layout unificado
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