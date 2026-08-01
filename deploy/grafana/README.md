# Grafana dashboard

Import [`valve-dashboard.json`](./valve-dashboard.json) into Grafana (Dashboards → Import).

Point the datasource variable at the Prometheus that scrapes `valved` `/metrics` (Compose scrapes on `:9091` via [`../prometheus/prometheus.yml`](../prometheus/prometheus.yml)).
