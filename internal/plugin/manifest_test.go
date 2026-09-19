package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patriceckhart/hrdx/internal/state"
)

const validManifest = `{
  "schema": 1,
  "id": "example.git-tools",
  "version": "1.0.0",
  "name": "Git tools",
  "entrypoint": "bin/plugin",
  "args": ["--stdio"],
  "protocol": {"min": 1, "max": 1},
  "requests": ["ui.action.contribute", "ui.notification"],
  "contributes": {
    "actions": [{"id": "example.git-tools.status", "label": "Git status", "targets": ["workspace", "pane"]}]
  }
}`

func TestParseManifest(t *testing.T) {
	manifest, err := ParseManifest(strings.NewReader(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "example.git-tools" || manifest.Protocol.Min != 1 || len(manifest.Contributes.Actions) != 1 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if manifest.Args[0] != "--stdio" || manifest.Contributes.Actions[0].Targets[1] != "pane" {
		t.Fatal("arguments or contributions lost")
	}
}

func TestManifestValidation(t *testing.T) {
	tests := []struct {
		name, old, replacement, code string
	}{
		{"schema", `"schema": 1`, `"schema": 2`, "unsupported_schema"},
		{"missing ID", `"id": "example.git-tools",`, ``, "invalid_manifest"},
		{"namespace", `example.git-tools`, `git-tools`, "invalid_manifest"},
		{"uppercase", `example.git-tools`, `Example.git-tools`, "invalid_manifest"},
		{"version", `1.0.0`, `1.0`, "invalid_manifest"},
		{"version prefix", `1.0.0`, `v1.0.0`, "invalid_manifest"},
		{"protocol zero", `"min": 1`, `"min": 0`, "invalid_manifest"},
		{"protocol order", `"min": 1`, `"min": 2`, "invalid_manifest"},
		{"unsupported protocol", `"min": 1, "max": 1`, `"min": 2, "max": 3`, "unsupported_protocol"},
		{"absolute executable", `bin/plugin`, `/bin/plugin`, "invalid_manifest"},
		{"escape executable", `bin/plugin`, `../plugin`, "invalid_manifest"},
		{"windows absolute", `bin/plugin`, `C:/plugin.exe`, "invalid_manifest"},
		{"windows separator", `bin/plugin`, `bin\\plugin`, "invalid_manifest"},
		{"empty executable", `bin/plugin`, ``, "invalid_manifest"},
		{"name control", `Git tools`, `Git\u001btools`, "invalid_manifest"},
		{"label control", `Git status`, `Git\nstatus`, "invalid_manifest"},
		{"argument nul", `--stdio`, `--stdio\u0000`, "invalid_manifest"},
		{"action namespace", `example.git-tools.status`, `other.status`, "invalid_manifest"},
		{"target", `"workspace", "pane"`, `"screen"`, "invalid_manifest"},
		{"duplicate target", `"workspace", "pane"`, `"pane", "pane"`, "invalid_manifest"},
		{"missing action grant request", `"ui.action.contribute", `, ``, "invalid_manifest"},
		{"duplicate request", `"ui.notification"`, `"ui.action.contribute"`, "invalid_manifest"},
		{"malformed capability", `ui.notification`, `ui.*`, "invalid_manifest"},
		{"duplicate key", `"schema": 1`, `"schema": 1, "schema": 1`, "invalid_json"},
		{"case alias duplicate", `"schema": 1`, `"schema": 1, "Schema": 1`, "invalid_json"},
		{"unicode alias duplicate", `"schema": 1`, `"schema": 1, "ſchema": 1`, "invalid_json"},
		{"reserved filename", `bin/plugin`, `bin/CON.exe`, "invalid_manifest"},
		{"reserved port", `bin/plugin`, `bin/COM¹.exe`, "invalid_manifest"},
		{"reserved name with space", `bin/plugin`, `bin/NUL .exe`, "invalid_manifest"},
		{"trailing dot", `bin/plugin`, `bin/plugin.`, "invalid_manifest"},
		{"nested duplicate", `"min": 1`, `"min": 1, "min": 1`, "invalid_json"},
		{"wrong field type", `"args": ["--stdio"]`, `"args": "--stdio"`, "invalid_json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseManifest(strings.NewReader(strings.Replace(validManifest, tt.old, tt.replacement, 1)))
			assertProblem(t, err, tt.code)
		})
	}
}

func TestManifestJSONLimits(t *testing.T) {
	for _, input := range []string{"null", "[]", "{} {}", validManifest + " false", "{", "\xff"} {
		_, err := ParseManifest(strings.NewReader(input))
		assertProblem(t, err, "invalid_json")
	}
	_, err := ParseManifest(strings.NewReader(strings.Repeat(" ", MaxManifestBytes+1)))
	assertProblem(t, err, "manifest_too_large")
	_, err = ParseManifest(strings.NewReader(`{"unknown":` + strings.Repeat("[", 40) + `0` + strings.Repeat("]", 40) + `}`))
	assertProblem(t, err, "invalid_json")
	_, err = ParseManifest(strings.NewReader(strings.Replace(validManifest, `Git tools`, strings.Repeat("x", 81), 1)))
	assertProblem(t, err, "invalid_manifest")
}

func TestManifestOptionalFieldsAndProtocolRange(t *testing.T) {
	input := strings.Replace(validManifest, `"schema": 1`, `"schema": 1, "future_optional": {"value": true}`, 1)
	input = strings.Replace(input, `"max": 1`, `"max": 3`, 1)
	if _, err := ParseManifest(strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	input = strings.Replace(input, `"ui.notification"`, `"future.optional"`, 1)
	if _, err := ParseManifest(strings.NewReader(input)); err != nil {
		t.Fatalf("capability requests are declarations, not grants: %v", err)
	}
}

func TestManifestFixtures(t *testing.T) {
	for _, name := range []string{"valid", "invalid-id", "invalid-protocol", "invalid-entrypoint"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			_, err = ParseManifest(strings.NewReader(string(data)))
			if (err == nil) != (name == "valid") {
				t.Fatalf("unexpected validation result: %v", err)
			}
		})
	}
}

func TestManifestIDsArePortableFileNames(t *testing.T) {
	for _, id := range []string{"con.tools", "aux.tools", "nul.tools", "com1.tools", "lpt1.tools", "com.example"} {
		manifest := Manifest{Schema: 1, ID: id, Version: "1.0.0", Entrypoint: "peer.exe", Protocol: ProtocolRange{Min: 1, Max: 1}}
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ParseManifest(strings.NewReader(string(data)))
		if (err == nil) != (id == "com.example") {
			t.Fatalf("portable ID %q: %v", id, err)
		}
	}
}

func TestManifestCollectionLimits(t *testing.T) {
	manifest, err := ParseManifest(strings.NewReader(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	check := func(m Manifest, wantValid bool) {
		t.Helper()
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ParseManifest(strings.NewReader(string(data)))
		if (err == nil) != wantValid {
			t.Fatalf("valid=%v, error=%v", wantValid, err)
		}
	}
	manifest.Args = make([]string, MaxArgs)
	check(manifest, true)
	manifest.Args = append(manifest.Args, "one too many")
	check(manifest, false)
	manifest.Args = []string{strings.Repeat("x", 4096)}
	check(manifest, true)
	manifest.Args[0] += "x"
	check(manifest, false)
	manifest.Args = nil
	manifest.Requests = []string{"ui.action.contribute"}
	for index := 1; index < MaxRequests; index++ {
		manifest.Requests = append(manifest.Requests, fmt.Sprintf("example.capability%d", index))
	}
	check(manifest, true)
	manifest.Requests = append(manifest.Requests, "example.excess")
	check(manifest, false)
	manifest.Requests = []string{"ui.action.contribute"}
	manifest.Contributes.Actions = nil
	for index := 0; index < MaxActions; index++ {
		manifest.Contributes.Actions = append(manifest.Contributes.Actions, Action{
			ID: fmt.Sprintf("%s.action%d", manifest.ID, index), Label: "Action", Targets: []string{"pane"},
		})
	}
	check(manifest, true)
	manifest.Contributes.Actions = append(manifest.Contributes.Actions, Action{ID: manifest.ID + ".excess", Label: "Extra", Targets: []string{"pane"}})
	check(manifest, false)
	manifest.Contributes.Actions = manifest.Contributes.Actions[:2]
	manifest.Contributes.Actions[1].ID = manifest.Contributes.Actions[0].ID
	check(manifest, false)
}

func FuzzParseManifest(f *testing.F) {
	f.Add(validManifest)
	f.Add(`{"schema":1,"Schema":2}`)
	f.Add(`null`)
	f.Fuzz(func(t *testing.T, input string) {
		manifest, err := ParseManifest(strings.NewReader(input))
		if err != nil {
			var failure *Problem
			if !errors.As(err, &failure) {
				t.Fatalf("unexpected error type: %T", err)
			}
			return
		}
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		// JSON escaping can expand otherwise valid text past the input limit.
		if len(data) <= MaxManifestBytes {
			if _, err := ParseManifest(strings.NewReader(string(data))); err != nil {
				t.Fatalf("validated manifest does not round-trip: %v", err)
			}
		}
	})
}

func assertProblem(t *testing.T, err error, code string) {
	t.Helper()
	var problem *Problem
	if !errors.As(err, &problem) || problem.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}

func TestManifestProvidersActivationAndConfig(t *testing.T) {
	base := strings.Replace(validManifest, `"requests": ["ui.action.contribute", "ui.notification"],`, `"requests": ["ui.action.contribute", "ui.notification", "ui.provider.contribute"],
  "activation": {"markers": [".git", "package.json"]},
  "config": [{"key": "show_clean", "type": "bool", "default": false, "label": "Show clean status"}, {"key": "limit", "type": "int", "default": 20}, {"key": "prefix", "type": "string"}],`, 1)
	base = strings.Replace(base, `"actions": [`, `"providers": [{"id": "example.git-tools.search", "kind": "search", "label": "Git search"}],
    "actions": [`, 1)
	manifest, err := ParseManifest(strings.NewReader(base))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Contributes.Providers) != 1 || len(manifest.Activation.Markers) != 2 || len(manifest.Config) != 3 {
		t.Fatalf("manifest extensions lost: %+v", manifest)
	}
	for _, tt := range []struct{ name, old, replacement string }{
		{"provider namespace", `example.git-tools.search`, `other.search`},
		{"provider kind", `"kind": "search"`, `"kind": "diagnostics"`},
		{"provider without request", `, "ui.provider.contribute"`, ``},
		{"marker traversal", `".git"`, `"../.git"`},
		{"marker nested", `".git"`, `"a/b"`},
		{"marker duplicate", `"package.json"`, `".git"`},
		{"config type", `"type": "int"`, `"type": "float"`},
		{"config default mismatch", `"default": false`, `"default": "no"`},
		{"config key", `"key": "prefix"`, `"key": "pre fix"`},
		{"config duplicate", `"key": "prefix"`, `"key": "limit"`},
		{"config int fraction", `"default": 20`, `"default": 20.5`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseManifest(strings.NewReader(strings.Replace(base, tt.old, tt.replacement, 1)))
			assertProblem(t, err, "invalid_manifest")
		})
	}
	values := EffectiveConfig(&manifest, state.PluginApproval{Config: map[string]any{"limit": float64(5), "prefix": "x", "show_clean": "bad"}})
	if values["limit"] != float64(5) || values["prefix"] != "x" || values["show_clean"] != false {
		t.Fatalf("effective config: %+v", values)
	}
	root := t.TempDir()
	if ActivatesFor(&manifest, root) {
		t.Fatal("activated without markers present")
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !ActivatesFor(&manifest, root) {
		t.Fatal("marker directory not detected")
	}
	if !ActivatesFor(&Manifest{}, root) {
		t.Fatal("plugin without markers must apply everywhere")
	}
}

func TestApprovalConfigMustMatchManifest(t *testing.T) {
	path := packageFixture(t, t.TempDir(), "package", "example.test")
	manifest := strings.ReplaceAll(strings.ReplaceAll(validManifest, "example.git-tools", "example.test"), "bin/plugin", "bin/plugin.exe")
	manifest = strings.Replace(manifest, `"requests"`, `"config": [{"key": "limit", "type": "int"}], "requests"`, 1)
	writeFixture(t, filepath.Join(path, ManifestFile), []byte(manifest), 0o600)
	entry := InspectPackage(path)
	if !entry.Valid {
		t.Fatalf("fixture invalid: %+v", entry.Diagnostics)
	}
	if _, err := Approve(entry, nil, nil, true, map[string]any{"limit": "ten"}); err == nil {
		t.Fatal("mistyped configuration approved")
	}
	if _, err := Approve(entry, nil, nil, true, map[string]any{"unknown": true}); err == nil {
		t.Fatal("undeclared configuration approved")
	}
	approval, err := Approve(entry, nil, nil, true, map[string]any{"limit": float64(3)})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyApproval(entry, approval); err != nil {
		t.Fatal(err)
	}
	approval.Config["limit"] = "drifted"
	if err := VerifyApproval(entry, approval); err == nil {
		t.Fatal("drifted configuration verified")
	}
}
