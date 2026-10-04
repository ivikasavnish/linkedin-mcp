**Part 3/5: IO-heavy services → medium CPUs win** 🌊

IO-bound = CPU at 10–40% under load, latency dominated by DB/network. CRUD APIs, gateways, webhooks, queue consumers, websocket servers.

Why medium (general-purpose, even burstable) works:
• Idle CPU earns credits; spikes get burst
• A blocked goroutine/Tokio task costs KBs of RAM, not a core
• RAM and network matter more → general-purpose has better RAM per $
• Several small boxes > one big one: redundancy, no single credit drain

When this breaks:
• TLS termination: handshakes are CPU-heavy
• Big JSON/protobuf bodies, response compression
• Tight p99 SLO: throttling + steal hurt tails even at low avg CPU
• Heavy GC from allocation churn
• Mixed boxes: nightly report drains the credits your API needed

Setup: in Kubernetes set CPU requests accurately, limits loose (or none) for latency-sensitive IO services. Move PDF/image/encode work to a separate compute pool.
