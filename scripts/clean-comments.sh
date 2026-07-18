#!/bin/bash

echo "🧹 Cleaning AI-generated comments in .go, .py, .rs files..."

# Get repo root (where .git is)
REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
echo "Repository root: $REPO_ROOT"

cd "$REPO_ROOT" || exit 1

# Count files
COUNT=0

# Find and process all files
while IFS= read -r file; do
    if grep -q "===" "$file" 2>/dev/null; then
        # Clean the file
        sed -i -E \
            -e 's/\/\/\s*=*\s*(.*)\s*=*\s*$/\/\/ \1/g' \
            -e 's/#\s*=*\s*(.*)\s*=*\s*$/# \1/g' \
            "$file" 2>/dev/null
        
        echo "✓ Cleaned: $file"
        COUNT=$((COUNT + 1))
    fi
done < <(find . -type f \
    -not -path "./.git/*" \
    -not -path "./scripts/*" \
    -not -path "./node_modules/*" \
    -not -path "./vendor/*" \
    -not -path "./target/*" \
    -not -path "./__pycache__/*" \
    \( -name "*.go" -o -name "*.py" -o -name "*.rs" \))

echo "✅ Cleaned $COUNT files!"