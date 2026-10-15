package kit

import "testing"

// A credential is only ever shown as its last four characters, so the mask is
// the one shape a secret takes on its way to a window.
func TestMaskSecret(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"sk-abcdef1234", "…1234"},
		{"  sk-abcdef1234  ", "…1234"},
		{"abcd", "••••"},
		{"abc", "•••"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := MaskSecret(c.in); got != c.want {
			t.Errorf("MaskSecret(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := MaskSecret("sk-abcdef1234"); got == "sk-abcdef1234" {
		t.Errorf("MaskSecret leaked the value: %q", got)
	}
}
