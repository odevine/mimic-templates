// Command reindex rebuilds index.json from the .mimic assets attached to this
// repo's GitHub Releases. index.json is the one file the app fetches to
// discover templates, so it must always reflect the live release assets: this
// tool lists every release, records each bundle's download URL, size, SHA-256,
// and the minEngine it declares, and writes them grouped by template.
//
// It runs in CI on every published release with GH_TOKEN and GITHUB_REPOSITORY
// in the environment. Release assets are immutable once published, so a bundle
// already present in the current index.json with the same URL and size is
// reused without re-downloading; only newly published bundles are fetched and
// hashed.
//
// Usage:
//
//	reindex -o index.json
//	reindex -o index.json -repo odevine/mimic-templates
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// indexSchema is the index.json schema version, bumped only when the index
// structure itself changes incompatibly
const indexSchema = 1

// Index is the whole catalog the app reads
type Index struct {
	Schema    int        `json:"schema"`
	Templates []Template `json:"templates"`
}

// Template groups every published version of one template. Latest is the
// highest semver, so the app can install it without scanning the list
type Template struct {
	Name     string    `json:"name"`
	Latest   string    `json:"latest"`
	Versions []Version `json:"versions"`
}

// Version is one downloadable bundle. SHA256 is the download-integrity check;
// MinEngine is copied up from the bundle so the app can filter incompatible
// versions before downloading anything
type Version struct {
	Version   string `json:"version"`
	MinEngine string `json:"minEngine"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "reindex:", err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("o", "index.json", "output index path")
	repo := flag.String("repo", os.Getenv("GITHUB_REPOSITORY"), "owner/name of the repo whose releases to index")
	flag.Parse()

	if *repo == "" {
		return fmt.Errorf("-repo is required (or set GITHUB_REPOSITORY)")
	}

	gh := &client{
		repo:  *repo,
		token: os.Getenv("GH_TOKEN"),
		http:  &http.Client{Timeout: 10 * time.Minute},
	}

	// Reuse SHA-256 and minEngine for assets already indexed. Release assets
	// are immutable, so a matching URL and size means the bundle is unchanged
	known := loadKnown(*out)

	releases, err := gh.releases()
	if err != nil {
		return err
	}

	byTemplate := map[string][]Version{}
	for _, rel := range releases {
		if rel.Draft {
			continue
		}
		name, ver, ok := splitTag(rel.TagName)
		if !ok {
			continue
		}
		asset, ok := findBundle(rel.Assets)
		if !ok {
			continue
		}

		v := Version{Version: ver, URL: asset.BrowserDownloadURL, Size: asset.Size}
		if prev, ok := known[asset.BrowserDownloadURL]; ok && prev.Size == asset.Size {
			v.SHA256, v.MinEngine = prev.SHA256, prev.MinEngine
		} else {
			sum, minEngine, err := gh.inspect(asset.BrowserDownloadURL)
			if err != nil {
				return fmt.Errorf("inspecting %s: %w", asset.BrowserDownloadURL, err)
			}
			v.SHA256, v.MinEngine = sum, minEngine
		}
		byTemplate[name] = append(byTemplate[name], v)
	}

	idx := Index{Schema: indexSchema}
	for name, versions := range byTemplate {
		sort.Slice(versions, func(i, j int) bool { return less(versions[j].Version, versions[i].Version) })
		idx.Templates = append(idx.Templates, Template{
			Name:     name,
			Latest:   versions[0].Version,
			Versions: versions,
		})
	}
	sort.Slice(idx.Templates, func(i, j int) bool { return idx.Templates[i].Name < idx.Templates[j].Name })

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d templates\n", *out, len(idx.Templates))
	return nil
}

// loadKnown indexes an existing index.json by download URL so unchanged assets
// need not be re-downloaded. A missing or unreadable file yields an empty map,
// so a first run simply fetches everything
func loadKnown(path string) map[string]Version {
	known := map[string]Version{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return known
	}
	var idx Index
	if json.Unmarshal(raw, &idx) != nil {
		return known
	}
	for _, t := range idx.Templates {
		for _, v := range t.Versions {
			known[v.URL] = v
		}
	}
	return known
}

// splitTag parses a release-please component tag "<template>/<version>". A tag
// that does not carry a version is not a template release and is skipped
func splitTag(tag string) (name, version string, ok bool) {
	name, version, ok = strings.Cut(tag, "/")
	if !ok || name == "" || version == "" {
		return "", "", false
	}
	return name, strings.TrimPrefix(version, "v"), true
}

// findBundle returns the single .mimic asset of a release
func findBundle(assets []asset) (asset, bool) {
	for _, a := range assets {
		if strings.HasSuffix(a.Name, ".mimic") {
			return a, true
		}
	}
	return asset{}, false
}

// less reports whether semver a precedes b, comparing major, minor, and patch
// numerically. A field that does not parse sorts as zero, which is enough for
// the plain X.Y.Z versions release-please emits
func less(a, b string) bool {
	pa, pb := parseSemver(a), parseSemver(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return a < b
}

func parseSemver(s string) [3]int {
	var out [3]int
	// Drop any build or prerelease suffix before splitting the core
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	for i, part := range strings.SplitN(s, ".", 3) {
		if i > 2 {
			break
		}
		out[i], _ = strconv.Atoi(part)
	}
	return out
}

// client talks to the GitHub REST API for one repo
type client struct {
	repo  string
	token string
	http  *http.Client
}

type release struct {
	TagName string  `json:"tag_name"`
	Draft   bool    `json:"draft"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// releases lists every release, following pagination
func (c *client) releases() ([]release, error) {
	var all []release
	for page := 1; ; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=100&page=%d", c.repo, page)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		c.auth(req)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("listing releases: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		var page1 []release
		if err := json.Unmarshal(body, &page1); err != nil {
			return nil, err
		}
		all = append(all, page1...)
		if len(page1) < 100 {
			return all, nil
		}
	}
}

// inspect downloads a bundle to a temp file, returning its SHA-256 and the
// minEngine its bundle.json declares. The file is streamed through the hash so
// a multi-hundred-MB bundle never sits fully in memory
func (c *client) inspect(url string) (sha, minEngine string, err error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("downloading: %s", resp.Status)
	}

	tmp, err := os.CreateTemp("", "reindex-*.mimic")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), resp.Body)
	if err != nil {
		return "", "", err
	}

	minEngine, err = readMinEngine(tmp, size)
	if err != nil {
		return "", "", err
	}
	return hex.EncodeToString(h.Sum(nil)), minEngine, nil
}

func (c *client) auth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// readMinEngine opens the bundle's zip and reads bundle.json's minEngine
func readMinEngine(r io.ReaderAt, size int64) (string, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		if f.Name != "bundle.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		var b struct {
			MinEngine string `json:"minEngine"`
		}
		if err := json.NewDecoder(rc).Decode(&b); err != nil {
			return "", err
		}
		return b.MinEngine, nil
	}
	return "", fmt.Errorf("bundle.json not found in archive")
}
