# ═══════════════════════════════════════════════════════════════════════════════
# Makefile - AWS API Gateway HTTP API v2 Local Proxy
# ═══════════════════════════════════════════════════════════════════════════════

.PHONY: build run test clean docker docker-run docker-compose help

# Variáveis
BINARY_NAME=proxy
DOCKER_IMAGE=api-gateway-proxy
GO_VERSION=1.21

# ─────────────────────────────────────────────────────────────────────────────────
# Build
# ─────────────────────────────────────────────────────────────────────────────────

## build: Compila o binário para a plataforma atual
build:
	@echo "🔨 Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME) main.go
	@echo "✅ Build complete: $(BINARY_NAME)"

## build-linux: Compila para Linux AMD64
build-linux:
	@echo "🔨 Building for Linux AMD64..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o $(BINARY_NAME)-linux-amd64 main.go
	@echo "✅ Build complete: $(BINARY_NAME)-linux-amd64"

## build-linux-arm: Compila para Linux ARM64 (Graviton)
build-linux-arm:
	@echo "🔨 Building for Linux ARM64..."
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-w -s" -o $(BINARY_NAME)-linux-arm64 main.go
	@echo "✅ Build complete: $(BINARY_NAME)-linux-arm64"

## build-windows: Compila para Windows
build-windows:
	@echo "🔨 Building for Windows..."
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-w -s" -o $(BINARY_NAME).exe main.go
	@echo "✅ Build complete: $(BINARY_NAME).exe"

## build-all: Compila para todas as plataformas
build-all: build-linux build-linux-arm build-windows
	@echo "✅ All builds complete"

# ─────────────────────────────────────────────────────────────────────────────────
# Run
# ─────────────────────────────────────────────────────────────────────────────────

## run: Executa o proxy localmente
run: build
	@echo "🚀 Starting proxy..."
	./$(BINARY_NAME)

## run-debug: Executa com modo debug habilitado
run-debug: build
	@echo "🚀 Starting proxy in debug mode..."
	DEBUG=true ./$(BINARY_NAME)

## run-example: Executa com configuração de exemplo (Keycloak-like)
run-example: build
	@echo "🚀 Starting proxy with example config..."
	ROUTE_TEMPLATE="/identity/v1/realms/{realm}/protocol/openid-connect/token" \
	DEBUG=true \
	./$(BINARY_NAME)

# ─────────────────────────────────────────────────────────────────────────────────
# Test
# ─────────────────────────────────────────────────────────────────────────────────

## test: Executa testes
test:
	@echo "🧪 Running tests..."
	go test -v ./...

## test-cover: Executa testes com coverage
test-cover:
	@echo "🧪 Running tests with coverage..."
	go test -v -coverprofile=coverage.txt ./...
	go tool cover -html=coverage.txt -o coverage.html
	@echo "✅ Coverage report: coverage.html"

# ─────────────────────────────────────────────────────────────────────────────────
# Docker
# ─────────────────────────────────────────────────────────────────────────────────

## docker: Build da imagem Docker
docker:
	@echo "🐳 Building Docker image..."
	docker build -t $(DOCKER_IMAGE) .
	@echo "✅ Docker image built: $(DOCKER_IMAGE)"

## docker-run: Executa container Docker
docker-run: docker
	@echo "🐳 Running Docker container..."
	docker run -p 3000:3000 \
		-e LAMBDA_INVOKE_URL=http://host.docker.internal:9000/2015-03-31/functions/function/invocations \
		-e DEBUG=true \
		$(DOCKER_IMAGE)

## docker-compose: Inicia stack completa via Docker Compose
docker-compose:
	@echo "🐳 Starting Docker Compose stack..."
	docker-compose up -d
	@echo "✅ Stack started. Logs: docker-compose logs -f"

## docker-compose-down: Para stack Docker Compose
docker-compose-down:
	@echo "🐳 Stopping Docker Compose stack..."
	docker-compose down
	@echo "✅ Stack stopped"

# ─────────────────────────────────────────────────────────────────────────────────
# Clean
# ─────────────────────────────────────────────────────────────────────────────────

## clean: Remove binários e arquivos temporários
clean:
	@echo "🧹 Cleaning..."
	rm -f $(BINARY_NAME) $(BINARY_NAME)-* $(BINARY_NAME).exe
	rm -f coverage.txt coverage.html
	@echo "✅ Clean complete"

# ─────────────────────────────────────────────────────────────────────────────────
# Help
# ─────────────────────────────────────────────────────────────────────────────────

## help: Mostra este help
help:
	@echo ""
	@echo "AWS API Gateway HTTP API v2 - Local Proxy"
	@echo ""
	@echo "Usage:"
	@echo "  make <target>"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
	@echo ""

# Default target
.DEFAULT_GOAL := help
