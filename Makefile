.PHONY: build test clean lint fmt vet run dev docker docker-push help deps check

# Build variables
BINARY_NAME=ems
BUILD_DIR=./bin
DOCKER_IMAGE=energy-management-system
DOCKER_TAG=latest
PLATFORMS=linux/amd64,linux/arm64,linux/arm/v7

# Go variables
GO_FILES=$(shell find . -name '*.go' -not -path './vendor/*')
GO_PACKAGES=$(shell go list ./...)

# Default target
all: check build

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Development targets
deps: ## Download and tidy dependencies
	go mod download
	go mod tidy

fmt: ## Format Go source files
	gofmt -s -w $(GO_FILES)
	go mod tidy

vet: ## Run go vet
	go vet $(GO_PACKAGES)

lint: ## Run golint (requires golint installed)
	@which golint > /dev/null || (echo "golint not installed, run: go install golang.org/x/lint/golint@latest" && exit 1)
	golint $(GO_PACKAGES)

check: fmt vet ## Run formatting and vetting

# Build targets
build: deps ## Build the binary
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o $(BUILD_DIR)/$(BINARY_NAME) .

build-all: deps ## Build for multiple architectures
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-w -s" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 .
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-w -s" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 .
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags="-w -s" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm7 .
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-w -s" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-w -s" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-w -s" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe .

# Test targets
test: ## Run tests
	go test -v -race -coverprofile=coverage.out $(GO_PACKAGES)

test-coverage: test ## Run tests with coverage report
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-bench: ## Run benchmark tests
	go test -bench=. -benchmem $(GO_PACKAGES)

# Run targets
run: build ## Build and run the application
	$(BUILD_DIR)/$(BINARY_NAME) -help

dev: ## Run in development mode with default settings
	go run . -price-limit=50.0 -network=192.168.1.0/24

dev-watch: ## Run with file watching (requires entr: brew install entr)
	@which entr > /dev/null || (echo "entr not installed for file watching" && exit 1)
	find . -name '*.go' | entr -r go run . -price-limit=50.0 -network=192.168.1.0/24

# Data-service Docker targets
DATA_SERVICE_IMAGE=data-service
DATA_SERVICE_DOCKERFILE=Dockerfile.data-service

docker-data-service-multi: ## Build Docker image for data-service for multiple platforms
	docker buildx build --platform $(PLATFORMS) -t $(DATA_SERVICE_IMAGE):$(DOCKER_TAG) -f $(DATA_SERVICE_DOCKERFILE) .

docker-data-service: ## Build Docker image for data-service for ARM7 (Raspberry Pi)
	rm -f ems-data-service-working.tar
	docker buildx build --platform linux/arm/v7 --no-cache --output=type=docker -t $(DATA_SERVICE_IMAGE):latest-arm7 -f $(DATA_SERVICE_DOCKERFILE) .
	docker save $(DATA_SERVICE_IMAGE):latest-arm7 > data-service.tar
	skopeo copy docker-archive:data-service.tar docker-archive:ems-data-service-working.tar
	cp ems-data-service-working.tar ~/Downloads/ems-data-service-working.tar

# Docker targets
docker: ## Build Docker image for ARM7 (Raspberry Pi)
	rm -f ems-working.tar
	docker buildx build --platform linux/arm/v7 --no-cache --output=type=docker -t $(DOCKER_IMAGE):$(DOCKER_TAG)-arm7 .
	docker save $(DOCKER_IMAGE):latest-arm7 > ems.tar
	skopeo copy docker-archive:ems.tar docker-archive:ems-working.tar
	cp ems-working.tar ~/Downloads/ems-working.tar

docker-multi: ## Build Docker image for multiple platforms
	docker buildx build --platform $(PLATFORMS) -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-push: docker-multi ## Build and push Docker image
	docker buildx build --platform $(PLATFORMS) -t $(DOCKER_IMAGE):$(DOCKER_TAG) --push .

docker-run: ## Run Docker container
	docker run --rm --network host $(DOCKER_IMAGE):$(DOCKER_TAG)

# Utility targets
clean: ## Clean build artifacts
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	go clean -cache
	go clean -testcache

install: build ## Install binary to system (requires sudo)
	sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/

uninstall: ## Remove binary from system (requires sudo)
	sudo rm -f /usr/local/bin/$(BINARY_NAME)

# Release targets
release: clean build-all test ## Prepare release build
	@echo "Release builds created in $(BUILD_DIR)/"
	@ls -la $(BUILD_DIR)/

# Development setup
setup: deps ## Set up development environment
	@echo "Installing development tools..."
	go install golang.org/x/lint/golint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	@echo "Development environment setup complete!"

# Documentation
docs: ## Generate documentation
	@echo "Generating documentation..."
	godoc -http=:6060 &
	@echo "Documentation server started at http://localhost:6060"
	@echo "Press Ctrl+C to stop"
