from __future__ import annotations

import json
from pathlib import Path


def write_install_metadata(
    target_dir: Path,
    *,
    app_id: str,
    skill_name: str,
    digest: str,
    source: str,
) -> None:
    payload = {
        "schemaVersion": 1,
        "appId": app_id,
        "skillName": skill_name,
        "source": source,
        "hash": digest,
    }
    (target_dir / ".kitup.json").write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")


def read_install_metadata(target_dir: Path) -> dict[str, object] | None:
    metadata_file = target_dir / ".kitup.json"
    if not metadata_file.exists():
        return None
    try:
        payload = json.loads(metadata_file.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    if not isinstance(payload, dict):
        return None
    return payload
