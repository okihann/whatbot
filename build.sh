#!/bin/bash

# Exit immediately if any command fails
set -e

# Enter the folder containing the Go code
echo "--- Navigating to the bot directory ---"
cd bot

# ==========================================
# CONFIGURATION
# ==========================================
GO_VERSION="1.22.5" 
GO_DIR="$(pwd)/.standalone-go"
UTILS_BIN="$(pwd)/utils/bin"

# NGROK CONFIGURATION (UPDATE THESE!)
BOT_PORT="3058" # Must match the port your Go app listens on

# 1. Setup Standalone Go if necessary
export PATH="$GO_DIR/bin:$PATH"

if ! command -v go &> /dev/null; then
    echo "--- Go is not installed. Setting up standalone Go ${GO_VERSION} ---"
    
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)
    if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; fi
    if [ "$ARCH" = "aarch64" ]; then ARCH="arm64"; fi
    
    TAR_FILE="go${GO_VERSION}.${OS}-${ARCH}.tar.gz"
    curl -sL -o "$TAR_FILE" "https://go.dev/dl/${TAR_FILE}"
    
    mkdir -p "$GO_DIR"
    tar -C "$GO_DIR" -xzf "$TAR_FILE" --strip-components=1
    rm "$TAR_FILE"
    echo "✅ Standalone Go installed."
else
    echo "--- Go is already installed ---"
fi

# 2. Download Embedded Dependencies (yt-dlp, ffmpeg, ffprobe, ngrok)
echo "--- Checking Embedded Dependencies ---"
mkdir -p "$UTILS_BIN"

# Detect Architecture for downloads
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then 
    BIN_ARCH="amd64"
    YTDLP_GLIBC="yt-dlp_linux"
elif [ "$ARCH" = "aarch64" ]; then 
    BIN_ARCH="arm64"
    YTDLP_GLIBC="yt-dlp_linux_aarch64"
else
    echo "Unsupported architecture: $ARCH"
    exit 1
fi

# Detect Libc (musl vs glibc)
if ldd --version 2>&1 | grep -iq musl; then
    LIBC="musl"
else
    LIBC="glibc"
fi

# Download yt-dlp based on libc
if [ ! -f "$UTILS_BIN/yt-dlp" ]; then
    echo "Downloading yt-dlp for $LIBC ($ARCH)..."
    if [ "$LIBC" = "musl" ]; then
        curl -L "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp" -o "$UTILS_BIN/yt-dlp"
    else
        curl -L "https://github.com/yt-dlp/yt-dlp/releases/latest/download/${YTDLP_GLIBC}" -o "$UTILS_BIN/yt-dlp"
    fi
    chmod +x "$UTILS_BIN/yt-dlp"
fi

# Download Static FFmpeg & FFprobe
if [ ! -f "$UTILS_BIN/ffmpeg" ] || [ ! -f "$UTILS_BIN/ffprobe" ]; then
    echo "Downloading static ffmpeg & ffprobe ($ARCH)..."
    FFMPEG_URL="https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-${BIN_ARCH}-static.tar.xz"
    curl -L "$FFMPEG_URL" -o "ffmpeg.tar.xz"
    
    mkdir -p ffmpeg_temp
    tar -xf ffmpeg.tar.xz -C ffmpeg_temp --strip-components=1
    mv ffmpeg_temp/ffmpeg "$UTILS_BIN/ffmpeg"
    mv ffmpeg_temp/ffprobe "$UTILS_BIN/ffprobe"
    rm -rf ffmpeg_temp ffmpeg.tar.xz
fi

# Download Ngrok
if [ ! -f "$UTILS_BIN/ngrok" ]; then
    echo "Downloading ngrok ($ARCH)..."
    NGROK_URL="https://bin.equinox.io/c/bNyj1mQVY4c/ngrok-v3-stable-linux-${BIN_ARCH}.tgz"
    curl -sL "$NGROK_URL" -o "ngrok.tgz"
    tar -xf ngrok.tgz -C "$UTILS_BIN"
    chmod +x "$UTILS_BIN/ngrok"
    rm ngrok.tgz
fi
echo "✅ Embedded dependencies ready."

# 3. Build Process (Only runs if the binary doesn't exist)
if [ ! -f "./bin/bot" ]; then
    echo "--- No binary found. Starting build process ---"

    export GOTMPDIR="$(pwd)/.gotmp"
    export GOCACHE="$(pwd)/.gocache"
    mkdir -p "$GOTMPDIR" "$GOCACHE"

    if [ ! -f "go.mod" ]; then
        echo "--- go.mod not found. Initializing Go module ---"
        go mod init nopebot
    fi

    go mod tidy

    echo "--- Building Executable ---"
    mkdir -p bin
    go build -o bin/bot main.go

    echo "✅ Build Complete!"
    rm -rf "$GOTMPDIR" "$GOCACHE"
else
    echo "--- Built binary found in bin/ folder. Skipping build... ---"
fi

# 4. Start Services (Ngrok + Bot)
echo "--- Starting Ngrok Tunnel ---"
if [ "$NGROK_AUTHTOKEN" != "YOUR_NGROK_AUTHTOKEN" ]; then
    "$UTILS_BIN/ngrok" config add-authtoken "$NGROK_AUTHTOKEN"
    
    # Run Ngrok in the background and pipe output to a log file
    "$UTILS_BIN/ngrok" http --domain="$NGROK_DOMAIN" "$BOT_PORT" --log=stdout > ngrok.log &
    
    echo "✅ Ngrok is running in the background routing to $NGROK_DOMAIN"
else
    echo "⚠️  Ngrok Authtoken not configured. Skipping tunnel startup."
fi

echo "--- Starting Bot ---"
cd bin
exec ./bot