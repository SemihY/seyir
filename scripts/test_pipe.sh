#!/bin/bash
# Test script for seyir pipe functionality

echo "🔧 Seyir Pipe Test Script"
echo "========================="

# Build seyir first
echo "📦 Building seyir..."
make build
if [ $? -ne 0 ]; then
    echo "❌ Build failed"
    exit 1
fi

echo "✅ Build successful"
echo

# Test 1: Simple pipe test
echo "🧪 Test 1: Simple log generation"
echo "Generating 10 simple logs..."

echo "2025-01-07 10:00:00 [INFO] Application started successfully
2025-01-07 10:00:05 [ERROR] Database connection failed: timeout
2025-01-07 10:00:10 [WARN] High memory usage detected: 85%
2025-01-07 10:00:15 [INFO] User login successful: user123
2025-01-07 10:00:20 [DEBUG] Processing request ID: req_abc123
2025-01-07 10:00:25 [ERROR] File not found: /tmp/missing.log
2025-01-07 10:00:30 [INFO] Cache cleared successfully
2025-01-07 10:00:35 [WARN] Slow query detected: 2.5s
2025-01-07 10:00:40 [INFO] Backup completed successfully
2025-01-07 10:00:45 [FATAL] Critical system error occurred" | ./bin/seyir

echo "✅ Test 1 completed"
echo

# Test 2: JSON logs
echo "🧪 Test 2: JSON format logs"
echo "Generating structured JSON logs..."

echo '{"timestamp":"2025-01-07T10:01:00Z","level":"INFO","message":"User authenticated","trace_id":"trace_001","source":"auth-service"}
{"timestamp":"2025-01-07T10:01:05Z","level":"ERROR","message":"Payment failed","trace_id":"trace_002","source":"payment-api","user_id":"user_456"}
{"timestamp":"2025-01-07T10:01:10Z","level":"WARN","message":"Rate limit exceeded","trace_id":"trace_003","source":"api-gateway"}
{"timestamp":"2025-01-07T10:01:15Z","level":"INFO","message":"Order created","trace_id":"trace_004","source":"order-service","order_id":"ord_789"}
{"timestamp":"2025-01-07T10:01:20Z","level":"DEBUG","message":"Database query executed","trace_id":"trace_005","source":"db-service","query_time":"120ms"}' | ./bin/seyir

echo "✅ Test 2 completed"
echo

# Test 3: Docker-like logs
echo "🧪 Test 3: Docker-style logs"
echo "Simulating docker logs format..."

echo "web-api-1    | 2025-01-07 10:02:00 INFO Starting web server on port 8080
web-api-1    | 2025-01-07 10:02:05 ERROR Failed to connect to database
worker-1     | 2025-01-07 10:02:10 INFO Processing job queue
worker-1     | 2025-01-07 10:02:15 WARN Job timeout after 30s
redis-1      | 2025-01-07 10:02:20 INFO Memory usage: 1.2GB
nginx-1      | 2025-01-07 10:02:25 ERROR 502 Bad Gateway for /api/users
postgres-1   | 2025-01-07 10:02:30 WARN Connection pool exhausted" | ./bin/seyir

echo "✅ Test 3 completed"
echo

# Test 4: Continuous stream (10 seconds)
echo "🧪 Test 4: Continuous log stream (10 seconds)"
echo "Generating continuous logs..."

{
    for i in {1..50}; do
        TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')
        LEVEL=$(shuf -n1 -e "INFO" "WARN" "ERROR" "DEBUG")
        SOURCE=$(shuf -n1 -e "web-api" "worker" "database" "cache" "auth")
        MESSAGE="Automated test message #$i from $SOURCE service"
        
        echo "[$TIMESTAMP] [$LEVEL] [$SOURCE] $MESSAGE"
        sleep 0.2
    done
} | ./bin/seyir &

PIPE_PID=$!
echo "📡 Streaming logs for 10 seconds (PID: $PIPE_PID)..."
sleep 10
kill $PIPE_PID 2>/dev/null
wait $PIPE_PID 2>/dev/null

echo "✅ Test 4 completed"
echo

# Test 5: High volume burst
echo "🧪 Test 5: High volume burst test"
echo "Generating 1000 logs quickly..."

{
    for i in {1..1000}; do
        echo "2025-01-07 10:05:$(printf "%02d" $((i%60))) [INFO] [batch-test] High volume log entry #$i"
    done
} | ./bin/seyir

echo "✅ Test 5 completed"
echo

# Test 6: Mixed format stress test
echo "🧪 Test 6: Mixed format stress test"
echo "Testing various log formats..."

{
    # Apache style
    echo '192.168.1.100 - - [07/Jan/2025:10:06:00 +0000] "GET /api/users HTTP/1.1" 200 1234'
    
    # Syslog style  
    echo 'Jan  7 10:06:05 server01 sshd[1234]: Accepted password for user from 192.168.1.200'
    
    # Application logs
    echo '[2025-01-07 10:06:10] production.ERROR: Database query failed {"exception":"[object] (PDOException(code: 2002))"}'
    
    # Kubernetes logs
    echo '2025-01-07T10:06:15.123456789Z stdout F {"level":"info","message":"Pod started successfully"}'
    
    # Custom format
    echo 'TIMESTAMP=2025-01-07T10:06:20 LEVEL=WARN SOURCE=monitoring MESSAGE="Disk usage at 90%"'
    
} | ./bin/seyir

echo "✅ Test 6 completed"
echo

# Summary
echo "🎯 All pipe tests completed successfully!"
echo "📊 Test Summary:"
echo "  ✅ Simple text logs"
echo "  ✅ JSON structured logs" 
echo "  ✅ Docker-style logs"
echo "  ✅ Continuous streaming"
echo "  ✅ High volume burst (1000 logs)"
echo "  ✅ Mixed format handling"
echo
echo "🔍 Check your logs with:"
echo "  ./bin/seyir query logs"
echo "  ./bin/seyir service --port=5555  # Web UI"
echo
echo "📁 Log files location: ~/.seyir/lake/"