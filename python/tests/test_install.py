import json

from kitup import resolve_install_targets
from kitup.types import BaseOptions


def test_resolve_install_targets_prefers_first_existing_user_dir(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()
    (home / ".agents" / "skills").mkdir(parents=True)

    targets, errors, detected = resolve_install_targets(
        BaseOptions(home=str(home), cwd=str(workspace)),
        ["codex"],
        "user",
        "basic",
    )

    assert [
        (target.host_ids, target.target_dir)
        for target in targets
    ] == [(["codex"], str(home / ".agents" / "skills" / "basic"))]
    assert errors == []
    assert detected == ["codex"]


def test_resolve_install_targets_groups_hosts_by_shared_target_dir(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()
    (home / ".agents" / "skills").mkdir(parents=True)

    targets, errors, detected = resolve_install_targets(
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
    assert errors == []
    assert detected == ["codex", "warp", "gemini-cli"]


def test_resolve_install_targets_auto_detects_supported_hosts(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()
    (home / ".codex").mkdir()
    (home / ".claude").mkdir()
    (home / ".agents" / "skills").mkdir(parents=True)
    (home / ".claude" / "skills").mkdir(parents=True)

    targets, errors, detected = resolve_install_targets(
        BaseOptions(home=str(home), cwd=str(workspace)),
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
    assert errors == []
    assert detected == ["codex", "claude-code"]


def test_resolve_install_targets_reports_unsupported_scope(tmp_path):
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

    targets, errors, detected = resolve_install_targets(
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
    assert errors == [
        {
            "hostId": "eve",
            "skillName": "basic",
            "scope": "user",
            "reason": "unsupported-scope",
        }
    ]
    assert detected == []
