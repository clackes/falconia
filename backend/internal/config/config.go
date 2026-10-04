// Package config gathers configuration from the environment.
//
// One rule runs through this file: if a variable is missing and its default
// is harmless, start anyway; if it is missing and it is a secret or an address
// that security decisions rest on, refuse to start. An instance that boots in
// production with a random secret is worse than one that does not boot.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr        string
	ContentListenAddr string
	DatabaseURL       string

	// AppBaseURL is where the interface lives: the origin that owns sessions.
	AppBaseURL string
	// ContentBaseURL is where artifacts are served. It MUST be a different
	// origin from AppBaseURL: that separation is what keeps the code inside an
	// artifact away from the token of whoever is looking at it.
	ContentBaseURL string
	// APIBaseURL is what the CLI and MCP clients put in their configuration.
	APIBaseURL string

	JWTSecret     []byte
	ContentSecret []byte
	// ContentGrantTTL is how long the permission to read a version's files
	// lasts. Short enough to limit the damage of a forwarded content URL, long
	// enough not to interrupt someone reading a long document.
	ContentGrantTTL time.Duration

	// AllowRegistration closes sign-up after the first start, when the
	// instance should only accept invited people.
	AllowRegistration bool

	BlobBackend string // "fs" | "s3"
	BlobRoot    string
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3Region    string
	S3UseSSL    bool

	MaxUploadBytes int64
	MaxBundleFiles int
}

func Load() (*Config, error) {
	c := &Config{
		ListenAddr:        envOr("LISTEN_ADDR", ":8080"),
		ContentListenAddr: envOr("CONTENT_LISTEN_ADDR", ":8081"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		AppBaseURL:        strings.TrimRight(envOr("APP_BASE_URL", "http://localhost:5173"), "/"),
		ContentBaseURL:    strings.TrimRight(envOr("CONTENT_BASE_URL", "http://localhost:8081"), "/"),
		APIBaseURL:        strings.TrimRight(envOr("API_BASE_URL", "http://localhost:8080"), "/"),
		AllowRegistration: envBool("ALLOW_REGISTRATION", true),
		BlobBackend:       strings.ToLower(envOr("BLOB_BACKEND", "fs")),
		BlobRoot:          envOr("BLOB_ROOT", "/data/blobs"),
		S3Endpoint:        os.Getenv("S3_ENDPOINT"),
		S3AccessKey:       os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:       os.Getenv("S3_SECRET_KEY"),
		S3Bucket:          envOr("S3_BUCKET", "falconia"),
		S3Region:          envOr("S3_REGION", "us-east-1"),
		S3UseSSL:          envBool("S3_USE_SSL", false),
		ContentGrantTTL:   time.Duration(envInt("CONTENT_GRANT_TTL_MINUTES", 120)) * time.Minute,
		MaxUploadBytes:    int64(envInt("MAX_UPLOAD_MB", 64)) * 1 << 20,
		MaxBundleFiles:    envInt("MAX_BUNDLE_FILES", 500),
	}
	if c.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}

	appHost, err := originOf(c.AppBaseURL)
	if err != nil {
		return nil, fmt.Errorf("APP_BASE_URL: %w", err)
	}
	contentHost, err := originOf(c.ContentBaseURL)
	if err != nil {
		return nil, fmt.Errorf("CONTENT_BASE_URL: %w", err)
	}
	// If the two origins are the same, there is no isolation: an artifact
	// would run in the same origin as the viewer's session. Better not to
	// start than to start insecure while believing to be protected.
	if appHost == contentHost {
		return nil, fmt.Errorf(
			"APP_BASE_URL and CONTENT_BASE_URL must be different origins (both are %s): "+
				"untrusted artifacts cannot share the application's origin", appHost)
	}

	c.JWTSecret, err = secretFromEnv("JWT_SECRET")
	if err != nil {
		return nil, err
	}
	// When the grant secret is not set it is derived from the JWT one:
	// rotating the first invalidates sessions, rotating the second only
	// invalidates content URLs, and keeping them apart allows one without the
	// other.
	if s := os.Getenv("CONTENT_SECRET"); s != "" {
		c.ContentSecret = []byte(s)
	} else {
		c.ContentSecret = append([]byte("content:"), c.JWTSecret...)
	}

	if c.BlobBackend == "s3" && (c.S3Endpoint == "" || c.S3AccessKey == "" || c.S3SecretKey == "") {
		return nil, errors.New("BLOB_BACKEND=s3 requires S3_ENDPOINT, S3_ACCESS_KEY and S3_SECRET_KEY")
	}
	return c, nil
}

// secretFromEnv demands a real secret in production and only generates an
// ephemeral one when it is obviously development.
func secretFromEnv(key string) ([]byte, error) {
	if s := os.Getenv(key); s != "" {
		if len(s) < 16 {
			return nil, fmt.Errorf("%s is too short: it needs at least 16 characters", key)
		}
		return []byte(s), nil
	}
	if strings.ToLower(envOr("ENV", "development")) == "production" {
		return nil, fmt.Errorf("%s is required when ENV=production", key)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return []byte(hex.EncodeToString(buf)), nil
}

func originOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("an absolute URL is required, got %q", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(envOr(k, "")); err == nil && v > 0 {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	switch strings.ToLower(envOr(k, "")) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}
