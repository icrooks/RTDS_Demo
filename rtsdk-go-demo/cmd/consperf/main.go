// ConsPerf — ETA-Style Performance Consumer
//
// This application mirrors the behavior of the RTSDK ETA ConsPerf performance
// tool. It connects to ProvPerf, subscribes to a configurable number of items,
// and measures throughput (updates/sec), latency distribution (avg/p50/p95/p99/max),
// CPU utilization, and memory consumption.
//
// Key RTSDK concepts demonstrated:
//   - High-frequency binary message decoding (ETA codec layer)
//   - Latency measurement with nanosecond precision
//   - Percentile latency reporting (p50, p95, p99, max)
//   - Throughput tracking with periodic statistics
//   - Transport-level metrics (bytes received, messages decoded)
//   - Memory and CPU monitoring
//   - Steady-state performance measurement
//
// Usage:
//
//	go run ./cmd/consperf -host localhost -port 14002 -itemCount 1000 -duration 60
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"example.com/rtsdk-go-demo/pkg/admin"
	"example.com/rtsdk-go-demo/pkg/omm"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

var (
	host          = flag.String("host", "localhost", "ProvPerf hostname")
	port          = flag.Int("port", 14002, "ProvPerf port")
	itemCount     = flag.Int("itemCount", 1000, "Number of items to subscribe")
	testDuration  = flag.Int("duration", 30, "Test duration in seconds (0=forever)")
	statsInterval = flag.Duration("statsInterval", 5*time.Second, "Statistics reporting interval")
	warmup        = flag.Duration("warmup", 3*time.Second, "Warmup period before measuring")
	serviceName   = flag.String("service", "DIRECT_FEED", "Service name")
)

// ---------------------------------------------------------------------------
// Statistics
// ---------------------------------------------------------------------------

type stats struct {
	updates       uint64
	refreshes     uint64
	bytesReceived uint64
	errors        uint64

	// Latency samples (microseconds)
	latencyMu      sync.Mutex
	latencySamples []float64
}

var (
	currentStats stats
	globalStart  time.Time
	warmupDone   uint32
)

func (s *stats) addLatency(us float64) {
	s.latencyMu.Lock()
	s.latencySamples = append(s.latencySamples, us)
	s.latencyMu.Unlock()
}

func (s *stats) getAndResetLatencies() []float64 {
	s.latencyMu.Lock()
	samples := s.latencySamples
	s.latencySamples = make([]float64, 0, len(samples))
	s.latencyMu.Unlock()
	return samples
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100.0*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// ---------------------------------------------------------------------------
// Latency histogram for final report
// ---------------------------------------------------------------------------

type histogram struct {
	mu      sync.Mutex
	samples []float64
}

var finalHistogram histogram

func (h *histogram) add(us float64) {
	h.mu.Lock()
	h.samples = append(h.samples, us)
	h.mu.Unlock()
}

func (h *histogram) report() {
	h.mu.Lock()
	samples := make([]float64, len(h.samples))
	copy(samples, h.samples)
	h.mu.Unlock()

	if len(samples) == 0 {
		log.Println("  No latency samples collected")
		return
	}

	sort.Float64s(samples)

	sum := 0.0
	for _, v := range samples {
		sum += v
	}
	avg := sum / float64(len(samples))

	log.Printf("  Latency Distribution (%d samples):", len(samples))
	log.Printf("    Average:  %10.1f μs", avg)
	log.Printf("    P50:      %10.1f μs", percentile(samples, 50))
	log.Printf("    P90:      %10.1f μs", percentile(samples, 90))
	log.Printf("    P95:      %10.1f μs", percentile(samples, 95))
	log.Printf("    P99:      %10.1f μs", percentile(samples, 99))
	log.Printf("    P99.9:    %10.1f μs", percentile(samples, 99.9))
	log.Printf("    Max:      %10.1f μs", samples[len(samples)-1])
	log.Printf("    Min:      %10.1f μs", samples[0])

	// Histogram buckets
	buckets := []float64{10, 50, 100, 500, 1000, 5000, 10000}
	log.Printf("  Latency Histogram:")
	prev := 0.0
	for _, b := range buckets {
		count := 0
		for _, v := range samples {
			if v > prev && v <= b {
				count++
			}
		}
		pct := float64(count) / float64(len(samples)) * 100
		bar := ""
		for i := 0; i < int(pct/2); i++ {
			bar += "█"
		}
		log.Printf("    %6.0f-%6.0fμs: %6d (%5.1f%%) %s", prev, b, count, pct, bar)
		prev = b
	}
	// Overflow
	count := 0
	for _, v := range samples {
		if v > buckets[len(buckets)-1] {
			count++
		}
	}
	if count > 0 {
		pct := float64(count) / float64(len(samples)) * 100
		log.Printf("    >%6.0fμs:     %6d (%5.1f%%)", buckets[len(buckets)-1], count, pct)
	}
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	flag.Parse()
	runtime.GOMAXPROCS(runtime.NumCPU())
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	fmt.Println()
	log.Printf("═══════════════════════════════════════════════════════════════")
	log.Printf("  RTSDK Go Demo — ETA-Style Performance Consumer (ConsPerf)")
	log.Printf("  Target:     %s:%d | Service: %s", *host, *port, *serviceName)
	log.Printf("  Items:      %d", *itemCount)
	log.Printf("  Duration:   %ds", *testDuration)
	log.Printf("  Warmup:     %s", *warmup)
	log.Printf("  CPUs:       %d", runtime.NumCPU())
	log.Printf("═══════════════════════════════════════════════════════════════")
	fmt.Println()

	// --- Connect ---
	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Printf("[CONN] Connecting to %s...", addr)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		log.Fatalf("[CONN] Failed to connect: %v", err)
	}
	defer conn.Close()
	log.Printf("[CONN] Connected")

	// Increase socket buffer
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetReadBuffer(4 * 1024 * 1024)
		tcpConn.SetNoDelay(true)
	}

	// --- Admin negotiation ---
	loginReq := admin.MakeLoginRequest("GoConsPerf")
	data, _ := omm.Encode(loginReq)
	conn.Write(data)

	loginResp, _ := omm.Decode(conn)
	if loginResp.State.Data != omm.DataOk {
		log.Fatalf("[LOGIN] Rejected: %s", loginResp.State)
	}
	log.Printf("[LOGIN] Accepted")

	dirReq := admin.MakeDirectoryRequest()
	data, _ = omm.Encode(dirReq)
	conn.Write(data)
	omm.Decode(conn) // consume directory response

	// --- Subscribe (send item count to ProvPerf) ---
	itemReq := &omm.Message{
		Type:     omm.MsgTypeRequest,
		Domain:   omm.DomainMarketPrice,
		StreamID: admin.FirstItemStreamID,
		Name:     "PERF_BATCH",
		Service:  *serviceName,
		SeqNum:   uint64(*itemCount),
	}
	data, _ = omm.Encode(itemReq)
	conn.Write(data)
	log.Printf("[SUB] Requested %d items", *itemCount)

	// --- Receive RefreshMsgs ---
	log.Printf("[PERF] Receiving initial images...")
	refreshCount := 0
	for refreshCount < *itemCount {
		msg, err := omm.Decode(conn)
		if err != nil {
			log.Fatalf("[PERF] Error receiving refresh: %v", err)
		}
		if msg.Type == omm.MsgTypeRefresh {
			refreshCount++
		}
	}
	log.Printf("[PERF] Received %d RefreshMsgs", refreshCount)
	atomic.StoreUint64(&currentStats.refreshes, uint64(refreshCount))

	// --- Warmup ---
	log.Printf("[PERF] Warmup period: %s", *warmup)
	time.Sleep(*warmup)
	atomic.StoreUint32(&warmupDone, 1)

	globalStart = time.Now()
	log.Printf("[PERF] Measurement started — streaming updates...")
	fmt.Println()

	// --- Graceful shutdown ---
	done := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		close(done)
	}()

	if *testDuration > 0 {
		go func() {
			time.Sleep(time.Duration(*testDuration) * time.Second)
			close(done)
		}()
	}

	// --- Message processing goroutine ---
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}

			msg, err := omm.Decode(conn)
			if err != nil {
				select {
				case <-done:
					return
				default:
					atomic.AddUint64(&currentStats.errors, 1)
					continue
				}
			}

			recvTime := time.Now().UnixNano()

			if msg.Type == omm.MsgTypeUpdate {
				atomic.AddUint64(&currentStats.updates, 1)

				// Latency measurement (only for timestamped messages)
				if msg.SendTimeNs > 0 && atomic.LoadUint32(&warmupDone) == 1 {
					latencyNs := recvTime - msg.SendTimeNs
					latencyUs := float64(latencyNs) / 1000.0
					if latencyUs > 0 && latencyUs < 1_000_000 { // Sanity: < 1 second
						currentStats.addLatency(latencyUs)
						finalHistogram.add(latencyUs)
					}
				}
			}
		}
	}()

	// --- Periodic statistics ---
	var lastUpdates uint64
	var memStats runtime.MemStats
	statsTicker := time.NewTicker(*statsInterval)

	for {
		select {
		case <-done:
			statsTicker.Stop()
			printFinalReport()
			return

		case <-statsTicker.C:
			updates := atomic.LoadUint64(&currentStats.updates)
			deltaUpdates := updates - lastUpdates
			rate := float64(deltaUpdates) / statsInterval.Seconds()

			// Get latency stats for this interval
			latencies := currentStats.getAndResetLatencies()
			sort.Float64s(latencies)

			var avgLat, p50, p95, p99, maxLat float64
			if len(latencies) > 0 {
				sum := 0.0
				for _, v := range latencies {
					sum += v
				}
				avgLat = sum / float64(len(latencies))
				p50 = percentile(latencies, 50)
				p95 = percentile(latencies, 95)
				p99 = percentile(latencies, 99)
				maxLat = latencies[len(latencies)-1]
			}

			runtime.ReadMemStats(&memStats)
			heapMB := float64(memStats.HeapAlloc) / 1024 / 1024

			elapsed := time.Since(globalStart).Seconds()

			log.Printf("[STATS] t=%.0fs | rate=%8.0f upd/s | lat avg=%7.1f p50=%7.1f p95=%7.1f p99=%7.1f max=%8.1f μs | heap=%.1fMB | total=%d",
				elapsed, rate, avgLat, p50, p95, p99, maxLat, heapMB, updates)

			lastUpdates = updates
		}
	}
}

func printFinalReport() {
	elapsed := time.Since(globalStart)
	totalUpdates := atomic.LoadUint64(&currentStats.updates)
	avgRate := float64(totalUpdates) / elapsed.Seconds()
	errors := atomic.LoadUint64(&currentStats.errors)

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	fmt.Println()
	log.Printf("═══════════════════════════════════════════════════════════════")
	log.Printf("  PERFORMANCE TEST FINAL REPORT")
	log.Printf("═══════════════════════════════════════════════════════════════")
	log.Printf("  Test Duration:    %s", elapsed.Round(time.Millisecond))
	log.Printf("  Items Subscribed: %d", *itemCount)
	log.Printf("  Total Refreshes:  %d", atomic.LoadUint64(&currentStats.refreshes))
	log.Printf("  Total Updates:    %d", totalUpdates)
	log.Printf("  Average Rate:     %.0f updates/sec", avgRate)
	log.Printf("  Decode Errors:    %d", errors)
	log.Printf("  Heap Memory:      %.1f MB", float64(memStats.HeapAlloc)/1024/1024)
	log.Printf("  Total Alloc:      %.1f MB", float64(memStats.TotalAlloc)/1024/1024)
	log.Printf("  GC Cycles:        %d", memStats.NumGC)
	log.Printf("  Goroutines:       %d", runtime.NumGoroutine())
	log.Printf("───────────────────────────────────────────────────────────────")
	finalHistogram.report()
	log.Printf("═══════════════════════════════════════════════════════════════")
}
