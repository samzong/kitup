from dataclasses import dataclass, field


class KitupError(Exception):
    pass


@dataclass(frozen=True)
class Host:
    id: str
    display_name: str
    project_skills_dirs: list[str]
    user_skills_dirs: list[str]
    detect: list[str]
    status: str
    aliases: list[str] = field(default_factory=list)
    notes: list[str] = field(default_factory=list)


@dataclass(frozen=True)
class HostSpec:
    hosts: list[Host]
    schema_version: int = 1


INSTALL_UX = {
    "skill_use": "skill",
    "install_use": "install",
    "scope_prompt": "Scope (user/project)",
    "agents_prompt": "Agents (numbers, ids, comma-separated, empty cancels)",
}
