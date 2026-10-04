package main

import (
	"flag"
	"fmt"
	"net/url"
	"strings"
)

// cmdShare changes who can open an artifact, the way the Share dialog of a
// Google Doc does: one general-access setting plus a list of people.
//
//	falconia share team/roadmap --access link        # anyone with the link
//	falconia share team/roadmap --access org --team-role viewer
//	falconia share team/roadmap --add ana@example.com --role editor
//	falconia share team/roadmap --remove ana@example.com
//	falconia share team/roadmap --rotate              # new secret link
//
// With no options it prints the current state and the link to share.
func cmdShare(args []string) error {
	fs := flag.NewFlagSet("share", flag.ContinueOnError)
	access := fs.String("access", "", "general access: private | org | link")
	teamRole := fs.String("team-role", "", "what team members can do with --access org: viewer | editor")
	add := fs.String("add", "", "give this email access")
	role := fs.String("role", "viewer", "role for --add: viewer | editor")
	remove := fs.String("remove", "", "revoke this email's access")
	rotate := fs.Bool("rotate", false, "replace the secret link with a new one")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("usage: falconia share <space/slug> [--access private|org|link] [--add email] ...")
	}
	cfg, cl, err := authed()
	if err != nil {
		return err
	}
	ref, err := resolveRef(cl, cfg, rest[0])
	if err != nil {
		return err
	}

	if *access != "" || *teamRole != "" {
		patch := map[string]string{}
		if *access != "" {
			patch["visibility"] = *access
		}
		if *teamRole != "" {
			patch["team_role"] = *teamRole
		}
		if err := cl.do("PATCH", "/api/artifacts/"+ref, patch, nil); err != nil {
			return err
		}
	}
	if *add != "" {
		if err := cl.do("POST", "/api/artifacts/"+ref+"/shares",
			map[string]string{"email": *add, "role": *role}, nil); err != nil {
			return err
		}
		fmt.Printf("%s can now %s it.\n", *add, map[bool]string{true: "edit", false: "view"}[*role == "editor"])
	}
	if *remove != "" {
		if err := cl.do("DELETE", "/api/artifacts/"+ref+"/shares/"+url.PathEscape(*remove), nil, nil); err != nil {
			return err
		}
		fmt.Printf("%s no longer has direct access.\n", *remove)
	}
	if *rotate {
		if err := cl.do("POST", "/api/artifacts/"+ref+"/link/rotate", map[string]string{}, nil); err != nil {
			return err
		}
		fmt.Println("The old link no longer works.")
	}

	var out struct {
		Artifact struct {
			Title      string `json:"title"`
			Path       string `json:"path"`
			LinkPath   string `json:"link_path"`
			Visibility string `json:"visibility"`
			TeamRole   string `json:"team_role"`
			SpaceName  string `json:"space_name"`
		} `json:"artifact"`
	}
	if err := cl.do("GET", "/api/artifacts/"+ref, nil, &out); err != nil {
		return err
	}
	var shares struct {
		Shares []struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"shares"`
	}
	_ = cl.do("GET", "/api/artifacts/"+ref+"/shares", nil, &shares)

	a := out.Artifact
	fmt.Printf("\n  %s\n", a.Title)
	switch a.Visibility {
	case "link":
		fmt.Printf("  General access: anyone with the link can view\n")
		fmt.Printf("  Link:           %s\n", cfg.appBase()+a.LinkPath)
	case "org":
		fmt.Printf("  General access: everyone in %s (%s)\n", a.SpaceName, a.TeamRole)
		fmt.Printf("  Link:           %s\n", cfg.appBase()+a.Path)
	default:
		fmt.Printf("  General access: restricted\n")
		fmt.Printf("  Link:           %s\n", cfg.appBase()+a.Path)
	}
	if len(shares.Shares) > 0 {
		people := make([]string, 0, len(shares.Shares))
		for _, s := range shares.Shares {
			people = append(people, s.Email+" ("+s.Role+")")
		}
		fmt.Printf("  People:         %s\n", strings.Join(people, ", "))
	}
	fmt.Println()
	return nil
}
