Part 1 deep dive: what a "vCPU" really is 👇

A vCPU is usually one hyper-thread (half a physical core), scheduled by a hypervisor. How much of it you get depends on the plan:

• Dedicated / compute-optimised (c-family, CPU-Optimized): full thread, reserved
• General-purpose (m-family, standard flex): full thread, minor contention
• Burstable / shared (t3/t4g, e2 shared-core, DO Basic): a fraction + credits

Why Go/Rust/C++ feel it first: efficient multi-threaded runtimes actually use every core you give them. A Go service will run 16 workers flat out, so it hits every ceiling sooner.

Quick check on your box right now:
• top → "st" above a few % = noisy neighbour
• cat /sys/fs/cgroup/cpu.stat → nr_throttled climbing = container quota throttling

Part 2 (compute-bound: buy real cores + size GOMAXPROCS/Tokio/OpenMP threads) coming next.
