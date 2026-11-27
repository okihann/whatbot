#!/usr/bin/env bash
# NopeBot Auto-Updater & Launcher for Pterodactyl Panel
# Checks GitHub Releases on every server start/restart, downloads & extracts
# (.tar.gz or .zip) only when a newer version is available, verifies SHA256SUMS,
# preserves db/config/.env, and execs the binary so Pterodactyl signals work cleanly.

set -euo pipefail

# --- Configuration (override via Pterodactyl Startup / Environment Variables) ---
GITHUB_REPO="${GITHUB_REPO:-okihann/whatbot}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
RELEASE_TAG="${RELEASE_TAG:-latest}"          # "latest" or a pinned tag like "v2026.11.27-51"
ASSET_PATTERN="${ASSET_PATTERN:-}"            # Optional exact substring match for release asset
BIN_NAME="${BIN_NAME:-nopebot}"               # Installed executable name in server root
VERSION_FILE="${VERSION_FILE:-.release_version}"
AUTO_UPDATE="${AUTO_UPDATE:-true}"            # Set to "false" to skip GitHub check
FORCE_UPDATE="${FORCE_UPDATE:-false}"         # Set to "true" to always re-download

log()  { printf '[NopeBot-Updater] %s\n' "$*"; }
warn() { printf '[NopeBot-Updater] WARNING: %s\n' "$*" >&2; }
err()  { printf '[NopeBot-Updater] ERROR: %s\n' "$*" >&2; }

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)   echo "amd64" ;;
    aarch64|arm64)  echo "arm64" ;;
    *)
      err "Unsupported CPU architecture: $(uname -m)"
      exit 1
      ;;
  esac
}

http_get() {
  local url="$1"
  local out="${2:-}"
  local auth_header=()
  if [ -n "$GITHUB_TOKEN" ]; then
    auth_header=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
  fi

  if command -v curl >/dev/null 2>&1; then
    if [ -n "$out" ]; then
      curl -fsSL --retry 3 --connect-timeout 15 --max-time 180 \
        -H "Accept: application/vnd.github+json" \
        -H "User-Agent: NopeBot-Pterodactyl-Updater" \
        "${auth_header[@]}" \
        "$url" -o "$out"
    else
      curl -fsSL --retry 3 --connect-timeout 15 --max-time 30 \
        -H "Accept: application/vnd.github+json" \
        -H "User-Agent: NopeBot-Pterodactyl-Updater" \
        "${auth_header[@]}" \
        "$url"
    fi
  elif command -v wget >/dev/null 2>&1; then
    local wget_args=(--tries=3 --timeout=30 --header="Accept: application/vnd.github+json" --header="User-Agent: NopeBot-Pterodactyl-Updater")
    if [ -n "$GITHUB_TOKEN" ]; then
      wget_args+=(--header="Authorization: Bearer ${GITHUB_TOKEN}")
    fi
    if [ -n "$out" ]; then
      wget -q "${wget_args[@]}" -O "$out" "$url"
    else
      wget -q "${wget_args[@]}" -O - "$url"
    fi
  else
    err "Neither curl nor wget is installed in this container."
    return 1
  fi
}

extract_archive() {
  local archive="$1"
  local dest="$2"

  case "$archive" in
    *.tar.gz|*.tgz)
      tar -xzf "$archive" -C "$dest"
      ;;
    *.zip)
      if command -v unzip >/dev/null 2>&1; then
        unzip -oq "$archive" -d "$dest"
      elif command -v busybox >/dev/null 2>&1; then
        busybox unzip -oq "$archive" -d "$dest"
      elif command -v python3 >/dev/null 2>&1; then
        python3 -c 'import sys, zipfile; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])' "$archive" "$dest"
      else
        err "Cannot extract .zip: neither unzip, busybox, nor python3 is installed."
        return 1
      fi
      ;;
    *)
      cp "$archive" "$dest/$BIN_NAME"
      ;;
  esac
}

update_from_github() {
  local arch
  arch="$(detect_arch)"

  local api_url
  if [ "$RELEASE_TAG" = "latest" ]; then
    api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
  else
    api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/tags/${RELEASE_TAG}"
  fi

  log "Checking GitHub release (${GITHUB_REPO} @ ${RELEASE_TAG}, linux-${arch})..."
  local release_json
  if ! release_json="$(http_get "$api_url")"; then
    warn "Could not reach GitHub API (${api_url})."
    return 1
  fi

  # Parse tag_name without requiring jq (safe for both pretty and minified JSON)
  local latest_tag
  latest_tag="$(printf '%s\n' "$release_json" | grep -oE '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n 1 | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')"
  if [ -z "$latest_tag" ]; then
    warn "Failed to parse tag_name from GitHub API response."
    return 1
  fi

  local current_tag=""
  if [ -f "$VERSION_FILE" ]; then
    current_tag="$(tr -d '[:space:]' < "$VERSION_FILE")"
  fi

  if [ "$FORCE_UPDATE" != "true" ] && [ -x "./$BIN_NAME" ] && [ "$current_tag" = "$latest_tag" ]; then
    log "Already up to date (${latest_tag}). Skipping download."
    return 0
  fi

  log "Update available: '${current_tag:-none}' -> '${latest_tag}'"

  # Collect all browser_download_url entries (works on single-line minified JSON too)
  local urls
  urls="$(printf '%s\n' "$release_json" | grep -oE '"browser_download_url"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')"

  local asset_url=""
  if [ -n "$ASSET_PATTERN" ]; then
    asset_url="$(printf '%s\n' "$urls" | grep -F "$ASSET_PATTERN" | head -n 1 || true)"
  fi
  if [ -z "$asset_url" ]; then
    # Prefer nopebot-linux-<arch>.(zip|tar.gz|tgz), then any linux-<arch> archive
    asset_url="$(printf '%s\n' "$urls" | grep -E "/nopebot-linux-${arch}\.(zip|tar\.gz|tgz)$" | head -n 1 || true)"
  fi
  if [ -z "$asset_url" ]; then
    asset_url="$(printf '%s\n' "$urls" | grep -iE "linux.*${arch}.*\.(zip|tar\.gz|tgz)$" | head -n 1 || true)"
  fi
  if [ -z "$asset_url" ]; then
    asset_url="$(printf '%s\n' "$urls" | grep -iE "linux.*${arch}" | grep -viE "(sha256|checksum|sig|asc)" | head -n 1 || true)"
  fi

  if [ -z "$asset_url" ]; then
    warn "No matching linux-${arch} asset found in release ${latest_tag}."
    return 1
  fi

  local asset_name
  asset_name="$(basename "$asset_url")"

  local tmpdir
  tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/nopebot-update.XXXXXX")"
  trap 'rm -rf "$tmpdir"' RETURN

  log "Downloading ${asset_name}..."
  if ! http_get "$asset_url" "${tmpdir}/${asset_name}"; then
    warn "Download failed for ${asset_url}"
    return 1
  fi

  # Optional SHA256SUMS verification if published in the release
  local sums_url
  sums_url="$(printf '%s\n' "$urls" | grep -E '/SHA256SUMS(\.txt)?$' | head -n 1 || true)"
  if [ -n "$sums_url" ] && command -v sha256sum >/dev/null 2>&1; then
    if http_get "$sums_url" "${tmpdir}/SHA256SUMS"; then
      local expected_hash actual_hash
      expected_hash="$(grep -E "[[:space:]\*](.*/)?${asset_name}$" "${tmpdir}/SHA256SUMS" | awk '{print $1}' | head -n 1 || true)"
      if [ -n "$expected_hash" ]; then
        actual_hash="$(sha256sum "${tmpdir}/${asset_name}" | awk '{print $1}')"
        if [ "$expected_hash" != "$actual_hash" ]; then
          err "SHA256 mismatch for ${asset_name}! Expected ${expected_hash}, got ${actual_hash}"
          return 1
        fi
        log "SHA256 verified (${actual_hash:0:12}...)."
      fi
    fi
  fi

  mkdir -p "${tmpdir}/extracted"
  log "Extracting ${asset_name}..."
  extract_archive "${tmpdir}/${asset_name}" "${tmpdir}/extracted"

  # Find the extracted executable binary
  local found_bin=""
  found_bin="$(find "${tmpdir}/extracted" -type f \( -name "nopebot*" -o -name "${BIN_NAME}*" \) ! -name "*.txt" ! -name "*.md" ! -name "*.json" | head -n 1 || true)"
  if [ -z "$found_bin" ]; then
    # Fallback: pick the largest regular file inside extracted archive
    found_bin="$(find "${tmpdir}/extracted" -type f -exec ls -S {} + 2>/dev/null | head -n 1 || true)"
  fi

  if [ -z "$found_bin" ] || [ ! -s "$found_bin" ]; then
    err "Could not locate binary inside ${asset_name}."
    return 1
  fi

  chmod +x "$found_bin"
  mv -f "$found_bin" "./${BIN_NAME}.new"
  mv -f "./${BIN_NAME}.new" "./${BIN_NAME}"
  printf '%s\n' "$latest_tag" > "$VERSION_FILE"
  log "Installed ${BIN_NAME} (${latest_tag}) successfully."
  return 0
}

main() {
  if [ "$AUTO_UPDATE" = "true" ]; then
    if ! update_from_github; then
      if [ -x "./$BIN_NAME" ]; then
        warn "Continuing with existing ./${BIN_NAME} binary."
      else
        err "Update failed and no existing ./${BIN_NAME} binary found. Exiting."
        exit 1
      fi
    fi
  else
    log "AUTO_UPDATE=false; skipping GitHub release check."
  fi

  if [ ! -x "./$BIN_NAME" ]; then
    err "Binary ./${BIN_NAME} not found or not executable."
    exit 1
  fi

  log "Starting ./${BIN_NAME}..."
  exec "./${BIN_NAME}" "$@"
}

main "$@"
