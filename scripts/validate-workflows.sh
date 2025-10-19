#!/bin/bash
# Validate GitHub Actions workflow files
# Usage: ./scripts/validate-workflows.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
WORKFLOWS_DIR="$REPO_ROOT/.github/workflows"

echo "🔍 Validating GitHub Actions workflows..."

if ! command -v python3 &> /dev/null; then
    echo "❌ python3 is required but not installed"
    exit 1
fi

# Check if PyYAML is available
if ! python3 -c "import yaml" 2>/dev/null; then
    echo "⚠️  PyYAML not found, installing..."
    python3 -m pip install --user PyYAML
fi

VALID=0
INVALID=0

for workflow in "$WORKFLOWS_DIR"/*.yml; do
    if [ -f "$workflow" ]; then
        filename=$(basename "$workflow")
        if python3 -c "import yaml; yaml.safe_load(open('$workflow'))" 2>/dev/null; then
            echo "✓ $filename is valid YAML"
            VALID=$((VALID + 1))
        else
            echo "✗ $filename is INVALID YAML"
            INVALID=$((INVALID + 1))
        fi
    fi
done

echo ""
echo "Summary: $VALID valid, $INVALID invalid"

if [ $INVALID -gt 0 ]; then
    echo "❌ Some workflows have invalid YAML syntax"
    exit 1
else
    echo "✅ All workflows are valid"
    exit 0
fi
