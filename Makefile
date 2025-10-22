.PHONY: build install install-user clean run deps uninstall help

# Variables
APP_NAME=seyir
BUILD_DIR=bin
INSTALL_DIR=/usr/local/bin

# Build commands
build:
	@echo "🔨 Building seyir..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/seyir
	@echo "✅ Build complete: $(BUILD_DIR)/$(APP_NAME)"

# Install system-wide
install: build
	@echo "Installing seyir to /usr/local/bin..."
	sudo cp ./bin/seyir /usr/local/bin/
	@echo "Installing UI files to /usr/local/share/seyir/..."
	sudo mkdir -p /usr/local/share/seyir
	@echo "Installation complete. You can now use 'seyir' from anywhere."

# Install to user's local bin directory  
install-user: build
	@mkdir -p ~/bin
	@mkdir -p ~/.seyir
	@echo "Installing seyir to ~/bin..."
	cp ./bin/seyir ~/bin/
	@echo "Installation complete. Make sure ~/bin is in your PATH."
	@echo "You can add this to your ~/.zshrc: export PATH=\"\$$HOME/bin:\$$PATH\""

# Clean build artifacts
clean:
	@echo "🧹 Cleaning..."
	@rm -rf $(BUILD_DIR)
	@echo "✅ Clean complete"

# Run in development
run:
	@echo "🚀 Running seyir..."
	@go run ./cmd/seyir

# Install Go dependencies
deps:
	@echo "📚 Installing dependencies..."
	@go mod tidy
	@go mod download
	@echo "✅ Dependencies installed"

# Uninstall seyir
uninstall:
	@echo "Removing seyir from /usr/local/bin..."
	sudo rm -f /usr/local/bin/seyir
	@echo "Removing UI files from /usr/local/share/seyir..."
	sudo rm -rf /usr/local/share/seyir
	@echo "Uninstallation complete."

# Show usage help
help:
	@echo "seyir - Build Commands"
	@echo ""
	@echo "📋 Available commands:"
	@echo "   make build         - Build the application"
	@echo "   make install       - Build and install system-wide (requires sudo)"
	@echo "   make install-user  - Install for current user only (~/bin)"
	@echo "   make uninstall     - Remove from system"
	@echo "   make clean         - Clean build artifacts"
	@echo "   make run           - Run in development mode"
	@echo "   make deps          - Install Go dependencies"
	@echo ""
	@echo "🎯 Usage examples:"
	@echo "   seyir service --port 8080"
	@echo "   seyir web --port 8080"  
	@echo "   seyir batch config set flush_interval 3"
	@echo "   tail -f app.log | seyir"
	@echo ""

# Default target
.DEFAULT_GOAL := help
