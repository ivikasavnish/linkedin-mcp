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
