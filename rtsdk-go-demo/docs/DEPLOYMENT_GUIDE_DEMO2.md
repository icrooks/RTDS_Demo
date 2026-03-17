# Deployment Guide — Demo 2: ETA-Style Performance Benchmark

## Overview

This guide walks through building and running the **ProvPerf + ConsPerf** pair, which demonstrates the ETA (Enterprise Transport API) performance-testing pattern from the LSEG Real-Time SDK. ProvPerf generates high-volume mock market data at configurable rates. ConsPerf measures throughput, latency distribution (avg/p50/p95/p99/max), CPU, and memory.

**Time to deploy: < 5 minutes** (assumes Go is installed)

---

## 1. Prerequisites

Same as Demo 1. For optimal results:

```bash
# Optional: Lock CPU frequency for consistent measurements (Linux)
sudo cpupower frequency-set -g performance

# Optional: Increase TCP buffer sizes
sudo sysctl -w net.core.rmem_max=16777216
sudo sysctl -w net.core.wmem_max=16777216
```

---

## 2. Build

```bash
cd rtsdk-go-demo
go build -o bin/provperf ./cmd/provperf
go build -o bin/consperf ./cmd/consperf
```

---

## 3. Run the Benchmark

### Terminal 1 — Start ProvPerf

```bash
./bin/provperf -port 14002 -updateRate 100000 -itemCount 1000 -packCount 20
```

**Parameters explained:**
- `-updateRate 100000`: Target 100K updates/second
- `-itemCount 1000`: Generate 1,000 mock instruments
- `-packCount 20`: Batch 20 messages per network write (critical for throughput)

### Terminal 2 — Start ConsPerf

```bash
./bin/consperf -host localhost -port 14002 -itemCount 1000 -duration 30 -warmup 3s
```

**Expected output:**

```
[PERF] Received 1000 RefreshMsgs
[PERF] Warmup period: 3s
[PERF] Measurement started — streaming updates...

[STATS] t=5s  | rate=  98432 upd/s | lat avg=  42.3 p50=  38.1 p95= 112.4 p99= 245.7 max=  892.1 μs | heap=2.1MB
[STATS] t=10s | rate= 100124 upd/s | lat avg=  41.8 p50=  37.5 p95= 108.9 p99= 238.2 max=  756.3 μs | heap=2.1MB

═══════════════════════════════════════════════════════════════
  PERFORMANCE TEST FINAL REPORT
═══════════════════════════════════════════════════════════════
  Test Duration:    30.001s
  Items Subscribed: 1000
  Total Updates:    2,987,432
  Average Rate:     99,581 updates/sec
  Heap Memory:      2.1 MB
  GC Cycles:        8
  ───────────────────────────────────────────────────────────
  Latency Distribution (29,874 samples):
    Average:        42.1 μs
    P50:            37.8 μs
    P95:           110.2 μs
    P99:           241.5 μs
    Max:           892.1 μs
  Latency Histogram:
       0-    10μs:   2841 (9.5%)  ████
      10-    50μs:  17924 (60.0%) ██████████████████████████████
      50-   100μs:   5962 (20.0%) ██████████
     100-   500μs:   2847 (9.5%)  ████
     500-  1000μs:    298 (1.0%)  
═══════════════════════════════════════════════════════════════
```

### Scaling Up

```bash
# 500K updates/sec with 5,000 items
./bin/provperf -port 14002 -updateRate 500000 -itemCount 5000
./bin/consperf -host localhost -port 14002 -itemCount 5000 -duration 60

# One-click with custom parameters
./scripts/demo.sh perf 500000 5000 60
```

---

## 4. Understanding the Output

### Periodic Statistics

Every 5 seconds, ConsPerf reports:
- **rate**: Sustained updates/sec in this interval
- **lat avg/p50/p95/p99/max**: Latency percentiles in microseconds
- **heap**: Go runtime heap memory allocation
- **total**: Cumulative update count

### Final Report

- **Latency Distribution**: Statistical summary across all timestamped samples
- **Latency Histogram**: Bucket distribution showing where most latencies fall
- **GC Cycles**: Garbage collection count (lower = less jitter)

### Key RTSDK Concepts Visible

| Metric | RTSDK Concept |
|---|---|
| Updates/sec throughput | ETA codec + transport performance |
| Message packing (-packCount) | RSSL message packing — multiple msgs per write |
| Latency percentiles | SendTimeNs timestamps in wire protocol |
| Low heap usage | Efficient binary encoding (no JSON allocation) |

---

## 5. Troubleshooting

| Problem | Cause | Solution |
|---|---|---|
| Throughput much lower than target | Container/VM overhead | Run on bare metal; increase `-packCount` |
| High latency variance | CPU frequency scaling | Lock to `performance` governor |
| `No latency samples` | latencyRate too low | Increase `-latencyRate` flag on ProvPerf |
| OOM on high item counts | Too many items for available RAM | Reduce `-itemCount` |
| ProvPerf not reaching target rate | Timer granularity | Increase `-packCount` to batch more per tick |

---

## 6. Demo Talking Points

- ETA gives wire-level control — every buffer allocation and write is explicit
- Message packing batches multiple small updates into single network writes (critical for throughput beyond 100K/sec)
- Latency measurement uses nanosecond timestamps embedded in the wire protocol
- The same ProvPerf/ConsPerf architecture is used in production RTSDK to validate infrastructure performance
- Binary RWF encoding is 3-5x more efficient than JSON for market data
- Results scale near-linearly with additional CPU cores using Go's goroutine model
