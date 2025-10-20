#!/bin/bash
# Performance regression detection script
# Compares benchmark results and fails if performance degrades by more than 10%

set -e

CURRENT_BENCH=$1
BASELINE_BENCH=$2
THRESHOLD=${3:-10}  # Default 10% regression threshold

if [ ! -f "$CURRENT_BENCH" ]; then
    echo "❌ Current benchmark file not found: $CURRENT_BENCH"
    exit 1
fi

if [ ! -f "$BASELINE_BENCH" ]; then
    echo "⚠️ Baseline benchmark file not found: $BASELINE_BENCH"
    echo "Skipping regression detection (this is normal for new features)"
    exit 0
fi

echo "🔍 Comparing benchmarks with ${THRESHOLD}% regression threshold"
echo ""

# Install benchstat if not available
if ! command -v benchstat &> /dev/null; then
    echo "Installing benchstat..."
    go install golang.org/x/perf/cmd/benchstat@latest
fi

# Run benchstat comparison
echo "### Benchmark Comparison"
benchstat "$BASELINE_BENCH" "$CURRENT_BENCH" | tee comparison.txt

# Check for significant regressions
# benchstat output format: "name old new delta"
# Look for lines with positive delta > threshold%
REGRESSION_FOUND=0

while IFS= read -r line; do
    # Skip headers and lines without delta
    if [[ ! "$line" =~ ^Benchmark ]] || [[ ! "$line" =~ \+.*% ]]; then
        continue
    fi
    
    # Extract percentage (e.g., "+12.5%" -> "12.5")
    DELTA=$(echo "$line" | sed -n 's/.*+\([0-9.][0-9.]*\)%.*/\1/p')
    
    if [ -n "$DELTA" ]; then
        # Compare with threshold using bc
        if awk "BEGIN {exit !($DELTA > $THRESHOLD)}"; then
            echo "❌ Performance regression detected: $line"
            REGRESSION_FOUND=1
        fi
    fi
done < comparison.txt

if [ $REGRESSION_FOUND -eq 1 ]; then
    echo ""
    echo "❌ Performance regressions exceeding ${THRESHOLD}% threshold detected!"
    exit 1
else
    echo ""
    echo "✅ No significant performance regressions detected"
    exit 0
fi
