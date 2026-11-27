package kit

import "testing"

// 凭据只以最后四个字符示人，故掩码是密钥通往窗口时唯一的形态。
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
