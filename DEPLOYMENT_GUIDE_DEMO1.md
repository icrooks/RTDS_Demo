# Deployment Guide — Demo 1: EMA-Style Market Price Streaming

## Overview

This guide walks through building and running the **IProvider + Consumer** pair, which demonstrates the EMA (Enterprise Message API) pattern from the LSEG Real-Time SDK. The IProvider publishes simulated Market Price data for 10 financial instruments. The Consumer subscribes and displays streaming BID/ASK/LAST prices with typed field extraction.

**Time to deploy: < 5 minutes** (assumes Go is installed)

---

## 1. Prerequisites

| Requirement | Version | Verify Command |
|---|---|---|
| Go | 1.21+ | `go version` |
| Git | Any | `git --version` |
| OS | Linux, macOS, or Windows | — |

### Install Go (if needed)

```bash
# Linux (amd64)
wget https://go.dev/dl/go1.22.2.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.22.2.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# macOS (Homebrew)
brew install go

# Windows — download installer from https://go.dev/dl/
```

---

## 2. Clone and Build

```bash
git clone <repository-url> rtsdk-go-demo
cd rtsdk-go-demo
mkdir -p bin
go build -o bin/iprovider ./cmd/iprovider
go build -o bin/consumer  ./cmd/consumer
ls -lh bin/iprovider bin/consumer
```

---

## 3. Run the Demo

### Terminal 1 — Start the IProvider

```bash
./bin/iprovider -port 14002 -tick 1s
```

### Terminal 2 — Start the Consumer

```bash
./bin/consumer -host localhost -port 14002 \
  -items "IBM.N,AAPL.OQ,MSFT.OQ,EUR=,LSEG.L" \
  -duration 30s
```

### One-Click Launch (Alternative)

```bash
./scripts/demo.sh streaming
```

---

## 4. What You're Seeing

1. **TCP Connect** — Consumer connects to IProvider on port 14002
2. **Login** — Automatic login negotiation (no credentials needed locally)
3. **Directory** — Consumer discovers DIRECT_FEED service
4. **Subscribe** — Consumer requests Market Price items
5. **RefreshMsg** — Initial snapshot with all fields (BID, ASK, LAST, HIGH, LOW, VOLUME)
6. **UpdateMsg** — Streaming ticks every second with changed fields only

---

## 5. Troubleshooting

| Problem | Cause | Solution |
|---|---|---|
| `Failed to connect` | IProvider not running | Start IProvider first |
| `connection refused` | Wrong port or firewall | Verify port; check `netstat -tlnp \| grep 14002` |
| `Item not found` | Typo in RIC | Use exact name from instrument table |
| `address already in use` | Port taken | Use `-port 14003` on both sides |

---

## 6. Demo Talking Points

- EMA automates login/directory/dictionary — the Consumer never explicitly handles admin domains
- RefreshMsg contains full field set; UpdateMsg is sparse (only changed fields)
- Fields extracted as native types: float64 for prices, int64 for volumes
- Same code works against production RTDS by changing the connection target
- IProvider supports multiple concurrent consumers without code changes
