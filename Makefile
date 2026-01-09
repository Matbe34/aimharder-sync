.PHONY: build run clean test docker-build docker-run help

# Variables
BINARY_NAME=aimharder-sync
DOCKER_IMAGE=aimharder-sync
GO_VERSION=1.22

# Default target
.DEFAULT_GOAL := help

## Build commands

build: ## Build the binary
	@echo "🔨 Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME) ./cmd/main.go
	@echo "✅ Build complete: ./$(BINARY_NAME)"

build-linux: ## Build for Linux (amd64)
	@echo "🔨 Building $(BINARY_NAME) for Linux..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BINARY_NAME)-linux-amd64 ./cmd/main.go
	@echo "✅ Build complete: ./$(BINARY_NAME)-linux-amd64"

build-darwin: ## Build for macOS (arm64)
	@echo "🔨 Building $(BINARY_NAME) for macOS..."
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o $(BINARY_NAME)-darwin-arm64 ./cmd/main.go
	@echo "✅ Build complete: ./$(BINARY_NAME)-darwin-arm64"

build-all: build-linux build-darwin ## Build for all platforms

## Development commands

run: build ## Build and run with default args
	./$(BINARY_NAME) --help

deps: ## Download dependencies
	@echo "📦 Downloading dependencies..."
	go mod download
	go mod tidy
	@echo "✅ Dependencies downloaded"

lint: ## Run linter
	@echo "🔍 Running linter..."
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

fmt: ## Format code
	@echo "✨ Formatting code..."
	go fmt ./...
	@echo "✅ Code formatted"

test: ## Run tests
	@echo "🧪 Running tests..."
	go test -v ./...

clean: ## Clean build artifacts
	@echo "🧹 Cleaning..."
	rm -f $(BINARY_NAME) $(BINARY_NAME)-*
	rm -rf ./data/tcx/*.tcx
	@echo "✅ Clean complete"

## Docker commands

docker-build: ## Build Docker image
	@echo "🐳 Building Docker image..."
	docker build -t $(DOCKER_IMAGE) .
	@echo "✅ Docker image built: $(DOCKER_IMAGE)"

docker-run: docker-build ## Build and run Docker container (interactive)
	@echo "🐳 Running Docker container..."
	docker run --rm -it \
		--env-file .env \
		-v aimharder-data:/data \
		-p 8080:8080 \
		$(DOCKER_IMAGE) $(ARGS)

docker-sync: docker-build ## Run sync in Docker
	docker run --rm -it \
		--env-file .env \
		-v aimharder-data:/data \
		$(DOCKER_IMAGE) sync --days 7

docker-auth: docker-build ## Run Strava auth in Docker
	docker run --rm -it \
		--env-file .env \
		-v aimharder-data:/data \
		-p 8080:8080 \
		$(DOCKER_IMAGE) auth strava

docker-status: docker-build ## Show status in Docker
	docker run --rm -it \
		--env-file .env \
		-v aimharder-data:/data \
		$(DOCKER_IMAGE) status

docker-shell: docker-build ## Open shell in Docker container
	docker run --rm -it \
		--env-file .env \
		-v aimharder-data:/data \
		-p 8080:8080 \
		--entrypoint /bin/sh \
		$(DOCKER_IMAGE)

## Docker Compose commands

compose-build: ## Build with docker-compose
	docker-compose build

compose-sync: ## Run sync with docker-compose
	docker-compose run --rm aimharder-sync sync --days 7

compose-auth: ## Run auth with docker-compose
	docker-compose run --rm --service-ports aimharder-sync auth strava

compose-status: ## Show status with docker-compose
	docker-compose run --rm aimharder-sync status

compose-scheduled: ## Start scheduled sync service
	docker-compose --profile scheduled up -d

compose-down: ## Stop all services
	docker-compose --profile scheduled down

## Setup commands

setup: ## Initial setup
	@echo "🚀 Setting up AimHarder Sync..."
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		echo "📄 Created .env file - please edit with your credentials"; \
	else \
		echo "📄 .env file already exists"; \
	fi
	@mkdir -p data/tcx
	go mod download
	@echo "✅ Setup complete!"
	@echo ""
	@echo "Next steps:"
	@echo "  1. Edit .env with your credentials"
	@echo "  2. Run 'make build' to build the binary"
	@echo "  3. Run './$(BINARY_NAME) auth strava' to authenticate with Strava"
	@echo "  4. Run './$(BINARY_NAME) sync' to sync your workouts"

## Help

help: ## Show this help
	@echo "AimHarder Sync - Makefile Commands"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
