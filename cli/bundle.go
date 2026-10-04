package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// fileEntry is one file of the bundle, already sized and hashed.
type fileEntry struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`

	local string // where it is on disk, never sent to the server
}

// skipDirs are folders that are never part of an artifact.
//
// This is not a convenience list: `.git` holds the repository's full history,
// often with secrets in old commits, and `.env` holds the current ones. An
// agent running `falconia deploy .` in a hurry must not be able to publish
// either of them by mistake.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".svn": true, ".hg": true,
	"__pycache__": true, ".venv": true, "venv": true,
	".next": true, ".nuxt": true, ".cache": true, ".terraform": true,
}

var skipFiles = map[string]bool{
	".env": true, ".env.local": true, ".env.production": true,
	".DS_Store": true, "Thumbs.db": true, ".npmrc": true, ".netrc": true,
	linkFileName: true,
	"id_rsa":     true, "id_ed25519": true,
}

// collectBundle reads a file or a folder and produces the manifest.
func collectBundle(root string) ([]fileEntry, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		e, err := describeFile(root, filepath.Base(root))
		if err != nil {
			return nil, err
		}
		return []fileEntry{e}, nil
	}

	var out []fileEntry
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && skipDirs[name] {
				return fs.SkipDir
			}
			return nil
		}
		if skipFiles[name] || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") {
			return nil
		}
		// Symlinks are skipped: following them would publish something that
		// lives outside the folder one asked to publish.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		e, err := describeFile(p, filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		out = append(out, e)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no files to publish in %s", root)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func describeFile(local, rel string) (fileEntry, error) {
	f, err := os.Open(local)
	if err != nil {
		return fileEntry{}, err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return fileEntry{}, err
	}
	ct := mime.TypeByExtension(strings.ToLower(path.Ext(rel)))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return fileEntry{
		Path: rel, SHA256: hex.EncodeToString(h.Sum(nil)),
		Size: size, ContentType: ct, local: local,
	}, nil
}

// pickEntry chooses the bundle's entry document.
func pickEntry(files []fileEntry) string {
	have := map[string]bool{}
	for _, f := range files {
		have[f.Path] = true
	}
	for _, c := range []string{"index.html", "index.htm", "index.md", "README.md"} {
		if have[c] {
			return c
		}
	}
	only := ""
	for _, f := range files {
		switch strings.ToLower(path.Ext(f.Path)) {
		case ".html", ".htm", ".md":
			if only != "" {
				return ""
			}
			only = f.Path
		}
	}
	return only
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func absClean(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Clean(abs)), nil
}
