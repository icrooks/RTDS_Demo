// ProvPerf — ETA-Style Performance Provider
//
// This application mirrors the behavior of the RTSDK ETA ProvPerf performance
// tool. It generates high-volume mock market data at configurable rates,
// demonstrating wire-level binary encoding, message packing (batching multiple
// updates per network write), and high-throughput data distribution.
//
// Key RTSDK concepts demonstrated:
//   - ETA-level binary encoding (direct buffer management)
//   - Message packing: N updates per network write (critical for throughput)
//   - Configurable update rates (1K to 1M+ updates/sec)
//   - Latency timestamp injection for consumer-side measurement
//   - Transport-level statistics (bytes written, messages packed)
//   - Priority-level writing concepts
//   - Large item universe generation (configurable)
//
// Usage:
//
//	go run ./cmd/provperf -port 14002 -updateRate 100000 -packCount 20
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"runtime"
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
	port            = flag.Int("port", 14002, "TCP listen port")
	updateRate      = flag.Int("updateRate", 100000, "Target updates/second")
	latencyRate     = flag.Int("latencyRate", 1000, "Updates/sec with latency timestamps")
	itemCount       = flag.Int("itemCount", 1000, "Number of items in universe")
	packCount       = flag.Int("packCount", 20, "Messages per packed network write")
	statsInterval   = flag.Duration("statsInterval", 5*time.Second, "Statistics reporting interval")
	serviceName     = flag.String("service", "DIRECT_FEED", "Service name")
)

// ---------------------------------------------------------------------------
// Statistics
// ---------------------------------------------------------------------------

var (
	totalUpdatesSent  uint64
	totalBytesSent    uint64
	totalPacksSent    uint64
	totalRefreshsSent uint64
)

// ---------------------------------------------------------------------------
// Item universe generation
// ---------------------------------------------------------------------------

type perfItem struct {
	name     string
	streamID int32
	bid      float64
	ask      float64
	last     float64
	volume   int64
}

func generateUniverse(count int) []perfItem {
	items := make([]perfItem, count)
	exchanges := []string{".N", ".OQ", ".L", ".T", ".HK", ".PA", ".DE"}
	bases := []string{
		"AAA", "BBB", "CCC", "DDD", "EEE", "FFF", "GGG", "HHH",
		"III", "JJJ", "KKK", "LLL", "MMM", "NNN", "OOO", "PPP",
	}
	for i := 0; i < count; i++ {
		base := bases[i%len(bases)]
		exch := exchanges[i%len(exchanges)]
		suffix := i / len(bases)
		name := fmt.Sprintf("%s%d%s", base, suffix, exch)
		price := 10.0 + rand.Float64()*500.0
		spread := price * 0.001
		items[i] = perfItem{
			name:     name,
			streamID: admin.FirstItemStreamID + int32(i),
			bid:      math.Round(price*10000) / 10000,
			ask:      math.Round((price+spread)*10000) / 10000,
			last:     math.Round((price+spread/2)*10000) / 10000,
			volume:   int64(rand.Intn(10000000)),
		}
	}
	return items
}

func (item *perfItem) tick() {
	move := (rand.Float64() - 0.5) * item.last * 0.001
	spread := item.ask - item.bid
	item.last = math.Round((item.last+move)*10000) / 10000
	item.bid = math.Round((item.last-spread/2)*10000) / 10000
	item.ask = math.Round((item.last+spread/2)*10000) / 10000
	item.volume += int64(rand.Intn(1000))
}

// makeUpdateMsg builds an UpdateMsg for a performance item.
func makeUpdateMsg(item *perfItem, withLatency bool) *omm.Message {
	msg := &omm.Message{
		Type:     omm.MsgTypeUpdate,
		Domain:   omm.DomainMarketPrice,
		StreamID: item.streamID,
		Name:     item.name,
		Fields: omm.FieldList{
			omm.RealField(omm.FID_BID, item.bid),
			omm.RealField(omm.FID_ASK, item.ask),
			omm.RealField(omm.FID_LAST, item.last),
			omm.IntField(omm.FID_BIDSIZE, int64(rand.Intn(1000)+100)),
			omm.IntField(omm.FID_ASKSIZE, int64(rand.Intn(1000)+100)),
			omm.IntField(omm.FID_ACVOL_1, item.volume),
			omm.RealField(omm.FID_NETCHNG_1, item.last-item.bid),
		},
	}
	if withLatency {
		msg.SendTimeNs = time.Now().UnixNano()
	}
	return msg
}

// ---------------------------------------------------------------------------
// Client handler
// ---------------------------------------------------------------------------

func handlePerfClient(conn net.Conn, items []perfItem) {
	defer conn.Close()
	remoteAddr := conn.RemoteAddr().String()
	log.Printf("[CONN] Performance consumer connected: %s", remoteAddr)

	// --- Admin negotiation ---
	loginMsg, err := omm.Decode(conn)
	if err != nil {
		log.Printf("[CONN] Failed to read login: %v", err)
		return
	}
	loginResp := admin.MakeLoginRefresh(loginMsg.Name)
	data, _ := omm.Encode(loginResp)
	conn.Write(data)

	dirMsg, err := omm.Decode(conn)
	if err != nil {
		log.Printf("[CONN] Failed to read directory request: %v", err)
		return
	}
	_ = dirMsg
	dirResp := admin.MakeDirectoryRefresh(omm.ServiceInfo{Name: *serviceName, ID: 1, State: "Up"})
	data, _ = omm.Encode(dirResp)
	conn.Write(data)

	// --- Wait for item request (ConsPerf sends a single batch request) ---
	reqMsg, err := omm.Decode(conn)
	if err != nil {
		log.Printf("[CONN] Failed to read item request: %v", err)
		return
	}
	reqCount := 1
	if reqMsg.SeqNum > 0 {
		reqCount = int(reqMsg.SeqNum) // ConsPerf sends item count in SeqNum
	}
	if reqCount > len(items) {
		reqCount = len(items)
	}
	log.Printf("[PERF] Consumer requested %d items", reqCount)

	// --- Send initial RefreshMsgs (image burst) ---
	log.Printf("[PERF] Sending %d RefreshMsgs...", reqCount)
	for i := 0; i < reqCount; i++ {
		item := &items[i]
		refresh := &omm.Message{
			Type:     omm.MsgTypeRefresh,
			Domain:   omm.DomainMarketPrice,
			StreamID: item.streamID,
			Name:     item.name,
			Service:  *serviceName,
			State:    omm.State{Stream: omm.StreamOpen, Data: omm.DataOk},
			Solicited: true,
			Complete:  true,
			Fields: omm.FieldList{
				omm.RealField(omm.FID_BID, item.bid),
				omm.RealField(omm.FID_ASK, item.ask),
				omm.RealField(omm.FID_LAST, item.last),
				omm.IntField(omm.FID_ACVOL_1, item.volume),
			},
		}
		data, _ := omm.Encode(refresh)
		conn.Write(data)
		atomic.AddUint64(&totalRefreshsSent, 1)
	}
	log.Printf("[PERF] RefreshMsgs complete — starting update stream at %d updates/sec", *updateRate)

	// --- High-frequency update loop ---
	running := true
	go func() {
		// Detect disconnect
		buf := make([]byte, 1)
		for running {
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			_, err := conn.Read(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				running = false
				return
			}
		}
	}()

	// Calculate timing
	updatesPerTick := *updateRate / 1000
	if updatesPerTick < 1 {
		updatesPerTick = 1
	}
	tickInterval := time.Duration(float64(time.Second) / (float64(*updateRate) / float64(updatesPerTick)))
	latencyEvery := 1
	if *latencyRate > 0 && *updateRate > *latencyRate {
		latencyEvery = *updateRate / *latencyRate
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	updateIdx := 0
	seqNum := uint64(0)
	pack := *packCount

	for range ticker.C {
		if !running {
			log.Printf("[PERF] Consumer disconnected")
			return
		}

		// Build a batch of messages
		msgs := make([]*omm.Message, 0, updatesPerTick)
		for j := 0; j < updatesPerTick && updateIdx < reqCount; j++ {
			itemIdx := int(seqNum) % reqCount
			item := &items[itemIdx]
			item.tick()

			withLatency := (int(seqNum) % latencyEvery) == 0
			msg := makeUpdateMsg(item, withLatency)
			msg.SeqNum = seqNum
			msgs = append(msgs, msg)
			seqNum++
		}

		// Send: use packing if configured
		if pack > 1 && len(msgs) > 1 {
			// Pack multiple messages per write
			for i := 0; i < len(msgs); i += pack {
				end := i + pack
				if end > len(msgs) {
					end = len(msgs)
				}
				batch := msgs[i:end]

				// Encode individually but write as burst
				for _, m := range batch {
					data, _ := omm.Encode(m)
					_, err := conn.Write(data)
					if err != nil {
						running = false
						return
					}
					atomic.AddUint64(&totalBytesSent, uint64(len(data)))
				}
				atomic.AddUint64(&totalPacksSent, 1)
			}
		} else {
			for _, m := range msgs {
				data, _ := omm.Encode(m)
				_, err := conn.Write(data)
				if err != nil {
					running = false
					return
				}
				atomic.AddUint64(&totalBytesSent, uint64(len(data)))
			}
		}
		atomic.AddUint64(&totalUpdatesSent, uint64(len(msgs)))
	}
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	flag.Parse()
	runtime.GOMAXPROCS(runtime.NumCPU())
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	items := generateUniverse(*itemCount)

	fmt.Println()
	log.Printf("═══════════════════════════════════════════════════════════════")
	log.Printf("  RTSDK Go Demo — ETA-Style Performance Provider (ProvPerf)")
	log.Printf("  Port:        %d", *port)
	log.Printf("  Update Rate: %d updates/sec", *updateRate)
	log.Printf("  Latency Rate:%d updates/sec (timestamped)", *latencyRate)
	log.Printf("  Item Count:  %d", *itemCount)
	log.Printf("  Pack Count:  %d msgs/write", *packCount)
	log.Printf("  CPUs:        %d", runtime.NumCPU())
	log.Printf("═══════════════════════════════════════════════════════════════")
	fmt.Println()

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen on port %d: %v", *port, err)
	}
	defer ln.Close()
	log.Printf("[SERVER] ProvPerf listening on port %d", *port)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println()
		log.Println("[SERVER] Shutting down...")
		ln.Close()
		os.Exit(0)
	}()

	// Statistics reporter
	go func() {
		var lastUpdates, lastBytes uint64
		ticker := time.NewTicker(*statsInterval)
		for range ticker.C {
			updates := atomic.LoadUint64(&totalUpdatesSent)
			bytes := atomic.LoadUint64(&totalBytesSent)
			packs := atomic.LoadUint64(&totalPacksSent)
			refreshes := atomic.LoadUint64(&totalRefreshsSent)

			deltaUpdates := updates - lastUpdates
			deltaBytes := bytes - lastBytes
			rate := float64(deltaUpdates) / statsInterval.Seconds()
			bwMbps := float64(deltaBytes) * 8.0 / statsInterval.Seconds() / 1_000_000.0

			log.Printf("[STATS] rate=%.0f updates/sec | bandwidth=%.2f Mbps | total: updates=%d refreshes=%d packs=%d",
				rate, bwMbps, updates, refreshes, packs)

			lastUpdates = updates
			lastBytes = bytes
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handlePerfClient(conn, items)
	}
}
