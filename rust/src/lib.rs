mod hosts_generated;

use serde::Deserialize;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, HashMap, HashSet};
use std::fs;
use std::io;
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Scope {
    User,
    Project,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AgentSelector {
    Auto,
    All,
    Explicit(Vec<String>),
}

#[derive(Clone, Debug, Default)]
pub struct BaseOptions {
    pub home: Option<PathBuf>,
    pub cwd: Option<PathBuf>,
    pub hosts_file: Option<PathBuf>,
}

#[derive(Clone, Debug)]
pub struct InstallOptions {
    pub base: BaseOptions,
    pub app_id: String,
    pub skill_dir: PathBuf,
    pub scope: Scope,
    pub agents: AgentSelector,
}

#[derive(Clone, Debug)]
pub struct UninstallOptions {
    pub base: BaseOptions,
    pub app_id: String,
    pub skill_name: String,
    pub scope: Scope,
    pub agents: AgentSelector,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Host {
    pub id: String,
    pub display_name: String,
    #[serde(default)]
    pub aliases: Vec<String>,
    pub project_skills_dirs: Vec<String>,
    pub user_skills_dirs: Vec<String>,
    pub detect: Vec<String>,
    pub status: String,
    #[serde(default)]
    pub notes: Vec<String>,
}

#[derive(Deserialize)]
struct HostSpec {
    hosts: Vec<Host>,
}

#[derive(Clone, Debug)]
pub struct SkillInfo {
    pub valid: bool,
    pub skill_name: Option<String>,
    pub description: Option<String>,
    pub error_code: Option<String>,
}

#[derive(Clone, Debug)]
pub struct TargetGroup {
    pub host_ids: Vec<String>,
    pub skill_name: String,
    pub target_dir: PathBuf,
}

#[derive(Deserialize)]
struct Metadata {
    #[serde(rename = "appId")]
    app_id: String,
    hash: String,
}

pub fn load_host_spec(hosts_file: Option<&Path>) -> io::Result<Vec<Host>> {
    let spec: HostSpec = match hosts_file {
        Some(path) => serde_json::from_slice(&fs::read(path)?)?,
        None => serde_json::from_str(hosts_generated::DEFAULT_HOSTS_SPEC_JSON)?,
    };
    Ok(spec.hosts)
}

pub fn resolve_hosts(agents: &AgentSelector, hosts: &[Host]) -> (Vec<Host>, Vec<Value>) {
    match agents {
        AgentSelector::All => (hosts.to_vec(), vec![]),
        AgentSelector::Auto => (vec![], vec![]),
        AgentSelector::Explicit(ids) => {
            let mut by_name = HashMap::new();
            for host in hosts {
                by_name.insert(host.id.as_str(), host);
                for alias in &host.aliases {
                    by_name.insert(alias.as_str(), host);
                }
            }
            let mut seen = HashSet::new();
            let mut resolved = Vec::new();
            let mut errors = Vec::new();
            for id in ids {
                if let Some(host) = by_name.get(id.as_str()) {
                    if seen.insert(host.id.clone()) {
                        resolved.push((*host).clone());
                    }
                } else {
                    errors.push(json!({ "agent": id, "reason": "unknown-host" }));
                }
            }
            (resolved, errors)
        }
    }
}

pub fn detect_hosts(options: &BaseOptions, scope: Option<Scope>) -> io::Result<Vec<Host>> {
    let hosts = load_host_spec(options.hosts_file.as_deref())?;
    let (home, cwd) = defaults(options)?;
    let mut detected = Vec::new();
    for host in hosts {
        if let Some(path) = host.detect.first() {
            if !is_generic_detect_path(path) && expand_host_path(path, &home, &cwd).exists() {
                detected.push(host);
            }
        }
    }
    if let Some(scope) = scope {
        detected.sort_by(|a, b| {
            let left = canonical_scope_path(a, scope, &home, &cwd).unwrap_or_default();
            let right = canonical_scope_path(b, scope, &home, &cwd).unwrap_or_default();
            left.cmp(&right).then_with(|| a.id.cmp(&b.id))
        });
    }
    Ok(detected)
}

pub fn resolve_install_targets(
    options: &BaseOptions,
    agents: &AgentSelector,
    scope: Scope,
    skill_name: &str,
) -> io::Result<(Vec<TargetGroup>, Vec<Value>, Vec<String>)> {
    let hosts = load_host_spec(options.hosts_file.as_deref())?;
    let (home, cwd) = defaults(options)?;
    let (selected, mut errors) = match agents {
        AgentSelector::Auto => (detect_hosts(options, Some(scope))?, vec![]),
        _ => resolve_hosts(agents, &hosts),
    };
    let mut by_target: BTreeMap<PathBuf, TargetGroup> = BTreeMap::new();
    for host in selected {
        if let Some(root) = choose_scope_path(&host, scope, &home, &cwd) {
            let target_dir = root.join(skill_name);
            by_target
                .entry(target_dir.clone())
                .or_insert_with(|| TargetGroup {
                    host_ids: Vec::new(),
                    skill_name: skill_name.to_string(),
                    target_dir,
                })
                .host_ids
                .push(host.id);
        } else {
            errors.push(json!({
                "hostId": host.id,
                "skillName": skill_name,
                "scope": scope_text(scope),
                "reason": "unsupported-scope"
            }));
        }
    }
    let targets: Vec<_> = by_target.into_values().collect();
    let detected_host_ids = targets
        .iter()
        .flat_map(|target| target.host_ids.clone())
        .collect();
    Ok((targets, errors, detected_host_ids))
}

pub fn validate_skill(skill_dir: &Path) -> SkillInfo {
    let content = match fs::read_to_string(skill_dir.join("SKILL.md")) {
        Ok(content) => content,
        Err(_) => {
            return SkillInfo {
                valid: false,
                skill_name: None,
                description: None,
                error_code: Some("missing-skill-md".to_string()),
            }
        }
    };
    let Some(rest) = content.strip_prefix("---\n") else {
        return invalid_frontmatter();
    };
    let Some(end) = rest.find("\n---\n") else {
        return invalid_frontmatter();
    };
    let fields = parse_frontmatter(&rest[..end]);
    let name = fields.get("name").cloned().unwrap_or_default();
    let description = fields.get("description").cloned().unwrap_or_default();
    if !valid_skill_name(&name) || description.is_empty() || description.len() > 1024 {
        return invalid_frontmatter();
    }
    SkillInfo {
        valid: true,
        skill_name: Some(name),
        description: Some(description),
        error_code: None,
    }
}

pub fn compute_content_hash(skill_dir: &Path) -> io::Result<String> {
    let files = list_skill_files(skill_dir)?;
    let mut hash = Sha256::new();
    for file in files {
        let bytes = fs::read(skill_dir.join(PathBuf::from(
            file.replace('/', std::path::MAIN_SEPARATOR_STR),
        )))?;
        hash.update(file.as_bytes());
        hash.update([0]);
        hash.update(bytes);
        hash.update([0]);
    }
    Ok(format!("sha256:{:x}", hash.finalize()))
}

pub fn install_bundled_skill(options: &InstallOptions) -> io::Result<Value> {
    install_or_plan(options, true)
}

pub fn plan_bundled_skill(options: &InstallOptions) -> io::Result<Value> {
    install_or_plan(options, false)
}

pub fn update_bundled_skill(options: &InstallOptions) -> io::Result<Value> {
    install_bundled_skill(options)
}

pub fn uninstall_bundled_skill(options: &UninstallOptions) -> io::Result<Value> {
    let (targets, errors, _) = resolve_install_targets(
        &options.base,
        &options.agents,
        options.scope,
        &options.skill_name,
    )?;
    let mut report = uninstall_report(errors);
    for target in targets {
        let result = target_result(&target);
        match read_metadata(&target.target_dir) {
            MetadataState::Missing => {
                push_report(&mut report, "skipped", with_reason(result, "missing"))
            }
            MetadataState::Unmanaged => {
                push_report(&mut report, "conflicts", with_reason(result, "unmanaged"))
            }
            MetadataState::Managed(meta) if meta.app_id != options.app_id => push_report(
                &mut report,
                "conflicts",
                with_reason(result, "owner-mismatch"),
            ),
            MetadataState::Managed(_) => {
                fs::remove_dir_all(&target.target_dir)?;
                push_report(&mut report, "removed", result);
            }
        }
    }
    Ok(report)
}

fn install_or_plan(options: &InstallOptions, write: bool) -> io::Result<Value> {
    let skill = validate_skill(&options.skill_dir);
    if !skill.valid {
        return Ok(install_report(vec![json!({
            "skillDir": options.skill_dir.to_string_lossy(),
            "reason": skill.error_code
        })]));
    }
    let skill_name = skill.skill_name.unwrap();
    let hash = compute_content_hash(&options.skill_dir)?;
    let (targets, errors, _) =
        resolve_install_targets(&options.base, &options.agents, options.scope, &skill_name)?;
    let mut report = install_report(errors);
    for target in targets {
        let result = target_result(&target);
        match read_metadata(&target.target_dir) {
            MetadataState::Missing => {
                if write {
                    copy_managed_skill(
                        &options.skill_dir,
                        &target.target_dir,
                        &options.app_id,
                        &skill_name,
                        &hash,
                    )?;
                }
                push_report(&mut report, "installed", result);
            }
            MetadataState::Unmanaged => {
                push_report(&mut report, "conflicts", with_reason(result, "unmanaged"))
            }
            MetadataState::Managed(meta) if meta.app_id != options.app_id => push_report(
                &mut report,
                "conflicts",
                with_reason(result, "owner-mismatch"),
            ),
            MetadataState::Managed(meta) if meta.hash == hash => {
                push_report(&mut report, "skipped", with_reason(result, "unchanged"))
            }
            MetadataState::Managed(_) => {
                if write {
                    replace_managed_skill(
                        &options.skill_dir,
                        &target.target_dir,
                        &options.app_id,
                        &skill_name,
                        &hash,
                    )?;
                }
                push_report(&mut report, "updated", result);
            }
        }
    }
    Ok(report)
}

enum MetadataState {
    Missing,
    Unmanaged,
    Managed(Metadata),
}

fn copy_managed_skill(
    skill_dir: &Path,
    target_dir: &Path,
    app_id: &str,
    skill_name: &str,
    hash: &str,
) -> io::Result<()> {
    let _ = fs::remove_dir_all(target_dir);
    copy_skill_dir(skill_dir, target_dir)?;
    write_metadata(target_dir, app_id, skill_name, hash)
}

fn replace_managed_skill(
    skill_dir: &Path,
    target_dir: &Path,
    app_id: &str,
    skill_name: &str,
    hash: &str,
) -> io::Result<()> {
    let suffix = format!(
        ".kitup-{}",
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    );
    let tmp = PathBuf::from(format!("{}{}", target_dir.display(), suffix));
    let backup = PathBuf::from(format!("{}{}-backup", target_dir.display(), suffix));
    let _ = fs::remove_dir_all(&tmp);
    copy_skill_dir(skill_dir, &tmp)?;
    write_metadata(&tmp, app_id, skill_name, hash)?;
    fs::rename(target_dir, &backup)?;
    if let Err(error) = fs::rename(&tmp, target_dir) {
        let _ = fs::remove_dir_all(&tmp);
        if !target_dir.exists() && backup.exists() {
            let _ = fs::rename(&backup, target_dir);
        }
        return Err(error);
    }
    fs::remove_dir_all(backup)
}

fn copy_skill_dir(src: &Path, dest: &Path) -> io::Result<()> {
    fs::create_dir_all(dest)?;
    for entry in fs::read_dir(src)? {
        let entry = entry?;
        let name = entry.file_name();
        let name = name.to_string_lossy();
        if skip_name(&name) {
            continue;
        }
        let from = entry.path();
        let to = dest.join(name.as_ref());
        let metadata = entry.metadata()?;
        if metadata.is_dir() {
            copy_skill_dir(&from, &to)?;
        } else if metadata.is_file() {
            if let Some(parent) = to.parent() {
                fs::create_dir_all(parent)?;
            }
            fs::copy(&from, &to)?;
            fs::set_permissions(&to, metadata.permissions())?;
        }
    }
    Ok(())
}

fn write_metadata(target_dir: &Path, app_id: &str, skill_name: &str, hash: &str) -> io::Result<()> {
    let data = serde_json::to_vec_pretty(&json!({
        "schemaVersion": 1,
        "appId": app_id,
        "skillName": skill_name,
        "source": "bundled",
        "hash": hash
    }))?;
    let mut data = data;
    data.push(b'\n');
    fs::write(target_dir.join(".kitup.json"), data)
}

fn read_metadata(target_dir: &Path) -> MetadataState {
    if !target_dir.exists() {
        return MetadataState::Missing;
    }
    let Ok(data) = fs::read(target_dir.join(".kitup.json")) else {
        return MetadataState::Unmanaged;
    };
    match serde_json::from_slice(&data) {
        Ok(meta) => MetadataState::Managed(meta),
        Err(_) => MetadataState::Unmanaged,
    }
}

fn target_result(target: &TargetGroup) -> Value {
    if target.host_ids.len() == 1 {
        json!({
            "hostId": target.host_ids[0],
            "skillName": target.skill_name,
            "targetDir": target.target_dir
        })
    } else {
        json!({
            "hostIds": target.host_ids,
            "skillName": target.skill_name,
            "targetDir": target.target_dir
        })
    }
}

fn with_reason(mut value: Value, reason: &str) -> Value {
    value["reason"] = json!(reason);
    value
}

fn install_report(errors: Vec<Value>) -> Value {
    json!({
        "installed": [],
        "updated": [],
        "skipped": [],
        "conflicts": [],
        "errors": errors
    })
}

fn uninstall_report(errors: Vec<Value>) -> Value {
    json!({
        "removed": [],
        "skipped": [],
        "conflicts": [],
        "errors": errors
    })
}

fn push_report(report: &mut Value, key: &str, value: Value) {
    report[key].as_array_mut().unwrap().push(value);
}

fn canonical_scope_path(host: &Host, scope: Scope, home: &Path, cwd: &Path) -> Option<PathBuf> {
    let paths = scope_paths(host, scope);
    paths.first().map(|path| expand_host_path(path, home, cwd))
}

fn choose_scope_path(host: &Host, scope: Scope, home: &Path, cwd: &Path) -> Option<PathBuf> {
    let paths = scope_paths(host, scope);
    for path in paths {
        let expanded = expand_host_path(path, home, cwd);
        if expanded.exists() {
            return Some(expanded);
        }
    }
    paths.first().map(|path| expand_host_path(path, home, cwd))
}

fn scope_paths(host: &Host, scope: Scope) -> &[String] {
    match scope {
        Scope::User => &host.user_skills_dirs,
        Scope::Project => &host.project_skills_dirs,
    }
}

fn expand_host_path(path: &str, home: &Path, cwd: &Path) -> PathBuf {
    if let Some(rest) = path.strip_prefix("~/") {
        home.join(rest)
    } else {
        cwd.join(path)
    }
}

fn defaults(options: &BaseOptions) -> io::Result<(PathBuf, PathBuf)> {
    let home = match &options.home {
        Some(home) => home.clone(),
        None => PathBuf::from(std::env::var("HOME").unwrap_or_default()),
    };
    let cwd = match &options.cwd {
        Some(cwd) => cwd.clone(),
        None => std::env::current_dir()?,
    };
    Ok((home, cwd))
}

fn parse_frontmatter(content: &str) -> HashMap<String, String> {
    let mut fields = HashMap::new();
    for line in content.lines() {
        if let Some((key, value)) = line.split_once(':') {
            fields.insert(key.to_string(), value.trim().to_string());
        }
    }
    fields
}

fn invalid_frontmatter() -> SkillInfo {
    SkillInfo {
        valid: false,
        skill_name: None,
        description: None,
        error_code: Some("invalid-frontmatter".to_string()),
    }
}

fn valid_skill_name(name: &str) -> bool {
    let mut last_dash = false;
    if name.is_empty() || name.starts_with('-') || name.ends_with('-') {
        return false;
    }
    for byte in name.bytes() {
        let ok = byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'-';
        if !ok || (byte == b'-' && last_dash) {
            return false;
        }
        last_dash = byte == b'-';
    }
    true
}

fn list_skill_files(root: &Path) -> io::Result<Vec<String>> {
    let mut files = Vec::new();
    collect_skill_files(root, root, &mut files)?;
    files.sort();
    Ok(files)
}

fn collect_skill_files(root: &Path, dir: &Path, files: &mut Vec<String>) -> io::Result<()> {
    for entry in fs::read_dir(dir)? {
        let entry = entry?;
        let name = entry.file_name();
        let name = name.to_string_lossy();
        if skip_name(&name) {
            continue;
        }
        let path = entry.path();
        let metadata = entry.metadata()?;
        if metadata.is_dir() {
            collect_skill_files(root, &path, files)?;
        } else if metadata.is_file() {
            let rel = path.strip_prefix(root).unwrap();
            files.push(rel.to_string_lossy().replace('\\', "/"));
        }
    }
    Ok(())
}

fn skip_name(name: &str) -> bool {
    name == ".git"
        || name == ".kitup.json"
        || name == ".DS_Store"
        || name.ends_with(".swp")
        || name.ends_with('~')
}

fn is_generic_detect_path(path: &str) -> bool {
    path == "~/.agents" || path == "~/.agents/skills" || path == "~/.config/agents"
}

fn scope_text(scope: Scope) -> &'static str {
    match scope {
        Scope::User => "user",
        Scope::Project => "project",
    }
}
