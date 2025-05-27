.PHONY: help build run test clean docker-build docker-run docker-stop docker-logs fmt lint deps

# Default target
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Development commands
build: ## Build the application
	go build -o main .

run: ## Run the application locally
	go run main.go

test: ## Run tests
	go test -v ./...

test-coverage: ## Run tests with coverage
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean: ## Clean build artifacts
	rm -f main sample-gin-project coverage.out coverage.html
	go clean

deps: ## Download and tidy dependencies
	go mod download
	go mod tidy

fmt: ## Format code
	go fmt ./...

lint: ## Run linter (requires golangci-lint)
	golangci-lint run

# Docker commands
docker-build: ## Build Docker image
	docker build -t sample-gin-project .

docker-run: ## Run with Docker Compose
	docker-compose up -d

docker-stop: ## Stop Docker Compose
	docker-compose down

docker-logs: ## View Docker logs
	docker-compose logs -f

docker-clean: ## Clean Docker containers and images
	docker-compose down -v --rmi all

# Database commands
db-setup: ## Setup MySQL database (requires MySQL to be running)
	mysql -u root -p -e "CREATE DATABASE IF NOT EXISTS testdb;"
	mysql -u root -p -e "GRANT ALL PRIVILEGES ON testdb.* TO 'root'@'localhost';"

# API testing commands
test-health: ## Test health endpoint
	curl -s http://localhost:8080/health | jq .

test-internal: ## Test internal health check endpoint
	curl -s http://localhost:8080/internal-check | jq .

test-users: ## Test users endpoint
	curl -s http://localhost:8080/api/v1/users | jq .

create-user: ## Create a test user
	curl -X POST http://localhost:8080/api/v1/users \
		-H "Content-Type: application/json" \
		-d '{"name":"Test User","email":"test@example.com"}' | jq .

# Utility commands
check-deps: ## Check for dependency updates
	go list -u -m all

install-tools: ## Install development tools
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

dev: ## Start development environment
	docker-compose up -d mysql phpmyadmin
	@echo "Waiting for MySQL to be ready..."
	@sleep 10
	go run main.go 