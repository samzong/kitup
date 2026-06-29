# Examples

Tiny consumer CLIs that bundle `skills/kitup` and install it through the local SDK.

The examples show the smallest SDK integration: pass `appId`, `skillDir`, and `scope`, then print the structured report.

The temporary `HOME` wrapper keeps the demos from writing to your real user skill directory and provides a Codex detection path for the SDK's default auto agent selection.

```bash
tmp="$(mktemp -d)" && mkdir -p "$tmp/.codex" && HOME="$tmp" pnpm --dir examples/ts install-skill

cd examples/go
tmp="$(mktemp -d)" && mkdir -p "$tmp/.codex" && HOME="$tmp" go run .

cd examples/rust
tmp="$(mktemp -d)" && mkdir -p "$tmp/.codex" && CARGO_HOME="${CARGO_HOME:-$HOME/.cargo}" RUSTUP_HOME="${RUSTUP_HOME:-$HOME/.rustup}" HOME="$tmp" cargo run --quiet
```

Each example calls `InstallBundledSkill` once. CLI flags and prompts belong to the embedding CLI, not to kitup.
