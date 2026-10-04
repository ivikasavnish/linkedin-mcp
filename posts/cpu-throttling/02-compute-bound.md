# Compute-Bound Go, Rust and C++: Buy Real Cores (Part 2/5)

If your service spends most of its time *computing* rather than *waiting*, the CPU class is the single biggest performance lever you have. More than most code optimisations.

## What counts as compute-bound

- Image/video encoding, compression (zstd, gzip), hashing, encryption
- Parsing and transforming large payloads (JSON, protobuf, CSV, logs)
- Numerical work: pricing models, risk calc, ML inference on CPU, simulations
- Search/indexing, regex-heavy pipelines, query engines
- Game servers, matching engines, order-book processing

Quick test: under load, is CPU near 100% of what you're allowed while the network/disk are underused? Compute-bound.

## Why burstable and shared CPUs are a trap here

1. **Credits drain fast.** A compute job runs above baseline continuously. Credits that took a day to earn are gone in an hour.
2. **Throughput becomes non-linear.** At baseline 20%, a 2 vCPU burstable is effectively 0.4 cores. Your job doesn't get 20% slower; it gets 5× slower.
3. **Steal adds jitter.** Batch time varies run to run. Hard to capacity-plan, hard to benchmark.
4. **"Unlimited" mode is often pricier** than just buying a compute-optimised instance.

**Pick:** dedicated vCPU / compute-optimised families (AWS c-family, GCP c3/c2d, Azure F/Fsv2, DO CPU-Optimized, Hetzner CCX, OCI standard shapes with full OCPUs).

## Match your runtime threads to your real CPU

Buying the right box is half of it. The other half: don't let the runtime spawn more busy threads than the CPU you can actually use — especially in containers.

**Go**
- `GOMAXPROCS` decides how many OS threads run Go code at once.
- Since **Go 1.25**, the default respects the cgroup CPU limit. On older versions it used the host's core count: a pod limited to 2 CPUs on a 64-core node ran 64 Ps and hit CFS throttling constantly.
- Older Go: use `go.uber.org/automaxprocs` or set `GOMAXPROCS` explicitly.

**Rust**
- `std::thread::available_parallelism()` reads cgroup quotas on Linux in modern Rust; Tokio and Rayon use it by default.
- If you pin thread counts manually (`worker_threads`, `RAYON_NUM_THREADS`), set them to the CPU limit, not `nproc` of the host.

**C++**
- `std::thread::hardware_concurrency()` returns host cores and **ignores cgroup limits**.
- Read `/sys/fs/cgroup/cpu.max` (cgroup v2) yourself, or pass thread count via config/env. Same for OpenMP (`OMP_NUM_THREADS`) and TBB.

Rule: **busy threads ≤ CPU limit**. More threads than quota doesn't give more throughput; it just burns quota faster and freezes everything.

## SMT matters for compute

Two vCPUs are often two hyper-threads of one physical core. For integer-heavy or AVX-heavy code, the second thread may add only 10–30%. If you're paying per vCPU for raw math, benchmark with SMT-off / "physical core" shapes where offered.

## Checklist

- [ ] Compute-optimised or dedicated vCPU family, not burstable/shared
- [ ] Runtime thread count = CPU limit (check GOMAXPROCS / Tokio / OpenMP)
- [ ] In Kubernetes: consider requests = limits (Guaranteed QoS) and the static CPU manager for pinned cores on latency-critical pods
- [ ] Benchmark on the same instance type you run in prod

## Next

Part 3: IO-heavy services — why a medium CPU is often the smarter buy, and the cases where that advice breaks.

#Golang #Rust #Cpp #PerformanceEngineering #CloudComputing #Kubernetes
