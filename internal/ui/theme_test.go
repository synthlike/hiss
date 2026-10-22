package ui

import "testing"

func TestIsLight(t *testing.T) {
	cases := []struct {
		reply     string
		light, ok bool
	}{
		{"\x1b]11;rgb:ffff/ffff/ffff\x1b\\\x1b[?62;22c", true, true},
		{"\x1b]11;rgb:fdfd/f6f6/e3e3\a", true, true}, // solarized light
		{"\x1b]11;rgb:1e1e/1e1e/2e2e\x1b\\", false, true},
		{"\x1b]11;rgb:28/2c/34\a", false, true}, // two hex digits per channel
		{"\x1b[?1;2c", false, false},            // only the device attributes: no answer
		{"", false, false},
	}
	for _, c := range cases {
		if light, ok := isLight([]byte(c.reply)); light != c.light || ok != c.ok {
			t.Errorf("%q: light=%v ok=%v, want %v %v", c.reply, light, ok, c.light, c.ok)
		}
	}
}

func TestHissThemeOverrides(t *testing.T) {
	t.Setenv("HISS_THEME", "light")
	if !LightBackground() {
		t.Error("HISS_THEME=light")
	}
	t.Setenv("HISS_THEME", "dark")
	if LightBackground() {
		t.Error("HISS_THEME=dark")
	}
}
