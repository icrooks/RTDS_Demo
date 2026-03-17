// IProvider — EMA-Style Interactive Provider Demo
//
// This application mirrors the behavior of the RTSDK EMA IProvider 100_MP_Streaming
// example. It listens on a TCP port, accepts consumer connections, handles Login
// and Directory admin domains automatically, and publishes streaming Market Price
// data (RefreshMsg + UpdateMsg) for requested instruments.
//
// Key RTSDK concepts demonstrated:
//   - Interactive Provider pattern (accept connections, respond to requests)
//   - Automatic admin domain handling (Login, Directory) — mimics EMA behavior
//   - Market Price domain with real FID numbers (BID=22, ASK=25, LAST=6, etc.)
//   - RefreshMsg (initial snapshot) + UpdateMsg (streaming ticks)
//   - Multi-client support with concurrent goroutines
//   - Service advertisement via Source Directory
//
// Usage:
//
//	go run ./cmd/iprovider -port 14002
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
	"sync"
	"syscall"
	"time"

	"example.com/rtsdk-go-demo/pkg/admin"
	"example.com/rtsdk-go-demo/pkg/omm"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

var (
	port        = flag.Int("port", 14002, "TCP listen port (matches RTSDK default)")
	tickRate    = flag.Duration("tick", 1*time.Second, "Update tick interval")
	serviceName = flag.String("service", "DIRECT_FEED", "Service name advertised in Directory")
	verbose     = flag.Bool("v", false, "Verbose logging")
)

// ---------------------------------------------------------------------------
// Simulated instrument universe — realistic exchange data
// ---------------------------------------------------------------------------

type instrument struct {
	name    string
	bid     float64
	ask     float64
	last    float64
	high    float64
	low     float64
	open    float64
	volume  int64
	display string
}

var universe = []instrument{
	{"IBM.N", 185.42, 185.55, 185.48, 186.20, 184.30, 185.00, 4231000, "INTL BUS MACHINES"},
	{"AAPL.OQ", 192.35, 192.50, 192.42, 193.10, 191.80, 192.00, 12450000, "APPLE INC"},
	{"MSFT.OQ", 415.20, 415.40, 415.30, 416.50, 413.90, 414.50, 8920000, "MICROSOFT CORP"},
	{"GOOG.OQ", 175.80, 175.95, 175.88, 176.40, 175.20, 175.50, 6780000, "ALPHABET INC-CL C"},
	{"JPM.N", 198.60, 198.80, 198.70, 199.50, 197.80, 198.20, 3560000, "JPMORGAN CHASE"},
	{"GS.N", 485.30, 485.70, 485.50, 487.00, 483.50, 484.80, 1230000, "GOLDMAN SACHS"},
	{"EUR=", 1.0892, 1.0894, 1.0893, 1.0910, 1.0875, 1.0885, 0, "EUR/USD SPOT"},
	{"GBP=", 1.2715, 1.2718, 1.2716, 1.2740, 1.2690, 1.2705, 0, "GBP/USD SPOT"},
	{"TRI.N", 168.45, 168.65, 168.55, 169.20, 167.80, 168.10, 2340000, "THOMSON REUTERS"},
	{"LSEG.L", 9845.00, 9855.00, 9850.00, 9880.00, 9810.00, 9830.00, 890000, "LSEG GROUP PLC"},
}

func findInstrument(name string) *instrument {
	for i := range universe {
		if universe[i].name == name {
			return &universe[i]
		}
	}
	return nil
}

// simulateTick creates realistic price movement.
func (inst *instrument) tick() {
	spread := inst.ask - inst.bid
	midMove := (rand.Float64() - 0.5) * inst.last * 0.002
	inst.last = math.Round((inst.last+midMove)*10000) / 10000
	inst.bid = math.Round((inst.last-spread/2)*10000) / 10000
	inst.ask = math.Round((inst.last+spread/2)*10000) / 10000
	if inst.last > inst.high {
		inst.high = inst.last
	}
	if inst.last < inst.low {
		inst.low = inst.last
	}
	inst.volume += int64(rand.Intn(10000))
}

// ---------------------------------------------------------------------------
// Client connection handler
// ---------------------------------------------------------------------------

type subscription struct {
	streamID int32
	inst     *instrument
}

func handleClient(conn net.Conn, svc omm.ServiceInfo) {
	defer conn.Close()
	remoteAddr := conn.RemoteAddr().String()
	log.Printf("[CONN] Client connected: %s", remoteAddr)

	var subs []subscription
	var subsMu sync.Mutex
	loggedIn := false
	running := true

	// --- Reader goroutine: process incoming requests ---
	go func() {
		for running {
			msg, err := omm.Decode(conn)
			if err != nil {
				if running {
					log.Printf("[CONN] Client %s disconnected: %v", remoteAddr, err)
				}
				running = false
				return
			}

			if *verbose {
				log.Printf("[RECV] %s", msg)
			}

			switch msg.Domain {
			case omm.DomainLogin:
				if msg.Type == omm.MsgTypeRequest {
					log.Printf("[LOGIN] User=%q from %s", msg.Name, remoteAddr)
					resp := admin.MakeLoginRefresh(msg.Name)
					data, _ := omm.Encode(resp)
					conn.Write(data)
					loggedIn = true
					log.Printf("[LOGIN] Accepted user=%q", msg.Name)
				}

			case omm.DomainDirectory:
				if msg.Type == omm.MsgTypeRequest {
					log.Printf("[DIR] Directory request from %s", remoteAddr)
					resp := admin.MakeDirectoryRefresh(svc)
					data, _ := omm.Encode(resp)
					conn.Write(data)
					log.Printf("[DIR] Advertised service=%q", svc.Name)
				}

			case omm.DomainMarketPrice:
				if msg.Type == omm.MsgTypeRequest {
					itemName := msg.Name
					inst := findInstrument(itemName)
					if inst == nil {
						status := &omm.Message{
							Type:     omm.MsgTypeStatus,
							Domain:   omm.DomainMarketPrice,
							StreamID: msg.StreamID,
							Name:     itemName,
							State: omm.State{
								Stream: omm.StreamClosed,
								Data:   omm.DataSuspect,
								Text:   fmt.Sprintf("Item %q not found in universe", itemName),
							},
						}
						data, _ := omm.Encode(status)
						conn.Write(data)
						log.Printf("[ITEM] Rejected unknown item=%q stream=%d", itemName, msg.StreamID)
						continue
					}

					refresh := &omm.Message{
						Type:     omm.MsgTypeRefresh,
						Domain:   omm.DomainMarketPrice,
						StreamID: msg.StreamID,
						Name:     itemName,
						Service:  svc.Name,
						State: omm.State{
							Stream: omm.StreamOpen,
							Data:   omm.DataOk,
							Text:   "All is well",
						},
						Solicited: true,
						Complete:  true,
						Fields:    makeSnapshot(inst),
					}
					data, _ := omm.Encode(refresh)
					conn.Write(data)
					log.Printf("[ITEM] RefreshMsg item=%q stream=%d BID=%.4f ASK=%.4f",
						itemName, msg.StreamID, inst.bid, inst.ask)

					subsMu.Lock()
					subs = append(subs, subscription{streamID: msg.StreamID, inst: inst})
					subsMu.Unlock()
				}

				if msg.Type == omm.MsgTypeClose {
					subsMu.Lock()
					for i := 0; i < len(subs); i++ {
						if subs[i].streamID == msg.StreamID {
							log.Printf("[ITEM] Closed stream=%d item=%q", msg.StreamID, subs[i].inst.name)
							subs = append(subs[:i], subs[i+1:]...)
							break
						}
					}
					subsMu.Unlock()
				}
			}
		}
	}()

	// --- Writer loop: publish updates at tick rate ---
	ticker := time.NewTicker(*tickRate)
	defer ticker.Stop()

	for range ticker.C {
		if !running || !loggedIn {
			if !running {
				return
			}
			continue
		}

		subsMu.Lock()
		for _, sub := range subs {
			sub.inst.tick()
			update := &omm.Message{
				Type:     omm.MsgTypeUpdate,
				Domain:   omm.DomainMarketPrice,
				StreamID: sub.streamID,
				Name:     sub.inst.name,
				Service:  svc.Name,
				Fields:   makeUpdate(sub.inst),
			}
			data, _ := omm.Encode(update)
			_, err := conn.Write(data)
			if err != nil {
				running = false
				subsMu.Unlock()
				return
			}
		}
		count := len(subs)
		subsMu.Unlock()

		if *verbose && count > 0 {
			log.Printf("[TICK] Published %d updates to %s", count, remoteAddr)
		}
	}
}

func makeSnapshot(inst *instrument) omm.FieldList {
	return omm.FieldList{
		omm.StrField(omm.FID_DSPLY_NAME, inst.display),
		omm.RealField(omm.FID_BID, inst.bid),
		omm.RealField(omm.FID_ASK, inst.ask),
		omm.RealField(omm.FID_LAST, inst.last),
		omm.IntField(omm.FID_BIDSIZE, int64(rand.Intn(500)+100)),
		omm.IntField(omm.FID_ASKSIZE, int64(rand.Intn(500)+100)),
		omm.IntField(omm.FID_ACVOL_1, inst.volume),
		omm.RealField(omm.FID_NETCHNG_1, inst.last-inst.open),
		omm.RealField(omm.FID_HIGH_1, inst.high),
		omm.RealField(omm.FID_LOW_1, inst.low),
		omm.RealField(omm.FID_OPEN_PRC, inst.open),
		omm.TimeField(omm.FID_QUOTIM, time.Now()),
	}
}

func makeUpdate(inst *instrument) omm.FieldList {
	return omm.FieldList{
		omm.RealField(omm.FID_BID, inst.bid),
		omm.RealField(omm.FID_ASK, inst.ask),
		omm.RealField(omm.FID_LAST, inst.last),
		omm.IntField(omm.FID_BIDSIZE, int64(rand.Intn(500)+100)),
		omm.IntField(omm.FID_ASKSIZE, int64(rand.Intn(500)+100)),
		omm.IntField(omm.FID_ACVOL_1, inst.volume),
		omm.RealField(omm.FID_NETCHNG_1, inst.last-inst.open),
		omm.TimeField(omm.FID_QUOTIM, time.Now()),
	}
}

func main() {
	flag.Parse()

	svc := omm.DefaultService()
	svc.Name = *serviceName

	log.SetFlags(log.Ltime | log.Lmicroseconds)
	fmt.Println()
	log.Printf("═══════════════════════════════════════════════════════════════")
	log.Printf("  RTSDK Go Demo — EMA-Style Interactive Provider")
	log.Printf("  Port: %d | Service: %s | Tick: %s", *port, svc.Name, *tickRate)
	log.Printf("  Instruments: %d in universe", len(universe))
	log.Printf("═══════════════════════════════════════════════════════════════")

	log.Printf("Available instruments:")
	for _, inst := range universe {
		log.Printf("  %-12s %-20s BID=%.4f ASK=%.4f", inst.name, inst.display, inst.bid, inst.ask)
	}
	fmt.Println()

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen on port %d: %v", *port, err)
	}
	defer ln.Close()
	log.Printf("[SERVER] Listening on port %d — waiting for consumer connections...", *port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println()
		log.Println("[SERVER] Shutting down...")
		ln.Close()
		os.Exit(0)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[SERVER] Accept error: %v", err)
			continue
		}
		go handleClient(conn, svc)
	}
}
