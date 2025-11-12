# Makefile for OpenTelemetry Demo Infrastructure
# Provides convenient commands for managing the observability stack

.PHONY: help start stop restart reload status logs clean build load-test urls health

# Default target - show help
.DEFAULT_GOAL := help

# Colors for output
BLUE := \033[0;34m
GREEN := \033[0;32m
YELLOW := \033[0;33m
RED := \033[0;31m
NC := \033[0m # No Color

help: ## Show this help message
	@echo "$(BLUE)OpenTelemetry Demo Infrastructure$(NC)"
	@echo "$(BLUE)====================================$(NC)"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-15s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "Examples:"
	@echo "  $(YELLOW)make start$(NC)          - Start all services"
	@echo "  $(YELLOW)make logs$(NC)           - Follow all logs"
	@echo "  $(YELLOW)make reload$(NC)         - Reload configurations"
	@echo "  $(YELLOW)make load-test$(NC)      - Run load tests"

start: ## Start all observability services
	@echo "$(BLUE)Starting observability stack...$(NC)"
	@docker-compose up -d
	@echo "$(GREEN)✓ Services started$(NC)"
	@echo ""
	@$(MAKE) urls

stop: ## Stop all services
	@echo "$(BLUE)Stopping services...$(NC)"
	@docker-compose stop
	@echo "$(GREEN)✓ Services stopped$(NC)"

restart: stop start ## Restart all services

down: ## Stop and remove all containers
	@echo "$(BLUE)Stopping and removing containers...$(NC)"
	@docker-compose down
	@echo "$(GREEN)✓ Containers removed$(NC)"

reload: ## Reload configurations without restarting containers
	@echo "$(BLUE)Reloading configurations...$(NC)"
	@echo "$(YELLOW)→ Restarting OpenTelemetry Collector...$(NC)"
	@docker-compose restart otel-collector
	@echo "$(YELLOW)→ Reloading Prometheus configuration...$(NC)"
	@curl -X POST http://localhost:9090/-/reload 2>/dev/null || echo "$(RED)✗ Prometheus reload failed (may not be running)$(NC)"
	@echo "$(YELLOW)→ Restarting Grafana...$(NC)"
	@docker-compose restart grafana
	@echo "$(GREEN)✓ Configurations reloaded$(NC)"

status: ## Show status of all services
	@echo "$(BLUE)Service Status:$(NC)"
	@docker-compose ps

logs: ## Follow logs from all services
	@docker-compose logs -f

logs-otel: ## Follow OpenTelemetry Collector logs
	@docker-compose logs -f otel-collector

logs-prometheus: ## Follow Prometheus logs
	@docker-compose logs -f prometheus

logs-jaeger: ## Follow Jaeger logs
	@docker-compose logs -f jaeger

logs-grafana: ## Follow Grafana logs
	@docker-compose logs -f grafana

logs-grpc: ## Follow gRPC server and client logs
	@docker-compose logs -f grpc-server grpc-client

health: ## Check health of all services
	@echo "$(BLUE)Health Check:$(NC)"
	@echo ""
	@echo -n "Grafana:         "
	@curl -f -s http://localhost:3000/api/health > /dev/null 2>&1 && echo "$(GREEN)✓ Healthy$(NC)" || echo "$(RED)✗ Unhealthy$(NC)"
	@echo -n "Prometheus:      "
	@curl -f -s http://localhost:9090/-/healthy > /dev/null 2>&1 && echo "$(GREEN)✓ Healthy$(NC)" || echo "$(RED)✗ Unhealthy$(NC)"
	@echo -n "Jaeger:          "
	@curl -f -s http://localhost:16686 > /dev/null 2>&1 && echo "$(GREEN)✓ Healthy$(NC)" || echo "$(RED)✗ Unhealthy$(NC)"
	@echo -n "OTel Collector:  "
	@curl -f -s http://localhost:13133 > /dev/null 2>&1 && echo "$(GREEN)✓ Healthy$(NC)" || echo "$(RED)✗ Unhealthy$(NC)"
	@echo ""

urls: ## Show URLs for all UIs
	@echo "$(BLUE)Access UIs:$(NC)"
	@echo "  $(GREEN)Grafana:$(NC)    http://localhost:3000"
	@echo "  $(GREEN)Jaeger:$(NC)     http://localhost:16686"
	@echo "  $(GREEN)Prometheus:$(NC) http://localhost:9090"
	@echo ""
	@echo "$(BLUE)Dashboards:$(NC)"
	@echo "  $(GREEN)Runtime: Go (Prometheus)$(NC)  http://localhost:3000/d/go-runtime-prometheus"
	@echo "  $(GREEN)Runtime: Go (OTel)$(NC)        http://localhost:3000/d/go-runtime-otel"
	@echo "  $(GREEN)Runtime: Collector$(NC)        http://localhost:3000/d/otel-collector-runtime"
	@echo "  $(GREEN)Collector: Metrics$(NC)        http://localhost:3000/d/otel-collector"
	@echo "  $(GREEN)Services: Overview$(NC)        http://localhost:3000/d/services-overview"
	@echo "  $(GREEN)Services: APM$(NC)             http://localhost:3000/d/opentelemetry-apm"
	@echo ""

build: ## Rebuild demo applications
	@echo "$(BLUE)Building demo applications...$(NC)"
	@docker-compose build
	@echo "$(GREEN)✓ Build complete$(NC)"

build-nocache: ## Rebuild demo applications without cache
	@echo "$(BLUE)Building demo applications (no cache)...$(NC)"
	@docker-compose build --no-cache
	@echo "$(GREEN)✓ Build complete$(NC)"

load-test: ## Run k6 load tests
	@echo "$(BLUE)Running load tests...$(NC)"
	@docker-compose --profile load-testing up k6
	@echo "$(GREEN)✓ Load test complete$(NC)"

load-test-http: ## Run k6 HTTP load test
	@echo "$(BLUE)Running HTTP load test...$(NC)"
	@docker-compose run --rm k6 run /scripts/http-load-test.js
	@echo "$(GREEN)✓ HTTP load test complete$(NC)"

load-test-grpc: ## Run k6 gRPC load test
	@echo "$(BLUE)Running gRPC load test...$(NC)"
	@docker-compose run --rm k6 run /scripts/grpc-load-test.js
	@echo "$(GREEN)✓ gRPC load test complete$(NC)"

clean: ## Remove all containers and clean up
	@echo "$(BLUE)Cleaning up...$(NC)"
	@docker-compose down -v
	@echo "$(GREEN)✓ Cleanup complete$(NC)"

clean-all: clean ## Remove containers, volumes, and images
	@echo "$(BLUE)Removing images...$(NC)"
	@docker-compose down -v --rmi all
	@echo "$(GREEN)✓ Full cleanup complete$(NC)"

pull: ## Pull latest images
	@echo "$(BLUE)Pulling latest images...$(NC)"
	@docker-compose pull
	@echo "$(GREEN)✓ Images updated$(NC)"

update: pull restart ## Update images and restart services

validate-docker-compose: ## Validate docker-compose configuration
	@echo "$(BLUE)Validating configuration...$(NC)"
	@docker-compose config > /dev/null && echo "$(GREEN)✓ Configuration valid$(NC)" || echo "$(RED)✗ Configuration invalid$(NC)"

validate-otel: ## Validate OpenTelemetry Collector configuration
	@echo "$(BLUE)Validating OTel Collector configuration...$(NC)"
	@docker-compose exec -T otel-collector /otelcol-contrib validate --config=/etc/otelcol-contrib/config.yaml && \
		echo "$(GREEN)✓ OTel Collector configuration valid$(NC)" || \
		echo "$(RED)✗ OTel Collector configuration invalid$(NC)"

validate-otel-local: ## Validate OTel Collector config locally (requires otelcol-contrib binary)
	@echo "$(BLUE)Validating OTel Collector configuration locally...$(NC)"
	@if command -v otelcol-contrib > /dev/null 2>&1; then \
		otelcol-contrib validate --config=otel-collector/config.yaml && \
		echo "$(GREEN)✓ Configuration valid$(NC)"; \
	else \
		echo "$(YELLOW)! otelcol-contrib not found. Install from: https://github.com/open-telemetry/opentelemetry-collector-releases$(NC)"; \
	fi

validate-prometheus: ## Validate Prometheus configuration
	@echo "$(BLUE)Validating Prometheus configuration...$(NC)"
	@docker-compose exec -T prometheus promtool check config /etc/prometheus/prometheus.yml && \
		echo "$(GREEN)✓ Prometheus configuration valid$(NC)" || \
		echo "$(RED)✗ Prometheus configuration invalid$(NC)"

validate: validate-docker-compose validate-otel validate-prometheus ## Validate all configurations (docker-compose, OTel, Prometheus)

metrics-otel: ## Show OpenTelemetry Collector metrics
	@echo "$(BLUE)OpenTelemetry Collector Metrics:$(NC)"
	@curl -s http://localhost:8888/metrics | grep -E "^(otelcol_receiver_accepted|otelcol_exporter_sent)" | head -20

metrics-prometheus: ## Query Prometheus metrics
	@echo "$(BLUE)Prometheus Targets:$(NC)"
	@curl -s 'http://localhost:9090/api/v1/targets' | jq -r '.data.activeTargets[] | "\(.labels.job): \(.health)"'

traces: ## Show recent traces from Jaeger
	@echo "$(BLUE)Recent Traces:$(NC)"
	@curl -s 'http://localhost:16686/api/traces?service=grpc-server&limit=5' | jq -r '.data[].spans[0] | "\(.startTime/1000 | strftime("%H:%M:%S")) - \(.operationName)"'

scale-client: ## Scale gRPC client instances (usage: make scale-client N=3)
	@echo "$(BLUE)Scaling gRPC client to $(or $(N),2) instances...$(NC)"
	@docker-compose up -d --scale grpc-client=$(or $(N),2)
	@echo "$(GREEN)✓ Scaled to $(or $(N),2) instances$(NC)"

exec-otel: ## Open shell in OpenTelemetry Collector container
	@docker-compose exec otel-collector sh

exec-prometheus: ## Open shell in Prometheus container
	@docker-compose exec prometheus sh

exec-grafana: ## Open shell in Grafana container
	@docker-compose exec grafana sh

backup-config: ## Backup all configuration files
	@echo "$(BLUE)Backing up configuration...$(NC)"
	@tar -czf ../otel-stack-backup-$$(date +%Y%m%d-%H%M%S).tar.gz \
		otel-collector/ prometheus/ grafana/ k6/ docker-compose.yml Makefile
	@echo "$(GREEN)✓ Backup created$(NC)"

ports: ## Check if required ports are available
	@echo "$(BLUE)Checking required ports...$(NC)"
	@echo -n "Port 3000 (Grafana):     "
	@lsof -i :3000 > /dev/null 2>&1 && echo "$(RED)✗ In use$(NC)" || echo "$(GREEN)✓ Available$(NC)"
	@echo -n "Port 9090 (Prometheus):  "
	@lsof -i :9090 > /dev/null 2>&1 && echo "$(RED)✗ In use$(NC)" || echo "$(GREEN)✓ Available$(NC)"
	@echo -n "Port 16686 (Jaeger):     "
	@lsof -i :16686 > /dev/null 2>&1 && echo "$(RED)✗ In use$(NC)" || echo "$(GREEN)✓ Available$(NC)"
	@echo -n "Port 4317 (OTLP gRPC):   "
	@lsof -i :4317 > /dev/null 2>&1 && echo "$(RED)✗ In use$(NC)" || echo "$(GREEN)✓ Available$(NC)"
	@echo -n "Port 4318 (OTLP HTTP):   "
	@lsof -i :4318 > /dev/null 2>&1 && echo "$(RED)✗ In use$(NC)" || echo "$(GREEN)✓ Available$(NC)"
	@echo -n "Port 50051 (gRPC Demo):  "
	@lsof -i :50051 > /dev/null 2>&1 && echo "$(RED)✗ In use$(NC)" || echo "$(GREEN)✓ Available$(NC)"
	@echo ""

prereqs: ## Check prerequisites
	@echo "$(BLUE)Checking prerequisites...$(NC)"
	@echo -n "Docker:          "
	@command -v docker > /dev/null 2>&1 && echo "$(GREEN)✓ Installed$(NC)" || echo "$(RED)✗ Not found$(NC)"
	@echo -n "Docker Compose:  "
	@command -v docker-compose > /dev/null 2>&1 && echo "$(GREEN)✓ Installed$(NC)" || echo "$(RED)✗ Not found$(NC)"
	@echo -n "curl:            "
	@command -v curl > /dev/null 2>&1 && echo "$(GREEN)✓ Installed$(NC)" || echo "$(RED)✗ Not found$(NC)"
	@echo -n "jq:              "
	@command -v jq > /dev/null 2>&1 && echo "$(GREEN)✓ Installed$(NC)" || echo "$(RED)✗ Optional$(NC)"
	@echo ""

quickstart: prereqs ports start health urls ## Complete setup: check prereqs, ports, start services, check health, show URLs
	@echo ""
	@echo "$(GREEN)✓ Observability stack is ready!$(NC)"
	@echo ""

# Development helpers
dev: start logs ## Start services and follow logs (development mode)

dev-rebuild: build-nocache start logs ## Rebuild without cache, start, and follow logs
