package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// hrdx themes are JSON files that override any subset of the built-in
// colors. Nothing is required: a theme can change one color or all of
// them; missing values inherit the default theme. Files live in
// <statedir>/themes/*.json and are chosen in the settings window.

// themeColors is the JSON color table. Values are ANSI 256 numbers or
// "#rrggbb" strings; both arrive as json.RawMessage and are normalized.
type themeColors struct {
	Accent json.RawMessage `json:"accent,omitempty"` // frames, highlights, logo
	Alt    json.RawMessage `json:"alt,omitempty"`    // prefix badge, behind-count
	Muted  json.RawMessage `json:"muted,omitempty"`  // secondary text
	Faint  json.RawMessage `json:"faint,omitempty"`  // inactive borders
	Good   json.RawMessage `json:"good,omitempty"`   // running dots, ok badge
	Busy   json.RawMessage `json:"busy,omitempty"`   // busy spinner
	Bad    json.RawMessage `json:"bad,omitempty"`    // errors, exited dots
	BarBg  json.RawMessage `json:"bar_bg,omitempty"` // header/footer background
	BarFg  json.RawMessage `json:"bar_fg,omitempty"` // header/footer text
	Ink    json.RawMessage `json:"ink,omitempty"`    // text on accent backgrounds
}

// themeFile is one theme JSON document.
type themeFile struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Colors      themeColors `json:"colors,omitempty"`
}

// defaultThemeName identifies the built-in theme.
const defaultThemeName = "default"

// themeRegistry holds the discovered user themes.
var themeRegistry struct {
	sync.Mutex
	themes map[string]themeFile
}

// loadThemes reads <dir>/themes/*.json. Returns a problem summary or "".
func loadThemes(dir string) string {
	themeRegistry.Lock()
	defer themeRegistry.Unlock()
	themeRegistry.themes = map[string]themeFile{}
	if dir == "" {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(dir, "themes", "*.json"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	var problems []string
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, filepath.Base(path)+": "+err.Error())
			continue
		}
		var loaded themeFile
		if err := json.Unmarshal(data, &loaded); err != nil {
			problems = append(problems, filepath.Base(path)+": "+err.Error())
			continue
		}
		name := strings.TrimSpace(loaded.Name)
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), ".json")
		}
		if name == defaultThemeName {
			problems = append(problems, filepath.Base(path)+": name 'default' is reserved")
			continue
		}
		loaded.Name = name
		themeRegistry.themes[name] = loaded
	}
	if len(problems) > 0 {
		return "themes: " + strings.Join(problems, ", ")
	}
	return ""
}

// themeNames returns the selectable themes: default first, then user
// themes alphabetically.
func themeNames() []string {
	themeRegistry.Lock()
	defer themeRegistry.Unlock()
	names := []string{defaultThemeName}
	var user []string
	for name := range themeRegistry.themes {
		user = append(user, name)
	}
	sort.Strings(user)
	return append(names, user...)
}

func isThemeName(name string) bool {
	for _, current := range themeNames() {
		if current == name {
			return true
		}
	}
	return false
}

// parseThemeColor converts a raw JSON value (number or "#hex"/"123"
// string) into a lipgloss color. ok is false for absent/invalid values.
func parseThemeColor(raw json.RawMessage) (lipgloss.Color, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var number int
	if json.Unmarshal(raw, &number) == nil {
		if number >= 0 && number <= 255 {
			return lipgloss.Color(fmt.Sprintf("%d", number)), true
		}
		return "", false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		text = strings.TrimSpace(text)
		if text != "" {
			return lipgloss.Color(text), true
		}
	}
	return "", false
}

// defaultColors is the built-in theme, matching hrdx's original look.
func defaultColors() map[string]lipgloss.Color {
	return map[string]lipgloss.Color{
		"accent": "81",  // cyan
		"alt":    "212", // pink
		"muted":  "243",
		"faint":  "238",
		"good":   "78",
		"busy":   "214",
		"bad":    "203",
		"bar_bg": "234",
		"bar_fg": "250",
		"ink":    "232",
	}
}

// applyTheme activates a theme by name: the default palette with the
// theme's overrides on top. Unknown names fall back to the default.
func applyTheme(name string) {
	palette := defaultColors()
	if name != defaultThemeName {
		themeRegistry.Lock()
		loaded, ok := themeRegistry.themes[name]
		themeRegistry.Unlock()
		if ok {
			overrides := map[string]json.RawMessage{
				"accent": loaded.Colors.Accent,
				"alt":    loaded.Colors.Alt,
				"muted":  loaded.Colors.Muted,
				"faint":  loaded.Colors.Faint,
				"good":   loaded.Colors.Good,
				"busy":   loaded.Colors.Busy,
				"bad":    loaded.Colors.Bad,
				"bar_bg": loaded.Colors.BarBg,
				"bar_fg": loaded.Colors.BarFg,
				"ink":    loaded.Colors.Ink,
			}
			for key, raw := range overrides {
				if color, ok := parseThemeColor(raw); ok {
					palette[key] = color
				}
			}
		}
	}

	colorAccent = palette["accent"]
	colorAlt = palette["alt"]
	colorMuted = palette["muted"]
	colorFaint = palette["faint"]
	colorGood = palette["good"]
	colorBusy = palette["busy"]
	colorBad = palette["bad"]
	colorBarBg = palette["bar_bg"]
	colorBarFg = palette["bar_fg"]
	colorInk = palette["ink"]
	rebuildStyles()
}
