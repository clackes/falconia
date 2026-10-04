package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// The instance distributes its own CLI.
//
// The binary is not downloaded from GitHub or a package registry: it comes
// from the same server it will talk to, with that server's address already
// configured. An instance on a network without internet access must still be
// installable, and whoever installs the CLI should not have to configure a URL
// before anything works. That matters doubly for agents: "run this one curl
// line" is an instruction every coding agent can follow.

// cliDir is where the Docker image puts the compiled binaries.
const cliDir = "/app/cli"

// installScript is what `curl -fsSL https://.../api/install | sh` runs.
func (a *api) installScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(`#!/bin/sh
# Installs the Falconia CLI.
#
#   curl -fsSL ` + a.deps.APIBaseURL + `/api/install | sh
#
# The binary is downloaded from this instance and configured to talk to it.
set -eu

API_BASE="` + a.deps.APIBaseURL + `"
BIN_DIR="${FALCONIA_BIN_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64)  arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
case "$os" in
  linux|darwin) ;;
  *) echo "unsupported system: $os (on Windows use ` + a.deps.APIBaseURL + `/api/install.ps1)" >&2; exit 1 ;;
esac

url="$API_BASE/api/cli/$os/$arch"
echo "Downloading falconia ($os/$arch) from $API_BASE"

mkdir -p "$BIN_DIR"
tmp=$(mktemp)
# Download to a temporary file and only then replace the old binary: a
# connection dropping halfway must not leave a truncated binary behind.
if ! curl -fsSL "$url" -o "$tmp"; then
  rm -f "$tmp"
  echo "download failed: $url" >&2
  exit 1
fi
chmod +x "$tmp"
mv "$tmp" "$BIN_DIR/falconia"

mkdir -p "$HOME/.falconia"
printf '{"api_base_url":"%s"}\n' "$API_BASE" > "$HOME/.falconia/instance.json"

echo ""
echo "Installed to $BIN_DIR/falconia"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "Add $BIN_DIR to your PATH:"
     echo "  export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac
echo ""
echo "Next step:  falconia login"
`))
}

// installScriptPowerShell is the Windows equivalent.
func (a *api) installScriptPowerShell(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(`# Installs the Falconia CLI.
#
#   irm ` + a.deps.APIBaseURL + `/api/install.ps1 | iex
$ErrorActionPreference = 'Stop'

$ApiBase = '` + a.deps.APIBaseURL + `'
$BinDir  = Join-Path $env:LOCALAPPDATA 'falconia'
$Arch    = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }

New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
$tmp = [System.IO.Path]::GetTempFileName()
Invoke-WebRequest -Uri "$ApiBase/api/cli/windows/$Arch" -OutFile $tmp
Move-Item -Force $tmp (Join-Path $BinDir 'falconia.exe')

$cfgDir = Join-Path $env:USERPROFILE '.falconia'
New-Item -ItemType Directory -Force -Path $cfgDir | Out-Null
"{""api_base_url"":""$ApiBase""}" | Set-Content (Join-Path $cfgDir 'instance.json')

Write-Host ""
Write-Host "Installed to $BinDir\falconia.exe"
Write-Host "Add $BinDir to your PATH, then run: falconia login"
`))
}

// serveCLI delivers the compiled binary for one platform.
func (a *api) serveCLI(w http.ResponseWriter, r *http.Request) {
	goos := filepath.Base(chi.URLParam(r, "os"))
	goarch := filepath.Base(chi.URLParam(r, "arch"))
	if !isWordy(goos) || !isWordy(goarch) {
		writeErr(w, http.StatusBadRequest, "invalid platform")
		return
	}
	name := "falconia_" + goos + "_" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	path := filepath.Join(cliDir, name)
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusNotFound,
			"this instance has no binary for "+goos+"/"+goarch)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// isWordy keeps anything but a plain word out of file names.
func isWordy(s string) bool {
	if s == "" || len(s) > 16 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789", c) {
			return false
		}
	}
	return true
}
