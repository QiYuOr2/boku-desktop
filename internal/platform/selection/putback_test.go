package selection

import (
	"context"
	"errors"
	"testing"

	"boku/internal/domain"
	"boku/internal/platform/logonce"
)

// recordingClipboard is a device that records what was written and can refuse
// the write, which is the only thing putBack reads.
type recordingClipboard struct {
	written []string
	err     error
}

func (c *recordingClipboard) Read(context.Context) (domain.ClipContent, error) {
	return domain.ClipContent{}, nil
}
func (c *recordingClipboard) WriteText(_ context.Context, text string) error {
	if c.err != nil {
		return c.err
	}
	c.written = append(c.written, "text:"+text)
	return nil
}
func (c *recordingClipboard) WriteImage(context.Context, []byte) error {
	if c.err != nil {
		return c.err
	}
	c.written = append(c.written, "image")
	return nil
}
func (c *recordingClipboard) WriteFiles(_ context.Context, files []string) error {
	if c.err != nil {
		return c.err
	}
	c.written = append(c.written, "files")
	return nil
}

func TestPutBackWritesWhatWasThere(t *testing.T) {
	logonce.Reset()
	device := &recordingClipboard{}
	putBack(context.Background(), device, domain.ClipContent{Kind: domain.ClipKindText, Text: "原来的内容"}, nil)
	if len(device.written) != 1 || device.written[0] != "text:原来的内容" {
		t.Fatalf("written = %v, want the saved text", device.written)
	}
}

func TestPutBackDoesNotFailTheReadingWhenTheWriteFails(t *testing.T) {
	logonce.Reset()
	device := &recordingClipboard{err: errors.New("剪贴板被占用")}
	// The point of the test: this returns normally. The reading it belongs to
	// has already been answered, and an error here has nowhere to go.
	putBack(context.Background(), device, domain.ClipContent{Kind: domain.ClipKindText, Text: "x"}, nil)
}

func TestPutBackWritesNothingWhenNothingCouldBeRead(t *testing.T) {
	logonce.Reset()
	device := &recordingClipboard{}
	putBack(context.Background(), device, domain.ClipContent{}, errors.New("读不到"))
	if len(device.written) != 0 {
		t.Errorf("written = %v, want nothing", device.written)
	}
}

func TestPutBackHandlesEveryKind(t *testing.T) {
	logonce.Reset()
	device := &recordingClipboard{}
	putBack(context.Background(), device, domain.ClipContent{Kind: domain.ClipKindFile, Files: []string{"a.txt"}}, nil)
	putBack(context.Background(), device, domain.ClipContent{Kind: domain.ClipKindImage, Image: []byte("png")}, nil)
	if len(device.written) != 2 {
		t.Fatalf("written = %v, want a file list and an image", device.written)
	}
}
