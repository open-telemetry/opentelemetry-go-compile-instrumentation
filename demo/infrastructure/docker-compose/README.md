# Docker Compose Observability Stack

Complete guide for running the OpenTelemetry observability stack with Docker Compose.

## Prerequisites

### System Requirements

- **Docker Engine**: 20.10.0 or later
- **Docker Compose**: 2.0.0 or later (with Compose Specification support)
- **Memory**: Minimum 4GB RAM allocated to Docker
- **Disk**: At least 2GB free space for images

### Port Requirements

Ensure these ports are available on your host:

| Port  | Service           | Purpose                    |
|-------|-------------------|----------------------------|
| 3000  | Grafana           | Web UI                     |
| 9090  | Prometheus        | Web UI & API               |
| 16686 | Jaeger            | Web UI                     |
| 4317  | OTel Collector    | OTLP gRPC receiver         |
| 4318  | OTel Collector    | OTLP HTTP receiver         |
| 8888  | OTel Collector    | Metrics endpoint           |
| 8889  | OTel Collector    | Prometheus exporter        |
| 50051 | gRPC Server       | Demo gRPC service          |

## Quick Start

### Using the Makefile (Recommended)

A comprehensive Makefile is provided for convenient operations:

```bash
# Show all available commands
make help

# Complete quickstart with health checks
make quickstart

# Basic operations
make start          # Start all services
make stop           # Stop all services
make restart        # Restart all services
make reload         # Reload configurations without restart
make status         # Show service status
make health         # Check health of all services

# Logs
make logs           # Follow all logs
make logs-otel      # Follow OpenTelemetry Collector logs
make logs-grpc      # Follow gRPC demo logs

# Load testing
make load-test      # Run k6 load tests
make load-test-grpc # Run gRPC-specific test

# Utilities
make urls           # Show all UI URLs
make metrics-otel   # Show collector metrics
make traces         # Show recent traces
make ports          # Check if ports are available
make prereqs        # Check prerequisites

# Development
make dev            # Start and follow logs
make build          # Rebuild applications
make clean          # Clean up everything
```

### 1. Start All Services (Manual)

```bash
# From the docker-compose directory
docker-compose up -d

# Or with logs visible
docker-compose up
```

### 2. Verify Services

```bash
# Check service status
docker-compose ps

# Expected output - all services should be "Up" and "healthy"
NAME            IMAGE                                         STATUS
grafana         grafana/grafana:12.2.1                        Up (healthy)
jaeger          jaegertracing/jaeger:2.11.0                   Up (healthy)
otel-collector  otel/opentelemetry-collector-contrib:0.139.0  Up (healthy)
prometheus      prom/prometheus:v3.7.3                        Up (healthy)
grpc-server     ...                                           Up
grpc-client     ...                                           Up
```

### 3. Access UIs

Open your browser to:

- **Grafana**: [http://localhost:3000](http://localhost:3000)
  - No login required (anonymous admin access enabled for demo)
  - Pre-configured dashboards in "OpenTelemetry Demo" folder

- **Jaeger**: [http://localhost:16686](http://localhost:16686)
  - Search for traces by service name
  - View distributed traces and dependencies

- **Prometheus**: [http://localhost:9090](http://localhost:9090)
  - Query metrics directly
  - View targets and service discovery

### 4. View Logs

```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f otel-collector

# Last 100 lines
docker-compose logs --tail=100 grpc-server
```

### 5. Stop Services

```bash
# Stop all services (containers remain)
docker-compose stop

# Stop and remove containers
docker-compose down

# Remove everything including volumes (if any)
docker-compose down -v
```

## Service-Specific Operations

### OpenTelemetry Collector

**View collector metrics:**

```bash
curl http://localhost:8888/metrics
```

**Check health:**

```bash
curl http://localhost:13133/
```

**Restart with new configuration:**

```bash
# Edit otel-collector/config.yaml
docker-compose restart otel-collector
```

### Prometheus

**Check targets:**

```bash
curl http://localhost:9090/api/v1/targets | jq
```

**Query metrics:**

```bash
curl 'http://localhost:9090/api/v1/query?query=up'
```

**Reload configuration:**

```bash
curl -X POST http://localhost:9090/-/reload
```

### Jaeger

**Search traces via API:**

```bash
curl 'http://localhost:16686/api/traces?service=grpc-server&limit=20'
```

**Get services:**

```bash
curl http://localhost:16686/api/services
```

### Grafana

**List datasources:**

```bash
curl http://localhost:3000/api/datasources
```

**Export dashboard:**

```bash
curl http://localhost:3000/api/dashboards/uid/go-metrics | jq > exported-dashboard.json
```

## Advanced Usage

### Load Testing with k6

The k6 service is configured with a profile to prevent it from running automatically.

**Run gRPC load test:**

```bash
docker-compose --profile load-testing up k6
```

**Run custom k6 script:**

```bash
docker-compose run --rm k6 run /scripts/http-load-test.js
```

**Interactive k6 execution:**

```bash
docker-compose run --rm k6 run --vus 10 --duration 30s /scripts/grpc-load-test.js
```

### Scaling Demo Applications

Scale the number of client instances:

```bash
docker-compose up -d --scale grpc-client=3
```

### Building Demo Applications

Rebuild demo applications after code changes:

```bash
# Rebuild all services
docker-compose build

# Rebuild specific service
docker-compose build grpc-server

# Rebuild without cache
docker-compose build --no-cache grpc-server
```

## Configuration

### Environment Variable Override

Override environment variables for demo apps:

```bash
docker-compose run -e OTEL_LOG_LEVEL=debug grpc-client
```

Or create a `.env` file in this directory:

```bash
# .env file
OTEL_LOG_LEVEL=debug
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317
```

### Custom Network

The stack uses a dedicated bridge network `otel-demo-network`. Services can communicate using service names as hostnames.

**Inspect network:**

```bash
docker network inspect otel-demo-network
```

**Connect external container:**

```bash
docker run --network otel-demo-network \
  -e OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317 \
  my-instrumented-app
```

### Persistent Storage

By default, data is ephemeral. To enable persistence, uncomment volume definitions in `docker-compose.yml`:

```yaml
volumes:
  prometheus-data:
  grafana-data:
```

Then add volume mounts to services:

```yaml
prometheus:
  volumes:
    - prometheus-data:/prometheus

grafana:
  volumes:
    - grafana-data:/var/lib/grafana
```

## Troubleshooting

### Issue: Services Not Starting

**Symptoms:**

- `docker-compose up` fails
- Services show "Restarting" status

**Solutions:**

1. Check port conflicts:

   ```bash
   lsof -i :3000  # Check if Grafana port is in use
   ```

2. Verify Docker resources:

   ```bash
   docker system df
   docker system prune  # Clean up if needed
   ```

3. Check service logs:

   ```bash
   docker-compose logs <service-name>
   ```

### Issue: No Telemetry Data in Grafana

**Symptoms:**

- Dashboards show "No data"
- Empty graphs

**Solutions:**

1. Verify collector is receiving data:

   ```bash
   curl http://localhost:8888/metrics | grep receiver_accepted
   ```

2. Check demo app is running:

   ```bash
   docker-compose ps grpc-server
   docker-compose logs grpc-server
   ```

3. Verify Prometheus is scraping collector:

   ```bash
   curl http://localhost:9090/api/v1/targets | jq '.data.activeTargets'
   ```

4. Check Grafana datasources:

   ```bash
   curl http://localhost:3000/api/datasources | jq
   ```

### Issue: Collector Export Failures

**Symptoms:**

- Collector logs show export errors
- Traces not appearing in Jaeger

**Solutions:**

1. Check Jaeger is healthy:

   ```bash
   docker-compose ps jaeger
   ```

2. Verify network connectivity:

   ```bash
   docker-compose exec otel-collector wget -O- http://jaeger:16686
   ```

3. Review collector configuration:

   ```bash
   cat otel-collector/config.yaml
   ```

### Issue: High Resource Usage

**Symptoms:**

- Docker consuming high CPU/memory
- System slowdown

**Solutions:**

1. Limit container resources in `docker-compose.yml`:

   ```yaml
   services:
     otel-collector:
       deploy:
         resources:
           limits:
             cpus: '0.5'
             memory: 512M
   ```

2. Reduce scrape frequency in Prometheus:

   ```yaml
   # prometheus/prometheus.yml
   global:
     scrape_interval: 30s  # Increase from 15s
   ```

3. Stop unused services:

   ```bash
   docker-compose stop k6  # If not load testing
   ```

### Issue: Dashboard Not Showing

**Symptoms:**

- Expected dashboard missing in Grafana
- Dashboard shows errors

**Solutions:**

1. Verify dashboard files exist:

   ```bash
   ls -la grafana/dashboards/dashboards/
   ```

2. Check Grafana logs:

   ```bash
   docker-compose logs grafana | grep -i dashboard
   ```

3. Manually import dashboard:
   - Open Grafana UI
   - Go to Dashboards → Import
   - Upload JSON file from `grafana/dashboards/dashboards/`

## Maintenance

### Update Images

```bash
# Pull latest images
docker-compose pull

# Recreate containers with new images
docker-compose up -d --force-recreate
```

### Backup Configuration

```bash
# Backup all configuration files
tar -czf otel-stack-config-$(date +%Y%m%d).tar.gz \
  otel-collector/ prometheus/ grafana/ k6/ docker-compose.yml
```

### Clean Up

```bash
# Remove stopped containers
docker-compose down

# Remove all data (careful!)
docker-compose down -v

# Remove unused images
docker image prune -a
```

## Performance Tuning

### For High Traffic

1. **Increase collector batch size:**

   ```yaml
   # otel-collector/config.yaml
   processors:
     batch:
       send_batch_size: 2048  # Increase from 1024
   ```

2. **Enable collector queue:**

   ```yaml
   exporters:
     otlp/jaeger:
       sending_queue:
         enabled: true
         num_consumers: 10
         queue_size: 1000
   ```

3. **Adjust Prometheus retention:**

   ```yaml
   # docker-compose.yml
   prometheus:
     command:
       - --storage.tsdb.retention.time=1d  # Reduce from default 15d
   ```

### For Low Traffic / Development

1. **Reduce collector memory:**

   ```yaml
   # otel-collector/config.yaml
   processors:
     memory_limiter:
       limit_mib: 256  # Reduce from 512
   ```

2. **Increase scrape intervals:**

   ```yaml
   # prometheus/prometheus.yml
   global:
     scrape_interval: 30s
   ```

## Security Notes

This setup is designed for **demo and development only**. For production:

1. **Enable authentication:**
   - Grafana: Disable anonymous access
   - Prometheus: Enable basic auth
   - Jaeger: Configure auth proxy

2. **Use TLS:**
   - Configure TLS for all exposed endpoints
   - Use certificates from trusted CA

3. **Network isolation:**
   - Use separate networks for different layers
   - Implement network policies

4. **Secrets management:**
   - Use Docker secrets or external secret managers
   - Never commit credentials to version control

## References

- [Docker Compose Specification](https://docs.docker.com/compose/compose-file/)
- [OpenTelemetry Collector Configuration](https://opentelemetry.io/docs/collector/configuration/)
- [Prometheus Configuration](https://prometheus.io/docs/prometheus/latest/configuration/configuration/)
- [Grafana Provisioning](https://grafana.com/docs/grafana/latest/administration/provisioning/)
- [k6 Documentation](https://k6.io/docs/)
