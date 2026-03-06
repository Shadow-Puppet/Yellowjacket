#!/usr/bin/env bash
#
# profile.sh — Interactive profiling helper for yellowjacket.
#
# Prerequisites:
#   - The app must be running via `make dev` (pprof server on :6060).
#   - Go toolchain must be installed (for `go tool pprof` / `go tool trace`).
#   - `curl` must be available (for trace capture).
#
# Usage:
#   ./scripts/profile.sh          # Interactive menu
#   ./scripts/profile.sh cpu      # Skip menu, run CPU profile directly
#   ./scripts/profile.sh heap     # Skip menu, run heap profile directly
#   ./scripts/profile.sh trace    # Skip menu, capture execution trace
#
set -euo pipefail

PPROF_BASE="http://localhost:6060"
PPROF_URL="${PPROF_BASE}/debug/pprof"
TRACE_URL="${PPROF_BASE}/debug/trace"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
RESET='\033[0m'

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

print_header() {
    echo ""
    echo -e "${BOLD}Yellowjacket Profiler${RESET}"
    echo -e "${DIM}────────────────────────────────────────${RESET}"
    echo ""
}

check_server() {
    if ! curl -s --max-time 2 "${PPROF_URL}/" > /dev/null 2>&1; then
        echo -e "${RED}Error: pprof server not reachable at ${PPROF_BASE}${RESET}"
        echo ""
        echo "  Make sure the app is running with:  make dev"
        echo "  The pprof server starts automatically in dev builds."
        echo ""
        exit 1
    fi
}

# prompt_duration asks the user for a duration in seconds.
# $1 = prompt label, $2 = default value.
prompt_duration() {
    local label="$1"
    local default="$2"

    read -rp "  ${label} [${default}s]: " input
    echo "${input:-$default}"
}

# WEB_PORT_MIN and WEB_PORT_MAX define the range of ports the pprof web
# UI will try when opening a browser. If a port is busy it moves to the
# next one automatically.
WEB_PORT_MIN=8080
WEB_PORT_MAX=8089

# find_free_port echoes the first available port in the range, or returns 1.
find_free_port() {
    for port in $(seq "${WEB_PORT_MIN}" "${WEB_PORT_MAX}"); do
        if ! ss -tlnp 2>/dev/null | grep -q ":${port} " &&
           ! lsof -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
            echo "${port}"
            return 0
        fi
    done

    return 1
}

# pprof_web opens a pprof profile in the browser. It finds a free port
# automatically so multiple profiles can be open at once.
# $1 = the pprof endpoint URL (e.g. http://…/profile?seconds=30).
pprof_web() {
    local url="$1"
    local port

    port=$(find_free_port) || {
        echo -e "${RED}No free port found in range ${WEB_PORT_MIN}-${WEB_PORT_MAX}.${RESET}"
        echo -e "${DIM}  Close an existing pprof browser tab and try again.${RESET}"
        return 1
    }

    echo -e "${DIM}  Opening browser UI on port ${port}...${RESET}"
    go tool pprof -http=":${port}" "${url}"
}

# ---------------------------------------------------------------------------
# Profile commands
# ---------------------------------------------------------------------------

do_cpu() {
    local secs
    secs=$(prompt_duration "Capture duration" "30")

    echo ""
    echo -e "${CYAN}Capturing CPU profile for ${secs}s...${RESET}"
    echo -e "${DIM}  While this runs, use the app normally to generate load.${RESET}"
    echo ""

    pprof_web "${PPROF_URL}/profile?seconds=${secs}"
}

do_heap() {
    echo ""
    echo -e "${CYAN}Capturing heap profile...${RESET}"
    echo ""

    pprof_web "${PPROF_URL}/heap"
}

do_allocs() {
    echo ""
    echo -e "${CYAN}Capturing allocation profile...${RESET}"
    echo -e "${DIM}  Shows where memory allocations happen (even if already freed).${RESET}"
    echo ""

    pprof_web "${PPROF_URL}/allocs"
}

do_goroutine() {
    echo ""
    echo -e "${CYAN}Capturing goroutine dump...${RESET}"
    echo -e "${DIM}  Shows all goroutines and what they are currently doing.${RESET}"
    echo ""

    pprof_web "${PPROF_URL}/goroutine"
}

do_block() {
    echo ""
    echo -e "${CYAN}Capturing block profile...${RESET}"
    echo -e "${DIM}  Shows where goroutines block waiting on synchronization"
    echo -e "  primitives (mutexes, channels, select).${RESET}"
    echo ""

    pprof_web "${PPROF_URL}/block"
}

do_mutex() {
    echo ""
    echo -e "${CYAN}Capturing mutex contention profile...${RESET}"
    echo -e "${DIM}  Shows where goroutines contend on mutexes.${RESET}"
    echo ""

    pprof_web "${PPROF_URL}/mutex"
}

do_trace() {
    local secs
    secs=$(prompt_duration "Capture duration" "5")

    local outfile="trace-$(date +%Y%m%d-%H%M%S).out"

    echo ""
    echo -e "${CYAN}Capturing execution trace for ${secs}s...${RESET}"
    echo -e "${DIM}  This records goroutine scheduling, GC pauses, syscalls,"
    echo -e "  and network activity at microsecond resolution.${RESET}"
    echo ""

    curl -s -o "${outfile}" "${TRACE_URL}?seconds=${secs}"

    echo -e "${GREEN}Trace saved to ${outfile}${RESET}"
    echo -e "Opening trace viewer in browser..."
    echo ""

    go tool trace "${outfile}"
}

do_health() {
    echo ""
    echo -e "${CYAN}Runtime health check${RESET}"
    echo -e "${DIM}────────────────────────────────────────${RESET}"

    # Goroutine count
    local goroutines
    goroutines=$(curl -s "${PPROF_URL}/goroutine?debug=0" | head -c 500 | wc -l)
    echo -e "  Goroutines:     $(curl -s "${PPROF_URL}/goroutine?debug=1" | head -1 | grep -oP '\d+')"

    # Heap stats via /debug/pprof/heap?debug=1
    local heap_info
    heap_info=$(curl -s "${PPROF_URL}/heap?debug=1" | head -20)

    local heap_inuse
    heap_inuse=$(echo "${heap_info}" | grep -oP '# Heap = \K\d+' || echo "unknown")
    if [ "${heap_inuse}" != "unknown" ]; then
        local heap_mb
        heap_mb=$(echo "scale=1; ${heap_inuse} / 1048576" | bc 2>/dev/null || echo "${heap_inuse} bytes")
        echo -e "  Heap in use:    ${heap_mb} MB"
    fi

    local heap_sys
    heap_sys=$(echo "${heap_info}" | grep -oP 'HeapSys = \K\d+' || echo "")
    if [ -n "${heap_sys}" ]; then
        local sys_mb
        sys_mb=$(echo "scale=1; ${heap_sys} / 1048576" | bc 2>/dev/null || echo "${heap_sys} bytes")
        echo -e "  Heap reserved:  ${sys_mb} MB"
    fi

    local num_gc
    num_gc=$(echo "${heap_info}" | grep -oP 'NumGC = \K\d+' || echo "unknown")
    echo -e "  GC cycles:      ${num_gc}"

    echo ""
    echo -e "${DIM}  For detailed runtime stats, visit:"
    echo -e "  ${PPROF_URL}/heap?debug=1${RESET}"
    echo ""
}

# ---------------------------------------------------------------------------
# Menu
# ---------------------------------------------------------------------------

show_menu() {
    echo -e "  ${BOLD}What would you like to profile?${RESET}"
    echo ""
    echo -e "  ${GREEN}1)${RESET}  CPU profile         ${DIM}Find slow functions (flame graph in browser)${RESET}"
    echo -e "  ${GREEN}2)${RESET}  Heap profile         ${DIM}See current memory usage by location${RESET}"
    echo -e "  ${GREEN}3)${RESET}  Allocation profile   ${DIM}Find where allocations happen (even freed ones)${RESET}"
    echo -e "  ${GREEN}4)${RESET}  Goroutine dump       ${DIM}See all goroutines and what they're doing${RESET}"
    echo -e "  ${GREEN}5)${RESET}  Block profile        ${DIM}Find where goroutines block on sync primitives${RESET}"
    echo -e "  ${GREEN}6)${RESET}  Mutex profile        ${DIM}Find mutex contention hotspots${RESET}"
    echo -e "  ${GREEN}7)${RESET}  Execution trace      ${DIM}Detailed timeline: scheduling, GC, syscalls${RESET}"
    echo -e "  ${GREEN}8)${RESET}  Quick health check   ${DIM}Goroutine count, heap size, GC stats${RESET}"
    echo ""
    echo -e "  ${GREEN}q)${RESET}  Quit"
    echo ""

    read -rp "  Choose [1-8, q]: " choice
    echo ""

    case "${choice}" in
        1|cpu)       do_cpu || true ;;
        2|heap)      do_heap || true ;;
        3|allocs)    do_allocs || true ;;
        4|goroutine) do_goroutine || true ;;
        5|block)     do_block || true ;;
        6|mutex)     do_mutex || true ;;
        7|trace)     do_trace || true ;;
        8|health)    do_health || true ;;
        q|Q|quit)    echo "Bye."; exit 0 ;;
        *)           echo -e "${RED}Invalid choice: ${choice}${RESET}"; echo "" ;;
    esac
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    # Direct invocation: ./scripts/profile.sh cpu
    if [ $# -gt 0 ]; then
        case "$1" in
            help|-h|--help)
                echo "Usage: $0 [cpu|heap|allocs|goroutine|block|mutex|trace|health]"
                echo ""
                echo "Run without arguments for an interactive menu."
                echo ""
                echo "Commands:"
                echo "  cpu        CPU profile — find slow functions (opens flame graph)"
                echo "  heap       Heap profile — see current memory usage by location"
                echo "  allocs     Allocation profile — find where allocations happen"
                echo "  goroutine  Goroutine dump — see all goroutines and their state"
                echo "  block      Block profile — find sync primitive bottlenecks"
                echo "  mutex      Mutex profile — find mutex contention hotspots"
                echo "  trace      Execution trace — detailed scheduling/GC/syscall timeline"
                echo "  health     Quick health check — goroutine count, heap, GC stats"
                exit 0
                ;;
        esac

        check_server

        case "$1" in
            cpu)       do_cpu ;;
            heap)      do_heap ;;
            allocs)    do_allocs ;;
            goroutine) do_goroutine ;;
            block)     do_block ;;
            mutex)     do_mutex ;;
            trace)     do_trace ;;
            health)    do_health ;;
            *)
                echo -e "${RED}Unknown command: $1${RESET}"
                echo "Usage: $0 [cpu|heap|allocs|goroutine|block|mutex|trace|health]"
                exit 1
                ;;
        esac

        exit 0
    fi

    # Interactive mode
    print_header
    check_server
    echo -e "  ${GREEN}Connected to pprof server at ${PPROF_BASE}${RESET}"
    echo ""

    while true; do
        show_menu
    done
}

main "$@"
