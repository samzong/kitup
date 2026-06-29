# Examples

Tiny consumer CLIs that bundle `skills/kitup` and install it through the local SDK.

Each example creates and prints a temp sandbox:

```bash
pnpm --dir examples/ts install-skill

cd examples/go
go run .

cd examples/rust
cargo run
```

Each run resets its own sandbox, calls `plan`, calls `install`, then calls `install` again to show the unchanged skip path.
