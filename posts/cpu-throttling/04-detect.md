# How to Detect CPU Throttling in 5 Commands (Part 4/5)

Choose instance types from evidence. These five checks tell you whether you're compute-bound, IO-bound, or being throttled.

## 1. Steal time — is the hypervisor taking your CPU?

```bash
mpstat -P ALL 1 10     # look at %steal
# or: top → "st" in the %Cpu(s) line
```

- `< 1%` → fine
- `2–10%` sustained → noisy neighbour; consider dedicated vCPU
- spiky → explains random latency outliers

## 2. CFS throttling — is your container frozen?

```bash
# cgroup v2, inside the container or on the node:
cat /sys/fs/cgroup/cpu.stat
# nr_periods  nr_throttled  throttled_usec
cat /sys/fs/cgroup/cpu.max      # e.g. "200000 100000" = 2 CPUs
```

`nr_throttled / nr_periods` above a few percent on a latency-sensitive service is a red flag. In Prometheus:

```promql
rate(container_cpu_cfs_throttled_periods_total[5m])
  / rate(container_cpu_cfs_periods_total[5m])
```

## 3. Burst credits — are you living on borrowed CPU?

```bash
aws cloudwatch get-metric-statistics \
  --namespace AWS/EC2 --metric-name CPUCreditBalance \
  --dimensions Name=InstanceId,Value=i-xxxx \
  --start-time $(date -u -d '-1 day' +%FT%TZ) --end-time $(date -u +%FT%TZ) \
  --period 300 --statistics Minimum
```

Balance trending to zero → you're compute-bound on a burstable box. Move up a class. (Other clouds expose similar "baseline utilisation" metrics.)

## 4. Compute vs IO — where does time go?

```bash
vmstat 1 10        # us+sy high → compute; wa high → waiting on disk
pidstat -u -p <pid> 1   # per-process CPU
```

Then profile the process:

- **Go:** `go tool pprof http://host:6060/debug/pprof/profile?seconds=30`
- **Rust/C++:** `perf record -g -p <pid> -- sleep 30 && perf report`

If the top of the profile is your own math/parsing code → compute. If it's syscalls, `epoll_wait`, runtime park/scheduler → IO-waiting.

## 5. Runtime thread count vs CPU limit

```bash
nproc                                   # respects cgroup cpuset only
cat /sys/fs/cgroup/cpu.max              # the actual quota
ps -o nlwp= -p <pid>                    # threads in the process
```

- Go: log `runtime.GOMAXPROCS(0)` at startup. It should equal your CPU limit.
- C++: if you use `hardware_concurrency()`, it's probably the host core count. Fix it.

## Pods in Grafana: measure from cgroups, not CPU %

CFS enforces quota every 100ms; Prometheus scrapes every 15–30s. A CPU % gauge averages the freezes away. Use cgroup counters with `rate()`, pressure, and histograms.

**Throttle ratio per container (cAdvisor):**

```promql
sum by (namespace, pod, container) (rate(container_cpu_cfs_throttled_periods_total{container!=""}[5m]))
  / sum by (namespace, pod, container) (rate(container_cpu_cfs_periods_total{container!=""}[5m]))
```

**Seconds frozen per second** — plot next to p99 latency:

```promql
rate(container_cpu_cfs_throttled_seconds_total{container!=""}[5m])
```

**Usage vs limit (cAdvisor + kube-state-metrics):**

```promql
sum by (namespace, pod, container) (rate(container_cpu_usage_seconds_total{container!=""}[5m]))
  / sum by (namespace, pod, container) (kube_pod_container_resource_limits{resource="cpu"})
```

Throttle ratio > 5% while usage/limit < 70% → bursty threads, not lack of CPU.

**CPU pressure (PSI, cgroup v2)** — how long tasks *waited* for CPU, covering throttling, contention and steal:

```bash
cat /sys/fs/cgroup/<pod-cgroup>/cpu.pressure
# some avg10=12.50 avg60=8.10 avg300=3.20 total=...
```

```promql
rate(node_pressure_cpu_waiting_seconds_total[5m])                 # node, node-exporter
rate(container_pressure_cpu_waiting_seconds_total[5m])            # per container, newer cAdvisor/kubelet with PSI enabled
```

**Go scheduler latency histogram** (`runtime/metrics` `/sched/latencies:seconds`, enable with client_golang `WithGoCollectorRuntimeMetrics`):

```promql
histogram_quantile(0.99, sum by (le, pod) (rate(go_sched_latencies_seconds_bucket[5m])))
```

p99 in milliseconds means goroutines are queueing for a CPU.

**eBPF run-queue latency** (bcc `runqlat`, Coroot, Pixie): per-container scheduling delay with no code changes — works for Rust and C++ too.

Rule: alert on *waiting* (throttled seconds, PSI, sched latency), not on usage.

## Reading the results

| Signal | Meaning | Action |
|---|---|---|
| High us, low wa, credits draining | Compute-bound on burstable | Compute-optimised |
| High steal | Noisy neighbour | Dedicated vCPU |
| High nr_throttled, low avg CPU | Limits too tight / too many threads | Raise limit or cut threads |
| Low CPU, high latency in DB/network | IO-bound | Stay medium; fix the IO |

## Next

Part 5: The decision matrix and cost math — putting it all together.

#DevOps #SRE #Linux #Kubernetes #Golang #PerformanceEngineering
