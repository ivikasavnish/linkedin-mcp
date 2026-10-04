# Choosing a CPU Class: Decision Matrix + Cost Math (Part 5/5)

Four parts of theory and measurement. Here's how to decide.

## The matrix

| Workload | Avg CPU under load | Latency sensitivity | CPU class |
|---|---|---|---|
| Batch compute: encoding, compression, ML on CPU, simulations | 80–100% | Low | Compute-optimised (consider spot) |
| Compute API: pricing, matching, search, heavy parsing | 50–100% | High | Compute-optimised / dedicated, Guaranteed QoS |
| Typical CRUD / gateway API | 10–40% | Medium | General-purpose (medium) |
| Low-traffic internal service, cron, dev/staging | < 15% | Low | Burstable / shared |
| IO API with strict p99 SLO | 10–40% | Very high | General-purpose, no tight CPU limits |
| TLS-heavy edge / proxy | 40–80% | High | Compute-optimised |

Language is a hint, not the rule. Go/Rust/C++ often land in the top rows because teams pick them *for* compute. A Go CRUD API is still an IO workload.

## Cost math: the cheap box that isn't

Throughput per dollar is the number that matters, not price per vCPU.

Illustrative example (plug in your own prices):

- Burstable 2 vCPU, baseline 20% → **0.4 cores sustained**
- Compute-optimised 2 vCPU → **2 cores sustained**

If the burstable costs half as much, you're paying **2.5× more per sustained core** for a compute workload. Turn on unlimited mode and the surplus charges usually erase the saving entirely.

Flip it for an IO service averaging 10% CPU: the burstable never touches baseline, and you save half for identical latency.

Formula:

```
cost per unit of work = hourly price / (sustained cores × work per core-hour)
```

For burstable: sustained cores = vCPUs × baseline (once credits are gone).

## Architecture moves that beat instance shopping

1. **Split compute from IO.** API on medium boxes; heavy jobs to a compute pool via a queue. Each side gets the right CPU class.
2. **Right-size threads.** `GOMAXPROCS`, Tokio workers, OpenMP threads = CPU limit. Often a free 20–50% p99 improvement in containers.
3. **Requests accurate, limits loose** for IO services; **requests = limits** with pinned cores for latency-critical compute.
4. **Spot/preemptible** for batch compute: compute-optimised at a fraction of the price.
5. **Re-measure quarterly.** Workloads drift. Yesterday's IO service gains a PDF export and becomes compute-bound.

## TL;DR of the series

- vCPUs get throttled: credits, steal, CFS quota.
- Compute-heavy (where Go/Rust/C++ usually live) → buy real cores, size threads to them.
- IO-heavy → medium CPUs; spend on RAM/network instead.
- Measure steal, `nr_throttled`, credit balance before deciding.

Thanks for following along. What should the next series cover? 👇

#CloudComputing #CostOptimization #Golang #Rust #Cpp #SRE #DevOps
