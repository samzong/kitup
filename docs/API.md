# API

`kitup` exposes the same installer concepts in TypeScript, Go, and Rust.

The core flow is:

1. validate a bundled skill directory
2. resolve target agent hosts
3. copy, update, skip, or report conflicts
4. write `.kitup.json` ownership metadata
5. return a structured report

## TypeScript

Package: `@kitup/sdk`

```ts
import {
  detectHosts,
  installBundledSkill,
  planBundledSkill,
  uninstallBundledSkill,
  updateBundledSkill,
  validateSkill,
} from "@kitup/sdk";
```

Common install call:

```ts
const report = await installBundledSkill({
  appId: "mycli",
  skillDir: "./skills/mycli",
  scope: "user",
});
```

Implemented functions:

- `loadHostSpec(hostsFile?)`
- `resolveHosts({ agents, hostsFile?, hosts? })`
- `detectHosts({ home?, cwd?, hostsFile?, scope? })`
- `resolveInstallTargets({ home?, cwd?, hostsFile?, agents?, scope, skillName })`
- `validateSkill(skillDir, cwd?)`
- `computeContentHash(skillDir, cwd?)`
- `planBundledSkill(options)`
- `installBundledSkill(options)`
- `updateBundledSkill(options)`
- `uninstallBundledSkill(options)`

## Go

Module: `github.com/samzong/kitup/go`

```go
import kitup "github.com/samzong/kitup/go"
```

Common install call:

```go
report, err := kitup.InstallBundledSkill(kitup.InstallOptions{
	AppID:    "mycli",
	SkillDir: "./skills/mycli",
	Scope:    kitup.UserScope,
})
```

Implemented functions:

- `LoadHostSpec(hostsFile string)`
- `ResolveHosts(agents, hosts)`
- `DetectHosts(opts, scope)`
- `ResolveInstallTargets(opts, agents, scope, skillName)`
- `ValidateSkill(skillDir)`
- `ComputeContentHash(skillDir)`
- `PlanBundledSkill(opts)`
- `InstallBundledSkill(opts)`
- `UpdateBundledSkill(opts)`
- `UninstallBundledSkill(opts)`

## Rust

Crate: `kitup`

Common install call:

```rust
let report = kitup::install_bundled_skill(&kitup::InstallOptions {
    base: kitup::BaseOptions::default(),
    app_id: "mycli".to_string(),
    skill_dir: "./skills/mycli".into(),
    scope: kitup::Scope::User,
    agents: kitup::AgentSelector::Auto,
})?;
```

Implemented functions:

- `load_host_spec(hosts_file)`
- `resolve_hosts(agents, hosts)`
- `detect_hosts(options, scope)`
- `resolve_install_targets(options, agents, scope, skill_name)`
- `validate_skill(skill_dir)`
- `compute_content_hash(skill_dir)`
- `plan_bundled_skill(options)`
- `install_bundled_skill(options)`
- `update_bundled_skill(options)`
- `uninstall_bundled_skill(options)`

## Options

Install options use the same fields across languages:

- `appId` / `AppID` / `app_id`: owner id written to `.kitup.json`
- `skillDir` / `SkillDir` / `skill_dir`: local bundled skill directory
- `scope`: `user` or `project`
- `agents`: `"auto"`, `"*"`, or explicit host ids
- `home`, `cwd`, `hostsFile`: optional test and embedding overrides

The embedding CLI owns command names, flags, prompts, and where its bundled skill lives. Convert those user-facing choices into `scope`, `agents`, and `skillDir` before calling the SDK.

Recommended CLI behavior:

```bash
mycli skill install
mycli skill install --scope user --agent codex
mycli skill install --scope project --agent codex --agent claude-code
```

If `--scope` is missing, ask the user to choose `user` or `project`.

If `--agent` is missing, call `detectHosts` for the selected scope and let the user choose from detected hosts. If detection finds no hosts, ask for an explicit host id. Explicit host ids and aliases resolve through the host spec and do not require detection paths to exist.

Selector semantics:

- `scope: "user"` installs into the first `userSkillsDirs` path for each host.
- `scope: "project"` installs into the first `projectSkillsDirs` path for each host.
- `agents: "auto"` uses host detection.
- `agents: "*"` selects every host adapter.
- explicit agents select canonical host ids or aliases.
- hosts without a path for the selected scope return an `unsupported-scope` error.

## Reports

Install reports include:

- `installed`
- `updated`
- `skipped`
- `conflicts`
- `errors`

Uninstall reports include:

- `removed`
- `skipped`
- `conflicts`
- `errors`

Conflict is the safe default. A target directory without matching `.kitup.json` ownership metadata is reported as a conflict, not overwritten.
