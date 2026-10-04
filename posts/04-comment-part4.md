**Part 4/5: Detect throttling in 5 checks** 🔍

1️⃣ Steal: mpstat -P ALL 1 → %steal. Over 2–10% sustained = noisy neighbour

2️⃣ Container throttling: cat /sys/fs/cgroup/cpu.stat → nr_throttled / nr_periods above a few % = red flag. Prometheus: container_cpu_cfs_throttled_periods_total ÷ container_cpu_cfs_periods_total

3️⃣ Burst credits: CloudWatch CPUCreditBalance trending to 0 = compute-bound on burstable

4️⃣ Compute vs IO: vmstat 1 → high us/sy = compute, high wa = disk wait. Then profile: go tool pprof, or perf record -g for Rust/C++. Your own code on top = compute; epoll_wait/park = waiting

5️⃣ Threads vs limit: cat /sys/fs/cgroup/cpu.max vs ps -o nlwp= -p PID. Log GOMAXPROCS at startup; it should equal the limit

Reading it:
• High us + draining credits → compute-optimised
• High steal → dedicated vCPU
• High nr_throttled + low avg CPU → raise limit or cut threads
• Low CPU + slow DB/network → stay medium, fix the IO
