#!/bin/bash
# Simple demo script for seyir pipe testing

echo "🔧 Seyir Pipe Demo"
echo "=================="

# Build first
echo "📦 Building seyir..."
make build

echo
echo "🧪 Demo 1: JSON logs"
echo '{"timestamp":"2025-01-07T10:01:00Z","level":"INFO","message":"User authenticated","trace_id":"trace_001","source":"auth-service"}
{"timestamp":"2025-01-07T10:01:05Z","level":"ERROR","message":"Payment failed","trace_id":"trace_002","source":"payment-api"}' | ./bin/seyir

echo
echo "🧪 Demo 2: Docker logs"
echo "web-api-1    | 2025-01-07 10:02:00 INFO Starting web server
worker-1     | 2025-01-07 10:02:05 ERROR Failed to connect" | ./bin/seyir

echo
echo "✅ Demo completed! Check logs with:"
echo "   ./bin/seyir service --port=5555"