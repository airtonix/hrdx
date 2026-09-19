// Package plugin validates and inventories experimental plugin packages.
// Discovery never executes a package or grants permission to run it.
package plugin

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/mod/semver"
)

const (
	ManifestFile     = "plugin.json"
	SchemaVersion    = 1
	ProtocolVersion  = 1 // Reserved for the experimental stdio protocol.
	MaxManifestBytes = 64 << 10
	MaxActions       = 32
	MaxRequests      = 64
	MaxArgs          = 64
	MaxProviders     = 8
	MaxMarkers       = 16
	MaxConfig        = 32
)

// Problem is safe to display without echoing manifest contents or OS errors.
// Paths are reported separately by discovery so clients can escape them.
type Problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (p *Problem) Error() string { return p.Message }

func problem(code, message string) *Problem {
	return &Problem{Code: code, Message: message}
}

type ProtocolRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type Action struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Targets []string `json:"targets"` // workspace, tab, pane
}

// Provider is a pull-based query source, for example search or diagnostics.
// Results are typed rows rendered by the host finder, never raw UI.
type Provider struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // search
	Label string `json:"label"`
}

type Contributions struct {
	Actions   []Action   `json:"actions,omitempty"`
	Views     []string   `json:"views,omitempty"`
	Providers []Provider `json:"providers,omitempty"`
}

// Activation declares when a plugin becomes relevant to a workspace without
// executing probes. Markers are workspace-relative file or directory names
// checked with a single stat each, never a recursive scan.
type Activation struct {
	Markers []string `json:"markers,omitempty"`
}

// ConfigOption is the deliberately small configuration schema: flat keys
// with primitive types and defaults. Values live in the host approval record
// and are passed to the peer in hello_ack.
type ConfigOption struct {
	Key     string `json:"key"`
	Type    string `json:"type"` // bool, string, int
	Default any    `json:"default,omitempty"`
	Label   string `json:"label,omitempty"`
}

// Manifest is provisional. Capability requests are declarations, not grants.
// Unknown optional fields are ignored. Required extensions need a new schema.
type Manifest struct {
	Schema      int            `json:"schema"`
	ID          string         `json:"id"`
	Version     string         `json:"version"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Entrypoint  string         `json:"entrypoint"`
	Args        []string       `json:"args,omitempty"`
	Protocol    ProtocolRange  `json:"protocol"`
	Requests    []string       `json:"requests,omitempty"`
	Contributes Contributions  `json:"contributes,omitempty"`
	Activation  Activation     `json:"activation,omitempty"`
	Config      []ConfigOption `json:"config,omitempty"`
}

var (
	identifier  = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z][a-z0-9_-]*)+$`)
	fullVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+($|[-+])`)
)

// ParseManifest consumes at most MaxManifestBytes+1 bytes and rejects ambiguous
// duplicate keys, excessive nesting, and multiple JSON values.
func ParseManifest(reader io.Reader) (Manifest, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxManifestBytes+1))
	if err != nil {
		return Manifest{}, problem("manifest_unreadable", "cannot read manifest")
	}
	if len(data) > MaxManifestBytes {
		return Manifest{}, problem("manifest_too_large", "manifest exceeds 64 KiB")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' || !utf8.Valid(data) || !uniqueJSON(data) {
		return Manifest{}, problem("invalid_json", "manifest must be one UTF-8 JSON object with unique keys and nesting at most 32")
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, problem("invalid_json", "manifest fields have invalid JSON types")
	}
	if err := manifest.validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) validate() error {
	invalid := func(message string) error { return problem("invalid_manifest", message) }
	if m.Schema != SchemaVersion {
		return problem("unsupported_schema", "supported manifest schema is 1")
	}
	if !validID(m.ID) || !validEntrypoint(m.ID) {
		return invalid("id must be a portable lowercase dotted namespace of at most 128 bytes")
	}
	if len(m.Version) > 128 || !fullVersion.MatchString(m.Version) || !semver.IsValid("v"+m.Version) {
		return invalid("version must be a full semantic version without a v prefix")
	}
	if !plainText(m.Name, 80, false) || !plainText(m.Description, 512, false) {
		return invalid("name and description must be bounded single-line text without controls")
	}
	if m.Protocol.Min < 1 || m.Protocol.Max < m.Protocol.Min {
		return invalid("protocol requires positive min and max with min <= max")
	}
	if m.Protocol.Min > ProtocolVersion || m.Protocol.Max < ProtocolVersion {
		return problem("unsupported_protocol", "protocol range must include experimental protocol 1")
	}
	if !validEntrypoint(m.Entrypoint) {
		return invalid("entrypoint must be a package-relative slash-separated path without traversal or Windows special names")
	}
	if len(m.Args) > MaxArgs {
		return invalid("at most 64 arguments may be declared")
	}
	for _, arg := range m.Args {
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return invalid("arguments must be at most 4096 bytes each without NUL")
		}
	}
	if len(m.Requests) > MaxRequests {
		return invalid("at most 64 capability requests may be declared")
	}
	requests := make(map[string]bool)
	for _, request := range m.Requests {
		if !validID(request) || requests[request] {
			return invalid("capability requests must be unique lowercase dotted names")
		}
		requests[request] = true
	}
	if len(m.Contributes.Actions) > MaxActions {
		return invalid("at most 32 actions may be declared")
	}
	if len(m.Contributes.Actions) > 0 && !requests["ui.action.contribute"] {
		return invalid("action contributions require a ui.action.contribute request")
	}
	if len(m.Contributes.Views) > 8 || (len(m.Contributes.Views) > 0 && !requests["ui.view.contribute"]) {
		return invalid("at most eight views may be declared and ui.view.contribute must be requested")
	}
	views := make(map[string]bool)
	for _, id := range m.Contributes.Views {
		if !validID(id) || !strings.HasPrefix(id, m.ID+".") || views[id] {
			return invalid("view IDs must be unique within the plugin namespace")
		}
		views[id] = true
	}
	if len(m.Contributes.Providers) > MaxProviders || (len(m.Contributes.Providers) > 0 && !requests["ui.provider.contribute"]) {
		return invalid("at most eight providers may be declared and ui.provider.contribute must be requested")
	}
	providers := make(map[string]bool)
	for _, provider := range m.Contributes.Providers {
		if !validID(provider.ID) || !strings.HasPrefix(provider.ID, m.ID+".") || providers[provider.ID] {
			return invalid("provider IDs must be unique within the plugin namespace")
		}
		providers[provider.ID] = true
		if provider.Kind != "search" {
			return invalid("provider kind must be search")
		}
		if !plainText(provider.Label, 80, true) {
			return invalid("provider labels must contain 1 to 80 characters without controls")
		}
	}
	if len(m.Activation.Markers) > MaxMarkers {
		return invalid("at most 16 activation markers may be declared")
	}
	markers := make(map[string]bool)
	for _, marker := range m.Activation.Markers {
		if !validEntrypoint(marker) || strings.Contains(marker, "/") || markers[marker] {
			return invalid("activation markers must be unique single-component workspace-relative names")
		}
		markers[marker] = true
	}
	if len(m.Config) > MaxConfig {
		return invalid("at most 32 configuration options may be declared")
	}
	keys := make(map[string]bool)
	for _, option := range m.Config {
		if !storageKey.MatchString(option.Key) || keys[option.Key] || !plainText(option.Label, 80, false) {
			return invalid("configuration keys must be unique alphanumeric names with bounded labels")
		}
		keys[option.Key] = true
		if !validConfigValue(option.Type, option.Default) {
			return invalid("configuration options need a bool, string, or int type with a matching default")
		}
	}
	actions := make(map[string]bool)
	for _, action := range m.Contributes.Actions {
		if !validID(action.ID) || !strings.HasPrefix(action.ID, m.ID+".") || actions[action.ID] {
			return invalid("action IDs must be unique and start with the plugin ID followed by a dot")
		}
		actions[action.ID] = true
		if !plainText(action.Label, 80, true) {
			return invalid("action labels must contain 1 to 80 characters without controls")
		}
		if len(action.Targets) == 0 || len(action.Targets) > 3 {
			return invalid("actions require between one and three targets")
		}
		targets := make(map[string]bool)
		for _, target := range action.Targets {
			if (target != "workspace" && target != "tab" && target != "pane") || targets[target] {
				return invalid("action targets must be unique workspace, tab, or pane values")
			}
			targets[target] = true
		}
	}
	return nil
}

// validConfigValue accepts nil (zero value) or a JSON value matching the type.
func validConfigValue(kind string, value any) bool {
	switch kind {
	case "bool":
		_, ok := value.(bool)
		return value == nil || ok
	case "string":
		text, ok := value.(string)
		return value == nil || (ok && plainText(text, 512, false))
	case "int":
		number, ok := value.(float64)
		return value == nil || (ok && number == float64(int64(number)) && number >= -1e9 && number <= 1e9)
	}
	return false
}

// ValidConfigValue reports whether a stored value satisfies an option type.
func ValidConfigValue(kind string, value any) bool { return validConfigValue(kind, value) }

// ValidIdentifier checks the shared plugin and contribution ID vocabulary.
func ValidIdentifier(value string) bool { return validID(value) }

// PlainText accepts blank or padded view rows but no terminal controls.
func PlainText(value string, limit int) bool { return plainText(value, limit, false) }

func validID(value string) bool {
	return len(value) <= 128 && identifier.MatchString(value)
}

func plainText(value string, limit int, required bool) bool {
	return (!required || strings.TrimSpace(value) != "") && utf8.RuneCountInString(value) <= limit &&
		strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) }) < 0
}

// Use a portable path spelling on every host so the same manifest cannot gain
// a different meaning when copied to Windows. No PATH or shell resolution.
func validEntrypoint(value string) bool {
	if value == "." || len(value) > 1024 || !fs.ValidPath(value) || strings.ContainsAny(value, `\:<>"|?*`) || !plainText(value, 1024, true) {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return false
		}
		base := strings.TrimRight(strings.ToUpper(strings.SplitN(component, ".", 2)[0]), " ")
		switch base {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
			suffix := base[3:]
			if utf8.RuneCountInString(suffix) == 1 && strings.ContainsAny(suffix, "0123456789¹²³") {
				return false
			}
		}
	}
	return true
}

// Match encoding/json's Unicode case folding, including long s and Kelvin K.
func foldJSONKey(key string) string {
	return strings.Map(func(r rune) rune {
		minimum := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < minimum {
				minimum = next
			}
		}
		return minimum
	}, key)
}

// encoding/json matches struct fields without case sensitivity. Fold keys here
// too so aliases cannot override a previously validated field.
func uniqueJSON(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 32 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delim, container := token.(json.Delim)
		if !container {
			return true
		}
		keys := make(map[string]bool)
		for decoder.More() {
			if delim == '{' {
				key, err := decoder.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				name = foldJSONKey(name)
				if !ok || keys[name] {
					return false
				}
				keys[name] = true
			}
			if !value(depth + 1) {
				return false
			}
		}
		_, err = decoder.Token()
		return err == nil
	}
	if !value(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}
