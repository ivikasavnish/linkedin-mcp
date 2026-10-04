**Part 2/5: Compute-bound Go/Rust/C++ → buy real cores** 🔥

Compute-bound = CPU near your limit while network/disk sit idle. Encoding, compression, crypto, heavy parsing, pricing/risk math, search, matching engines.

Why burstable/shared is a trap here:
• Credits that took a day to earn drain in an hour
• Baseline 20% on 2 vCPU = 0.4 cores. Not 20% slower, 5× slower
• Steal adds run-to-run jitter

Pick compute-optimised / dedicated vCPU (AWS c-family, GCP c3, DO CPU-Optimized, Hetzner CCX).

Then size threads to the CPU you really have:
• Go: GOMAXPROCS respects container limits since Go 1.25. Older? use automaxprocs
• Rust: available_parallelism() reads cgroup quota; Tokio/Rayon follow it
• C++: hardware_concurrency() returns HOST cores, ignores limits. Read /sys/fs/cgroup/cpu.max or set OMP_NUM_THREADS

Rule: busy threads ≤ CPU limit. More threads only burn quota faster, then everything freezes.

Bonus: 2 vCPU is often 2 hyper-threads of 1 core. For AVX-heavy math the 2nd adds 10–30%. Benchmark before paying per vCPU.
