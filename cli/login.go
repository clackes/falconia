package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

// cmdLogin runs the device-code flow.
//
// It never asks for the password, and not out of fussiness: `falconia login`
// is often run from inside an agent session, where everything typed ends up in
// the model's context and the shell history. The short code is worthless on
// its own, and the confirmation happens in the browser where the person is
// already signed in.
func cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	label := fs.String("label", defaultLabel(), "a name for this session")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cl := newClient(cfg)

	var start struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURL string `json:"verification_url"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := cl.do("POST", "/api/auth/device/start", map[string]string{"label": *label}, &start); err != nil {
		return err
	}

	fmt.Printf("\n  Open:  %s\n  Code:  %s\n\n  Waiting for confirmation…\n",
		start.VerificationURL, start.UserCode)

	interval := time.Duration(max(start.Interval, 2)) * time.Second
	deadline := time.Now().Add(time.Duration(max(start.ExpiresIn, 60)) * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(interval)
		var poll struct {
			Status     string `json:"status"`
			Token      string `json:"token"`
			APIBaseURL string `json:"api_base_url"`
			User       struct {
				Email       string `json:"email"`
				DisplayName string `json:"display_name"`
			} `json:"user"`
		}
		err := cl.do("POST", "/api/auth/device/poll",
			map[string]string{"device_code": start.DeviceCode}, &poll)
		if e, ok := err.(*apiError); ok {
			// 202 means "not yet"; 410 means the code expired.
			if e.Status == 410 {
				return fmt.Errorf("the code expired: try `falconia login` again")
			}
			return err
		}
		if err != nil {
			return err
		}
		if poll.Status != "ok" || poll.Token == "" {
			continue
		}
		cfg.Token = poll.Token
		cfg.Email = poll.User.Email
		if poll.APIBaseURL != "" {
			cfg.APIBaseURL = poll.APIBaseURL
		}
		if err := cfg.save(); err != nil {
			return err
		}
		fmt.Printf("\n  Signed in as %s <%s>\n\n", poll.User.DisplayName, poll.User.Email)
		return nil
	}
	return fmt.Errorf("no confirmation received in time")
}

// defaultLabel gives the session a recognizable name, so whoever looks at
// their list of keys can tell which machine (and agent) it belongs to.
func defaultLabel() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown host"
	}
	if a := detectAgent(); a != "" {
		return a + " on " + host
	}
	return "falconia cli on " + host
}
