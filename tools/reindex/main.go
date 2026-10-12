// Command reindex rebuilds index.json from the .mimic assets attached to this
// repo's GitHub Releases. index.json is the one file the app fetches to
// discover templates, so it must always reflect the live release assets: this
// tool lists every release, records each bundle's download URL, size, SHA-256,
// container format, and the minEngine it declares, and writes them grouped by
// template. A template is named by the id in its newest bundle's header, such as
// mimic.m15, and carries that bundle's description and the card shapes it
// renders, so the app can describe a template before it is installed.
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
const indexSchema = 2

// Index is the whole catalog the app reads
type Index struct {
	Schema    int        `json:"schema"`
	Templates []Template `json:"templates"`
}

// Template groups every published version of one template. Latest is the
// highest semver, so the app can install it without scanning the list
type Template struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Shapes      []Shape   `json:"shapes,omitempty"`
	Latest      string    `json:"latest"`
	Versions    []Version `json:"versions"`
}

// Shape is one face a template renders, as the engine names it
type Shape struct {
	Role string `json:"role"`
	Kind string `json:"kind"`
}

// Version is one downloadable bundle. SHA256 is the download-integrity check;
// Format and MinEngine are copied up from the bundle so the app can filter
// incompatible versions before downloading anything. A version with no Format is
// from the first container format
type Version struct {
	Version   string `json:"version"`
	Format    int    `json:"format,omitempty"`
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

	// Reuse what is known about assets already indexed. Release assets are
	// immutable, so a matching URL and size means the bundle is unchanged
	known := loadKnown(*out)

	releases, err := gh.releases()
	if err != nil {
		return err
	}

	byTemplate := map[string][]Version{}
	// described holds the description and shapes read from a bundle this run
	// downloaded, by the template it belongs to and the version it is
	type described struct {
		version     string
		description string
		shapes      []Shape
	}
	fresh := map[string]described{}
	for _, rel := range releases {
		if rel.Draft {
			continue
		}
		tagName, ver, ok := splitTag(rel.TagName)
		if !ok {
			continue
		}
		asset, ok := findBundle(rel.Assets)
		if !ok {
			continue
		}

		v := Version{Version: ver, URL: asset.BrowserDownloadURL, Size: asset.Size}
		name := tagName
		if prev, ok := known.versions[asset.BrowserDownloadURL]; ok && prev.Size == asset.Size {
			v.SHA256, v.MinEngine, v.Format = prev.SHA256, prev.MinEngine, prev.Format
			if prevName := known.names[asset.BrowserDownloadURL]; prevName != "" {
				name = prevName
			}
		} else {
			info, err := gh.inspect(asset.BrowserDownloadURL)
			if err != nil {
				return fmt.Errorf("inspecting %s: %w", asset.BrowserDownloadURL, err)
			}
			v.SHA256, v.MinEngine, v.Format = info.SHA256, info.MinEngine, info.Format
			if info.ID != "" {
				name = info.ID
			}
			if cur, ok := fresh[name]; !ok || less(cur.version, ver) {
				fresh[name] = described{ver, info.Description, info.Shapes}
			}
		}
		byTemplate[name] = append(byTemplate[name], v)
	}

	idx := Index{Schema: indexSchema}
	for name, versions := range byTemplate {
		sort.Slice(versions, func(i, j int) bool { return less(versions[j].Version, versions[i].Version) })
		t := Template{Name: name, Latest: versions[0].Version, Versions: versions}
		// The description and shapes are the newest bundle's. A bundle that was not
		// downloaded this run keeps what the index already said about its template
		if f, ok := fresh[name]; ok && f.version == t.Latest {
			t.Description, t.Shapes = f.description, f.shapes
		} else if prev, ok := known.templates[name]; ok && prev.Latest == t.Latest {
			t.Description, t.Shapes = prev.Description, prev.Shapes
		}
		idx.Templates = append(idx.Templates, t)
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

// knownIndex is what an existing index.json says, so unchanged assets need not
// be downloaded again
type knownIndex struct {
	// versions are the bundles by download URL, and names the template each
	// belongs to
	versions  map[string]Version
	names     map[string]string
	templates map[string]Template
}

// loadKnown reads an existing index.json. A missing or unreadable file, or one
// from an older schema, yields nothing known, so a first run simply fetches
// everything and an older index is rebuilt with the new fields
func loadKnown(path string) knownIndex {
	known := knownIndex{versions: map[string]Version{}, names: map[string]string{}, templates: map[string]Template{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		return known
	}
	var idx Index
	if json.Unmarshal(raw, &idx) != nil || idx.Schema != indexSchema {
		return known
	}
	for _, t := range idx.Templates {
		known.templates[t.Name] = t
		for _, v := range t.Versions {
			known.versions[v.URL] = v
			known.names[v.URL] = t.Name
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

// bundleInfo is what a downloaded bundle says about itself
type bundleInfo struct {
	SHA256      string
	Format      int
	ID          string
	MinEngine   string
	Description string
	Shapes      []Shape
}

// inspect downloads a bundle to a temp file, returning its SHA-256 and what its
// header and manifest declare. The file is streamed through the hash so a
// multi-hundred-MB bundle never sits fully in memory
func (c *client) inspect(url string) (bundleInfo, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return bundleInfo{}, err
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return bundleInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return bundleInfo{}, fmt.Errorf("downloading: %s", resp.Status)
	}

	tmp, err := os.CreateTemp("", "reindex-*.mimic")
	if err != nil {
		return bundleInfo{}, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), resp.Body)
	if err != nil {
		return bundleInfo{}, err
	}

	info, err := readBundleInfo(tmp, size)
	if err != nil {
		return bundleInfo{}, err
	}
	info.SHA256 = hex.EncodeToString(h.Sum(nil))
	return info, nil
}

func (c *client) auth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// readBundleInfo opens the bundle's zip and reads bundle.json, and from a bundle
// in a format that has an id, the description and the shapes its manifest says
// its layouts serve
func readBundleInfo(r io.ReaderAt, size int64) (bundleInfo, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return bundleInfo{}, err
	}
	var header *zip.File
	var manifest *zip.File
	for _, f := range zr.File {
		switch f.Name {
		case "bundle.json":
			header = f
		case "manifest.json":
			manifest = f
		}
	}
	if header == nil {
		return bundleInfo{}, fmt.Errorf("bundle.json not found in archive")
	}
	var info bundleInfo
	var b struct {
		Format    int    `json:"format"`
		ID        string `json:"id"`
		MinEngine string `json:"minEngine"`
	}
	if err := decodeEntry(header, &b); err != nil {
		return bundleInfo{}, err
	}
	info.Format, info.ID, info.MinEngine = b.Format, b.ID, b.MinEngine
	// The first container format has no id and a manifest this tool does not read
	if b.ID == "" || manifest == nil {
		return info, nil
	}
	var m struct {
		Description string `json:"description"`
		Layouts     map[string]struct {
			Serves []Shape `json:"serves"`
		} `json:"layouts"`
	}
	if err := decodeEntry(manifest, &m); err != nil {
		return bundleInfo{}, err
	}
	info.Description = m.Description
	seen := map[Shape]bool{}
	for _, l := range m.Layouts {
		for _, sh := range l.Serves {
			if !seen[sh] {
				seen[sh] = true
				info.Shapes = append(info.Shapes, sh)
			}
		}
	}
	sort.Slice(info.Shapes, func(i, j int) bool {
		if info.Shapes[i].Role != info.Shapes[j].Role {
			return info.Shapes[i].Role < info.Shapes[j].Role
		}
		return info.Shapes[i].Kind < info.Shapes[j].Kind
	})
	return info, nil
}

func decodeEntry(f *zip.File, v any) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return json.NewDecoder(rc).Decode(v)
}
