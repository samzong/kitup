import { access, mkdtemp, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { installBundledSkill, planBundledSkill } from "@kitup/sdk";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const sandbox = await mkdtemp(resolve(tmpdir(), "kitup-example-ts-"));
const home = resolve(sandbox, "home");
const cwd = resolve(sandbox, "workspace");
const skillDir = resolve(repo, "skills/kitup");

await mkdir(home, { recursive: true });
await mkdir(cwd, { recursive: true });

const options = {
  appId: "kitup-example-ts",
  skillDir,
  scope: "user" as const,
  agents: ["codex"],
  home,
  cwd,
};

console.log(`sandbox: ${sandbox}`);

console.log("plan");
const plan = await planBundledSkill(options);
console.log(JSON.stringify(plan, null, 2));
expect(
  plan.installed.length === 1 && plan.errors.length === 0,
  "plan did not find one install target",
);

console.log("install");
const install = await installBundledSkill(options);
console.log(JSON.stringify(install, null, 2));
expect(
  install.installed.length === 1 && install.errors.length === 0,
  "install did not write one target",
);
await access(resolve(home, ".agents/skills/kitup/.kitup.json"));

console.log("install again");
const again = await installBundledSkill(options);
console.log(JSON.stringify(again, null, 2));
expect(
  again.skipped.length === 1 && again.skipped[0]?.reason === "unchanged",
  "second install did not skip unchanged target",
);

function expect(condition: boolean, message: string) {
  if (!condition) throw new Error(message);
}
