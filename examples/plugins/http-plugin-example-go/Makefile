.PHONY: build run test clean

PLUGIN_NAME = http-plugin-example-go
OUTPUT_DIR  = build
OUTPUT      = $(OUTPUT_DIR)/$(PLUGIN_NAME)
ADDR       ?= 127.0.0.1:18081

build: ## Build the plugin binary (listens on $(ADDR) by default)
	@mkdir -p $(OUTPUT_DIR)
	go build -trimpath -ldflags="-s -w" -o $(OUTPUT) .

run: build ## Build and start the plugin (override the address with ADDR=host:port)
	$(OUTPUT) -addr $(ADDR)

test: ## Run tests
	go test ./...

clean: ## Remove build artifacts
	rm -rf $(OUTPUT_DIR)

.DEFAULT_GOAL := build
