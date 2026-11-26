package utils

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

//go:embed bin/ffmpeg
var ffmpegData []byte

//go:embed bin/ffprobe
var ffprobeData []byte

//go:embed bin/yt-dlp
var ytDlpData []byte

//go:embed bin/shazam-cli
var shazamCliData []byte

var (
	binCacheMu    sync.Mutex
	extractedBins = make(map[string]string)
)

func getExecCacheDir() (string, error) {
	const dirName = "bin_cache"
	if err := os.MkdirAll(dirName, 0755); err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(dirName)
	if err != nil {
		return dirName, nil
	}
	return absPath, nil
}

// extractEmbeddedBin writes an embedded binary into bin_cache if it does not
// exist yet or if its SHA-256 differs from the embedded payload (e.g. after a
// bot upgrade). Writes atomically via temp file + rename to avoid ETXTBSY races.
func extractEmbeddedBin(name string, data []byte) (string, func(), error) {
	binCacheMu.Lock()
	defer binCacheMu.Unlock()

	if cachedPath, ok := extractedBins[name]; ok {
		if _, err := os.Stat(cachedPath); err == nil {
			return cachedPath, func() {}, nil
		}
		delete(extractedBins, name)
	}

	execDir, err := getExecCacheDir()
	if err != nil {
		return "", nil, fmt.Errorf("could not prepare bin_cache dir: %w", err)
	}

	fileName := name
	if runtime.GOOS == "windows" {
		fileName += ".exe"
	}
	binPath := filepath.Join(execDir, fileName)
	hashPath := binPath + ".sha256"

	sum := sha256.Sum256(data)
	wantHash := hex.EncodeToString(sum[:])

	if info, statErr := os.Stat(binPath); statErr == nil && info.Size() == int64(len(data)) {
		if gotHash, err := os.ReadFile(hashPath); err == nil && string(gotHash) == wantHash {
			extractedBins[name] = binPath
			return binPath, func() {}, nil
		}
	}

	tmp, err := os.CreateTemp(execDir, name+".tmp-*")
	if err != nil {
		return "", nil, fmt.Errorf("could not create temp file for %s: %w", name, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", nil, fmt.Errorf("could not write %s: %w", name, err)
	}
	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", nil, fmt.Errorf("could not chmod %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", nil, fmt.Errorf("could not close %s: %w", name, err)
	}

	if err := os.Rename(tmpPath, binPath); err != nil {
		os.Remove(tmpPath)
		return "", nil, fmt.Errorf("could not install %s: %w", name, err)
	}
	_ = os.WriteFile(hashPath, []byte(wantHash), 0644)
	extractedBins[name] = binPath

	return binPath, func() {}, nil
}

func GetFFmpegPath() (string, func(), error) {
	return extractEmbeddedBin("ffmpeg", ffmpegData)
}

func GetFFprobePath() (string, func(), error) {
	return extractEmbeddedBin("ffprobe", ffprobeData)
}

func GetYtDlpPath() (string, func(), error) {
	return extractEmbeddedBin("yt-dlp", ytDlpData)
}

// GetYtDlpPathForVersionCheck is kept as a compatibility alias for GetYtDlpPath.
func GetYtDlpPathForVersionCheck() (string, func(), error) {
	return GetYtDlpPath()
}

func GetShazamCliPath() (string, func(), error) {
	return extractEmbeddedBin("shazam-cli", shazamCliData)
}
