# CPU Throttling: The Hidden Tax on Your Cloud Bill (Part 1/5)

You pick an instance with "4 vCPUs". You assume four cores of compute, all the time. Often you get far less, and nothing in your code tells you.

This series is about choosing the right CPU class for the workload. Short version:

- **Compute-intensive work** (typical of Go, Rust, C++ backends doing real computation) → **dedicated / compute-optimised cores**.
- **IO-heavy work** (services that mostly wait on DB, network, disk) → **medium / general-purpose, sometimes burstable**.

Before choosing, understand what throttling is.

## What "a vCPU" actually is

A vCPU is usually one hardware thread (half a physical core with SMT/Hyper-Threading), scheduled by a hypervisor. How much of that thread you really get depends on the plan:

| Plan type | What you get | Examples |
|---|---|---|
| Dedicated / compute-optimised | Full thread, pinned or reserved | AWS c7i/c7g, GCP c3, DO CPU-Optimized, Hetzner CCX |
| General-purpose | Full thread, minor contention | AWS m7i, GCP n2, OCI standard flex |
| Burstable / shared | Fraction of a thread + credits | AWS t3/t4g, GCP e2 shared-core, OCI burstable flex, DO Basic |

## Throttle #1: Burst credits

Burstable instances have a **baseline** (e.g. 10–40% of each vCPU depending on size). Below baseline, you earn credits. Above it, you spend them.

When credits hit zero:
- **Standard mode:** CPU is capped at baseline. Your 2 vCPU box behaves like 0.4 of a core.
- **Unlimited mode:** CPU keeps running, you pay surplus charges.

A nightly batch job, a cache warm-up, or a traffic spike can drain credits in minutes. Then everything on that box slows down, including the latency-sensitive API sharing it.

## Throttle #2: Steal time

On shared hosts, your vCPU is runnable but the hypervisor is running someone else. Linux reports this as **steal** (`st` in `top`, `%steal` in `sar`/`mpstat`).

Steal of 5–10% under load means you are paying for CPU you can't use. Spiky steal means unpredictable latency.

## Throttle #3: Container CPU limits (CFS quota)

In Kubernetes/Docker, `limits.cpu: 2` is enforced by the Linux CFS bandwidth controller:

- Period: 100ms
- Quota: 200ms of CPU time per period (across all threads)

If your process runs 8 threads in parallel, it consumes 200ms of quota in **25ms of wall time**, then **all threads freeze for 75ms** until the next period.

Average CPU over a minute looks like "1.4 cores, fine". p99 latency looks terrible. This is the most common invisible throttle in production.

## Throttle #4: Thermal and frequency

Less common in cloud, very common on laptops, edge boxes, and cheap bare metal: sustained AVX-heavy work drops clock speed. Benchmarks on your laptop don't transfer.

## Why it hurts Go/Rust/C++ first

Compiled, multi-threaded languages are efficient. They actually *use* every core you give them. A Python service may never exceed one core because of the GIL; a Go service will happily run 16 goroutine workers flat out. So efficient runtimes hit every ceiling above sooner and harder.

## Next

Part 2: Compute-bound Go/Rust/C++ — why you should buy real cores, and how to size threads to the CPU you actually have.

*What's the worst throttling surprise you've hit in production?*

#CloudComputing #PerformanceEngineering #Golang #Rust #Cpp #Kubernetes
