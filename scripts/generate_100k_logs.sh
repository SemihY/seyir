#!/bin/bash

# Generate 100K log entries with realistic patterns
# Usage: ./generate_100k_logs.sh | ./bin/seyir

TOTAL_LOGS=100000
START_TIME=$(date +%s)

echo "Starting to generate $TOTAL_LOGS log entries..." >&2

LEVELS=("INFO" "WARN" "ERROR" "DEBUG")
SERVICES=("api-gateway" "auth-service" "user-service" "payment-service" "notification-service")
OPERATIONS=("request_received" "processing" "database_query" "cache_hit" "cache_miss" "api_call" "completed" "failed")
USERS=("user_1234" "user_5678" "user_9012" "user_3456" "user_7890")

for i in $(seq 1 $TOTAL_LOGS); do
    # Random selections
    LEVEL=${LEVELS[$((RANDOM % ${#LEVELS[@]}))]}
    SERVICE=${SERVICES[$((RANDOM % ${#SERVICES[@]}))]}
    OPERATION=${OPERATIONS[$((RANDOM % ${#OPERATIONS[@]}))]}
    USER=${USERS[$((RANDOM % ${#USERS[@]}))]}
    
    # Generate realistic timestamps
    TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%S.%3NZ")
    
    # Random request ID
    REQUEST_ID=$(printf "%08x" $((RANDOM * RANDOM)))
    
    # Random duration for timing info
    DURATION=$((RANDOM % 5000))
    
    # Generate log line
    echo "$TIMESTAMP [$LEVEL] [$SERVICE] request_id=$REQUEST_ID user=$USER operation=$OPERATION duration=${DURATION}ms"
    
    # Progress indicator every 10K logs
    if [ $((i % 10000)) -eq 0 ]; then
        echo "Generated $i logs..." >&2
    fi
done

END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))

echo "Completed generating $TOTAL_LOGS logs in ${ELAPSED}s" >&2
