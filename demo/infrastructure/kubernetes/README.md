# Kubernetes Demo Infrastructure

This directory deploys the observability backends used by the demo: the OpenTelemetry Collector, Jaeger, Prometheus, and Grafana. The demo applications are not deployed here; configure them to export OTLP telemetry to the Collector.

The manifests use the same component versions and configuration as the Docker Compose stack. Data is ephemeral and is lost when the pods are replaced.
This setup is for local demos: Jaeger stores traces in memory, storage is ephemeral, and Grafana allows anonymous administrator access. Do not expose it publicly.

## Deploy

From the repository root, apply the Kustomize configuration:

```sh
kubectl apply -k demo/infrastructure/kubernetes
kubectl -n otel-demo rollout status deployment/jaeger
kubectl -n otel-demo rollout status deployment/prometheus
kubectl -n otel-demo rollout status deployment/otel-collector
kubectl -n otel-demo rollout status deployment/grafana
```

The Services are cluster-internal. Forward ports from separate terminals to access the UIs and send telemetry from an application running on your machine:

```sh
kubectl -n otel-demo port-forward service/grafana 3000:3000
kubectl -n otel-demo port-forward service/jaeger 16686:16686
kubectl -n otel-demo port-forward service/prometheus 9090:9090
kubectl -n otel-demo port-forward service/otel-collector 4317:4317 4318:4318
```

- Grafana: <http://localhost:3000> (anonymous administrator access is enabled for local demos)
- Jaeger: <http://localhost:16686>
- Prometheus: <http://localhost:9090>

Set `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317` and `OTEL_EXPORTER_OTLP_PROTOCOL=grpc` for a demo application running on your machine. Applications running in the `otel-demo` namespace can send telemetry to `http://otel-collector:4317` instead.

## Remove

```sh
kubectl delete -k demo/infrastructure/kubernetes
```
