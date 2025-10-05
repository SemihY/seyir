.PHONY: build install install-user clean run test run-cli deps macos-app uninstall help

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

# Run with test logs
test:
	@echo "🧪 Running with test logs..."
	@go run ./cmd/seyir -test

# Force CLI mode (useful for testing on macOS)
run-cli:
	@echo "🖥️ Running in CLI mode..."
	@go run ./cmd/seyir -cli

# Install Go dependencies
deps:
	@echo "📚 Installing dependencies..."
	@go mod tidy
	@go mod download
	@echo "✅ Dependencies installed"

# macOS specific - create .app bundle
macos-app: build
ifeq ($(shell uname -s),Darwin)
	@echo "🍎 Creating macOS app bundle..."
	@mkdir -p "$(APP_NAME).app/Contents/MacOS"
	@mkdir -p "$(APP_NAME).app/Contents/Resources"
	@cp $(BUILD_DIR)/$(APP_NAME) "$(APP_NAME).app/Contents/MacOS/"
	@echo '<?xml version="1.0" encoding="UTF-8"?>' > "$(APP_NAME).app/Contents/Info.plist"
	@echo '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo '<plist version="1.0"><dict>' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo '<key>CFBundleExecutable</key><string>$(APP_NAME)</string>' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo '<key>CFBundleIdentifier</key><string>com.seyir.app</string>' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo '<key>CFBundleName</key><string>seyir</string>' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo '<key>CFBundleVersion</key><string>1.0</string>' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo '<key>LSUIElement</key><true/>' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo '</dict></plist>' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo "✅ macOS app created: $(APP_NAME).app"
	@echo "💡 Double-click $(APP_NAME).app to run as menubar app"
else
	@echo "❌ macOS app bundle can only be created on macOS"
endif

# Uninstall seyir
uninstall:
	@echo "Removing seyir from /usr/local/bin..."
	sudo rm -f /usr/local/bin/seyir
	@echo "Removing UI files from /usr/local/share/seyir..."
	sudo rm -rf /usr/local/share/seyir
	@echo "Uninstallation complete."

# Show usage help
help:
	@echo "🪶 seyir - Build Commands"
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
	@echo "🐳 Docker commands:"
	@echo "   make docker-build  - Build Docker image"  
	@echo "   make docker-run    - Run with docker-compose"
	@echo "   make docker-stop   - Stop Docker containers"
	@echo "   make docker-logs   - Show container logs"
	@echo ""
	@echo "🎯 Usage examples:"
	@echo "   seyir service --project myapp --port 8080"
	@echo "   seyir web --port 8080"  
	@echo "   seyir search --search 'error' --limit 50"
	@echo "   tail -f app.log | seyir"
	@echo ""
	@echo "🍎 On macOS: Runs as menubar app by default"
	@echo "🐧 On Linux/Windows: Runs as CLI app"

# Docker commands
docker-build:
	@echo "🐳 Building Docker image..."
	docker build -t seyir:latest .
	@echo "✅ Docker image built: seyir:latest"

docker-run: docker-build
	@echo "� Running seyir in Docker..."
	docker-compose up -d
	@echo "✅ seyir running at http://localhost:8080"

docker-stop:
	@echo "🛑 Stopping seyir Docker containers..."
	docker-compose down
	@echo "✅ Containers stopped"

docker-logs:
	@echo "📋 Showing seyir container logs..."
	docker-compose logs -f seyir

# Default target
.DEFAULT_GOAL := help
