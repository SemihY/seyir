#!/bin/bash

# Test script for release process
# This script tests the GoReleaser configuration without actually releasing

echo "🧪 Testing seyir release configuration..."

# Check if GoReleaser is installed
if ! command -v goreleaser &> /dev/null; then
    echo "📦 Installing GoReleaser..."
    go install github.com/goreleaser/goreleaser@latest
fi

# Test the build process
echo "🔨 Testing build process..."
goreleaser build --snapshot --clean

if [ $? -eq 0 ]; then
    echo "✅ Build test successful!"
    echo "📋 Generated binaries:"
    ls -la dist/seyir_*/ | head -10
    
    echo ""
    echo "🔍 Testing version information:"
    if [ -f "dist/seyir_linux_amd64_v1/seyir" ]; then
        ./dist/seyir_linux_amd64_v1/seyir version
    elif [ -f "dist/seyir_darwin_amd64_v1/seyir" ]; then
        ./dist/seyir_darwin_amd64_v1/seyir version
    elif [ -f "dist/seyir_darwin_arm64/seyir" ]; then
        ./dist/seyir_darwin_arm64/seyir version
    fi
    
    echo ""
    echo "🎉 Release configuration looks good!"
    echo "🚀 To create a real release:"
    echo "   1. git tag v1.0.0"
    echo "   2. git push origin v1.0.0"
    echo "   3. GitHub Actions will handle the rest!"
else
    echo "❌ Build test failed!"
    exit 1
fi