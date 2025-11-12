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

#### Spanmetrics Connector

The OpenTelemetry Collector is configured with a spanmetrics connector that automatically generates RED (Rate, Errors, Duration) metrics from trace spans. This provides Application Performance Management (APM) capabilities without requiring manual metric instrumentation.

**How it works:**

1. Traces flow through the collector's traces pipeline
2. The spanmetrics connector processes these traces
3. Generates metrics with the `traces_span_` namespace prefix (OTel Collector Contrib v0.109.0+)
4. Metrics are exported to Prometheus via the metrics/spanmetrics pipeline

**Generated metrics:**

```promql
# Request count
traces_span_duration_seconds_count{service_name="http-server", http_method="GET", http_status_code="200"}

# Request duration sum (for calculating average)
traces_span_duration_seconds_sum{service_name="http-server", http_method="GET"}

# Request duration histogram (for percentile calculations)
traces_span_duration_seconds_bucket{service_name="http-server", http_method="GET", le="0.1"}
```

**Available dimensions (labels):**

- `service_name` - Name of the service
- `span_name` - Name of the span operation
- `http_method` - HTTP method (GET, POST, etc.)
- `http_status_code` - HTTP response status code
- `http_route` - HTTP endpoint/route pattern
- `service_version` - Version of the service

**Example queries:**

```promql
# Request rate by service
rate(traces_span_duration_seconds_count[5m])

# P95 latency by service
histogram_quantile(0.95, rate(traces_span_duration_seconds_bucket[5m]))

# Error rate (5xx responses)
sum(rate(traces_span_duration_seconds_count{http_status_code=~"5.."}[5m]))
```

**Configuration:**

See `otel-collector/config.yaml` for the complete spanmetrics connector configuration.

**References:**

- [Spanmetrics Connector Documentation](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/connector/spanmetricsconnector)
- [blueswen/opentelemetry-apm](https://github.com/blueswen/opentelemetry-apm) - Reference implementation

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
curl http://localhost:3000/api/dashboards/uid/go-runtime-prometheus | jq > exported-dashboard.json
```

#### Go Runtime Dashboards

Three dashboards are available for monitoring Go runtime metrics:

##### 1. Go Runtime Metrics (Prometheus)

- **Purpose**: Monitor infrastructure components (Jaeger, Prometheus, OTel Collector)
- **Metrics Format**: Traditional Prometheus `go_collector` metrics
- **Dashboard UID**: `go-runtime-prometheus`
- **Current Status**: ✅ Active - showing real-time metrics from all infrastructure services
- **Features**:
  - Service selector dropdown to filter by specific services
  - Memory usage metrics (heap, stack, allocations)
  - Goroutine count and growth patterns
  - Garbage collection metrics (duration, frequency)
  - System threads and resource usage

**Example Prometheus queries:**

```promql
# Jaeger memory usage
go_memstats_heap_alloc_bytes{service="jaeger"}

# All services goroutines
go_goroutines{service=~"jaeger|prometheus|otel-collector"}

# GC duration for all infrastructure
rate(go_gc_duration_seconds_sum{service=~".*"}[5m]) / rate(go_gc_duration_seconds_count{service=~".*"}[5m])
```

**Access the dashboard:**

```bash
# Direct URL
open http://localhost:3000/d/go-runtime-prometheus
```

##### 2. Go Runtime Metrics (OpenTelemetry)

- **Purpose**: Monitor demo applications with OTel runtime instrumentation
- **Metrics Format**: OpenTelemetry semantic convention metrics
- **Dashboard UID**: `go-runtime-otel`
- **Current Status**: 🔜 Pending - will populate when demo apps include runtime instrumentation
- **Metrics Expected**:
  - `go.memory.used` - Memory in use by Go runtime
  - `go.memory.limit` - Go memory limit
  - `go.goroutine.count` - Number of goroutines
  - `go.gc.duration` - GC pause duration
  - `go.processor.limit` - Number of OS threads

**Note**: This dashboard uses metrics from `go.opentelemetry.io/contrib/instrumentation/runtime` which will be automatically added to demo applications when the compile-time instrumentation tool adds runtime metrics support.

**Access the dashboard:**

```bash
# Direct URL
open http://localhost:3000/d/go-runtime-otel
```

##### 3. OTel Collector Runtime Metrics

- **Purpose**: Monitor OTel Collector-based services (Jaeger v2, OTel Collector)
- **Metrics Format**: OpenTelemetry Collector internal telemetry metrics
- **Dashboard UID**: `otel-collector-runtime`
- **Current Status**: ✅ Active - showing real-time metrics from Jaeger and OTel Collector
- **Features**:
  - Service selector dropdown to filter by specific services
  - Heap memory allocation tracking
  - Process memory (RSS) monitoring
  - CPU usage rate
  - Memory allocation rate
  - Process uptime

**Metrics displayed:**

```promql
# Jaeger heap memory
otel_otelcol_process_runtime_heap_alloc_bytes_bytes{service="jaeger"}

# OTel Collector heap memory
otelcol_process_runtime_heap_alloc_bytes{service="otel-collector"}

# CPU usage rate for all services
rate(otelcol_process_cpu_seconds{service=~".*"}[5m])
```

**Access the dashboard:**

```bash
# Direct URL
open http://localhost:3000/d/otel-collector-runtime
```

**Note**: This dashboard shows metrics from the OpenTelemetry Collector's internal telemetry system. Jaeger v2 is built on the OTel Collector and exposes these metrics. The metric names differ slightly depending on the scrape source (direct vs. via prometheus receiver).

**References:**

- [OTel Collector Internal Telemetry](https://opentelemetry.io/docs/collector/internal-telemetry/#basic-level-metrics)
- [OTel Go Runtime Metrics Specification](https://opentelemetry.io/docs/specs/semconv/runtime/go-metrics/)
- [Jaeger Monitoring Documentation](https://www.jaegertracing.io/docs/2.11/operations/monitoring/#go-runtime-metrics)

#### 4. OpenTelemetry APM Dashboard

- **Purpose**: Application Performance Management (APM) for monitoring service-level RED metrics (Rate, Errors, Duration)
- **Metrics Source**: Spanmetrics connector (generates metrics from traces)
- **Dashboard UID**: `opentelemetry-apm`
- **Current Status**: ✅ Active - showing real-time APM metrics from all instrumented services
- **Features**:
  - Service-level overview with request rate, error rate, and latency
  - P50, P90, P95, and P99 latency percentiles
  - Request success/error breakdown
  - Endpoint-level metrics with sparkline visualizations
  - Automatic service discovery
  - Trace exemplar integration for drill-down

**Metrics displayed:**

The dashboard uses standard OpenTelemetry spanmetrics generated by the OTel Collector:

```promql
# Request rate by service
sum by (service_name) (rate(traces_span_duration_seconds_count[5m]))

# P95 latency by service
histogram_quantile(0.95,
  sum by (service_name, le) (rate(traces_span_duration_seconds_bucket[5m]))
)

# Error rate (HTTP 5xx responses)
sum by (service_name) (
  rate(traces_span_duration_seconds_count{http_status_code=~"5.."}[5m])
)

# Requests by endpoint
sum by (service_name, http_route, http_method) (
  rate(traces_span_duration_seconds_count[5m])
)
```

**How it works:**

1. Applications send traces to OTel Collector via OTLP
2. OTel Collector's spanmetrics connector processes traces
3. Generates `traces_span_duration_seconds` histogram metrics with dimensions:
   - `service_name` - Auto-detected service name
   - `http_method`, `http_status_code`, `http_route` - HTTP-specific attributes
   - `span_name` - Operation name
   - `service_version` - Service version
4. Prometheus scrapes metrics from OTel Collector (port 8889)
5. Grafana visualizes metrics in the APM dashboard

**Access the dashboard:**

```bash
# Direct URL
open http://localhost:3000/d/opentelemetry-apm

# Or navigate via Grafana UI: Dashboards → OpenTelemetry Demo → OpenTelemetry APM
```

**Key capabilities:**

- **Language-agnostic**: Works with any service instrumented with OpenTelemetry SDK
- **Zero metric instrumentation**: Metrics automatically generated from traces
- **Service auto-discovery**: Dashboard dynamically detects all services
- **Exemplar support**: Click on metrics to view corresponding traces in Jaeger
- **Endpoint analysis**: Breakdown of performance by HTTP endpoint

**Dashboard source:**

This dashboard is based on [blueswen/opentelemetry-apm](https://github.com/blueswen/opentelemetry-apm) and adapted for the demo environment. Original dashboard: [Grafana Dashboard #19419](https://grafana.com/grafana/dashboards/19419-opentelemetry-apm/)

**Note**: This dashboard complements the existing "Services Overview" dashboard. While both show application metrics:

- **Services Overview**: Custom metrics specific to demo apps (uses `http_server_request_duration` metrics)
- **OpenTelemetry APM**: Standard spanmetrics for any OpenTelemetry-instrumented service (uses `traces_span_duration_seconds` metrics)

**References:**

- [Spanmetrics Connector Documentation](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/connector/spanmetricsconnector)
- [OpenTelemetry APM GitHub Repository](https://github.com/blueswen/opentelemetry-apm)
- [Grafana Spanmetrics Guide](https://grafana.com/blog/2022/08/18/new-in-grafana-9.1-real-time-streaming-and-kubernetes-monitoring-improvements/)

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

## Configuration Validation

### Built-in Validation Tools

The observability stack includes several validation tools to check configurations before deployment:

#### Validate All Configurations

```bash
make validate-all
```

This runs validation for:

- Docker Compose configuration
- OpenTelemetry Collector configuration
- Prometheus configuration

#### Individual Validation

**Docker Compose:**

```bash
make validate
# Or manually:
docker-compose config
```

**OpenTelemetry Collector:**

```bash
make validate-otel
# Or manually:
docker-compose exec otel-collector /otelcol-contrib validate --config=/etc/otelcol-contrib/config.yaml
```

**Prometheus:**

```bash
make validate-prometheus
# Or manually:
docker-compose exec prometheus promtool check config /etc/prometheus/prometheus.yml
```

### External Validation Tools

For more comprehensive validation and best practices checking:

1. **otel-config-validator** (by Lightstep/AWS):
   - GUI: <https://github.com/lightstep/otel-config-validator>
   - Validates OTel configurations
   - Identifies common misconfigurations

2. **OTelBin** (online tool):
   - URL: <https://www.otelbin.io/>
   - Visualizes OTel Collector pipelines
   - YAML linting and syntax highlighting

3. **CoGuard CLI**:
   - Security-focused configuration scanner
   - Detects misconfigurations and vulnerabilities
   - Install: <https://www.coguard.io/>

### Best Practice: Validate Before Deploy

Always validate configurations before deploying:

```bash
# Before starting services
make validate-all

# If validation passes, then start
make start
```

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
