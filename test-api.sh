#!/bin/bash

# SubTrackr API Test Script
# This script demonstrates how to use the SubTrackr API with authentication

API_KEY="sk_your_api_key_here"  # Replace with your actual API key
BASE_URL="http://localhost:8080"

echo "SubTrackr API Test Script"
echo "========================"
echo ""
echo "Make sure to:"
echo "1. Start the SubTrackr server (go run cmd/server/main.go)"
echo "2. Create an API key from the Settings page"
echo "3. Replace the API_KEY variable in this script with your actual key"
echo ""
echo "Press Enter to continue..."
read

# Test 1: Get all subscriptions
echo "Test 1: Getting all subscriptions..."
curl -s -H "Authorization: Bearer $API_KEY" \
  "$BASE_URL/api/v1/subscriptions" | jq .

echo ""
echo "Press Enter to continue..."
read

# Test 2: Get statistics
echo "Test 2: Getting statistics..."
curl -s -H "Authorization: Bearer $API_KEY" \
  "$BASE_URL/api/v1/stats" | jq .

echo ""
echo "Press Enter to continue..."
read

# Test 3: Create a new subscription
echo "Test 3: Creating a new subscription..."
curl -s -X POST \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test Subscription",
    "cost": 9.99,
    "schedule": "Monthly",
    "status": "Active",
    "category": "Testing"
  }' \
  "$BASE_URL/api/v1/subscriptions" | jq .

echo ""
echo "Press Enter to continue..."
read

# Test 4: Export as JSON
echo "Test 4: Exporting as JSON..."
curl -s -H "Authorization: Bearer $API_KEY" \
  "$BASE_URL/api/v1/export/json" | jq .

echo ""
echo "Test complete!"