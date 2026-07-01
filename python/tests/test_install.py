import json

from kitup import directory_bundle, resolve_install_targets
from kitup.bundle import compute_bundle_content_hash
from kitup.install import (
    install_bundled_skill,
    uninstall_bundled_skill,
    update_bundled_skill,
)
from kitup.types import BaseOptions, InstallOptions, UninstallOptions


def test_resolve_install_targets_prefers_first_existing_user_dir(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()
    (home / ".agents" / "skills").mkdir(parents=True)

    targets = resolve_install_targets(
        BaseOptions(home=str(home), cwd=str(workspace)),
        ["codex"],
        "user",
        "basic",
    )

    assert [
        (target.host_ids, target.target_dir)
        for target in targets
    ] == [(["codex"], str(home / ".agents" / "skills" / "basic"))]


def test_resolve_install_targets_groups_hosts_by_shared_target_dir(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()
    (home / ".agents" / "skills").mkdir(parents=True)

    targets = resolve_install_targets(
        BaseOptions(home=str(home), cwd=str(workspace)),
        ["codex", "warp", "gemini-cli"],
        "user",
        "basic",
    )

    assert [
        (target.host_ids, target.target_dir)
        for target in targets
    ] == [
        (
            ["codex", "warp", "gemini-cli"],
            str(home / ".agents" / "skills" / "basic"),
        )
    ]


def test_resolve_install_targets_auto_detects_supported_hosts(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()
    (home / ".codex").mkdir()
    (home / ".claude").mkdir()
    (home / ".agents" / "skills").mkdir(parents=True)
    (home / ".claude" / "skills").mkdir(parents=True)
    hosts_file = tmp_path / "hosts.json"
    hosts_file.write_text(
        json.dumps(
            {
                "$schema": "./hosts.schema.json",
                "schemaVersion": 1,
                "hosts": [
                    {
                        "id": "codex",
                        "displayName": "Codex",
                        "projectSkillsDirs": [".agents/skills"],
                        "userSkillsDirs": ["~/.agents/skills", "~/.codex/skills"],
                        "detect": ["~/.codex", "~/.agents/skills", "~/.agents"],
                        "status": "verified",
                    },
                    {
                        "id": "claude-code",
                        "displayName": "Claude Code",
                        "projectSkillsDirs": [".claude/skills"],
                        "userSkillsDirs": ["~/.claude/skills"],
                        "detect": ["~/.claude"],
                        "status": "verified",
                    },
                ],
            }
        )
    )

    targets = resolve_install_targets(
        BaseOptions(home=str(home), cwd=str(workspace), hosts_file=str(hosts_file)),
        "auto",
        "user",
        "basic",
    )

    assert [
        (target.host_ids, target.target_dir)
        for target in targets
    ] == [
        (["codex"], str(home / ".agents" / "skills" / "basic")),
        (["claude-code"], str(home / ".claude" / "skills" / "basic")),
    ]


def test_resolve_install_targets_skips_hosts_without_scope_path(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()

    hosts_file = tmp_path / "hosts.json"
    hosts_file.write_text(
        json.dumps(
            {
                "$schema": "./hosts.schema.json",
                "schemaVersion": 1,
                "hosts": [
                    {
                        "id": "eve",
                        "displayName": "Eve",
                        "projectSkillsDirs": ["agent/skills"],
                        "userSkillsDirs": [],
                        "detect": ["agent", "package.json"],
                        "status": "community",
                    }
                ],
            }
        )
    )

    targets = resolve_install_targets(
        BaseOptions(
            home=str(home),
            cwd=str(workspace),
            hosts_file=str(hosts_file),
        ),
        ["eve"],
        "user",
        "basic",
    )

    assert targets == []


def test_install_update_uninstall_round_trip(tmp_path):
    skill = tmp_path / "skill"
    skill.mkdir()
    (skill / "SKILL.md").write_text(
        "---\nname: basic\ndescription: demo\n---\n",
        encoding="utf-8",
    )
    (skill / "bin").mkdir()
    script = skill / "bin" / "run.sh"
    script.write_text("#!/bin/sh\necho updated\n", encoding="utf-8")
    script.chmod(0o755)

    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()

    install_options = InstallOptions(
        base=BaseOptions(home=str(home), cwd=str(workspace)),
        app_id="kitup-python-test",
        skill_bundle=directory_bundle(str(skill)),
        scope="user",
        agents=["codex"],
    )

    install_report = install_bundled_skill(install_options)

    assert len(install_report.installed) == 1

    target = home / ".agents" / "skills" / "basic"
    assert (target / "SKILL.md").read_text(encoding="utf-8").startswith("---\nname: basic\n")
    assert (target / "bin" / "run.sh").read_text(encoding="utf-8") == "#!/bin/sh\necho updated\n"
    assert (target / "bin" / "run.sh").stat().st_mode & 0o777 == 0o755
    assert json.loads((target / ".kitup.json").read_text(encoding="utf-8")) == {
        "schemaVersion": 1,
        "appId": "kitup-python-test",
        "skillName": "basic",
        "source": "bundled",
        "hash": compute_bundle_content_hash(directory_bundle(str(skill))),
    }

    script.write_text("#!/bin/sh\necho second\n", encoding="utf-8")

    update_report = update_bundled_skill(install_options)

    assert len(update_report.updated) == 1
    assert (target / "bin" / "run.sh").read_text(encoding="utf-8") == "#!/bin/sh\necho second\n"

    unchanged_report = update_bundled_skill(install_options)

    assert unchanged_report.skipped[0].reason == "unchanged"

    uninstall_report = uninstall_bundled_skill(
        UninstallOptions(
            base=BaseOptions(home=str(home), cwd=str(workspace)),
            app_id="kitup-python-test",
            skill_name="basic",
            scope="user",
            agents=["codex"],
        )
    )

    assert len(uninstall_report.removed) == 1
    assert not target.exists()


def test_install_lifecycle_reports_owner_mismatch_and_missing(tmp_path):
    skill = tmp_path / "skill"
    skill.mkdir()
    (skill / "SKILL.md").write_text(
        "---\nname: basic\ndescription: demo\n---\n",
        encoding="utf-8",
    )
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()

    install_bundled_skill(
        InstallOptions(
            base=BaseOptions(home=str(home), cwd=str(workspace)),
            app_id="kitup-python-test",
            skill_bundle=directory_bundle(str(skill)),
            scope="user",
            agents=["codex"],
        )
    )

    conflict_report = install_bundled_skill(
        InstallOptions(
            base=BaseOptions(home=str(home), cwd=str(workspace)),
            app_id="other-app",
            skill_bundle=directory_bundle(str(skill)),
            scope="user",
            agents=["codex"],
        )
    )
    missing_report = uninstall_bundled_skill(
        UninstallOptions(
            base=BaseOptions(home=str(home), cwd=str(workspace)),
            app_id="kitup-python-test",
            skill_name="missing",
            scope="user",
            agents=["codex"],
        )
    )

    assert conflict_report.conflicts[0].reason == "owner-mismatch"
    assert missing_report.skipped[0].reason == "missing"
