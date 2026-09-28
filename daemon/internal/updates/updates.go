// Package updates asks GitHub whether a newer Bench release exists. It
// reads the latest published release only (drafts and prereleases don't
// count) and compares versions; nothing is downloaded or installed.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// LatestURL is GitHub's latest-release endpoint for Bench.
const LatestURL = "https://api.github.com/repos/d3v0psdan/bench/releases/latest"

// Release is a published Bench release.
type Release struct {
	Version     string    `json:"version"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
}

// ErrNoRelease means nothing has been published yet.
var ErrNoRelease = errors.New("no Bench release is published yet")

// Latest fetches the newest published release from url (LatestURL).
func Latest(ctx context.Context, client *http.Client, url string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("asking GitHub for releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Release{}, ErrNoRelease
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var body struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("reading GitHub's answer: %w", err)
	}
	return Release{Version: strings.TrimPrefix(body.TagName, "v"), URL: body.HTMLURL, PublishedAt: body.PublishedAt}, nil
}

// Newer reports whether version a is newer than b ("0.2.0" > "0.1.3").
// A prerelease ("0.1.0-dev") is older than its release ("0.1.0").
func Newer(a, b string) bool {
	an, ap := parse(a)
	bn, bp := parse(b)
	for i := range an {
		if an[i] != bn[i] {
			return an[i] > bn[i]
		}
	}
	return ap == "" && bp != ""
}

func parse(v string) ([3]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	core, pre, _ := strings.Cut(v, "-")
	var n [3]int
	for i, part := range strings.SplitN(core, ".", 3) {
		n[i], _ = strconv.Atoi(part)
	}
	return n, pre
}
