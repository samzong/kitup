package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	kitup "github.com/samzong/kitup/go"
)

func main() {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot locate example source")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	sandbox, err := os.MkdirTemp("", "kitup-example-go-")
	must(err)
	home := filepath.Join(sandbox, "home")
	cwd := filepath.Join(sandbox, "workspace")

	must(os.MkdirAll(home, 0o755))
	must(os.MkdirAll(cwd, 0o755))
	fmt.Println("sandbox:", sandbox)

	options := kitup.InstallOptions{
		BaseOptions: kitup.BaseOptions{
			Home: home,
			CWD:  cwd,
		},
		AppID:    "kitup-example-go",
		SkillDir: filepath.Join(repo, "skills/kitup"),
		Scope:    kitup.UserScope,
		Agents:   kitup.ExplicitAgents("codex"),
	}

	plan, err := kitup.PlanBundledSkill(options)
	must(err)
	printJSON("plan", plan)
	expect(len(plan.Installed) == 1 && len(plan.Errors) == 0, "plan did not find one install target")

	installed, err := kitup.InstallBundledSkill(options)
	must(err)
	printJSON("install", installed)
	expect(len(installed.Installed) == 1 && len(installed.Errors) == 0, "install did not write one target")
	_, err = os.Stat(filepath.Join(home, ".agents/skills/kitup/.kitup.json"))
	must(err)

	skipped, err := kitup.InstallBundledSkill(options)
	must(err)
	printJSON("install again", skipped)
	expect(len(skipped.Skipped) == 1 && skipped.Skipped[0]["reason"] == "unchanged", "second install did not skip unchanged target")
}

func printJSON(label string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	must(err)
	fmt.Println(label)
	fmt.Println(string(data))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func expect(condition bool, message string) {
	if !condition {
		panic(message)
	}
}
