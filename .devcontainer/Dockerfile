# Development container for GNOME Power Display
# Includes all dependencies for building with Bazel, Go, Fyne GUI, D-Bus, and SQLite

FROM mcr.microsoft.com/devcontainers/base:bookworm

# Avoid prompts during package installation
ARG DEBIAN_FRONTEND=noninteractive

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    # Build essentials for CGO
    build-essential \
    gcc \
    g++ \
    pkg-config \
    # Go (will be managed by Bazel, but useful for IDE support)
    golang-go \
    # SQLite development libraries (for go-sqlite3)
    libsqlite3-dev \
    # D-Bus development libraries
    libdbus-1-dev \
    # Fyne GUI dependencies (OpenGL, GLFW, X11)
    libgl1-mesa-dev \
    libglfw3-dev \
    libxcursor-dev \
    libxrandr-dev \
    libxinerama-dev \
    libxi-dev \
    libxxf86vm-dev \
    # X11 and Wayland support
    libx11-dev \
    libxkbcommon-dev \
    # Additional Fyne dependencies
    xorg-dev \
    # Git for version control
    git \
    # Utilities
    curl \
    wget \
    ca-certificates \
    gnupg \
    && rm -rf /var/lib/apt/lists/*

# Install Bazelisk (Bazel version manager) - it will download the correct Bazel version
RUN curl -fsSL https://github.com/bazelbuild/bazelisk/releases/download/v1.25.0/bazelisk-linux-amd64 \
    -o /usr/local/bin/bazel && \
    chmod +x /usr/local/bin/bazel

# Set up environment for CGO
ENV CGO_ENABLED=1

# Verify installations
RUN bazel version && go version && gcc --version
