# ==========================================
# WhatsApp Voice Call Service Makefile
# ==========================================

.PHONY: help dev-up dev-down dev-logs dev-reset test-db build run clean swagger swagger-install swagger-clean \
        docker-build docker-run docker-stop import-agents test lint

# Colors for output
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[1;33m
BLUE=\033[0;34m
NC=\033[0m # No Color

# Variables
BINARY_NAME=whatsapp-voice-service
DOCKER_IMAGE=whatsapp-voice-gateway
DOCKER_TAG=latest
GO_VERSION=1.24

# Default target
help:
	@echo "$(BLUE)WhatsApp Voice Call Service - Available Commands$(NC)"
	@echo ""
	@echo "$(GREEN)🏠 Local Development:$(NC)"
	@echo "  make setup           - Initial setup (database + config)"
	@echo "  make dev-up          - Start development environment (PostgreSQL)"
	@echo "  make dev-down        - Stop development environment"
	@echo "  make dev-logs        - Show logs from services"
	@echo "  make dev-reset       - Reset development environment (removes all data)"
	@echo "  make test-db         - Test database connection"
	@echo ""
	@echo "$(GREEN)🔨 Build & Run:$(NC)"
	@echo "  make build           - Build the Go service"
	@echo "  make run             - Run the Go service locally"
	@echo "  make clean           - Clean up Docker resources and binaries"
	@echo ""
	@echo "$(GREEN)🐳 Docker:$(NC)"
	@echo "  make docker-build    - Build Docker image"
	@echo "  make docker-run      - Run service in Docker (production mode)"
	@echo "  make docker-stop     - Stop Docker containers"
	@echo "  make docker-dev      - Run full dev environment in Docker"
	@echo ""
	@echo "$(GREEN)📊 Data Management:$(NC)"
	@echo "  make import-agents   - Import sample agent data"
	@echo ""
	@echo "$(GREEN)📝 Documentation:$(NC)"
	@echo "  make swagger-install - Install swag CLI tool"
	@echo "  make swagger         - Generate Swagger documentation"
	@echo "  make swagger-clean   - Remove generated Swagger docs"
	@echo ""
	@echo "$(GREEN)✅ Testing & Quality:$(NC)"
	@echo "  make test            - Run tests"
	@echo "  make lint            - Run linters"
	@echo ""

# ==========================================
# Development Environment
# ==========================================

# Initial setup for new developers
setup: dev-up
	@echo ""
	@echo "$(GREEN)🎉 Setup complete! Next steps:$(NC)"
	@echo "1. Copy example.env to .env: $(YELLOW)cp example.env .env$(NC)"
	@echo "2. Edit .env with your API keys"
	@echo "3. Import sample data: $(YELLOW)make import-agents$(NC)"
	@echo "4. Run the service: $(YELLOW)make run$(NC)"
	@echo ""

# Start development environment
dev-up:
	@echo "$(BLUE)Starting development environment (PostgreSQL)...$(NC)"
	docker-compose -f docker-compose.dev.yml up -d postgres
	@echo "$(YELLOW)Waiting for database to be ready...$(NC)"
	@sleep 5
	@echo "$(GREEN)✅ Development environment is ready!$(NC)"
	@echo "- PostgreSQL: localhost:5432"
	@echo "- Database:   voice_gateway"
	@echo "- User:       postgres"

# Stop development environment
dev-down:
	@echo "$(YELLOW)Stopping development environment...$(NC)"
	docker-compose -f docker-compose.dev.yml down
	@echo "$(GREEN)✅ Stopped$(NC)"

# Show logs
dev-logs:
	docker-compose -f docker-compose.dev.yml logs -f postgres

# Reset development environment
dev-reset:
	@echo "$(RED)⚠️  Resetting development environment (this will DELETE all data)...$(NC)"
	@read -p "Are you sure? [y/N] " -n 1 -r; \
	echo; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		docker-compose -f docker-compose.dev.yml down -v; \
		docker-compose -f docker-compose.dev.yml up -d postgres; \
		echo "$(GREEN)✅ Environment reset complete!$(NC)"; \
	else \
		echo "$(YELLOW)Cancelled.$(NC)"; \
	fi

# Test database connection
test-db:
	@echo "$(BLUE)Testing database connection...$(NC)"
	@docker exec astra-voice-postgres pg_isready -U postgres -d voice_gateway && \
		echo "$(GREEN)✅ Database connection successful!$(NC)" || \
		echo "$(RED)❌ Database connection failed!$(NC)"

# ==========================================
# Build & Run
# ==========================================

# Build the Go service
build:
	@echo "$(BLUE)Building WhatsApp Voice Service...$(NC)"
	@mkdir -p bin
	go build -o bin/$(BINARY_NAME) ./cmd/server/main.go
	@echo "$(GREEN)✅ Build complete! Binary: bin/$(BINARY_NAME)$(NC)"

# Build for production (optimized)
build-prod:
	@echo "$(BLUE)Building for production (optimized)...$(NC)"
	@mkdir -p bin
	CGO_ENABLED=1 go build \
		-ldflags="-w -s" \
		-tags netgo \
		-o bin/$(BINARY_NAME) \
		./cmd/server/main.go
	@echo "$(GREEN)✅ Production build complete!$(NC)"

# Run the Go service locally
run: build
	@echo "$(BLUE)Starting WhatsApp Voice Service...$(NC)"
	@if [ ! -f .env ]; then \
		echo "$(YELLOW)Warning: .env file not found. Copying from example.env...$(NC)"; \
		cp example.env .env; \
		echo "$(RED)Please edit .env file with your configuration before running again.$(NC)"; \
		exit 1; \
	fi
	@echo "$(GREEN)Service starting on port 8082...$(NC)"
	./bin/$(BINARY_NAME)

# Clean up Docker resources and binaries
clean:
	@echo "$(YELLOW)Cleaning up Docker resources and binaries...$(NC)"
	@docker-compose -f docker-compose.dev.yml down -v 2>/dev/null || true
	@docker-compose -f docker-compose.prod.yml down -v 2>/dev/null || true
	@rm -rf bin/
	@echo "$(GREEN)✅ Cleanup complete!$(NC)"

# ==========================================
# Docker Operations
# ==========================================

# Build Docker image
docker-build:
	@echo "$(BLUE)Building Docker image: $(DOCKER_IMAGE):$(DOCKER_TAG)$(NC)"
	@if [ -z "$$GITHUB_TOKEN" ]; then \
		echo "$(YELLOW)Warning: GITHUB_TOKEN not set. Private repos may fail.$(NC)"; \
	fi
	docker build \
		--build-arg GITHUB_TOKEN=$${GITHUB_TOKEN} \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-f Dockerfile.whatsapp-call .
	@echo "$(GREEN)✅ Docker image built successfully!$(NC)"

# Run service in Docker (production mode)
docker-run: docker-build
	@echo "$(BLUE)Starting service in Docker (production mode)...$(NC)"
	@if [ ! -f .env ]; then \
		echo "$(RED)Error: .env file not found!$(NC)"; \
		exit 1; \
	fi
	docker-compose -f docker-compose.prod.yml up -d
	@echo "$(GREEN)✅ Service started!$(NC)"
	@echo "View logs: $(YELLOW)docker-compose -f docker-compose.prod.yml logs -f$(NC)"

# Stop Docker containers
docker-stop:
	@echo "$(YELLOW)Stopping Docker containers...$(NC)"
	@docker-compose -f docker-compose.prod.yml down
	@echo "$(GREEN)✅ Stopped$(NC)"

# Run full development environment in Docker
docker-dev:
	@echo "$(BLUE)Starting full development environment in Docker...$(NC)"
	docker-compose -f docker-compose.dev.yml up -d
	@echo "$(GREEN)✅ Development environment running in Docker!$(NC)"
	@echo "View logs: $(YELLOW)docker-compose -f docker-compose.dev.yml logs -f$(NC)"

# ==========================================
# Data Management
# ==========================================

# Import sample agent data
import-agents:
	@echo "$(BLUE)Importing sample agent data...$(NC)"
	@if [ ! -f .env ]; then \
		echo "$(RED)Error: .env file not found!$(NC)"; \
		exit 1; \
	fi
	@./scripts/run-import-agents.sh
	@echo "$(GREEN)✅ Agents imported successfully!$(NC)"

# ==========================================
# Swagger Documentation
# ==========================================

# Install swag CLI tool
swagger-install:
	@echo "$(BLUE)Installing swag CLI tool...$(NC)"
	@go install github.com/swaggo/swag/cmd/swag@latest
	@echo "$(GREEN)✅ swag installed successfully!$(NC)"
	@echo "$(YELLOW)💡 Make sure $$(go env GOPATH)/bin is in your PATH$(NC)"

# Generate Swagger documentation
swagger:
	@echo "$(BLUE)📝 Generating Swagger documentation...$(NC)"
	@if ! command -v swag &> /dev/null; then \
		echo "$(YELLOW)⚠️  swag not found. Installing...$(NC)"; \
		make swagger-install; \
	fi
	@./scripts/generate-swagger.sh
	@echo ""
	@echo "$(GREEN)✅ Swagger documentation generated!$(NC)"
	@echo "📚 View docs at: $(BLUE)http://localhost:8082/swagger/index.html$(NC) (after starting the service)"

# Clean generated Swagger docs
swagger-clean:
	@echo "$(YELLOW)🗑️  Removing generated Swagger docs...$(NC)"
	@rm -rf docs/swagger.json docs/swagger.yaml docs/docs.go
	@echo "$(GREEN)✅ Swagger docs cleaned!$(NC)"

# ==========================================
# Testing & Quality
# ==========================================

# Run tests
test:
	@echo "$(BLUE)Running tests...$(NC)"
	go test -v ./internal/... ./pkg/...
	@echo "$(GREEN)✅ Tests completed!$(NC)"

# Run tests with coverage
test-coverage:
	@echo "$(BLUE)Running tests with coverage...$(NC)"
	go test -v -coverprofile=coverage.out ./internal/... ./pkg/...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)✅ Coverage report generated: coverage.html$(NC)"

# Run linters
lint:
	@echo "$(BLUE)Running linters...$(NC)"
	@if ! command -v golangci-lint &> /dev/null; then \
		echo "$(YELLOW)golangci-lint not found. Install from: https://golangci-lint.run/usage/install/$(NC)"; \
		exit 1; \
	fi
	golangci-lint run ./internal/... ./pkg/...
	@echo "$(GREEN)✅ Linting completed!$(NC)"

# ==========================================
# Utilities
# ==========================================

# Show current Go version
go-version:
	@go version

# Check environment setup
check-env:
	@echo "$(BLUE)Checking environment...$(NC)"
	@echo "Go version: $$(go version)"
	@echo "Docker version: $$(docker --version)"
	@echo "Docker Compose version: $$(docker-compose --version)"
	@if [ -f .env ]; then \
		echo "$(GREEN)✅ .env file exists$(NC)"; \
	else \
		echo "$(RED)❌ .env file not found$(NC)"; \
	fi
	@if docker ps | grep -q astra-voice-postgres; then \
		echo "$(GREEN)✅ PostgreSQL container running$(NC)"; \
	else \
		echo "$(YELLOW)⚠️  PostgreSQL container not running$(NC)"; \
	fi

