#!/bin/bash

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "🧪 Running SubTrackr Test Suite"
echo "================================"

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    echo -e "${RED}Error: go.mod not found. Please run this script from the project root.${NC}"
    exit 1
fi

# Clean test cache
echo -e "${YELLOW}Cleaning test cache...${NC}"
go clean -testcache

# Run tests with coverage
echo -e "${YELLOW}Running tests with coverage...${NC}"
go test -v -race -coverprofile=coverage.out ./...

# Check test result
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ All tests passed!${NC}"
    
    # Generate coverage report
    echo -e "${YELLOW}Generating coverage report...${NC}"
    go tool cover -html=coverage.out -o coverage.html
    
    # Show coverage summary
    echo -e "${YELLOW}Coverage Summary:${NC}"
    go tool cover -func=coverage.out | grep total | awk '{print "Total Coverage: " $3}'
    
    echo -e "${GREEN}📊 Coverage report generated: coverage.html${NC}"
else
    echo -e "${RED}❌ Tests failed!${NC}"
    exit 1
fi

# Optional: Run specific package tests
if [ "$1" == "models" ]; then
    echo -e "${YELLOW}Running model tests only...${NC}"
    go test -v ./internal/models/...
elif [ "$1" == "repository" ]; then
    echo -e "${YELLOW}Running repository tests only...${NC}"
    go test -v ./internal/repository/...
elif [ "$1" == "service" ]; then
    echo -e "${YELLOW}Running service tests only...${NC}"
    go test -v ./internal/service/...
elif [ "$1" == "config" ]; then
    echo -e "${YELLOW}Running config tests only...${NC}"
    go test -v ./internal/config/...
fi

# Check for race conditions
echo -e "${YELLOW}Checking for race conditions...${NC}"
go test -race ./...

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ No race conditions detected!${NC}"
else
    echo -e "${RED}❌ Race conditions detected!${NC}"
fi

echo -e "${GREEN}Test suite completed!${NC}"