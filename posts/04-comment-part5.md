**Part 5/5: Decision matrix + cost math** 📊

• Batch compute (encode, ML on CPU, sims), 80–100% CPU → compute-optimised, try spot
• Compute API (pricing, matching, search), latency-critical → dedicated cores, requests = limits
• Typical CRUD/gateway, 10–40% → general-purpose
• Internal/cron/staging, <15% → burstable
• IO API with strict p99 → general-purpose, no tight CPU limits
• TLS-heavy edge/proxy → compute-optimised

Language is a hint, not the rule. A Go CRUD API is still IO-bound.

Cost math: compare price per *sustained* core.
Burstable 2 vCPU @ 20% baseline = 0.4 cores. At half the price of a 2-core compute box, that's 2.5× more per core for compute work. For an IO service at 10% CPU, same burstable saves 50% at equal latency.

Moves that beat instance shopping:
1. Split compute from IO via a queue
2. Threads = CPU limit
3. Spot for batch
4. Re-measure quarterly; workloads drift

Series done. What should the next one cover? 👇
