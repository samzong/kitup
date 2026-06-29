package kitup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type goldenFile struct {
	Cases []goldenCase `json:"cases"`
}

type goldenCase struct {
	ID        string         `json:"id"`
	Operation string         `json:"operation"`
	Options   map[string]any `json:"options"`
	Given     map[string]any `json:"given"`
	Expected  map[string]any `json:"expected"`
}

func TestGoldenCases(t *testing.T) {
	var file goldenFile
	readJSON(t, "../testdata/cases/bundled-skill-install.json", &file)
	for _, tc := range file.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			workspace := filepath.Join(root, "workspace")
			must(t, os.MkdirAll(home, 0o755))
			must(t, os.MkdirAll(workspace, 0o755))
			setupGiven(t, tc, home, workspace)
			runCase(t, tc, home, workspace)
		})
	}
}

func runCase(t *testing.T, tc goldenCase, home, workspace string) {
	opts := expandValue(tc.Options, home, workspace).(map[string]any)
	switch tc.Operation {
	case "resolve-hosts":
		hostsFile := repoPathFromCase(tc.Given["hostsFile"].(string))
		hosts, err := LoadHostSpec(hostsFile)
		must(t, err)
		resolved, errs := ResolveHosts(agentSelector(opts["agents"]), hosts)
		if expected, ok := tc.Expected["count"]; ok && int(expected.(float64)) != len(resolved) {
			t.Fatalf("count: got %d want %v", len(resolved), expected)
		}
		if expected, ok := tc.Expected["hostIds"]; ok {
			equal(t, hostIDs(resolved), expected)
		}
		if expected, ok := tc.Expected["resolvedHostIds"]; ok {
			equal(t, hostIDs(resolved), expected)
		}
		if expected, ok := tc.Expected["errors"]; ok {
			equal(t, errs, expected)
		}
	case "validate":
		result := ValidateSkill(repoPathFromCase(opts["skillDir"].(string)))
		equal(t, result.Valid, tc.Expected["valid"])
		equal(t, result.ErrorCode, tc.Expected["errorCode"])
	default:
		if expected, ok := tc.Expected["detectedHosts"]; ok {
			hosts, err := DetectHosts(BaseOptions{Home: home, CWD: workspace, HostsFile: repoPathFromCase("spec/hosts.json")}, Scope(opts["scope"].(string)))
			must(t, err)
			equal(t, hostIDs(hosts), expected)
		}
		report := runReportCase(t, tc, opts, home, workspace)
		if expected, ok := tc.Expected["report"]; ok {
			equal(t, report, expandValue(expected, home, workspace))
		}
		assertExpectedWriteCounts(t, tc, report, home, workspace)
		assertExpectedFiles(t, tc, home, workspace)
		assertExpectedMetadata(t, tc, home, workspace)
	}
}

func runReportCase(t *testing.T, tc goldenCase, opts map[string]any, home, workspace string) any {
	base := BaseOptions{Home: home, CWD: workspace, HostsFile: repoPathFromCase("spec/hosts.json")}
	switch tc.Operation {
	case "uninstall":
		report, err := UninstallBundledSkill(UninstallOptions{
			BaseOptions: base,
			AppID:       opts["appId"].(string),
			SkillName:   opts["skillName"].(string),
			Scope:       Scope(opts["scope"].(string)),
			Agents:      agentSelector(opts["agents"]),
		})
		must(t, err)
		return report
	case "install", "update", "plan":
		fn := InstallBundledSkill
		if tc.Operation == "update" {
			fn = UpdateBundledSkill
		}
		if tc.Operation == "plan" {
			fn = PlanBundledSkill
		}
		report, err := fn(InstallOptions{
			BaseOptions: base,
			AppID:       opts["appId"].(string),
			SkillDir:    repoPathFromCase(opts["skillDir"].(string)),
			Scope:       Scope(opts["scope"].(string)),
			Agents:      agentSelector(opts["agents"]),
		})
		must(t, err)
		return report
	default:
		t.Fatalf("unsupported operation: %s", tc.Operation)
		return nil
	}
}

func setupGiven(t *testing.T, tc goldenCase, home, workspace string) {
	for _, dir := range stringSlice(tc.Given["dirs"]) {
		must(t, os.MkdirAll(expandString(dir, home, workspace), 0o755))
	}
	if files, ok := tc.Given["files"].(map[string]any); ok {
		for path, value := range files {
			writeFixtureFile(t, expandString(path, home, workspace), value)
		}
	}
	if target, ok := tc.Given["copySkillDirTo"].(string); ok {
		must(t, os.RemoveAll(expandString(target, home, workspace)))
		must(t, copySkillDir(caseSkillDir(tc), expandString(target, home, workspace)))
	}
	if meta, ok := tc.Given["metadata"].(map[string]any); ok {
		writeMetadataFixture(t, tc, home, workspace, meta)
	}
}

func assertExpectedFiles(t *testing.T, tc goldenCase, home, workspace string) {
	for _, path := range stringSlice(tc.Expected["filesPresent"]) {
		if !exists(expandString(path, home, workspace)) {
			t.Fatalf("expected file to exist: %s", expandString(path, home, workspace))
		}
	}
	for _, path := range stringSlice(tc.Expected["filesAbsent"]) {
		if exists(expandString(path, home, workspace)) {
			t.Fatalf("expected file to be absent: %s", expandString(path, home, workspace))
		}
	}
}

func assertExpectedMetadata(t *testing.T, tc goldenCase, home, workspace string) {
	meta, ok := tc.Expected["metadata"].(map[string]any)
	if !ok {
		return
	}
	path := expandString(meta["path"].(string), home, workspace)
	var actual map[string]any
	readJSON(t, path, &actual)
	for key, value := range meta["fields"].(map[string]any) {
		equal(t, actual[key], value)
	}
	hash := meta["hash"].(string)
	if hash == "from-skill-dir" {
		var err error
		hash, err = ComputeContentHash(repoPathFromCase(tc.Options["skillDir"].(string)))
		must(t, err)
	}
	equal(t, actual["hash"], hash)
}

func assertExpectedWriteCounts(t *testing.T, tc goldenCase, report any, home, workspace string) {
	expected, ok := tc.Expected["writeCountByTargetDir"]
	if !ok {
		return
	}
	actual := map[string]any{}
	reportMap := normalize(report).(map[string]any)
	for _, key := range []string{"installed", "updated"} {
		for _, item := range reportMap[key].([]any) {
			targetDir := item.(map[string]any)["targetDir"].(string)
			if actual[targetDir] == nil {
				actual[targetDir] = float64(0)
			}
			actual[targetDir] = actual[targetDir].(float64) + 1
		}
	}
	equal(t, actual, expandValue(expected, home, workspace))
}

func writeMetadataFixture(t *testing.T, tc goldenCase, home, workspace string, meta map[string]any) {
	hash := meta["hash"].(string)
	if hash == "from-skill-dir" {
		var err error
		hash, err = ComputeContentHash(caseSkillDir(tc))
		must(t, err)
	}
	fields := map[string]any{}
	for key, value := range meta["fields"].(map[string]any) {
		fields[key] = value
	}
	fields["hash"] = hash
	writeFixtureFile(t, expandString(meta["path"].(string), home, workspace), fields)
}

func writeFixtureFile(t *testing.T, path string, value any) {
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	switch value := value.(type) {
	case string:
		must(t, os.WriteFile(path, []byte(value), 0o644))
	default:
		data, err := json.MarshalIndent(value, "", "  ")
		must(t, err)
		must(t, os.WriteFile(path, append(data, '\n'), 0o644))
	}
}

func agentSelector(value any) AgentSelector {
	if value == nil {
		return AutoAgents()
	}
	if text, ok := value.(string); ok {
		if text == "*" {
			return AllAgents()
		}
		return AutoAgents()
	}
	return ExplicitAgents(stringSlice(value)...)
}

func caseSkillDir(tc goldenCase) string {
	if skillDir, ok := tc.Options["skillDir"].(string); ok {
		return repoPathFromCase(skillDir)
	}
	return repoPathFromCase("testdata/skills/" + tc.Options["skillName"].(string))
}

func repoPathFromCase(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join("..", path)
}

func hostIDs(hosts []Host) []string {
	ids := []string{}
	for _, host := range hosts {
		ids = append(ids, host.ID)
	}
	return ids
}

func stringSlice(value any) []string {
	if value == nil {
		return nil
	}
	var out []string
	for _, item := range value.([]any) {
		out = append(out, item.(string))
	}
	return out
}

func expandValue(value any, home, workspace string) any {
	switch value := value.(type) {
	case string:
		return expandString(value, home, workspace)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = expandValue(item, home, workspace)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for key, item := range value {
			out[expandString(key, home, workspace)] = expandValue(item, home, workspace)
		}
		return out
	default:
		return value
	}
}

func expandString(value, home, workspace string) string {
	value = strings.ReplaceAll(value, "$HOME", home)
	return strings.ReplaceAll(value, "$WORKSPACE", workspace)
}

func equal(t *testing.T, got, want any) {
	t.Helper()
	got = normalize(got)
	want = normalize(want)
	if !reflect.DeepEqual(got, want) {
		g, _ := json.MarshalIndent(got, "", "  ")
		w, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("got:\n%s\nwant:\n%s", g, w)
	}
}

func normalize(value any) any {
	data, _ := json.Marshal(value)
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}

func readJSON(t *testing.T, path string, out any) {
	data, err := os.ReadFile(path)
	must(t, err)
	must(t, json.Unmarshal(data, out))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
