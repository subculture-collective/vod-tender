#!/bin/bash
# Helper script to create a new release
# Usage: ./scripts/create-release.sh v1.0.0

set -e

VERSION=$1

if [ -z "$VERSION" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 v1.0.0"
    exit 1
fi

# Validate version format
if ! echo "$VERSION" | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+(-[a-z0-9]+)?$' > /dev/null; then
    echo "❌ Invalid version format. Must be vX.Y.Z or vX.Y.Z-suffix"
    echo "Examples: v1.0.0, v1.2.3-beta, v2.0.0-rc1"
    exit 1
fi

echo "🏷️  Creating release $VERSION"

# Check if we're on main branch
CURRENT_BRANCH=$(git branch --show-current)
if [ "$CURRENT_BRANCH" != "main" ]; then
    echo "⚠️  You are on branch '$CURRENT_BRANCH', not 'main'"
    read -p "Continue anyway? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Check for uncommitted changes
if ! git diff-index --quiet HEAD --; then
    echo "❌ You have uncommitted changes. Please commit or stash them first."
    git status --short
    exit 1
fi

# Pull latest changes
echo "📥 Pulling latest changes..."
git pull --rebase

# Check if tag already exists
if git rev-parse "$VERSION" >/dev/null 2>&1; then
    echo "❌ Tag $VERSION already exists"
    echo "To delete and recreate:"
    echo "  git tag -d $VERSION"
    echo "  git push origin :refs/tags/$VERSION"
    exit 1
fi

# Run tests
echo "🧪 Running tests..."
if ! command -v go &> /dev/null; then
    echo "⚠️  Go not found, skipping backend tests"
else
    (cd backend && go test ./... -short)
fi

if ! command -v npm &> /dev/null; then
    echo "⚠️  npm not found, skipping frontend build"
else
    (cd frontend && npm run build)
fi

# Create tag
echo "🏷️  Creating tag $VERSION..."
git tag -a "$VERSION" -m "Release $VERSION"

# Show what will be pushed
echo ""
echo "Tag created locally. To push:"
echo "  git push origin $VERSION"
echo ""
echo "Or to cancel:"
echo "  git tag -d $VERSION"
echo ""

read -p "Push tag now? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "🚀 Pushing tag..."
    git push origin "$VERSION"
    
    echo ""
    echo "✅ Release $VERSION created!"
    echo ""
    echo "Next steps:"
    echo "1. Monitor the release workflow:"
    echo "   https://github.com/$(git remote get-url origin | sed 's|.*github.com[:/]\(.*\)\.git|\1|')/actions/workflows/release.yml"
    echo ""
    echo "2. Once complete, verify the release:"
    echo "   https://github.com/$(git remote get-url origin | sed 's|.*github.com[:/]\(.*\)\.git|\1|')/releases"
else
    echo "Tag created locally but not pushed."
fi
