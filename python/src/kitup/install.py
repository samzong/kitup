from pathlib import Path

from .hosts import detect_hosts, load_host_spec, resolve_hosts
from .types import BaseOptions, Host, Scope, TargetGroup


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
) -> tuple[list[TargetGroup], list[dict[str, str]], list[str]]:
    spec = load_host_spec(options.hosts_file)
    home = Path(options.home).expanduser() if options.home else Path.home()
    cwd = Path(options.cwd) if options.cwd else Path.cwd()
    selected: list[Host]
    errors: list[dict[str, str]]
    if agents in (None, "auto"):
        selected = detect_hosts(options, scope)
        errors = []
    else:
        selected, errors = resolve_hosts(agents, spec.hosts)

    by_target: dict[str, TargetGroup] = {}
    for host in selected:
        root = choose_scope_path(host, scope=scope, home=home, cwd=cwd)
        if root is None:
            errors.append(
                {
                    "hostId": host.id,
                    "skillName": skill_name,
                    "scope": scope,
                    "reason": "unsupported-scope",
                }
            )
            continue
        target_dir = str(root / skill_name)
        group = by_target.get(target_dir)
        if group is None:
            group = TargetGroup(skill_name=skill_name, target_dir=target_dir)
            by_target[target_dir] = group
        group.host_ids.append(host.id)

    targets = [by_target[path] for path in sorted(by_target)]
    detected_host_ids = [host_id for target in targets for host_id in target.host_ids]
    return targets, errors, detected_host_ids
