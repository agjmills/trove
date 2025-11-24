.PHONY: help build up down restart logs shell exec psql migrate test clean

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the Docker containers
	docker compose build

up: ## Start the development environment
	docker compose up -d

down: ## Stop the development environment
	docker compose down

restart: ## Restart the application container
	docker compose restart app

logs: ## Show application logs (follow mode)
	docker compose logs -f app

logs-all: ## Show all container logs (follow mode)
	docker compose logs -f

shell: ## Open a shell in the application container
	docker compose exec app sh

exec: ## Execute a command in the app container. Usage: make exec cmd="go test ./..."
	docker compose exec app $(cmd)

psql: ## Open PostgreSQL shell
	docker compose exec postgres psql -U trove -d trove

migrate: ## Run database migrations (placeholder - will implement with migration tool)
	docker compose exec app go run cmd/server/main.go migrate

seed: ## Seed the database with test data
	docker compose exec app go run cmd/server/main.go seed

test: ## Run tests inside container
	docker compose exec app go test -v ./...

test-coverage: ## Run tests with coverage
	docker compose exec app go test -v -coverprofile=coverage.out ./...
	docker compose exec app go tool cover -html=coverage.out -o coverage.html

fmt: ## Format Go code
	docker compose exec app go fmt ./...

build-css: ## Build Tailwind CSS
	./build-tailwind.sh

lint: ## Run linter (requires golangci-lint)
	docker compose exec app golangci-lint run

clean: ## Remove containers, volumes, and temporary files
	docker compose down -v
	rm -rf tmp/ data/

clean-all: clean ## Remove everything including Go module cache
	docker volume rm trove_go-modules || true
	docker volume rm trove_postgres-data || true

setup: ## Initial setup - build and start everything
	@echo "Setting up Trove development environment..."
	@cp -n .env.example .env || true
	@mkdir -p data/files
	docker compose build
	docker compose up -d
	@echo "Waiting for database to be ready..."
	@sleep 5
	@echo "Setup complete! Run 'make logs' to see application logs"

dev: up logs ## Start development environment and show logs
