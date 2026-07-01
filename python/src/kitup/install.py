from __future__ import annotations

import shutil
from pathlib import Path

from ._metadata import read_install_metadata, write_install_metadata
from .bundle import (
    copy_normalized_bundle,
    compute_bundle_content_hash,
    normalize_skill_bundle,
    validate_skill_bundle,
)
from .hosts import detect_hosts, load_host_spec, resolve_hosts
from .types import (
    BaseOptions,
    Host,
    InstallOptions,
    InstallReport,
    Scope,
    TargetError,
    TargetGroup,
    TargetResult,
    TargetStatus,
    UninstallOptions,
    UninstallReport,
)


def expand_host_path(path: str, *, home: Path, cwd: Path) -> Path:
    if path.startswith("~/"):
        return home / path[2:]
    return cwd / path


def choose_scope_path(host: Host, *, scope: Scope, home: Path, cwd: Path) -> Path | None:
    paths = host.user_skills_dirs if scope == "user" else host.project_skills_dirs
    for path in paths:
        expanded = expand_host_path(path, home=home, cwd=cwd)
        if expanded.exists():
            return expanded
    if not paths:
        return None
    return expand_host_path(paths[0], home=home, cwd=cwd)


def resolve_install_targets(
    options: BaseOptions,
    agents: str | list[str] | None,
    scope: Scope,
    skill_name: str,
) -> list[TargetGroup]:
    spec = load_host_spec(options.hosts_file)
    home = Path(options.home).expanduser() if options.home else Path.home()
    cwd = Path(options.cwd) if options.cwd else Path.cwd()
    if agents in (None, "auto"):
        selected = detect_hosts(options, scope)
    else:
        selected, errors = resolve_hosts(agents, spec.hosts)
        if errors:
            return []

    by_target: dict[str, TargetGroup] = {}
    for host in selected:
        root = choose_scope_path(host, scope=scope, home=home, cwd=cwd)
        if root is None:
            continue
        target_dir = str(root / skill_name)
        group = by_target.get(target_dir)
        if group is None:
            group = TargetGroup(skill_name=skill_name, target_dir=target_dir)
            by_target[target_dir] = group
        group.host_ids.append(host.id)

    return [by_target[path] for path in sorted(by_target)]


def empty_install_report(errors: list[TargetError] | None = None) -> InstallReport:
    return InstallReport(errors=errors or [])


def empty_uninstall_report(errors: list[TargetError] | None = None) -> UninstallReport:
    return UninstallReport(errors=errors or [])


def target_result(target: TargetGroup) -> TargetResult:
    if len(target.host_ids) == 1:
        return TargetResult(
            host_id=target.host_ids[0],
            skill_name=target.skill_name,
            target_dir=target.target_dir,
        )
    return TargetResult(
        host_ids=list(target.host_ids),
        skill_name=target.skill_name,
        target_dir=target.target_dir,
    )


def target_status(target: TargetGroup, reason: str) -> TargetStatus:
    result = target_result(target)
    return TargetStatus(
        host_id=result.host_id,
        host_ids=result.host_ids,
        skill_name=result.skill_name,
        target_dir=result.target_dir,
        reason=reason,
    )


def plan_bundled_skill(options: InstallOptions) -> InstallReport:
    return install_or_plan(options, write=False)


def install_bundled_skill(options: InstallOptions) -> InstallReport:
    return install_or_plan(options, write=True)


def update_bundled_skill(options: InstallOptions) -> InstallReport:
    return install_bundled_skill(options)


def install_or_plan(options: InstallOptions, *, write: bool) -> InstallReport:
    info = validate_skill_bundle(options.skill_bundle, cwd=options.base.cwd)
    if not info.valid or not info.skill_name:
        return empty_install_report([TargetError(reason=info.error_code or "invalid-skill-bundle")])

    normalized = normalize_skill_bundle(options.skill_bundle, cwd=options.base.cwd)
    digest = compute_bundle_content_hash(options.skill_bundle, cwd=options.base.cwd)
    report = empty_install_report()
    for target in resolve_install_targets(
        options.base,
        options.agents,
        options.scope,
        info.skill_name,
    ):
        result = target_result(target)
        target_dir = Path(target.target_dir)
        metadata_file = target_dir / ".kitup.json"
        metadata = read_install_metadata(target_dir)

        if not target_dir.exists():
            if write:
                copy_normalized_bundle(normalized.files, target_dir)
                write_install_metadata(
                    target_dir,
                    app_id=options.app_id,
                    skill_name=info.skill_name,
                    digest=digest,
                    source="bundled",
                )
            report.installed.append(result)
            continue

        if metadata is None and metadata_file.exists():
            report.conflicts.append(target_status(target, "unmanaged"))
            continue
        if metadata is None:
            report.conflicts.append(target_status(target, "unmanaged"))
            continue
        if metadata.get("appId") != options.app_id:
            report.conflicts.append(target_status(target, "owner-mismatch"))
            continue
        if metadata.get("hash") == digest:
            report.skipped.append(target_status(target, "unchanged"))
            continue

        if write:
            copy_normalized_bundle(normalized.files, target_dir)
            write_install_metadata(
                target_dir,
                app_id=options.app_id,
                skill_name=info.skill_name,
                digest=digest,
                source=str(metadata.get("source", "bundled")),
            )
        report.updated.append(result)

    return report


def uninstall_bundled_skill(options: UninstallOptions) -> UninstallReport:
    report = empty_uninstall_report()
    for target in resolve_install_targets(
        options.base,
        options.agents,
        options.scope,
        options.skill_name,
    ):
        result = target_result(target)
        target_dir = Path(target.target_dir)
        metadata_file = target_dir / ".kitup.json"
        metadata = read_install_metadata(target_dir)

        if not target_dir.exists():
            report.skipped.append(target_status(target, "missing"))
            continue
        if metadata is None and metadata_file.exists():
            report.conflicts.append(target_status(target, "unmanaged"))
            continue
        if metadata is None:
            report.conflicts.append(target_status(target, "unmanaged"))
            continue
        if metadata.get("appId") != options.app_id:
            report.conflicts.append(target_status(target, "owner-mismatch"))
            continue

        shutil.rmtree(target_dir)
        report.removed.append(result)

    return report
