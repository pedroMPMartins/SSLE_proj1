# Build
go build -o /tmp/registry ./cmd/registry
go build -o /tmp/storage ./cmd/storage
go build -o /tmp/ingestion ./cmd/ingestion
go build -o /tmp/analytics ./cmd/analytics
go build -o /tmp/temperature ./cmd/temperature

# Start in background
/tmp/registry & REG_PID=$!
sleep 1
/tmp/storage & STORAGE_PID=$!
sleep 1
/tmp/ingestion & INGEST_PID=$!
sleep 1
/tmp/analytics & ANALYTICS_PID=$!
sleep 1
/tmp/temperature & TEMP_PID=$!

# Let the sensor send a few readings
sleep 6

# Check results
echo -e "\n--- Registry Services ---"
curl -s http://localhost:5050/services
echo -e "\n\n--- Analytics Average ---"
curl -s http://localhost:5003/average
echo ""

# Stop all background processes
kill $REG_PID $STORAGE_PID $INGEST_PID $ANALYTICS_PID $TEMP_PID 2>/dev/null || true
