import { createHash } from "node:crypto";
import {
  chmod,
  copyFile,
  mkdir,
  readdir,
  readFile,
  rename,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { constants } from "node:fs";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { defaultHostsSpecJson } from "./hosts.generated.js";

export type Scope = "user" | "project";
export type AgentSelector = "*" | "auto" | string[];

export interface Host {
  id: string;
  displayName: string;
  aliases?: string[];
  projectSkillsDirs: string[];
  userSkillsDirs: string[];
  detect: string[];
  status: "verified" | "documented" | "community" | "experimental";
  notes?: string[];
}

export interface HostSpec {
  schemaVersion: 1;
  hosts: Host[];
}

export interface BaseOptions {
  home?: string;
  cwd?: string;
  hostsFile?: string;
}

export interface InstallOptions extends BaseOptions {
  appId: string;
  skillDir: string;
  scope: Scope;
  agents?: AgentSelector;
}

export interface UninstallOptions extends BaseOptions {
  appId: string;
  skillName: string;
  scope: Scope;
  agents?: AgentSelector;
}

export interface TargetGroup {
  hostIds: string[];
  skillName: string;
  targetDir: string;
}

export type TargetResult =
  | {
      hostId: string;
      skillName: string;
      targetDir: string;
    }
  | {
      hostIds: string[];
      skillName: string;
      targetDir: string;
    };

export type SkipReason = "unchanged" | "missing";
export type ConflictReason = "unmanaged" | "owner-mismatch";
export type TargetConflict = TargetResult & { reason: ConflictReason };
export type TargetSkip = TargetResult & { reason: SkipReason };
export type UnknownHostError = { agent: string; reason: "unknown-host" };
export type UnsupportedScopeError = {
  hostId: string;
  skillName: string;
  scope: Scope;
  reason: "unsupported-scope";
};
export type SkillError = { skillDir: string; reason: SkillInfo["errorCode"] };
export type TargetError = UnknownHostError | UnsupportedScopeError | SkillError;

export interface InstallReport {
  installed: TargetResult[];
  updated: TargetResult[];
  skipped: TargetSkip[];
  conflicts: TargetConflict[];
  errors: TargetError[];
}

export interface UninstallReport {
  removed: TargetResult[];
  skipped: TargetSkip[];
  conflicts: TargetConflict[];
  errors: TargetError[];
}

export interface SkillInfo {
  valid: boolean;
  skillName?: string;
  description?: string;
  errorCode?: "missing-skill-md" | "invalid-frontmatter";
}

interface InstallMetadata {
  schemaVersion: 1;
  appId: string;
  skillName: string;
  source: "bundled";
  hash: string;
}

const source = "bundled";
const defaultAgents: AgentSelector = "auto";

export async function loadHostSpec(hostsFile?: string): Promise<HostSpec> {
  return JSON.parse(
    hostsFile ? await readFile(hostsFile, "utf8") : defaultHostsSpecJson,
  );
}

export async function resolveHosts(options: {
  agents: AgentSelector;
  hostsFile?: string;
  hosts?: Host[];
}): Promise<{ hosts: Host[]; errors: UnknownHostError[] }> {
  const hosts = options.hosts ?? (await loadHostSpec(options.hostsFile)).hosts;
  if (options.agents === "*") return { hosts, errors: [] };
  if (options.agents === "auto") return { hosts: [], errors: [] };

  const byName = new Map<string, Host>();
  for (const host of hosts) {
    byName.set(host.id, host);
    for (const alias of host.aliases ?? []) byName.set(alias, host);
  }

  const resolvedHosts: Host[] = [];
  const errors: UnknownHostError[] = [];
  const seen = new Set<string>();
  for (const agent of options.agents) {
    const host = byName.get(agent);
    if (!host) {
      errors.push({ agent, reason: "unknown-host" });
    } else if (!seen.has(host.id)) {
      seen.add(host.id);
      resolvedHosts.push(host);
    }
  }
  return { hosts: resolvedHosts, errors };
}

export async function detectHosts(
  options: BaseOptions & { scope?: Scope } = {},
): Promise<Host[]> {
  const spec = await loadHostSpec(options.hostsFile);
  const home = options.home ?? process.env.HOME ?? "";
  const cwd = options.cwd ?? process.cwd();
  const detected: Host[] = [];

  for (const host of spec.hosts) {
    const detectPath = host.detect[0];
    if (
      detectPath &&
      !isGenericDetectPath(detectPath) &&
      (await exists(expandHostPath(detectPath, home, cwd)))
    ) {
      detected.push(host);
    }
  }

  const scope = options.scope;
  if (!scope) return detected;
  return detected.sort((a, b) => {
    const aPath = canonicalScopePath(a, scope, home, cwd) ?? "";
    const bPath = canonicalScopePath(b, scope, home, cwd) ?? "";
    return aPath.localeCompare(bPath) || a.id.localeCompare(b.id);
  });
}

export async function resolveInstallTargets(
  options: BaseOptions & {
    agents?: AgentSelector;
    scope: Scope;
    skillName: string;
  },
): Promise<{
  targets: TargetGroup[];
  errors: TargetError[];
  detectedHostIds: string[];
}> {
  const spec = await loadHostSpec(options.hostsFile);
  const home = options.home ?? process.env.HOME ?? "";
  const cwd = options.cwd ?? process.cwd();
  const agents = options.agents ?? defaultAgents;
  const resolved =
    agents === "auto"
      ? undefined
      : await resolveHosts({ agents, hosts: spec.hosts });
  const selected =
    agents === "auto"
      ? await detectHosts({ ...options, scope: options.scope })
      : resolved!.hosts;
  const errors: TargetError[] = agents === "auto" ? [] : [...resolved!.errors];
  const byTarget = new Map<string, TargetGroup>();

  for (const host of selected) {
    const root = await chooseScopePath(host, options.scope, home, cwd);
    if (!root) {
      errors.push({
        hostId: host.id,
        skillName: options.skillName,
        scope: options.scope,
        reason: "unsupported-scope",
      });
      continue;
    }
    const targetDir = join(root, options.skillName);
    const group = byTarget.get(targetDir) ?? {
      hostIds: [],
      skillName: options.skillName,
      targetDir,
    };
    group.hostIds.push(host.id);
    byTarget.set(targetDir, group);
  }

  const targets = [...byTarget.values()].sort((a, b) =>
    a.targetDir.localeCompare(b.targetDir),
  );
  return {
    targets,
    errors,
    detectedHostIds: targets.flatMap((target) => target.hostIds),
  };
}

export async function validateSkill(
  skillDir: string,
  cwd = process.cwd(),
): Promise<SkillInfo> {
  const dir = resolvePath(skillDir, cwd);
  let content: string;
  try {
    content = await readFile(join(dir, "SKILL.md"), "utf8");
  } catch {
    return { valid: false, errorCode: "missing-skill-md" };
  }

  const match = content.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n/);
  if (!match) return { valid: false, errorCode: "invalid-frontmatter" };
  const frontmatter = parseFrontmatter(match[1]);
  const name = frontmatter.get("name") ?? "";
  const description = frontmatter.get("description") ?? "";
  if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(name)) {
    return { valid: false, errorCode: "invalid-frontmatter" };
  }
  if (description.length < 1 || description.length > 1024) {
    return { valid: false, errorCode: "invalid-frontmatter" };
  }
  return { valid: true, skillName: name, description };
}

export async function computeContentHash(
  skillDir: string,
  cwd = process.cwd(),
): Promise<string> {
  const dir = resolvePath(skillDir, cwd);
  const files = await listSkillFiles(dir);
  const hash = createHash("sha256");
  for (const file of files) {
    const bytes = await readFile(join(dir, file));
    hash.update(file);
    hash.update("\0");
    hash.update(bytes);
    hash.update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

export async function installBundledSkill(
  options: InstallOptions,
): Promise<InstallReport> {
  const cwd = options.cwd ?? process.cwd();
  const skill = await validateSkill(options.skillDir, cwd);
  if (!skill.valid || !skill.skillName) {
    return emptyInstallReport([
      { skillDir: options.skillDir, reason: skill.errorCode },
    ]);
  }

  const skillDir = resolvePath(options.skillDir, cwd);
  const hash = await computeContentHash(skillDir);
  const { targets, errors } = await resolveInstallTargets({
    ...options,
    skillName: skill.skillName,
  });
  const report = emptyInstallReport(errors);

  for (const target of targets) {
    const result = targetResult(target);
    const metadata = await readMetadata(target.targetDir);
    if (!metadata.exists) {
      await copyManagedSkill(
        skillDir,
        target.targetDir,
        options.appId,
        skill.skillName,
        hash,
      );
      report.installed.push(result);
    } else if (!metadata.value) {
      report.conflicts.push({ ...result, reason: "unmanaged" });
    } else if (metadata.value.appId !== options.appId) {
      report.conflicts.push({ ...result, reason: "owner-mismatch" });
    } else if (metadata.value.hash === hash) {
      report.skipped.push({ ...result, reason: "unchanged" });
    } else {
      await replaceManagedSkill(
        skillDir,
        target.targetDir,
        options.appId,
        skill.skillName,
        hash,
      );
      report.updated.push(result);
    }
  }

  return report;
}

export async function planBundledSkill(
  options: InstallOptions,
): Promise<InstallReport> {
  const cwd = options.cwd ?? process.cwd();
  const skill = await validateSkill(options.skillDir, cwd);
  if (!skill.valid || !skill.skillName) {
    return emptyInstallReport([
      { skillDir: options.skillDir, reason: skill.errorCode },
    ]);
  }

  const skillDir = resolvePath(options.skillDir, cwd);
  const hash = await computeContentHash(skillDir);
  const { targets, errors } = await resolveInstallTargets({
    ...options,
    skillName: skill.skillName,
  });
  const report = emptyInstallReport(errors);

  for (const target of targets) {
    const result = targetResult(target);
    const metadata = await readMetadata(target.targetDir);
    if (!metadata.exists) {
      report.installed.push(result);
    } else if (!metadata.value) {
      report.conflicts.push({ ...result, reason: "unmanaged" });
    } else if (metadata.value.appId !== options.appId) {
      report.conflicts.push({ ...result, reason: "owner-mismatch" });
    } else if (metadata.value.hash === hash) {
      report.skipped.push({ ...result, reason: "unchanged" });
    } else {
      report.updated.push(result);
    }
  }

  return report;
}

export async function updateBundledSkill(
  options: InstallOptions,
): Promise<InstallReport> {
  return installBundledSkill(options);
}

export async function uninstallBundledSkill(
  options: UninstallOptions,
): Promise<UninstallReport> {
  const { targets, errors } = await resolveInstallTargets({
    ...options,
    skillName: options.skillName,
  });
  const report: UninstallReport = {
    removed: [],
    skipped: [],
    conflicts: [],
    errors,
  };

  for (const target of targets) {
    const result = targetResult(target);
    const metadata = await readMetadata(target.targetDir);
    if (!metadata.exists) {
      report.skipped.push({ ...result, reason: "missing" });
    } else if (!metadata.value) {
      report.conflicts.push({ ...result, reason: "unmanaged" });
    } else if (metadata.value.appId !== options.appId) {
      report.conflicts.push({ ...result, reason: "owner-mismatch" });
    } else {
      await rm(target.targetDir, { recursive: true, force: true });
      report.removed.push(result);
    }
  }

  return report;
}

async function copyManagedSkill(
  skillDir: string,
  targetDir: string,
  appId: string,
  skillName: string,
  hash: string,
) {
  await rm(targetDir, { recursive: true, force: true });
  await copySkillDir(skillDir, targetDir);
  await writeMetadata(targetDir, appId, skillName, hash);
}

async function replaceManagedSkill(
  skillDir: string,
  targetDir: string,
  appId: string,
  skillName: string,
  hash: string,
) {
  const suffix = `.kitup-${process.pid}-${Date.now()}`;
  const tmp = `${targetDir}${suffix}`;
  const backup = `${targetDir}${suffix}-backup`;
  await rm(tmp, { recursive: true, force: true });
  await copySkillDir(skillDir, tmp);
  await writeMetadata(tmp, appId, skillName, hash);
  try {
    await rename(targetDir, backup);
    await rename(tmp, targetDir);
    await rm(backup, { recursive: true, force: true });
  } catch (error) {
    await rm(tmp, { recursive: true, force: true });
    if ((await exists(backup)) && !(await exists(targetDir)))
      await rename(backup, targetDir);
    throw error;
  }
}

async function copySkillDir(src: string, dest: string) {
  await mkdir(dest, { recursive: true });
  for (const entry of await readdir(src, { withFileTypes: true })) {
    if (skipName(entry.name)) continue;
    const from = join(src, entry.name);
    const to = join(dest, entry.name);
    if (entry.isDirectory()) {
      await copySkillDir(from, to);
    } else if (entry.isFile()) {
      await mkdir(dirname(to), { recursive: true });
      await copyFile(from, to, constants.COPYFILE_FICLONE_FORCE).catch(
        async () => {
          await copyFile(from, to);
        },
      );
      await chmod(to, (await stat(from)).mode);
    }
  }
}

async function writeMetadata(
  targetDir: string,
  appId: string,
  skillName: string,
  hash: string,
) {
  await writeFile(
    join(targetDir, ".kitup.json"),
    `${JSON.stringify({ schemaVersion: 1, appId, skillName, source, hash }, null, 2)}\n`,
  );
}

async function readMetadata(
  targetDir: string,
): Promise<{ exists: boolean; value?: InstallMetadata }> {
  if (!(await exists(targetDir))) return { exists: false };
  try {
    return {
      exists: true,
      value: JSON.parse(await readFile(join(targetDir, ".kitup.json"), "utf8")),
    };
  } catch {
    return { exists: true };
  }
}

function targetResult(target: TargetGroup): TargetResult {
  const base = { skillName: target.skillName, targetDir: target.targetDir };
  return target.hostIds.length === 1
    ? { hostId: target.hostIds[0], ...base }
    : { hostIds: target.hostIds, ...base };
}

function emptyInstallReport(errors: TargetError[] = []): InstallReport {
  return { installed: [], updated: [], skipped: [], conflicts: [], errors };
}

function canonicalScopePath(
  host: Host,
  scope: Scope,
  home: string,
  cwd: string,
) {
  const paths = scope === "user" ? host.userSkillsDirs : host.projectSkillsDirs;
  return paths[0] ? expandHostPath(paths[0], home, cwd) : undefined;
}

async function chooseScopePath(
  host: Host,
  scope: Scope,
  home: string,
  cwd: string,
) {
  const paths = scope === "user" ? host.userSkillsDirs : host.projectSkillsDirs;
  for (const path of paths) {
    const expanded = expandHostPath(path, home, cwd);
    if (await exists(expanded)) return expanded;
  }
  return paths[0] ? expandHostPath(paths[0], home, cwd) : undefined;
}

function expandHostPath(path: string, home: string, cwd: string) {
  return path.startsWith("~/") ? join(home, path.slice(2)) : join(cwd, path);
}

function resolvePath(path: string, cwd: string) {
  return resolve(isAbsolute(path) ? path : join(cwd, path));
}

function parseFrontmatter(content: string) {
  const values = new Map<string, string>();
  for (const line of content.split(/\r?\n/)) {
    const match = line.match(/^([A-Za-z0-9_-]+):\s*(.*)$/);
    if (match) values.set(match[1], match[2].trim());
  }
  return values;
}

async function listSkillFiles(dir: string, base = dir): Promise<string[]> {
  const files: string[] = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    if (skipName(entry.name)) continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) files.push(...(await listSkillFiles(full, base)));
    if (entry.isFile()) files.push(relative(base, full).split(sep).join("/"));
  }
  return files.sort();
}

function skipName(name: string) {
  return (
    name === ".git" ||
    name === ".kitup.json" ||
    name === ".DS_Store" ||
    name.endsWith(".swp") ||
    name.endsWith("~")
  );
}

function isGenericDetectPath(path: string) {
  return (
    path === "~/.agents" ||
    path === "~/.agents/skills" ||
    path === "~/.config/agents"
  );
}

async function exists(path: string) {
  try {
    await stat(path);
    return true;
  } catch {
    return false;
  }
}
