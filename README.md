# kitup

Shared installer SDK for bundled Agent Skills.

CLI authors ship a skill with their tool. `kitup` detects local coding-agent hosts, validates `SKILL.md`, copies the bundled skill into the right host directories, and writes ownership metadata so updates stay safe.

```text
mycli skill install
  -> kitup SDK
  -> local agent hosts
  -> installed SKILL.md + .kitup.json
```

## What it does

- detect installed agent hosts
- resolve user and project skill directories
- validate bundled skills
- copy, update, and uninstall kitup-owned installs
- refuse unsafe overwrite conflicts
- return structured install reports

## What it is not

- not a skill marketplace
- not a remote registry
- not a replacement for user-facing skill discovery tools

## Usage

Bundle a skill in your CLI project:

```text
mycli/
  skills/mycli/SKILL.md
```

Your CLI owns the command name, flags, prompts, and bundled skill location. `kitup` owns host detection, target paths, copy/update semantics, metadata, and conflicts.

### TypeScript

Install:

```bash
npm install @kitup/sdk
```

Use:

```ts
import { installBundledSkill } from "@kitup/sdk";

const report = await installBundledSkill({
  appId: "mycli",
  skillDir: "./skills/mycli",
  scope: "user",
});
```

### Go

Install:

```bash
go get github.com/samzong/kitup/go
```

Use:

```go
import kitup "github.com/samzong/kitup/go"

report, err := kitup.InstallBundledSkill(kitup.InstallOptions{
	AppID:    "mycli",
	SkillDir: "./skills/mycli",
	Scope:    kitup.UserScope,
})
```

### Rust

Install:

```bash
cargo add kitup
```

Use:

```rust
let report = kitup::install_bundled_skill(&kitup::InstallOptions {
    base: kitup::BaseOptions::default(),
    app_id: "mycli".to_string(),
    skill_dir: "./skills/mycli".into(),
    scope: kitup::Scope::User,
    agents: kitup::AgentSelector::Auto,
})?;
```

The report contains `installed`, `updated`, `skipped`, `conflicts`, and `errors`.

## Docs

- [API](docs/API.md)
- [Contributing](CONTRIBUTING.md)
- [Host adapter contract](docs/host-adapter-contract.md)
- [Release](docs/RELEASE.md)

## Acknowledgments

Host adapter coverage builds on prior work from [GitHub CLI `gh skill`](https://cli.github.com/manual/gh_skill_install) and [`npx skills` / skills.sh](https://github.com/vercel-labs/skills).
