.PHONY: build install install-user clean run test run-cli deps macos-app uninstall help

# Variables
APP_NAME=logspot
BUILD_DIR=bin
INSTALL_DIR=/usr/local/bin

# Build commands
build:
	@echo "🔨 Building logspot..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/logspot
	@echo "✅ Build complete: $(BUILD_DIR)/$(APP_NAME)"

# Install system-wide
install: build
	@echo "Installing logspot to /usr/local/bin..."
	sudo cp ./bin/logspot /usr/local/bin/
	@echo "Installing UI files to /usr/local/share/logspot/..."
	sudo mkdir -p /usr/local/share/logspot
	sudo cp -r ./ui /usr/local/share/logspot/ui
	@echo "Installation complete. You can now use 'logspot' from anywhere."

# Install to user's local bin directory  
install-user: build
	@mkdir -p ~/bin
	@mkdir -p ~/.logspot
	@echo "Installing logspot to ~/bin..."
	cp ./bin/logspot ~/bin/
	@echo "Installing UI files to ~/.logspot/..."
	cp -r ./ui ~/.logspot/ui
	@echo "Installation complete. Make sure ~/bin is in your PATH."
	@echo "You can add this to your ~/.zshrc: export PATH=\"\$$HOME/bin:\$$PATH\""

# Clean build artifacts
clean:
	@echo "🧹 Cleaning..."
	@rm -rf $(BUILD_DIR)
	@echo "✅ Clean complete"

# Run in development
run:
	@echo "🚀 Running logspot..."
	@go run ./cmd/logspot

# Run with test logs
test:
	@echo "🧪 Running with test logs..."
	@go run ./cmd/logspot -test

# Force CLI mode (useful for testing on macOS)
run-cli:
	@echo "🖥️ Running in CLI mode..."
	@go run ./cmd/logspot -cli

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
	@echo '<key>CFBundleIdentifier</key><string>com.logspot.app</string>' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo '<key>CFBundleName</key><string>Logspot</string>' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo '<key>CFBundleVersion</key><string>1.0</string>' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo '<key>LSUIElement</key><true/>' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo '</dict></plist>' >> "$(APP_NAME).app/Contents/Info.plist"
	@echo "✅ macOS app created: $(APP_NAME).app"
	@echo "💡 Double-click $(APP_NAME).app to run as menubar app"
else
	@echo "❌ macOS app bundle can only be created on macOS"
endif

# Uninstall logspot
uninstall:
	@echo "Removing logspot from /usr/local/bin..."
	sudo rm -f /usr/local/bin/logspot
	@echo "Removing UI files from /usr/local/share/logspot..."
	sudo rm -rf /usr/local/share/logspot
	@echo "Uninstallation complete."

# Show usage help
help:
	@echo "🪶 Logspot - Build Commands"
	@echo ""
	@echo "📋 Available commands:"
	@echo "   make build         - Build the application"
	@echo "   make install       - Build and install system-wide (requires sudo)"
	@echo "   make install-user  - Install for current user only (~/bin)"
	@echo "   make uninstall     - Remove from system"
	@echo "   make clean         - Clean build artifacts"
	@echo "   make run           - Run in development mode"
	@echo "   make test          - Run with test logs"
	@echo "   make run-cli       - Force CLI mode (macOS)"
	@echo "   make deps          - Install Go dependencies"
	@echo "   make macos-app     - Create macOS .app bundle"
	@echo "   make help          - Show this help"
	@echo ""
	@echo "🎯 Usage after install:"
	@echo "   tail -f app.log | logspot      # Pipe logs"
	@echo "   docker logs container | logspot # Docker logs"
	@echo "   logspot -test                  # Test mode"
	@echo "   logspot -cli                   # Force CLI on macOS"
	@echo ""
	@echo "🍎 On macOS: Runs as menubar app by default"
	@echo "🐧 On Linux/Windows: Runs as CLI app"

# Default target
.DEFAULT_GOAL := help

release:
	@echo "🚀 Creating release..."
	@goreleaser release --rm-dist

brew:
	@echo "🍺 Creating brew formula..."
	@goreleaser release --rm-dist --snapshot
