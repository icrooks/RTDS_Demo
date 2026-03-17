#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BIN_DIR="$PROJECT_DIR/bin"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'
info()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }

check_go() {
    if ! command -v go &>/dev/null; then
        echo -e "${RED}[ERR]${NC} Go not installed. Get Go 1.21+ from https://go.dev/dl/"; exit 1
    fi
    info "Go version: $(go version)"
}

build_all() {
    info "Building all binaries..."
    cd "$PROJECT_DIR"; mkdir -p "$BIN_DIR"
    go build -o "$BIN_DIR/iprovider" ./cmd/iprovider; ok "Built: bin/iprovider"
    go build -o "$BIN_DIR/consumer" ./cmd/consumer;   ok "Built: bin/consumer"
    go build -o "$BIN_DIR/provperf" ./cmd/provperf;   ok "Built: bin/provperf"
    go build -o "$BIN_DIR/consperf" ./cmd/consperf;   ok "Built: bin/consperf"
    echo ""; ok "All binaries built in $BIN_DIR/"
}

run_streaming() {
    echo -e "\n${BOLD}  Demo 1: EMA-Style Market Price Streaming${NC}\n"
    "$BIN_DIR/iprovider" -port 14002 -tick 1s &
    PROV_PID=$!; sleep 1
    "$BIN_DIR/consumer" -host localhost -port 14002 -items "IBM.N,AAPL.OQ,MSFT.OQ,EUR=,LSEG.L" -duration 30s
    kill $PROV_PID 2>/dev/null || true; wait $PROV_PID 2>/dev/null || true
    ok "Demo 1 complete."
}

run_perf() {
    local RATE=${1:-100000} ITEMS=${2:-1000} DUR=${3:-30}
    echo -e "\n${BOLD}  Demo 2: ETA-Style Performance Benchmark (${RATE} upd/s, ${ITEMS} items, ${DUR}s)${NC}\n"
    "$BIN_DIR/provperf" -port 14002 -updateRate "$RATE" -itemCount "$ITEMS" -packCount 20 &
    PROV_PID=$!; sleep 1
    "$BIN_DIR/consperf" -host localhost -port 14002 -itemCount "$ITEMS" -duration "$DUR" -warmup 3s
    kill $PROV_PID 2>/dev/null || true; wait $PROV_PID 2>/dev/null || true
    ok "Demo 2 complete."
}

cleanup() { pkill -f "bin/(iprovider|consumer|provperf|consperf)" 2>/dev/null || true; }
trap cleanup EXIT
check_go

case "${1:-help}" in
    build)       build_all ;;
    streaming)   build_all; run_streaming ;;
    perf)        build_all; run_perf "${2:-100000}" "${3:-1000}" "${4:-30}" ;;
    *)           echo -e "\nUsage: $0 {build|streaming|perf [rate] [items] [duration]}\n" ;;
esac
