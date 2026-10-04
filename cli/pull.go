package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// cmdPull downloads an artifact's files into a folder.
//
// This is the "no lock-in" command. Whatever agent created an artifact, any
// other agent can pick it up from here:
//
//	falconia pull team/roadmap      # files land in ./roadmap
//	# ...let Codex, Claude Code or Gemini CLI edit them...
//	falconia deploy ./roadmap -m "new Q4 section"
//
// The folder keeps a .falconia.json that points back to the artifact, so the
// deploy updates it instead of creating a copy.
func cmdPull(args []string) error {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	version := fs.Int("version", 0, "pull a specific version (default: latest)")
	force := fs.Bool("force", false, "overwrite files in a non-empty folder")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("usage: falconia pull <space/slug | url> [folder]")
	}
	cfg, cl, err := authed()
	if err != nil {
		return err
	}
	ref, err := resolveRef(cl, cfg, rest[0])
	if err != nil {
		return err
	}

	var meta struct {
		Artifact struct {
			ID        string `json:"id"`
			Slug      string `json:"slug"`
			SpaceSlug string `json:"space_slug"`
			Path      string `json:"path"`
			MyRole    string `json:"my_role"`
		} `json:"artifact"`
	}
	if err := cl.do("GET", "/api/artifacts/"+ref, nil, &meta); err != nil {
		return err
	}
	if meta.Artifact.MyRole == "viewer" {
		return fmt.Errorf("you can view this artifact but not edit it: ask its owner for edit access")
	}

	dir := meta.Artifact.Slug
	if dir == "" {
		dir = "artifact"
	}
	if len(rest) > 1 {
		dir = rest[1]
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 && !*force {
		if readLinkFile(dir) == nil {
			return fmt.Errorf("%s is not empty: pass --force to overwrite, or choose another folder", dir)
		}
	}

	path := "/api/artifacts/" + meta.Artifact.ID + "/download"
	if *version > 0 {
		path += "?v=" + strconv.Itoa(*version)
	}
	var buf bytes.Buffer
	hdr, err := cl.download(path, &buf)
	if err != nil {
		return err
	}
	n, err := extractZip(buf.Bytes(), dir)
	if err != nil {
		return err
	}
	appURL := cfg.appBase() + meta.Artifact.Path
	writeLinkFile(dir, linkFile{
		ArtifactID: meta.Artifact.ID, Space: meta.Artifact.SpaceSlug,
		Slug: meta.Artifact.Slug, URL: appURL,
	})
	fmt.Printf("Pulled %d files of version %s into %s/ (entry: %s)\n",
		n, hdr.Get("X-Falconia-Version"), dir, hdr.Get("X-Falconia-Entry"))
	fmt.Printf("Edit them with any agent, then: falconia deploy %s -m \"what changed\"\n", dir)
	return nil
}

// extractZip unpacks an archive into dir, refusing any entry that would land
// outside it ("zip slip"): the archive comes from a server, but its paths
// were chosen by whoever published the artifact.
func extractZip(data []byte, dir string) (int, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid archive: %w", err)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return 0, err
	}
	n := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(f.Name))
		if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return n, fmt.Errorf("refusing to extract %q outside %s", f.Name, dir)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return n, err
		}
		rc, err := f.Open()
		if err != nil {
			return n, err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			rc.Close()
			return n, err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 1<<30))
		rc.Close()
		out.Close()
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
