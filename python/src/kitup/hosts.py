import json
from pathlib import Path

from ._hosts_generated import DEFAULT_HOSTS_SPEC_JSON
from .types import Host, HostSpec


def load_host_spec(hosts_file: str | None = None) -> HostSpec:
    raw = json.loads(
        Path(hosts_file).read_text() if hosts_file else DEFAULT_HOSTS_SPEC_JSON
    )
    return HostSpec(
        schema_version=raw["schemaVersion"],
        hosts=[
            Host(
                id=item["id"],
                display_name=item["displayName"],
                aliases=item.get("aliases", []),
                project_skills_dirs=item["projectSkillsDirs"],
                user_skills_dirs=item["userSkillsDirs"],
                detect=item["detect"],
                status=item["status"],
                notes=item.get("notes", []),
            )
            for item in raw["hosts"]
        ],
    )
