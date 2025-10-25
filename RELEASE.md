# Release Guide

## Creating a New Release

Seyir uses GoReleaser with GitHub Actions for automated releases. When you push a git tag, it automatically builds binaries for multiple platforms and creates a GitHub release.

### 1. Version Tagging

```bash
# Create and push a version tag
git tag v1.0.0
git push origin v1.0.0
```

### 2. Automated Release Process

The GitHub Actions workflow (`.github/workflows/release.yml`) will automatically:

- Build binaries for multiple platforms (Linux, macOS, Windows)
- Create Docker images and push to GitHub Container Registry
- Generate checksums
- Create a GitHub release with changelog
- Build packages (deb, rpm, apk)

### 3. Supported Platforms

- **Linux**: amd64, arm64
- **macOS**: amd64, arm64 
- **Windows**: amd64

### 4. Installation Methods

After release, users can install seyir in several ways:

#### Direct Download
```bash
# Linux/macOS - automatic detection
curl -L "https://github.com/SemihY/seyir/releases/latest/download/seyir_$(uname -s)_$(uname -m).tar.gz" | tar xz
sudo mv seyir /usr/local/bin/

# Specific version
curl -L "https://github.com/SemihY/seyir/releases/download/v1.0.0/seyir_Linux_x86_64.tar.gz" | tar xz
```

#### Docker
```bash
# Latest version
docker run ghcr.io/semihy/seyir:latest

# Specific version
docker run ghcr.io/semihy/seyir:v1.0.0
```

#### Package Managers
```bash
# Debian/Ubuntu
wget https://github.com/SemihY/seyir/releases/download/v1.0.0/seyir_1.0.0_linux_amd64.deb
sudo dpkg -i seyir_1.0.0_linux_amd64.deb

# RHEL/CentOS/Fedora
wget https://github.com/SemihY/seyir/releases/download/v1.0.0/seyir_1.0.0_linux_amd64.rpm
sudo rpm -i seyir_1.0.0_linux_amd64.rpm

# Alpine Linux
wget https://github.com/SemihY/seyir/releases/download/v1.0.0/seyir_1.0.0_linux_amd64.apk
sudo apk add --allow-untrusted seyir_1.0.0_linux_amd64.apk
```

### 5. Version Information

After installation, users can check the version:

```bash
# Full version information
seyir version
seyir --version
seyir -v

# Example output:
# Version: v1.0.0
# Commit: a1b2c3d4
# Built: 2025-10-25_14:30:15
# Go: go1.24.7
# Platform: linux/amd64
```

### 6. Development vs Release Builds

- **Development builds** (local `make build`): Version shows as "dev"
- **Release builds** (GoReleaser): Version shows actual tag (e.g., "v1.0.0")

### 7. Testing a Release Locally

You can test the release process locally before pushing tags:

```bash
# Install GoReleaser
go install github.com/goreleaser/goreleaser@latest

# Test the release (doesn't publish anything)
goreleaser release --snapshot --clean

# Check generated binaries
ls dist/
```

### 8. Changelog Generation

GoReleaser automatically generates changelogs from git commits. Use conventional commit messages for better categorization:

- `feat:` - New features
- `fix:` - Bug fixes
- `perf:` - Performance improvements
- `docs:` - Documentation (excluded from changelog)
- `ci:` - CI/CD changes (excluded from changelog)

### 9. Pre-release and Draft Releases

For testing purposes, you can create pre-release versions:

```bash
# Create a pre-release tag
git tag v1.0.0-beta.1
git push origin v1.0.0-beta.1
```

GoReleaser will automatically mark releases with `-alpha`, `-beta`, `-rc` as pre-releases.

### 10. Docker Image Tags

Each release creates multiple Docker image tags:

- `ghcr.io/semihy/seyir:v1.0.0` - Exact version
- `ghcr.io/semihy/seyir:v1` - Major version
- `ghcr.io/semihy/seyir:v1.0` - Minor version  
- `ghcr.io/semihy/seyir:latest` - Latest release

This allows users to pin to specific versions or automatically get updates within a version range.