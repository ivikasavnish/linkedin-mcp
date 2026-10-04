**Your Go service isn't slow. Your CPU is being rationed.** ⏱️

Same binary. Same load test. One VM gives p99 of 40ms, another gives 400ms. Code didn't change. The CPU did.

Most cloud "vCPUs" are not a full core you own. Three common ways they get throttled:

**1. Burst credits**
Burstable instances (AWS t3/t4g, OCI burstable flex, GCP e2 shared-core) give you a baseline, often 10–40% of a core. Spend more and you burn credits. Credits gone → back to baseline, or extra billing.

**2. Shared vCPU / noisy neighbours**
On shared plans your vCPU waits while someone else's runs. Shows up as `st` (steal) in top. Your code is ready, the hypervisor says "wait".

**3. Container CPU limits (CFS quota)**
`limits.cpu: 2` means 200ms of CPU per 100ms window. 8 busy threads burn it in 25ms, then sit frozen for 75ms. Average usage looks fine. Tail latency explodes.

**Rule of thumb I now follow:**
🔥 Compute-heavy (Go/Rust/C++ number crunching, compression, encoding, crypto, parsing) → dedicated or compute-optimised cores. These languages *will* saturate a core, so they hit the ceiling first.
🌊 IO-heavy (APIs mostly waiting on DB, network, disk) → medium / general-purpose or even burstable is fine. The CPU idles while you wait.

Caveat: "IO-heavy" with heavy TLS, JSON, or tight p99 SLOs still needs care. Part 3 covers it.

Starting a 5-part series on this:
1️⃣ CPU throttling: the hidden tax (today)
2️⃣ Compute-bound Go/Rust/C++: buy real cores
3️⃣ IO-heavy services: why medium CPUs win
4️⃣ How to detect throttling in 5 commands
5️⃣ Decision matrix + cost math

Have you ever chased a "performance bug" that turned out to be the instance type? 👇

#CloudComputing #Golang #Rust #Cpp #Kubernetes #PerformanceEngineering #DevOps
