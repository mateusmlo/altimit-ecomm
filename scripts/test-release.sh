#!/bin/bash
set -e

SAGA_ID=$(uuidgen | tr '[:upper:]' '[:lower:]')
ORDER_ID=$(uuidgen | tr '[:upper:]' '[:lower:]')
EVENT_ID=$(uuidgen | tr '[:upper:]' '[:lower:]')
TIMESTAMP=$(date +%s)

# Create metadata JSON properly
cat > /tmp/metadata.json <<EOF
{"event_type":"RELEASE_INVENTORY","event_id":"$EVENT_ID","saga_id":"$SAGA_ID","order_id":"60d5cd46-dcee-47e4-b5d1-bd94de60822d","timestamp":$TIMESTAMP}
EOF

# Create payload JSON
cat > /tmp/payload.json <<EOF
{"products":[{"product_id":"550e8400-e29b-41d4-a716-446655440001","quantity":2},{"product_id":"550e8400-e29b-41d4-a716-446655440002","quantity":1}]}
EOF

METADATA=$(cat /tmp/metadata.json)
PAYLOAD=$(cat /tmp/payload.json)

echo "📤 Producing ReleaseInventory command..."
echo "Order ID: $ORDER_ID"
echo "Metadata: $METADATA"

echo "metadata:$METADATA|$ORDER_ID:$PAYLOAD" | docker exec -i kafka kafka-console-producer \
  --bootstrap-server localhost:9092 \
  --topic inventory.commands \
  --property "parse.key=true" \
  --property "key.separator=:" \
  --property "parse.headers=true" \
  --property "headers.delimiter=|" \
  --property "headers.separator=@"

rm /tmp/metadata.json /tmp/payload.json

echo "✅ Message sent!"
