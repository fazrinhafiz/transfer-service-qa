"""Environment configuration.

The environment is chosen with TEST_ENV (default: local), which selects
config/environments/<TEST_ENV>.yaml. Individual values can be overridden
with environment variables, e.g. BASE_URL=http://staging:8080.
"""

import os
from dataclasses import dataclass
from functools import cache
from pathlib import Path

import yaml

ENVIRONMENTS_DIR = Path(__file__).parent / "environments"


@dataclass(frozen=True)
class Settings:
    env: str
    base_url: str
    timeout_seconds: float
    startup_timeout_seconds: float


@cache
def load_settings() -> Settings:
    env = os.getenv("TEST_ENV", "local")
    path = ENVIRONMENTS_DIR / f"{env}.yaml"
    if not path.exists():
        available = sorted(p.stem for p in ENVIRONMENTS_DIR.glob("*.yaml"))
        raise ValueError(f"Unknown TEST_ENV {env!r}; available: {available}")

    values = yaml.safe_load(path.read_text())
    return Settings(
        env=env,
        base_url=os.getenv("BASE_URL", values["base_url"]).rstrip("/"),
        timeout_seconds=float(os.getenv("TIMEOUT_SECONDS", values["timeout_seconds"])),
        startup_timeout_seconds=float(values["startup_timeout_seconds"]),
    )
