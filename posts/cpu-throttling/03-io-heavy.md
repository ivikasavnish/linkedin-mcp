# IO-Heavy Services: Why Medium CPUs Win (Part 3/5)

Most backend services don't compute much. They wait: for the database, for a downstream API, for disk, for the client. For these, paying for dedicated cores is often burning money.

## What IO-heavy looks like

- CRUD APIs in front of Postgres/MySQL
- API gateways, proxies, webhooks, fan-out to other services
- Queue consumers that mostly call external APIs
- Chat/notification/websocket servers holding many idle connections
- Scrapers and crawlers bottlenecked by remote servers

Quick test: under load, CPU sits at 10–40% while request latency is dominated by DB/network time. IO-bound.

## Why medium (general-purpose, even burstable) works

1. **Idle CPU earns credits.** A service at 15% average stays under a burstable baseline most of the day and bursts during spikes.
2. **Go and Rust async are cheap at waiting.** A goroutine or Tokio task blocked on IO costs a few KB of memory, not a CPU core. One modest CPU can hold tens of thousands of connections.
3. **Memory and network matter more.** For IO-heavy work, choose by RAM (connection pools, caches) and network bandwidth/PPS first. General-purpose families give a better RAM-per-dollar ratio than compute-optimised ones.
4. **Scale out beats scale up.** Several small instances behind a load balancer give redundancy and survive credit drain on any one box.

**Pick:** general-purpose families (AWS m-family / t-family for small services, GCP e2/n2, OCI flex, DO Basic/General Purpose).

## When this advice breaks

"IO-heavy" is not always "CPU-light". Watch for:

- **TLS termination.** Handshakes are CPU-heavy. A gateway terminating thousands of new TLS connections per second is compute-bound in disguise.
- **Serialization.** Big JSON bodies, protobuf marshalling, compression of responses. Profile it; it's often the top CPU consumer in "simple" APIs.
- **Tight p99 SLOs.** CFS throttling and steal hurt tail latency even at low average usage. If p99 matters more than cost, avoid shared/burstable even for IO workloads, and avoid tight CPU limits in Kubernetes (or set limits well above requests).
- **GC pressure.** Go/Java services allocating heavily spend real CPU in GC. At baseline CPU, GC pauses stretch.
- **Mixed workloads.** An API that also runs a nightly report or image resize on the same box. Split the compute job out, or the batch drains the credits your API needed.

## Practical setup

- Start medium/general-purpose. Measure (Part 4).
- In Kubernetes, set CPU **requests** accurately; set **limits** loosely or not at all for latency-sensitive IO services, and rely on requests for scheduling.
- Alarm on credit balance (burstable) and steal time (shared).
- Move the CPU-hungry parts (resizing, PDFs, encoding) to a separate compute-optimised worker pool.

## Next

Part 4: How to detect throttling in 5 commands — so you choose the CPU class from data, not guesses.

#CloudComputing #Golang #Rust #Backend #DevOps #CostOptimization
