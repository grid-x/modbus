#!/bin/sh
set -e

MODE=$1
TEST_FILTER=$2

if [ -z "$MODE" ] || [ "$MODE" != "tcp" -a "$MODE" != "rtu" -a "$MODE" != "ascii" ]; then
    echo "Usage: $0 {tcp|rtu|ascii} TEST_FILTER"
    exit 1
fi

echo "Running tests in $MODE mode with filter '$TEST_FILTER'"

tmpdir=$(mktemp -d)
diagslave_pids=""
socat_pid=""

cleanup() {
    # Kill entire process group to catch any child processes
    if [ -n "$diagslave_pids" ]; then
        kill $diagslave_pids 2>/dev/null || [ $? -eq 1 ]
    fi
    if [ -n "$socat_pid" ]; then
        kill $socat_pid 2>/dev/null || [ $? -eq 1 ]
    fi
    # Fallback: kill any remaining processes in our process group
    kill -- -$$ 2>/dev/null || [ $? -eq 1 ]
    rm -rf "$tmpdir"
}

trap cleanup EXIT INT TERM

wait_for_pty() {
    local pty_path=$1
    local max_iterations=100  # 100 * 0.05s = 5s timeout
    local iteration=0
    while [ ! -e "$pty_path" ]; do
        if [ "$iteration" -ge "$max_iterations" ]; then
            echo "ERROR: Timeout waiting for $pty_path to appear" >&2
            exit 1
        fi
        sleep 0.05
        iteration=$((iteration + 1))
    done
}

wait_for_tcp_port() {
    local port=$1
    local max_iterations=100  # 100 * 0.05s = 5s timeout
    local iteration=0
    while ! nc -z localhost "$port" 2>/dev/null; do
        if [ "$iteration" -ge "$max_iterations" ]; then
            echo "ERROR: Timeout waiting for port $port to be listening" >&2
            exit 1
        fi
        sleep 0.05
        iteration=$((iteration + 1))
    done
}

case "$MODE" in
    tcp)
        diagslave -m tcp -p 5020 &
        diagslave_pids="$! $diagslave_pids"
        wait_for_tcp_port 5020

        diagslave -m enc -p 5021 &
        diagslave_pids="$! $diagslave_pids"
        wait_for_tcp_port 5021

        go test -run "$TEST_FILTER" -v .
        ;;
    rtu|ascii)
        socat -d -d pty,raw,echo=0,link="$tmpdir/pty0" pty,raw,echo=0,link="$tmpdir/pty1" &
        socat_pid=$!
        wait_for_pty "$tmpdir/pty0"
        wait_for_pty "$tmpdir/pty1"

        diagslave -m "$MODE" "$tmpdir/pty1" &
        diagslave_pids="$! $diagslave_pids"

        go test -run "$TEST_FILTER" -v .
        ;;
esac
