// Consumer — EMA-Style Market Price Consumer Demo
//
// This application mirrors the behavior of the RTSDK EMA Consumer 200_MP_Streaming
// example. It connects to an Interactive Provider, automatically negotiates Login
// and Directory admin domains, then subscribes to Market Price items and extracts
// field values using native Go types (float64 for prices, int64 for volumes).
//
// Key RTSDK concepts demonstrated:
//   - Consumer pattern (connect → login → directory → subscribe → stream)
//   - Automatic admin domain negotiation — mimics EMA's OmmConsumer behavior
//   - OmmConsumerClient callback pattern (onRefreshMsg, onUpdateMsg, onStatusMsg)
//   - Native data extraction from FieldList (typed access, no string parsing)
//   - Market Price streaming with sparse update decoding
//   - Graceful stream closure
//
// Usage:
//
//	go run ./cmd/consumer -host localhost -port 14002 -items "IBM.N,AAPL.OQ,EUR="
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
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
	host        = flag.String("host", "localhost", "Provider hostname")
	port        = flag.Int("port", 14002, "Provider port")
	items       = flag.String("items", "IBM.N,AAPL.OQ,MSFT.OQ,EUR=,LSEG.L", "Comma-separated item names")
	serviceName = flag.String("service", "DIRECT_FEED", "Service name to request")
	duration    = flag.Duration("duration", 0, "Run duration (0 = run forever)")
	verbose     = flag.Bool("v", false, "Verbose field-level logging")
)

// ---------------------------------------------------------------------------
// Statistics tracking
// ---------------------------------------------------------------------------

var (
	refreshCount uint64
	updateCount  uint64
	statusCount  uint64
	startTime    time.Time
)

// ---------------------------------------------------------------------------
// Callback handlers — mirrors EMA OmmConsumerClient interface
// ---------------------------------------------------------------------------

func onRefreshMsg(msg *omm.Message) {
	atomic.AddUint64(&refreshCount, 1)

	bid := fieldReal(msg.Fields, omm.FID_BID)
	ask := fieldReal(msg.Fields, omm.FID_ASK)
	last := fieldReal(msg.Fields, omm.FID_LAST)
	name := fieldStr(msg.Fields, omm.FID_DSPLY_NAME)

	fmt.Println()
	log.Printf("╔═══ RefreshMsg ═══════════════════════════════════════════╗")
	log.Printf("║  Item:     %-12s  (%s)", msg.Name, name)
	log.Printf("║  Service:  %s", msg.Service)
	log.Printf("║  State:    %s", msg.State)
	log.Printf("║  Stream:   %d", msg.StreamID)
	log.Printf("╟──────────────────────────────────────────────────────────╢")
	log.Printf("║  BID      = %12.4f", bid)
	log.Printf("║  ASK      = %12.4f", ask)
	log.Printf("║  LAST     = %12.4f", last)

	if vol := fieldInt(msg.Fields, omm.FID_ACVOL_1); vol > 0 {
		log.Printf("║  VOLUME   = %12d", vol)
	}
	if high := fieldReal(msg.Fields, omm.FID_HIGH_1); high > 0 {
		log.Printf("║  HIGH     = %12.4f", high)
	}
	if low := fieldReal(msg.Fields, omm.FID_LOW_1); low > 0 {
		log.Printf("║  LOW      = %12.4f", low)
	}
	if netchng := fieldReal(msg.Fields, omm.FID_NETCHNG_1); netchng != 0 {
		sign := "+"
		if netchng < 0 {
			sign = ""
		}
		log.Printf("║  NETCHNG  = %s%.4f", sign, netchng)
	}
	log.Printf("╚══════════════════════════════════════════════════════════╝")
}

func onUpdateMsg(msg *omm.Message) {
	count := atomic.AddUint64(&updateCount, 1)

	bid := fieldReal(msg.Fields, omm.FID_BID)
	ask := fieldReal(msg.Fields, omm.FID_ASK)
	last := fieldReal(msg.Fields, omm.FID_LAST)
	vol := fieldInt(msg.Fields, omm.FID_ACVOL_1)
	netchng := fieldReal(msg.Fields, omm.FID_NETCHNG_1)

	sign := "+"
	if netchng < 0 {
		sign = ""
	}

	log.Printf("  UpdateMsg #%-6d %-12s BID=%10.4f  ASK=%10.4f  LAST=%10.4f  VOL=%10d  CHG=%s%.4f",
		count, msg.Name, bid, ask, last, vol, sign, netchng)

	if *verbose {
		for _, f := range msg.Fields {
			log.Printf("    %s", f)
		}
	}
}

func onStatusMsg(msg *omm.Message) {
	atomic.AddUint64(&statusCount, 1)
	log.Printf("  StatusMsg item=%q stream=%d state=%s", msg.Name, msg.StreamID, msg.State)
}

// ---------------------------------------------------------------------------
// Field extraction helpers — typed access like EMA's getDouble()/getInt()
// ---------------------------------------------------------------------------

func fieldReal(fl omm.FieldList, id omm.FieldID) float64 {
	if f := fl.Get(id); f != nil && f.Type == omm.FieldTypeReal {
		return f.RealVal
	}
	return 0
}

func fieldInt(fl omm.FieldList, id omm.FieldID) int64 {
	if f := fl.Get(id); f != nil && f.Type == omm.FieldTypeInt {
		return f.IntVal
	}
	return 0
}

func fieldStr(fl omm.FieldList, id omm.FieldID) string {
	if f := fl.Get(id); f != nil && f.Type == omm.FieldTypeString {
		return f.StrVal
	}
	return ""
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	flag.Parse()
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	itemList := strings.Split(*items, ",")
	for i := range itemList {
		itemList[i] = strings.TrimSpace(itemList[i])
	}

	fmt.Println()
	log.Printf("═══════════════════════════════════════════════════════════════")
	log.Printf("  RTSDK Go Demo — EMA-Style Market Price Consumer")
	log.Printf("  Target: %s:%d | Service: %s", *host, *port, *serviceName)
	log.Printf("  Items:  %s", strings.Join(itemList, ", "))
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
	log.Printf("[CONN] Connected to %s", addr)

	// --- Login (automatic, like EMA) ---
	loginReq := admin.MakeLoginRequest("GoConsumer")
	data, _ := omm.Encode(loginReq)
	conn.Write(data)
	log.Printf("[LOGIN] Sent login request user=%q", loginReq.Name)

	loginResp, err := omm.Decode(conn)
	if err != nil {
		log.Fatalf("[LOGIN] Failed to receive login response: %v", err)
	}
	if loginResp.State.Stream != omm.StreamOpen || loginResp.State.Data != omm.DataOk {
		log.Fatalf("[LOGIN] Login rejected: %s", loginResp.State)
	}
	log.Printf("[LOGIN] Login accepted: %s", loginResp.State)

	// --- Directory (automatic, like EMA) ---
	dirReq := admin.MakeDirectoryRequest()
	data, _ = omm.Encode(dirReq)
	conn.Write(data)

	dirResp, err := omm.Decode(conn)
	if err != nil {
		log.Fatalf("[DIR] Failed to receive directory: %v", err)
	}
	log.Printf("[DIR] Service discovered: %q state=%s", dirResp.Name, dirResp.State)

	// --- Subscribe to items ---
	streamMap := make(map[int32]string)
	for i, item := range itemList {
		streamID := admin.FirstItemStreamID + int32(i)
		req := admin.MakeItemRequest(streamID, item, *serviceName)
		data, _ = omm.Encode(req)
		conn.Write(data)
		streamMap[streamID] = item
		log.Printf("[SUB] Requested item=%q stream=%d", item, streamID)
	}

	startTime = time.Now()
	log.Printf("[STREAM] Waiting for data...\n")

	// --- Graceful shutdown ---
	done := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println()
		log.Println("[STREAM] Shutting down — closing streams...")
		for streamID := range streamMap {
			closeMsg := admin.MakeItemClose(streamID, omm.DomainMarketPrice)
			data, _ := omm.Encode(closeMsg)
			conn.Write(data)
		}
		close(done)
	}()

	// Duration timer
	if *duration > 0 {
		go func() {
			time.Sleep(*duration)
			log.Printf("[TIMER] Duration %s elapsed — shutting down", *duration)
			close(done)
		}()
	}

	// --- Message dispatch loop ---
	go func() {
		for {
			msg, err := omm.Decode(conn)
			if err != nil {
				select {
				case <-done:
					return
				default:
					log.Printf("[CONN] Read error: %v", err)
					close(done)
					return
				}
			}

			msg.RecvTimeNs = time.Now().UnixNano()

			switch msg.Type {
			case omm.MsgTypeRefresh:
				onRefreshMsg(msg)
			case omm.MsgTypeUpdate:
				onUpdateMsg(msg)
			case omm.MsgTypeStatus:
				onStatusMsg(msg)
			default:
				if *verbose {
					log.Printf("[MSG] Unhandled: %s", msg)
				}
			}
		}
	}()

	// --- Statistics ticker ---
	statsTicker := time.NewTicker(10 * time.Second)
	defer statsTicker.Stop()

	for {
		select {
		case <-done:
			printFinalStats()
			return
		case <-statsTicker.C:
			elapsed := time.Since(startTime).Seconds()
			updates := atomic.LoadUint64(&updateCount)
			rate := float64(updates) / elapsed
			log.Printf("[STATS] elapsed=%.0fs refreshes=%d updates=%d (%.1f/sec) statuses=%d",
				elapsed, atomic.LoadUint64(&refreshCount), updates, rate, atomic.LoadUint64(&statusCount))
		}
	}
}

func printFinalStats() {
	elapsed := time.Since(startTime)
	updates := atomic.LoadUint64(&updateCount)
	rate := float64(updates) / elapsed.Seconds()
	fmt.Println()
	log.Printf("═══════════════════════════════════════════════════════════════")
	log.Printf("  Final Statistics")
	log.Printf("  Duration:   %s", elapsed.Round(time.Second))
	log.Printf("  Refreshes:  %d", atomic.LoadUint64(&refreshCount))
	log.Printf("  Updates:    %d (avg %.1f/sec)", updates, rate)
	log.Printf("  Statuses:   %d", atomic.LoadUint64(&statusCount))
	log.Printf("═══════════════════════════════════════════════════════════════")
}
