# OpenTelemetry Go Compile Instrumentation - Demo Infrastructure

Complete observability infrastructure for demonstrating OpenTelemetry compile-time instrumentation in Go applications.

## Overview

This infrastructure provides a production-like observability stack to demonstrate and test the OpenTelemetry Go compile-time instrumentation tool. It includes distributed tracing, metrics collection, visualization, and load testing capabilities.

## Architecture

```
┌─────────────────┐
│  Demo Apps      │
│  (gRPC/HTTP)    │──┐
└─────────────────┘  │
                     │  OTLP (4317/4318)
                     ▼
┌─────────────────────────────────────┐
│  OpenTelemetry Collector            │
│  - Receives telemetry (OTLP)       │
│  - Processes & batches              │
│  - Exports to backends              │
└──────────┬─────────────┬────────────┘
           │             │
           │ Traces      │ Metrics
           ▼             ▼
    ┌──────────┐   ┌──────────┐
    │  Jaeger  │   │Prometheus│
    │  :16686  │   │  :9090   │
    └──────────┘   └──────────┘
           │             │
           └──────┬──────┘
                  │ Datasources
                  ▼
           ┌──────────────┐
           │   Grafana    │
           │    :3000     │
           │  Dashboards  │
           └──────────────┘
```

## Components

### Observability Stack

1. **OpenTelemetry Collector** (v0.139.0)
   - OTLP receivers (gRPC: 4317, HTTP: 4318)
   - Batch processing for efficiency
   - Exports to Jaeger and Prometheus

2. **Jaeger** (v2.11.0)
   - Distributed tracing backend
   - Native OTLP support
   - UI available at [http://localhost:16686](http://localhost:16686)

3. **Prometheus** (v3.7.3)
   - Metrics storage and querying
   - Native OTLP receiver support
   - UI available at [http://localhost:9090](http://localhost:9090)

4. **Grafana** (v12.2.1)
   - Pre-configured dashboards
   - Prometheus and Jaeger datasources
   - UI available at [http://localhost:3000](http://localhost:3000)

### Demo Applications

- **gRPC Server/Client**: Located in [demo/grpc/](../grpc/)
- **HTTP Server/Client**: Located in [demo/http/](../http/) (when available)

### Load Testing

- **k6**: Scriptable load testing tool
  - HTTP load test script
  - gRPC load test script

## Directory Structure

```
infrastructure/
├── docker-compose/
│   ├── docker-compose.yml          # Main orchestration file
│   ├── otel-collector/
│   │   └── config.yaml             # Collector configuration
│   ├── prometheus/
│   │   └── prometheus.yml          # Prometheus configuration
│   ├── grafana/
│   │   ├── datasources/
│   │   │   └── datasources.yaml   # Datasource provisioning
│   │   └── dashboards/
│   │       ├── dashboard.yaml      # Dashboard provider config
│   │       └── dashboards/
│   │           ├── go-runtime-prometheus.json    # Go runtime metrics (Prometheus)
│   │           ├── go-runtime-otel.json          # Go runtime metrics (OTel)
│   │           ├── opentelemetry-apm.json        # APM dashboard
│   │           ├── otel-collector.json           # Collector health
│   │           ├── otel-collector-runtime.json   # Collector runtime metrics
│   │           └── services-overview.json        # Service metrics & traces
│   ├── k6/
│   │   ├── http-load-test.js      # HTTP load test
│   │   └── grpc-load-test.js      # gRPC load test
│   └── README.md                   # Detailed usage guide
└── kubernetes/                     # Future Kubernetes deployment
```

## Quick Start

### Prerequisites

- Docker Engine 20.10+
- Docker Compose 2.0+
- 4GB RAM available for containers
- Ports 3000, 9090, 16686, 4317, 4318, 50051 available

### Using the Makefile (Recommended)

A convenient Makefile is provided with common operations:

```bash
cd demo/infrastructure/docker-compose

# Show all available commands
make help

# Quick start - check prerequisites, start services, show URLs
make quickstart

# Start all services
make start

# Check service health
make health

# View logs
make logs

# Reload configurations without restart
make reload

# Run load tests
make load-test

# Stop services
make stop

# Clean up everything
make clean
```

For the complete list of available commands, run `make help`.

### Running the Stack (Manual)

1. Navigate to the docker-compose directory:

   ```bash
   cd demo/infrastructure/docker-compose
   ```

2. Start all services:

   ```bash
   docker-compose up -d
   ```

3. Verify services are running:

   ```bash
   docker-compose ps
   ```

4. Access the UIs:
   - **Grafana**: [http://localhost:3000](http://localhost:3000) (anonymous access enabled)
   - **Jaeger**: [http://localhost:16686](http://localhost:16686)
   - **Prometheus**: [http://localhost:9090](http://localhost:9090)

5. View logs:

   ```bash
   docker-compose logs -f
   ```

6. Stop all services:

   ```bash
   docker-compose down
   ```

### Running Load Tests

To run k6 load tests:

```bash
# Start infrastructure with load testing profile
docker-compose --profile load-testing up

# Or run k6 separately
docker-compose run --rm k6 run /scripts/grpc-load-test.js
```

## Dashboards

Six pre-configured Grafana dashboards are automatically provisioned:

1. **Runtime: Go (Prometheus)**
   - Goroutines count
   - Memory usage (heap, stack, alloc)
   - GC duration and rate
   - For infrastructure components (Jaeger, Prometheus, OTel Collector)
   - Direct link: [http://localhost:3000/d/go-runtime-prometheus](http://localhost:3000/d/go-runtime-prometheus)

2. **Runtime: Go (OTel)**
   - Goroutines count
   - Memory usage (heap, stack, alloc)
   - GC duration and rate
   - For demo applications using OpenTelemetry metrics
   - Direct link: [http://localhost:3000/d/go-runtime-otel](http://localhost:3000/d/go-runtime-otel)

3. **Runtime: Collector**
   - OTel Collector internal runtime metrics
   - Memory and CPU usage
   - Internal telemetry
   - Direct link: [http://localhost:3000/d/otel-collector-runtime](http://localhost:3000/d/otel-collector-runtime)

4. **Collector: Metrics**
   - Spans/metrics received and exported
   - Batch processor statistics
   - Memory usage and health
   - Direct link: [http://localhost:3000/d/otel-collector](http://localhost:3000/d/otel-collector)

5. **Services: Overview**
   - HTTP request rate and latency
   - Error rates by status code
   - Recent traces with metric correlation
   - Request breakdown by endpoint
   - Direct link: [http://localhost:3000/d/services-overview](http://localhost:3000/d/services-overview)

6. **Services: APM**
   - Comprehensive application performance monitoring
   - Request rate, latency, and error rates
   - Service dependency visualization
   - Direct link: [http://localhost:3000/d/opentelemetry-apm](http://localhost:3000/d/opentelemetry-apm)

## Features

### Metric-to-Trace Correlation

Grafana is configured with exemplars support, allowing you to:

1. Click on a metric data point in Prometheus
2. Jump directly to the corresponding trace in Jaeger
3. See full distributed trace context for performance issues

### OTLP-First Approach

All components use OpenTelemetry Protocol (OTLP):

- Native OTLP support in Jaeger v2
- Native OTLP receiver in Prometheus v3
- No protocol translation overhead
- Future-proof architecture

### Ephemeral Demo Environment

Data is not persisted between restarts:

- Quick cleanup and reset
- No disk space concerns
- Ideal for testing and demos
- Can be modified for persistent storage (see docker-compose.yml comments)

## Configuration

### Environment Variables

Demo applications can be configured via environment variables:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317
OTEL_EXPORTER_OTLP_PROTOCOL=grpc
OTEL_SERVICE_NAME=my-service
OTEL_RESOURCE_ATTRIBUTES=service.namespace=demo,service.version=1.0.0
OTEL_LOG_LEVEL=info
```

### Customization

To customize the stack:

1. **OpenTelemetry Collector**: Edit `otel-collector/config.yaml`
2. **Prometheus**: Edit `prometheus/prometheus.yml`
3. **Grafana Datasources**: Edit `grafana/datasources/datasources.yaml`
4. **Grafana Dashboards**: Add JSON files to `grafana/dashboards/dashboards/`

After changes, restart the affected services:

```bash
docker-compose restart otel-collector
```

## Troubleshooting

### Services Not Starting

Check logs for specific service:

```bash
docker-compose logs <service-name>
```

### No Telemetry Data

1. Verify collector is receiving data:

   ```bash
   curl http://localhost:8888/metrics | grep receiver_accepted
   ```

2. Check application OTLP endpoint configuration
3. Verify network connectivity between containers

### Port Conflicts

If ports are already in use, modify `docker-compose.yml` port mappings:

```yaml
ports:
  - "3001:3000"  # Use 3001 instead of 3000 on host
```

## Next Steps

- Explore [Docker Compose documentation](./docker-compose/README.md) for detailed configuration
- Review [gRPC demo](../grpc/README.md) for application-level instrumentation
- Customize dashboards for your specific metrics
- Add persistent storage for production-like testing

## Future Enhancements

- Kubernetes deployment configurations (in `kubernetes/` directory)
- Additional demo applications (HTTP, database interactions)
- Alerting rules and notification channels
- Log aggregation with Loki
- Service mesh integration examples

## References

- [OpenTelemetry Collector Documentation](https://opentelemetry.io/docs/collector/)
- [Jaeger Documentation](https://www.jaegertracing.io/docs/)
- [Prometheus Documentation](https://prometheus.io/docs/)
- [Grafana Documentation](https://grafana.com/docs/)
- [k6 Documentation](https://k6.io/docs/)
