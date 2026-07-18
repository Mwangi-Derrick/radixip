#!/bin/bash

# Find and clean comment decorations in all code files
find . -type f \
    -not -path "./.git/*" \
    -not -path "./node_modules/*" \
    -not -path "./vendor/*" \
    -not -path "./dist/*" \
    -not -path "./build/*" \
    \( -name "*.go" -o \
       -name "*.py" -o \
       -name "*.js" -o \
       -name "*.ts" -o \
       -name "*.jsx" -o \
       -name "*.tsx" -o \
       -name "*.java" -o \
       -name "*.rs" -o \
       -name "*.c" -o \
       -name "*.cpp" -o \
       -name "*.h" -o \
       -name "*.rb" -o \
       -name "*.php" -o \
       -name "*.swift" -o \
       -name "*.kt" -o \
       -name "*.sh" \) \
    -exec sed -i -E \
        -e 's/\/\/\s*=+\s*(.*?)\s*=+\s*$$/\/\/ \1/g' \
        -e 's/\/\*\s*=+\s*(.*?)\s*=+\s*\*\//\/\/ \1/g' \
        -e 's/#\s*=+\s*(.*?)\s*=+\s*$$/# \1/g' \
        -e 's/<!--\s*=+\s*(.*?)\s*=+\s*-->/\1/g' \
        {} +

echo "✅ Cleaned AI-generated comment decorations!"