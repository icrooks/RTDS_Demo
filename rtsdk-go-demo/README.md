# RTSDK Go Demo — Real-Time Market Data Applications

A complete demonstration package implementing the core concepts of the **LSEG Real-Time SDK (RTSDK)** in Go. This project provides four applications that mirror the behavior of the C/C++ RTSDK examples, demonstrating real-time market data distribution patterns without requiring any commercial licenses or external data feeds.

## What's Inside

| Application | RTSDK Equivalent | Description |
|---|---|---|
| **iprovider** | EMA IProvider 100 | Interactive Provider — publishes streaming Market Price data |
| **consumer** | EMA Consumer 200 | Market Price Consumer — subscribes and extracts typed field data |
| **provperf** | ETA ProvPerf | Performance Provider — high-volume mock data generation |
| **consperf** | ETA ConsPerf | Performance Consumer — throughput/latency benchmarking |

## RTSDK Concepts Demonstrated

### Architecture (EMA vs ETA layers)

The RTSDK has two API layers. **EMA (Enterprise Message API)** is a high-level C++ API that handles login, directory, dictionary, and recovery automatically — Demo 1 (iprovider + consumer) mirrors this pattern. **ETA (Enterprise Transport API)** is a low-level C transport layer giving wire-level control for maximum throughput — Demo 2 (provperf + consperf) mirrors this pattern.

### Protocol Implementation

- **Binary framing**: Length-prefixed messages with typed field encoding (mirrors RWF)
- **OMM domain model**: Login (1), Directory (4), MarketPrice (6) domains
- **Message types**: RequestMsg, RefreshMsg, UpdateMsg, StatusMsg, CloseMsg
- **Field dictionary**: Real FID numbers from RDMFieldDictionary (BID=22, ASK=25, TRDPRC_1=6, etc.)
- **Admin automation**: Login/Directory negotiation handled automatically (EMA pattern)
- **Sparse updates**: UpdateMsg contains only changed fields (bandwidth optimization)
- **Message packing**: Multiple updates per network write (throughput optimization)

### Real-World Patterns

- **Interactive Provider**: Accept connections, respond to specific subscription requests
- **Consumer**: Connect → Login → Directory → Subscribe → Stream
- **Performance benchmarking**: Throughput measurement, latency percentiles (p50/p95/p99)
- **Multi-client support**: Provider handles concurrent consumer connections
- **Graceful lifecycle**: Clean stream closure, signal handling

## Quick Start

```bash
# Prerequisites: Go 1.21+
go version

# Clone and build
git clone <this-repo>
cd rtsdk-go-demo
go build -o bin/ ./...

# Demo 1: Market Price Streaming (Terminal 1)
./bin/iprovider -port 14002

# Demo 1: Consumer (Terminal 2)
./bin/consumer -host localhost -port 14002 -items "IBM.N,AAPL.OQ,EUR="

# Demo 2: Performance Benchmark (Terminal 1)
./bin/provperf -port 14002 -updateRate 100000 -itemCount 1000

# Demo 2: Performance Consumer (Terminal 2)
./bin/consperf -host localhost -port 14002 -itemCount 1000 -duration 30
```

Or use the one-click launcher:

```bash
./scripts/demo.sh streaming    # Demo 1
./scripts/demo.sh perf         # Demo 2 (100K updates/sec)
./scripts/demo.sh perf 500000 5000 60  # Custom: 500K/sec, 5K items, 60s
```

## Project Structure

```
rtsdk-go-demo/
├── cmd/
│   ├── iprovider/main.go    # EMA-style Interactive Provider
│   ├── consumer/main.go     # EMA-style Market Price Consumer
│   ├── provperf/main.go     # ETA-style Performance Provider
│   └── consperf/main.go     # ETA-style Performance Consumer
├── pkg/
│   ├── omm/
│   │   ├── types.go         # OMM types: domains, messages, fields, states
│   │   └── codec.go         # Binary encode/decode (RSSL-like framing)
│   └── admin/
│       └── admin.go         # Admin domain helpers (Login, Directory)
├── scripts/
│   └── demo.sh              # One-click build & launch
├── go.mod
└── README.md
```

## Available Instruments (Demo 1)

| RIC | Name | Type |
|---|---|---|
| IBM.N | Intl Bus Machines | NYSE Equity |
| AAPL.OQ | Apple Inc | NASDAQ Equity |
| MSFT.OQ | Microsoft Corp | NASDAQ Equity |
| GOOG.OQ | Alphabet Inc | NASDAQ Equity |
| JPM.N | JPMorgan Chase | NYSE Equity |
| GS.N | Goldman Sachs | NYSE Equity |
| EUR= | EUR/USD Spot | FX |
| GBP= | GBP/USD Spot | FX |
| TRI.N | Thomson Reuters | NYSE Equity |
| LSEG.L | LSEG Group PLC | LSE Equity |

## Command-Line Options

### iprovider
```
-port 14002       TCP listen port
-tick 1s          Update interval
-service NAME     Service name (default: DIRECT_FEED)
-v                Verbose logging
```

### consumer
```
-host HOST        Provider hostname (default: localhost)
-port 14002       Provider port
-items LIST       Comma-separated RICs (default: IBM.N,AAPL.OQ,MSFT.OQ,EUR=,LSEG.L)
-service NAME     Service name (default: DIRECT_FEED)
-duration 30s     Run duration (0 = forever)
-v                Verbose field-level logging
```

### provperf
```
-port 14002       TCP listen port
-updateRate N     Target updates/sec (default: 100000)
-latencyRate N    Timestamped updates/sec (default: 1000)
-itemCount N      Items in universe (default: 1000)
-packCount N      Messages per network write (default: 20)
```

### consperf
```
-host HOST        ProvPerf hostname
-port 14002       ProvPerf port
-itemCount N      Items to subscribe (default: 1000)
-duration N       Test duration in seconds (default: 30)
-warmup 3s        Warmup before measurement
```

## Mapping to RTSDK C/C++ Concepts

| This Project | RTSDK C/C++ Equivalent |
|---|---|
| `pkg/omm` | ETA RSSL codec + OMM containers |
| `pkg/admin` | EMA automatic admin domain handling |
| `Message.Fields` (FieldList) | `FieldList` OMM container type |
| `RealField(FID_BID, 185.42)` | `fieldList.addReal(22, 18542, OmmReal::ExponentNeg2Enum)` |
| `omm.Encode()` / `omm.Decode()` | `rsslEncodeMsg()` / `rsslDecodeMsg()` |
| Binary frame format | RSSL wire format (RWF) |
| `DomainMarketPrice` | `MMT_MARKET_PRICE` (domain type 6) |
| `MsgTypeRefresh` | `RSSL_MC_REFRESH` |
| `MsgTypeUpdate` | `RSSL_MC_UPDATE` |
| Provider `handleClient()` | `OmmProviderClient::onReqMsg()` |
| Consumer `onRefreshMsg()` | `OmmConsumerClient::onRefreshMsg()` |

## License

This demonstration code is provided for educational purposes. The RTSDK C/C++ Edition is open-source under Apache License 2.0.
