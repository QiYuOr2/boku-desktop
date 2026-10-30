package playtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"boku/internal/platform/httpc"
)

// covers keeps one image per Steam app next to the session database.
// The flyout reads the file; it does not ask the network itself.
type covers struct {
	dir    string
	client *httpc.Client
	fetch  func(ctx context.Context, appID string) (image []byte, name string, err error)

	mu     sync.Mutex
	busy   map[string]bool
	failed map[string]bool
}

func newCovers(dir string) *covers {
	client := httpc.New(httpc.Options{})
	c := &covers{
		dir:    filepath.Join(dir, "covers"),
		client: client,
		busy:   map[string]bool{},
		failed: map[string]bool{},
	}
	c.fetch = func(ctx context.Context, appID string) ([]byte, string, error) {
		return fetchSteamCover(ctx, c.client, appID)
	}
	return c
}

// Want starts a download for each app that does not already have a file.
// done runs, off the caller, after a new file is written.
func (c *covers) Want(ids []string, done func()) {
	if c == nil || c.fetch == nil {
		return
	}
	for _, id := range ids {
		if !validAppID(id) {
			continue
		}
		c.mu.Lock()
		if c.busy[id] || c.failed[id] || c.fresh(id) {
			c.mu.Unlock()
			continue
		}
		c.busy[id] = true
		c.mu.Unlock()
		go c.pull(id, done)
	}
}

func (c *covers) pull(id string, done func()) {
	data, name, err := c.fetch(context.Background(), id)
	wrote := false
	if name != "" {
		if werr := c.writeTitle(id, name); werr != nil {
			if err == nil {
				err = werr
			}
		} else {
			wrote = true
		}
	}
	if err == nil && !isImage(data) {
		err = fmt.Errorf("playtime: cover for %s is not an image", id)
	}
	if err == nil {
		if werr := c.write(id, data); werr != nil {
			err = werr
		} else {
			wrote = true
			if widerThanTall(data) {
				_ = c.writeWide(id)
			} else {
				_ = os.Remove(c.widePath(id))
			}
		}
	}
	c.mu.Lock()
	delete(c.busy, id)
	if err != nil {
		c.failed[id] = true
	}
	c.mu.Unlock()
	if err != nil {
		slog.Warn("playtime cover was not saved", "app", id, "err", err)
	}
	if wrote && done != nil {
		done()
	}
}

func (c *covers) bytes(appID string) []byte {
	if c == nil || !validAppID(appID) {
		return nil
	}
	data, err := os.ReadFile(c.path(appID))
	if err != nil || !isImage(data) {
		return nil
	}
	return data
}

func (c *covers) path(appID string) string {
	return filepath.Join(c.dir, appID+".jpg")
}

func (c *covers) titlePath(appID string) string {
	return filepath.Join(c.dir, appID+".title")
}

// widePath remembers that the store had no portrait, so a saved header is not
// fetched again on every open.
func (c *covers) widePath(appID string) string {
	return filepath.Join(c.dir, appID+".wide")
}

func (c *covers) fresh(appID string) bool {
	if !fileExists(c.path(appID)) || !fileExists(c.titlePath(appID)) {
		return false
	}
	data, err := os.ReadFile(c.path(appID))
	if err != nil || !isImage(data) {
		return false
	}
	if !widerThanTall(data) {
		return true
	}
	return fileExists(c.widePath(appID))
}

func (c *covers) writeWide(appID string) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.widePath(appID), nil, 0o600)
}

func (c *covers) writeTitle(appID, name string) error {
	name = cleanStoreName(name)
	if name == "" || !validAppID(appID) {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.titlePath(appID), []byte(name), 0o600)
}

func (c *covers) title(appID string) string {
	if c == nil || !validAppID(appID) {
		return ""
	}
	data, err := os.ReadFile(c.titlePath(appID))
	if err != nil {
		return ""
	}
	return cleanStoreName(string(data))
}

func (c *covers) write(appID string, data []byte) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.path(appID), data, 0o600)
}

func fetchSteamCover(ctx context.Context, client *httpc.Client, appID string) ([]byte, string, error) {
	if client == nil {
		return nil, "", fmt.Errorf("playtime: no cover client")
	}
	details, err := client.GetBytes(ctx, "https://store.steampowered.com/api/appdetails?appids="+appID+"&l=schinese&filters=basic", nil)
	if err != nil {
		return nil, "", err
	}
	name, header, listErr := storeListing(appID, details.Payload)
	candidates := portraitURLs(appID, header)
	if items, itemsErr := client.GetBytes(ctx, libraryItemsURL(appID), nil); itemsErr == nil {
		if capsule, capErr := libraryCapsuleURL(items.Payload); capErr == nil {
			candidates = uniqueURLs(append([]string{capsule}, candidates...))
		}
	}
	if len(candidates) == 0 {
		if listErr == nil {
			listErr = fmt.Errorf("playtime: store has no cover for %s", appID)
		}
		return nil, name, listErr
	}
	image, err := fetchFirstImage(ctx, client, candidates)
	if err != nil {
		return nil, name, err
	}
	return image, name, nil
}

// libraryItemsURL asks Steam which portrait file this game actually has.
// Newer games keep it behind a hash, so a fixed library_600x900.jpg 404s.
func libraryItemsURL(appID string) string {
	query := `{"ids":[{"appid":` + appID + `}],"context":{"language":"schinese","country_code":"CN"},"data_request":{"include_assets":true}}`
	return "https://api.steampowered.com/IStoreBrowseService/GetItems/v1/?input_json=" + url.QueryEscape(query)
}

// libraryCapsuleURL builds the 2:3 library image named by a GetItems response.
func libraryCapsuleURL(payload []byte) (string, error) {
	var body struct {
		Response struct {
			StoreItems []struct {
				Assets struct {
					Format  string `json:"asset_url_format"`
					Capsule string `json:"library_capsule"`
				} `json:"assets"`
			} `json:"store_items"`
		} `json:"response"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return "", fmt.Errorf("playtime: library assets: %w", err)
	}
	if len(body.Response.StoreItems) == 0 {
		return "", fmt.Errorf("playtime: store has no library capsule")
	}
	assets := body.Response.StoreItems[0].Assets
	if assets.Format == "" || assets.Capsule == "" || !strings.Contains(assets.Format, "${FILENAME}") {
		return "", fmt.Errorf("playtime: store has no library capsule")
	}
	raw := "https://shared.akamai.steamstatic.com/store_item_assets/" + strings.Replace(assets.Format, "${FILENAME}", assets.Capsule, 1)
	if !allowedCoverURL(raw) {
		return "", fmt.Errorf("playtime: library capsule is not a steam image")
	}
	return raw, nil
}

// portraitURLs tries a 2:3 library image before the landscape header.
// The header is only the fallback: a portrait frame crops its sides.
func portraitURLs(appID, header string) []string {
	var urls []string
	if dir := steamAssetDir(header); dir != "" {
		urls = append(urls,
			dir+"/library_600x900_schinese.jpg",
			dir+"/library_600x900.jpg",
		)
	}
	urls = append(urls,
		"https://cdn.akamai.steamstatic.com/steam/apps/"+appID+"/library_600x900.jpg",
		"https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/"+appID+"/library_600x900.jpg",
	)
	if allowedCoverURL(header) {
		urls = append(urls, header)
	}
	return uniqueURLs(urls)
}

func steamAssetDir(header string) string {
	if !allowedCoverURL(header) {
		return ""
	}
	parsed, err := url.Parse(header)
	if err != nil {
		return ""
	}
	dir := path.Dir(parsed.Path)
	if !strings.Contains(dir, "/apps/") {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = dir
	return parsed.String()
}

func uniqueURLs(urls []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(urls))
	for _, item := range urls {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// storeListing reads the simplified-Chinese name and the header image.
// Newer games do not have a picture at the old fixed address; the store
// response names the one that exists. The name may be empty.
func storeListing(appID string, payload []byte) (name, header string, err error) {
	var body map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Name        string `json:"name"`
			HeaderImage string `json:"header_image"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return "", "", fmt.Errorf("playtime: store details for %s: %w", appID, err)
	}
	entry := body[appID]
	if !entry.Success || entry.Data.HeaderImage == "" {
		return cleanStoreName(entry.Data.Name), "", fmt.Errorf("playtime: store has no cover for %s", appID)
	}
	return cleanStoreName(entry.Data.Name), entry.Data.HeaderImage, nil
}

func cleanStoreName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.IndexAny(name, "\r\n"); i >= 0 {
		name = strings.TrimSpace(name[:i])
	}
	runes := []rune(name)
	if len(runes) > 80 {
		name = string(runes[:80])
	}
	return name
}

func allowedCoverURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	host := parsed.Hostname()
	return host == "steamstatic.com" || host == "steampowered.com" ||
		strings.HasSuffix(host, ".steamstatic.com") ||
		strings.HasSuffix(host, ".steampowered.com") ||
		strings.HasSuffix(host, ".akamaihd.net")
}

func fetchFirstImage(ctx context.Context, client *httpc.Client, urls []string) ([]byte, error) {
	if client == nil {
		return nil, fmt.Errorf("playtime: no cover client")
	}
	var last error
	for _, endpoint := range urls {
		result, err := client.GetBytes(ctx, endpoint, nil)
		if err != nil || !isImage(result.Payload) {
			if err == nil {
				err = fmt.Errorf("playtime: %s is not an image", endpoint)
			}
			last = err
			continue
		}
		return result.Payload, nil
	}
	if last == nil {
		last = fmt.Errorf("playtime: no cover")
	}
	return nil, last
}

func validAppID(appID string) bool {
	if appID == "" || len(appID) > 12 {
		return false
	}
	for _, r := range appID {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isImage(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	_, _, err := image.DecodeConfig(bytes.NewReader(data))
	return err == nil
}

func widerThanTall(data []byte) bool {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	return err == nil && cfg.Width > cfg.Height
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
