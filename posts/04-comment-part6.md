**Bonus 6/5: Measure pods from cgroups, not CPU %** 📈

CFS acts every 100ms. Prometheus scrapes every 15–30s. A CPU % gauge averages the freezes away. Use cgroup counters + rate():

1️⃣ Throttle ratio per container:
rate(container_cpu_cfs_throttled_periods_total{container!=""}[5m]) / rate(container_cpu_cfs_periods_total[5m])

2️⃣ Seconds frozen per second:
rate(container_cpu_cfs_throttled_seconds_total[5m])

3️⃣ Usage vs limit (cAdvisor + kube-state-metrics):
sum by (namespace,pod,container) (rate(container_cpu_usage_seconds_total{container!=""}[5m])) / sum by (namespace,pod,container) (kube_pod_container_resource_limits{resource="cpu"})

Alert: throttle ratio > 5% while usage/limit < 70% → bursty threads, not lack of CPU. Fix thread count or limit.

Grafana tip: plot 2️⃣ next to p99 latency. Spikes line up.
