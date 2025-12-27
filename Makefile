.PHONY: build run test clean deps

# Build the facilitator
build:
	@echo "Building hathor-facilitator..."
	@go build -o hathor-facilitator .

# Run the facilitator
run: build
	@echo "Running hathor-facilitator..."
	@./hathor-facilitator

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -f hathor-facilitator
	@go clean

# Install dependencies and build
all: deps build

# Run with default settings
dev:
	@PORT=3000 \
	HATHOR_NODE_URL=http://localhost:8080 \
	HATHOR_WALLET_URL=http://localhost:8000 \
	HATHOR_MINING_SERVICE_URL=https://txmining.india.testnet.hathor.network \
	HATHOR_WALLET_ID=facilitator-wallet \
	MIN_CONFIRMATIONS=1 \
	go run main.go
