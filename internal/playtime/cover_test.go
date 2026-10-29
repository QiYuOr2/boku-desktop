package playtime

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"boku/internal/platform/httpc"
)

func TestCoverSavesAnImageAndDoesNotRetryAFailure(t *testing.T) {
	dir := t.TempDir()
	picture := testJPEG(t)
	var mu sync.Mutex
	calls := map[string]int{}
	c := &covers{dir: dir, busy: map[string]bool{}, failed: map[string]bool{}}
	c.fetch = func(_ context.Context, appID string) ([]byte, string, error) {
		mu.Lock()
		calls[appID]++
		mu.Unlock()
		if appID == "2" {
			return []byte("not an image"), "", nil
		}
		return picture, "影之刃零", nil
	}
	c.pull("4115450", nil)
	c.pull("2", nil)
	if _, err := os.Stat(filepath.Join(dir, "4115450.jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2.jpg")); !os.IsNotExist(err) {
		t.Fatal("a non-image was saved")
	}
	if got := c.bytes("4115450"); !bytes.Equal(got, picture) {
		t.Fatalf("bytes = %d", len(got))
	}
	if c.title("4115450") != "影之刃零" {
		t.Fatalf("title = %q", c.title("4115450"))
	}
	c.Want([]string{"4115450", "2", "../nope"}, nil)
	mu.Lock()
	defer mu.Unlock()
	if calls["4115450"] != 1 || calls["2"] != 1 || calls["../nope"] != 0 {
		t.Fatalf("calls = %v", calls)
	}
}

func TestHeaderImageReadsTheStoreField(t *testing.T) {
	raw := []byte(`{"4115450":{"success":true,"data":{"name":"影之刃零","header_image":"https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/4115450/header.jpg?t=1"}}}`)
	name, got, err := storeListing("4115450", raw)
	if err != nil || name != "影之刃零" || !allowedCoverURL(got) {
		t.Fatalf("listing = %q %q %v", name, got, err)
	}
	if _, _, err := storeListing("4115450", []byte(`{"4115450":{"success":false}}`)); err == nil {
		t.Fatal("a store miss was accepted")
	}
	if _, _, err := storeListing("4115450", []byte(`{`)); err == nil {
		t.Fatal("broken json was accepted")
	}
	if cleanStoreName("  影之刃零\nextra") != "影之刃零" {
		t.Fatal("store name was not trimmed")
	}
	if allowedCoverURL("http://shared.akamai.steamstatic.com/a.jpg") || allowedCoverURL("https://example.test/a.jpg") {
		t.Fatal("a non-steam address was allowed")
	}
}

func TestFetchFirstImageSkipsWhatIsNotAPicture(t *testing.T) {
	picture := testJPEG(t)
	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		if r.URL.Path == "/bad" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("missing"))
			return
		}
		if r.URL.Path == "/html" {
			_, _ = w.Write([]byte("<html></html>"))
			return
		}
		_, _ = w.Write(picture)
	}))
	defer server.Close()
	client := httpc.New(httpc.Options{HTTPClient: server.Client()})
	got, err := fetchFirstImage(context.Background(), client, []string{
		server.URL + "/bad",
		server.URL + "/html",
		server.URL + "/cover",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, picture) {
		t.Fatalf("got %d bytes", len(got))
	}
	if len(asked) != 3 {
		t.Fatalf("asked %v", asked)
	}
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 59, G: 105, B: 61, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
