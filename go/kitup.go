package kitup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Scope string

const (
	UserScope    Scope = "user"
	ProjectScope Scope = "project"
)

type AgentSelector struct {
	Kind string
	IDs  []string
}

func AutoAgents() AgentSelector { return AgentSelector{Kind: "auto"} }
func AllAgents() AgentSelector  { return AgentSelector{Kind: "*"} }
func ExplicitAgents(ids ...string) AgentSelector {
	return AgentSelector{Kind: "explicit", IDs: ids}
}

type Host struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"displayName"`
	Aliases          []string `json:"aliases,omitempty"`
	ProjectSkillsDir []string `json:"projectSkillsDirs"`
	UserSkillsDir    []string `json:"userSkillsDirs"`
	Detect           []string `json:"detect"`
	Status           string   `json:"status"`
	Notes            []string `json:"notes,omitempty"`
}

type hostSpec struct {
	SchemaVersion int    `json:"schemaVersion"`
	Hosts         []Host `json:"hosts"`
}

type BaseOptions struct {
	Home      string
	CWD       string
	HostsFile string
}

type InstallOptions struct {
	BaseOptions
	AppID    string
	SkillDir string
	Scope    Scope
	Agents   AgentSelector
}

type UninstallOptions struct {
	BaseOptions
	AppID     string
	SkillName string
	Scope     Scope
	Agents    AgentSelector
}

type SkillInfo struct {
	Valid       bool   `json:"valid"`
	SkillName   string `json:"skillName,omitempty"`
	Description string `json:"description,omitempty"`
	ErrorCode   string `json:"errorCode,omitempty"`
}

type TargetGroup struct {
	HostIDs   []string
	SkillName string
	TargetDir string
}

type InstallReport struct {
	Installed []map[string]any `json:"installed"`
	Updated   []map[string]any `json:"updated"`
	Skipped   []map[string]any `json:"skipped"`
	Conflicts []map[string]any `json:"conflicts"`
	Errors    []map[string]any `json:"errors"`
}

type UninstallReport struct {
	Removed   []map[string]any `json:"removed"`
	Skipped   []map[string]any `json:"skipped"`
	Conflicts []map[string]any `json:"conflicts"`
	Errors    []map[string]any `json:"errors"`
}

type metadata struct {
	SchemaVersion int    `json:"schemaVersion"`
	AppID         string `json:"appId"`
	SkillName     string `json:"skillName"`
	Source        string `json:"source"`
	Hash          string `json:"hash"`
}

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func LoadHostSpec(hostsFile string) ([]Host, error) {
	data := []byte(defaultHostsSpecJSON)
	if hostsFile != "" {
		var err error
		data, err = os.ReadFile(hostsFile)
		if err != nil {
			return nil, err
		}
	}
	var spec hostSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, err
	}
	return spec.Hosts, nil
}

func ResolveHosts(agents AgentSelector, hosts []Host) ([]Host, []map[string]any) {
	if agents.Kind == "*" {
		return hosts, []map[string]any{}
	}
	if agents.Kind == "" || agents.Kind == "auto" {
		return []Host{}, []map[string]any{}
	}
	byName := map[string]Host{}
	for _, host := range hosts {
		byName[host.ID] = host
		for _, alias := range host.Aliases {
			byName[alias] = host
		}
	}
	seen := map[string]bool{}
	resolved := []Host{}
	errs := []map[string]any{}
	for _, id := range agents.IDs {
		host, ok := byName[id]
		if !ok {
			errs = append(errs, map[string]any{"agent": id, "reason": "unknown-host"})
			continue
		}
		if !seen[host.ID] {
			seen[host.ID] = true
			resolved = append(resolved, host)
		}
	}
	return resolved, errs
}

func DetectHosts(opts BaseOptions, scope Scope) ([]Host, error) {
	hosts, err := LoadHostSpec(opts.HostsFile)
	if err != nil {
		return nil, err
	}
	home, cwd := defaults(opts)
	detected := []Host{}
	for _, host := range hosts {
		if len(host.Detect) == 0 || isGenericDetectPath(host.Detect[0]) {
			continue
		}
		if exists(expandHostPath(host.Detect[0], home, cwd)) {
			detected = append(detected, host)
		}
	}
	if scope == "" {
		return detected, nil
	}
	sort.Slice(detected, func(i, j int) bool {
		a := canonicalScopePath(detected[i], scope, home, cwd)
		b := canonicalScopePath(detected[j], scope, home, cwd)
		if a == b {
			return detected[i].ID < detected[j].ID
		}
		return a < b
	})
	return detected, nil
}

func ResolveInstallTargets(opts BaseOptions, agents AgentSelector, scope Scope, skillName string) ([]TargetGroup, []map[string]any, []string, error) {
	hosts, err := LoadHostSpec(opts.HostsFile)
	if err != nil {
		return nil, nil, nil, err
	}
	home, cwd := defaults(opts)
	if agents.Kind == "" {
		agents = AutoAgents()
	}
	selected := []Host{}
	errs := []map[string]any{}
	if agents.Kind == "auto" {
		selected, err = DetectHosts(opts, scope)
		if err != nil {
			return nil, nil, nil, err
		}
	} else {
		selected, errs = ResolveHosts(agents, hosts)
	}
	byTarget := map[string]*TargetGroup{}
	for _, host := range selected {
		root := chooseScopePath(host, scope, home, cwd)
		if root == "" {
			errs = append(errs, map[string]any{
				"hostId": host.ID, "skillName": skillName, "scope": string(scope), "reason": "unsupported-scope",
			})
			continue
		}
		targetDir := filepath.Join(root, skillName)
		group := byTarget[targetDir]
		if group == nil {
			group = &TargetGroup{SkillName: skillName, TargetDir: targetDir}
			byTarget[targetDir] = group
		}
		group.HostIDs = append(group.HostIDs, host.ID)
	}
	targets := []TargetGroup{}
	for _, target := range byTarget {
		targets = append(targets, *target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].TargetDir < targets[j].TargetDir })
	detected := []string{}
	for _, target := range targets {
		detected = append(detected, target.HostIDs...)
	}
	return targets, errs, detected, nil
}

func ValidateSkill(skillDir string) SkillInfo {
	content, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return SkillInfo{Valid: false, ErrorCode: "missing-skill-md"}
	}
	text := string(content)
	if !strings.HasPrefix(text, "---\n") {
		return SkillInfo{Valid: false, ErrorCode: "invalid-frontmatter"}
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return SkillInfo{Valid: false, ErrorCode: "invalid-frontmatter"}
	}
	fields := parseFrontmatter(text[4 : 4+end])
	name := fields["name"]
	description := fields["description"]
	if !skillNamePattern.MatchString(name) || len(description) < 1 || len(description) > 1024 {
		return SkillInfo{Valid: false, ErrorCode: "invalid-frontmatter"}
	}
	return SkillInfo{Valid: true, SkillName: name, Description: description}
}

func ComputeContentHash(skillDir string) (string, error) {
	files, err := listSkillFiles(skillDir)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, file := range files {
		bytes, err := os.ReadFile(filepath.Join(skillDir, filepath.FromSlash(file)))
		if err != nil {
			return "", err
		}
		hash.Write([]byte(file))
		hash.Write([]byte{0})
		hash.Write(bytes)
		hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func InstallBundledSkill(opts InstallOptions) (InstallReport, error) {
	return installOrPlan(opts, true)
}

func PlanBundledSkill(opts InstallOptions) (InstallReport, error) {
	return installOrPlan(opts, false)
}

func UpdateBundledSkill(opts InstallOptions) (InstallReport, error) {
	return InstallBundledSkill(opts)
}

func UninstallBundledSkill(opts UninstallOptions) (UninstallReport, error) {
	targets, errs, _, err := ResolveInstallTargets(opts.BaseOptions, opts.Agents, opts.Scope, opts.SkillName)
	if err != nil {
		return UninstallReport{}, err
	}
	if errs == nil {
		errs = []map[string]any{}
	}
	report := UninstallReport{Removed: []map[string]any{}, Skipped: []map[string]any{}, Conflicts: []map[string]any{}, Errors: errs}
	for _, target := range targets {
		result := targetResult(target)
		meta, present, managed := readMetadata(target.TargetDir)
		switch {
		case !present:
			report.Skipped = append(report.Skipped, withReason(result, "missing"))
		case !managed:
			report.Conflicts = append(report.Conflicts, withReason(result, "unmanaged"))
		case meta.AppID != opts.AppID:
			report.Conflicts = append(report.Conflicts, withReason(result, "owner-mismatch"))
		default:
			if err := os.RemoveAll(target.TargetDir); err != nil {
				return report, err
			}
			report.Removed = append(report.Removed, result)
		}
	}
	return report, nil
}

func installOrPlan(opts InstallOptions, write bool) (InstallReport, error) {
	skill := ValidateSkill(opts.SkillDir)
	if !skill.Valid {
		return emptyInstallReport([]map[string]any{{"skillDir": opts.SkillDir, "reason": skill.ErrorCode}}), nil
	}
	hash, err := ComputeContentHash(opts.SkillDir)
	if err != nil {
		return InstallReport{}, err
	}
	targets, errs, _, err := ResolveInstallTargets(opts.BaseOptions, opts.Agents, opts.Scope, skill.SkillName)
	if err != nil {
		return InstallReport{}, err
	}
	report := emptyInstallReport(errs)
	for _, target := range targets {
		result := targetResult(target)
		meta, present, managed := readMetadata(target.TargetDir)
		switch {
		case !present:
			if write {
				if err := copyManagedSkill(opts.SkillDir, target.TargetDir, opts.AppID, skill.SkillName, hash); err != nil {
					return report, err
				}
			}
			report.Installed = append(report.Installed, result)
		case !managed:
			report.Conflicts = append(report.Conflicts, withReason(result, "unmanaged"))
		case meta.AppID != opts.AppID:
			report.Conflicts = append(report.Conflicts, withReason(result, "owner-mismatch"))
		case meta.Hash == hash:
			report.Skipped = append(report.Skipped, withReason(result, "unchanged"))
		default:
			if write {
				if err := replaceManagedSkill(opts.SkillDir, target.TargetDir, opts.AppID, skill.SkillName, hash); err != nil {
					return report, err
				}
			}
			report.Updated = append(report.Updated, result)
		}
	}
	return report, nil
}

func copyManagedSkill(skillDir, targetDir, appID, skillName, hash string) error {
	if err := os.RemoveAll(targetDir); err != nil {
		return err
	}
	if err := copySkillDir(skillDir, targetDir); err != nil {
		return err
	}
	return writeMetadata(targetDir, appID, skillName, hash)
}

func replaceManagedSkill(skillDir, targetDir, appID, skillName, hash string) error {
	suffix := ".kitup-" + time.Now().Format("20060102150405.000000000")
	tmp := targetDir + suffix
	backup := targetDir + suffix + "-backup"
	_ = os.RemoveAll(tmp)
	if err := copySkillDir(skillDir, tmp); err != nil {
		return err
	}
	if err := writeMetadata(tmp, appID, skillName, hash); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(targetDir, backup); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, targetDir); err != nil {
		_ = os.RemoveAll(tmp)
		if !exists(targetDir) && exists(backup) {
			_ = os.Rename(backup, targetDir)
		}
		return err
	}
	return os.RemoveAll(backup)
}

func copySkillDir(src, dest string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		if skipName(entry.Name()) {
			continue
		}
		from := filepath.Join(src, entry.Name())
		to := filepath.Join(dest, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if err := copySkillDir(from, to); err != nil {
				return err
			}
		} else if info.Mode().IsRegular() {
			bytes, err := os.ReadFile(from)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(to, bytes, info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeMetadata(targetDir, appID, skillName, hash string) error {
	data, err := json.MarshalIndent(metadata{SchemaVersion: 1, AppID: appID, SkillName: skillName, Source: "bundled", Hash: hash}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(targetDir, ".kitup.json"), append(data, '\n'), 0o644)
}

func readMetadata(targetDir string) (metadata, bool, bool) {
	if !exists(targetDir) {
		return metadata{}, false, false
	}
	data, err := os.ReadFile(filepath.Join(targetDir, ".kitup.json"))
	if err != nil {
		return metadata{}, true, false
	}
	var meta metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return metadata{}, true, false
	}
	return meta, true, true
}

func targetResult(target TargetGroup) map[string]any {
	result := map[string]any{"skillName": target.SkillName, "targetDir": target.TargetDir}
	if len(target.HostIDs) == 1 {
		result["hostId"] = target.HostIDs[0]
	} else {
		result["hostIds"] = target.HostIDs
	}
	return result
}

func withReason(result map[string]any, reason string) map[string]any {
	result["reason"] = reason
	return result
}

func emptyInstallReport(errs []map[string]any) InstallReport {
	if errs == nil {
		errs = []map[string]any{}
	}
	return InstallReport{
		Installed: []map[string]any{},
		Updated:   []map[string]any{},
		Skipped:   []map[string]any{},
		Conflicts: []map[string]any{},
		Errors:    errs,
	}
}

func canonicalScopePath(host Host, scope Scope, home, cwd string) string {
	paths := scopePaths(host, scope)
	if len(paths) == 0 {
		return ""
	}
	return expandHostPath(paths[0], home, cwd)
}

func chooseScopePath(host Host, scope Scope, home, cwd string) string {
	paths := scopePaths(host, scope)
	for _, path := range paths {
		expanded := expandHostPath(path, home, cwd)
		if exists(expanded) {
			return expanded
		}
	}
	if len(paths) == 0 {
		return ""
	}
	return expandHostPath(paths[0], home, cwd)
}

func scopePaths(host Host, scope Scope) []string {
	if scope == UserScope {
		return host.UserSkillsDir
	}
	return host.ProjectSkillsDir
}

func expandHostPath(path, home, cwd string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return filepath.Join(cwd, path)
}

func defaults(opts BaseOptions) (string, string) {
	home := opts.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	cwd := opts.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return home, cwd
}

func parseFrontmatter(content string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		before, after, ok := strings.Cut(line, ":")
		if ok {
			fields[before] = strings.TrimSpace(after)
		}
	}
	return fields
}

func listSkillFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if skipName(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func skipName(name string) bool {
	return name == ".git" || name == ".kitup.json" || name == ".DS_Store" || strings.HasSuffix(name, ".swp") || strings.HasSuffix(name, "~")
}

func isGenericDetectPath(path string) bool {
	return path == "~/.agents" || path == "~/.agents/skills" || path == "~/.config/agents"
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}
