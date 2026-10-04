package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// linkFileName is written next to a deployed or pulled folder. It remembers
// which artifact the folder belongs to, so that `falconia deploy` run later —
// by you, by Codex, by Gemini — updates the same artifact instead of creating
// a new one. It never contains secrets and is never uploaded.
const linkFileName = ".falconia.json"

type linkFile struct {
	ArtifactID string `json:"artifact_id"`
	Space      string `json:"space"`
	Slug       string `json:"slug"`
	URL        string `json:"url"`
}

func readLinkFile(dir string) *linkFile {
	b, err := os.ReadFile(filepath.Join(dir, linkFileName))
	if err != nil {
		return nil
	}
	var l linkFile
	if json.Unmarshal(b, &l) != nil {
		return nil
	}
	return &l
}

func writeLinkFile(dir string, l linkFile) {
	b, _ := json.MarshalIndent(l, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, linkFileName), append(b, '\n'), 0o644)
}

// cmdDeploy publishes a file or a folder.
//
// The three-step protocol is not gratuitous complexity: the first step asks
// the server which files it is missing, and for an artifact republished twenty
// times a day almost every file is already there. The difference travels, not
// the bundle.
func cmdDeploy(args []string) error {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	space := fs.String("space", "", "destination space")
	slug := fs.String("slug", "", "stable identifier in the URL")
	title := fs.String("title", "", "displayed title")
	message := fs.String("message", "", "what changed")
	visibility := fs.String("visibility", "", "private | org | link")
	description := fs.String("description", "", "short description")
	category := fs.String("category", "", "category")
	entry := fs.String("entry", "", "the bundle's entry document")
	agent := fs.String("agent", "", "label this version with an agent name (auto-detected)")
	quiet := fs.Bool("open", false, "print only the URL")
	if err := addMessageAlias(fs, message); err != nil {
		return err
	}
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	root := "."
	if len(rest) > 0 {
		root = rest[0]
	}
	_, cl, err := authed()
	if err != nil {
		return err
	}
	if *agent != "" {
		cl.agent = *agent
	}

	files, err := collectBundle(root)
	if err != nil {
		return err
	}

	// A folder that was deployed or pulled before knows its artifact.
	var link *linkFile
	if info, err := os.Stat(root); err == nil && info.IsDir() {
		link = readLinkFile(root)
	}
	artifactID := ""
	if link != nil && *slug == "" && *space == "" {
		artifactID = link.ArtifactID
		*slug, *space = link.Slug, link.Space
	}
	if *slug == "" {
		*slug = deriveSlug(root)
	}
	entryPath := *entry
	if entryPath == "" {
		entryPath = pickEntry(files)
	}
	if entryPath == "" {
		return fmt.Errorf("no document to open: add an index.html or pass --entry")
	}

	var total int64
	shas := make([]string, 0, len(files))
	for _, f := range files {
		total += f.Size
		shas = append(shas, f.SHA256)
	}
	if !*quiet {
		fmt.Printf("%d files, %s\n", len(files), humanBytes(total))
	}

	// Step 1: what is actually missing.
	var missing struct {
		Missing []string `json:"missing"`
	}
	if err := cl.do("POST", "/api/blobs/missing", map[string]any{"shas": shas}, &missing); err != nil {
		return err
	}
	need := map[string]bool{}
	for _, s := range missing.Missing {
		need[s] = true
	}

	// Step 2: upload only those.
	uploaded, uploadedBytes := 0, int64(0)
	for _, f := range files {
		if !need[f.SHA256] {
			continue
		}
		if err := uploadOne(cl, f); err != nil {
			return fmt.Errorf("uploading %s: %w", f.Path, err)
		}
		uploaded++
		uploadedBytes += f.Size
		// The same hash can appear twice in a bundle: one upload is enough.
		need[f.SHA256] = false
	}
	if !*quiet {
		if uploaded == 0 {
			fmt.Println("nothing to upload: the server already has every file")
		} else {
			fmt.Printf("uploaded %d files (%s)\n", uploaded, humanBytes(uploadedBytes))
		}
	}

	// Step 3: declare the version. From here on it is atomic.
	payload := map[string]any{
		"artifact_id": artifactID,
		"space":       *space, "slug": *slug, "title": *title,
		"description": *description, "message": *message,
		"visibility": *visibility, "category": *category,
		"entry_path": entryPath, "files": files,
	}
	var out struct {
		URL      string `json:"url"`
		ShareURL string `json:"share_url"`
		Artifact struct {
			ID          string   `json:"id"`
			Title       string   `json:"title"`
			Slug        string   `json:"slug"`
			SpaceSlug   string   `json:"space_slug"`
			Visibility  string   `json:"visibility"`
			OpenThreads int      `json:"open_threads"`
			Agents      []string `json:"agents"`
		} `json:"artifact"`
		Version struct {
			VersionNumber int    `json:"version_number"`
			Agent         string `json:"agent"`
		} `json:"version"`
	}
	err = cl.do("POST", "/api/deploy", payload, &out)
	// The artifact recorded in the link file may have been deleted since:
	// fall back to space and slug rather than failing.
	if e, ok := err.(*apiError); ok && e.Status == 404 && artifactID != "" {
		payload["artifact_id"] = ""
		err = cl.do("POST", "/api/deploy", payload, &out)
	}
	if err != nil {
		return err
	}

	if info, err := os.Stat(root); err == nil && info.IsDir() {
		writeLinkFile(root, linkFile{
			ArtifactID: out.Artifact.ID, Space: out.Artifact.SpaceSlug,
			Slug: out.Artifact.Slug, URL: out.URL,
		})
	}

	if *quiet {
		if out.ShareURL != "" {
			fmt.Println(out.ShareURL)
		} else {
			fmt.Println(out.URL)
		}
		return nil
	}
	by := ""
	if out.Version.Agent != "" {
		by = " · by " + out.Version.Agent
	}
	fmt.Printf("\n  %s\n  version %d · %s%s\n",
		out.URL, out.Version.VersionNumber, visibilityLabel(out.Artifact.Visibility), by)
	if out.ShareURL != "" {
		fmt.Printf("  share link: %s\n", out.ShareURL)
	}
	if out.Artifact.OpenThreads > 0 {
		fmt.Printf("  %d open comments: falconia comments %s/%s\n",
			out.Artifact.OpenThreads, out.Artifact.SpaceSlug, out.Artifact.Slug)
	}
	fmt.Println()
	return nil
}

// addMessageAlias lets `-m` stand for `--message`, as in git: agents reach
// for it by reflex.
func addMessageAlias(fs *flag.FlagSet, message *string) error {
	fs.StringVar(message, "m", "", "alias for --message")
	return nil
}

func uploadOne(cl *Client, f fileEntry) error {
	fh, err := os.Open(f.local)
	if err != nil {
		return err
	}
	defer fh.Close()
	return cl.putRaw("/api/blobs/"+f.SHA256, fh, f.Size, nil)
}

// deriveSlug derives a stable identifier from the path, so republishing the
// same folder updates the same artifact instead of creating a new one.
func deriveSlug(root string) string {
	abs, err := absClean(root)
	if err != nil || abs == "" {
		return "artifact"
	}
	base := abs[strings.LastIndexByte(abs, '/')+1:]
	// "dist", "build" and "out" say nothing about the artifact: use the
	// project folder above them instead.
	switch base {
	case "dist", "build", "out", "_site":
		parent := abs[:strings.LastIndexByte(abs, '/')]
		if p := parent[strings.LastIndexByte(parent, '/')+1:]; p != "" {
			base = p
		}
	}
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case b.Len() > 0 && !prevDash:
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "artifact"
	}
	return out
}

func visibilityLabel(v string) string {
	switch v {
	case "org":
		return "visible to the whole team"
	case "link":
		return "anyone with the link can view"
	default:
		return "restricted (you and the people you added)"
	}
}
