**Bonus 7/5: Measure CPU *waiting*, not just usage** ⏳

Usage says how much CPU you got. Pressure says how long you waited for it.

1️⃣ PSI per cgroup (cgroup v2):
cat /sys/fs/cgroup/<pod-cgroup>/cpu.pressure
"some avg10=12.5" = tasks stalled waiting for CPU 12.5% of last 10s. Catches throttling + contention + steal in one number.
Grafana: node-exporter node_pressure_cpu_waiting_seconds_total (node), and per container in newer cAdvisor/kubelet with PSI enabled: rate(container_pressure_cpu_waiting_seconds_total[5m])

2️⃣ Go scheduler latency histogram (runtime/metrics /sched/latencies:seconds). Enable via client_golang WithGoCollectorRuntimeMetrics, then:
histogram_quantile(0.99, sum by (le,pod) (rate(go_sched_latencies_seconds_bucket[5m])))
p99 in ms = goroutines queued for a CPU. Clear throttling signal.

3️⃣ eBPF run-queue latency per container (bcc runqlat, Coroot, Pixie): no code changes, works for Rust/C++ too.

Rule: alert on waiting, not on usage.
