import io
import json

import kitup
from kitup import (
    BaseOptions,
    InstallOptions,
    InstallSelectionOptions,
    InstallWorkflowOptions,
    agent_selector_from_flags,
    classify_install_workflow_exit,
    directory_bundle,
    install_flag_error,
    install_workflow_error,
    parse_install_flags,
    parse_scope_flag,
    resolve_install_selection,
    run_bundled_skill_install_with_io,
)
from kitup.workflow import split_flag_values


def write_hosts_file(path, hosts) -> None:
    path.write_text(
        json.dumps(
            {
                "$schema": "./hosts.schema.json",
                "schemaVersion": 1,
                "hosts": hosts,
            }
        ),
        encoding="utf-8",
    )


def test_parse_install_flags_defaults_to_user_auto():
    parsed = parse_install_flags({})

    assert parsed.scope == "user"
    assert parsed.scope_set is False
    assert parsed.agents == "auto"
    assert parsed.yes is False
    assert parsed.dry_run is False
    assert parsed.errors == []


def test_parse_install_flags_explicit_scope_agents_and_errors():
    parsed = parse_install_flags(
        {
            "scope": "global",
            "agents": ["*", "codex,claude-code"],
            "yes": True,
            "dryRun": True,
        }
    )

    assert parsed.scope == "user"
    assert parsed.scope_set is True
    assert parsed.agents == "*"
    assert parsed.yes is True
    assert parsed.dry_run is True
    assert parsed.errors == [
        {"flag": "scope", "reason": "invalid-scope", "value": "global"},
        {
            "flag": "agent",
            "reason": "agent-star-must-be-alone",
            "value": "*,codex,claude-code",
        },
    ]


def test_flag_helpers_normalize_lists():
    errors: list[dict[str, str]] = []

    assert split_flag_values(["codex,claude-code", " codex "]) == [
        "codex",
        "claude-code",
        "codex",
    ]
    assert agent_selector_from_flags(["codex,claude-code", "codex"], errors) == [
        "codex",
        "claude-code",
    ]
    assert parse_scope_flag("project", errors) == "project"
    assert errors == []


def test_resolve_install_selection_requires_agents_without_tty_or_yes(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    hosts_file = tmp_path / "hosts.json"
    home.mkdir()
    workspace.mkdir()
    (home / ".codex").mkdir()
    write_hosts_file(
        hosts_file,
        [
            {
                "id": "codex",
                "displayName": "Codex",
                "projectSkillsDirs": [".agents/skills"],
                "userSkillsDirs": ["~/.agents/skills"],
                "detect": ["~/.codex"],
                "status": "verified",
            }
        ],
    )

    selection = resolve_install_selection(
        InstallSelectionOptions(
            base=BaseOptions(home=str(home), cwd=str(workspace), hosts_file=str(hosts_file)),
            scope="user",
            stdin_tty=False,
            yes=False,
        )
    )

    assert selection.action == "error"
    assert selection.detected_host_ids == ["codex"]
    assert selection.errors == [{"reason": "agent-selection-required"}]


def test_resolve_install_selection_tty_prompts_for_multiple_detected_hosts(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    hosts_file = tmp_path / "hosts.json"
    home.mkdir()
    workspace.mkdir()
    (home / ".codex").mkdir()
    (home / ".claude").mkdir()
    write_hosts_file(
        hosts_file,
        [
            {
                "id": "codex",
                "displayName": "Codex",
                "projectSkillsDirs": [".agents/skills"],
                "userSkillsDirs": ["~/.agents/skills"],
                "detect": ["~/.codex"],
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
    )

    selection = resolve_install_selection(
        InstallSelectionOptions(
            base=BaseOptions(home=str(home), cwd=str(workspace), hosts_file=str(hosts_file)),
            scope="user",
            stdin_tty=True,
            yes=False,
        )
    )

    assert selection.action == "select-agents"
    assert selection.candidate_host_ids == ["codex", "claude-code"]
    assert selection.detected_host_ids == ["codex", "claude-code"]
    assert selection.needs_confirmation is True


def test_classify_install_workflow_exit_reports_conflict():
    exit_info = classify_install_workflow_exit(
        {
            "canceled": False,
            "selection": {"errors": []},
            "report": {"conflicts": [{}], "errors": []},
        }
    )

    assert exit_info.ok is False
    assert exit_info.code == "conflict"
    assert exit_info.message == "Installation has conflicts."


def test_error_helpers_map_flag_and_workflow_failures():
    assert str(install_flag_error([{"reason": "invalid-scope"}])) == "Invalid install flags."
    assert install_workflow_error(
        {
            "canceled": False,
            "selection": {"errors": [{"reason": "scope-selection-required"}]},
            "report": {"conflicts": [], "errors": []},
        }
    ).args[0] == "Agent selection failed."
    assert install_workflow_error(
        {
            "canceled": True,
            "selection": {"errors": []},
            "report": {"conflicts": [], "errors": []},
        }
    ) is None


def test_run_bundled_skill_install_scope_prompt_and_top_level_exports(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()
    skill = workspace / "skill"
    skill.mkdir()
    (skill / "SKILL.md").write_text(
        "---\nname: basic\ndescription: demo\n---\n",
        encoding="utf-8",
    )
    output = io.StringIO()

    report = run_bundled_skill_install_with_io(
        InstallWorkflowOptions(
            install=InstallOptions(
                base=BaseOptions(home=str(home), cwd=str(workspace)),
                app_id="example-cli",
                skill_bundle=directory_bundle(str(skill)),
                scope="user",
                agents=["codex"],
            ),
            stdin_tty=True,
            prompt_scope=True,
            scope_set=False,
        ),
        io.StringIO("project\ny\n"),
        output,
    )

    assert report.scope == "project"
    assert report.canceled is False
    assert "Select install scope:" in output.getvalue()
    assert "Proceed? [y/N] " in output.getvalue()
    assert (workspace / ".agents" / "skills" / "basic" / "SKILL.md").exists()

    assert kitup.parse_install_flags is parse_install_flags
    assert kitup.parse_scope_flag is parse_scope_flag
    assert kitup.agent_selector_from_flags is agent_selector_from_flags
    assert kitup.resolve_install_selection is resolve_install_selection
    assert kitup.classify_install_workflow_exit is classify_install_workflow_exit
    assert kitup.install_flag_error is install_flag_error
    assert kitup.install_workflow_error is install_workflow_error
    assert kitup.run_bundled_skill_install_with_io is run_bundled_skill_install_with_io
