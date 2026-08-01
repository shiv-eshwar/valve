# Helm chart: valve

Minimal chart for Redis + `valved` (mirrors [`deploy/k8s`](../../k8s)).

```bash
helm template valve ./deploy/helm/valve
helm upgrade --install valve ./deploy/helm/valve --set image.repository=ghcr.io/example/valve --set image.tag=v0.2.0
```
