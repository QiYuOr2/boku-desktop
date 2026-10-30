package flash

import (
	"context"
	"encoding/json"
	"testing"

	"boku/internal/domain"
)

func TestSectionOmitsMissingKey(t *testing.T) {
	section := NewSection()
	if err := section.Decode(map[string]json.RawMessage{}); err != nil {
		t.Fatal(err)
	}
	if section.Current().AcceleratorOr() != "" {
		t.Fatalf("accelerator = %q", section.Current().AcceleratorOr())
	}
	encoded, err := section.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if encoded[keyAccelerator] != "" {
		t.Fatalf("encoded = %+v", encoded)
	}
}

func TestSetAccelerator(t *testing.T) {
	section := NewSection()
	service, err := Open("flashes.json", section, memSettings{}, domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetAccelerator(context.Background(), "Ctrl+Shift+N"); err != nil {
		t.Fatal(err)
	}
	if service.Accelerator() != "Ctrl+Shift+N" {
		t.Fatalf("accelerator = %q", service.Accelerator())
	}
	if err := service.SetAccelerator(context.Background(), "N"); err == nil {
		t.Fatal("bare key was stored")
	}
	if err := service.SetAccelerator(context.Background(), "  "); err != nil {
		t.Fatal(err)
	}
	if section.Current().Accelerator != "" || service.Accelerator() != "" {
		t.Fatalf("cleared = %q / %q", section.Current().Accelerator, service.Accelerator())
	}
	if !SameAccelerator("cmdorctrl+shift+n", "CmdOrCtrl+Shift+N") {
		t.Fatal("same shortcut compared different")
	}
}

type memSettings struct{}

func (memSettings) Update(_ context.Context, name string, change func() error) error {
	if name != sectionName {
		return errName
	}
	return change()
}

var errName = errString("section")

type errString string

func (e errString) Error() string { return string(e) }
