from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass, field
from enum import Enum
from typing import AsyncIterator, Dict, List, Optional, Protocol


class Status(str, Enum):
    PENDING = "pending"
    RUNNING = "running"
    DONE = "done"


class Repository(Protocol):
    async def get(self, id: str) -> Optional[Processor]:
        ...

    async def save(self, entity: Processor) -> None:
        ...


@dataclass
class Config:
    dsn: str
    timeout: float = 30.0
    retries: int = 3
    meta: Dict[str, str] = field(default_factory=dict)


@dataclass
class Processor:
    name: str
    status: Status = Status.PENDING
    config: Optional[Config] = None
    _cache: Dict[str, str] = field(default_factory=dict, repr=False)

    def __post_init__(self) -> None:
        if not self.name:
            raise ValueError("name required")

    @property
    def is_done(self) -> bool:
        return self.status == Status.DONE

    def process(self) -> bool:
        self.status = Status.RUNNING
        return True

    @classmethod
    def from_json(cls, raw: str) -> Processor:
        data = json.loads(raw)
        return cls(name=data["name"])

    @staticmethod
    def validate(name: str) -> bool:
        return len(name) > 0

    async def run_async(self, items: List[str]) -> AsyncIterator[str]:
        for it in items:
            await asyncio.sleep(0)
            yield it.upper()

    def substitute(self, text: str) -> str:
        # Use simple template to avoid triple-quote pitfalls
        return text.replace("{{key}}", self.name)

    def save_state(self, path: str) -> None:
        with open(path, "w", encoding="utf-8") as fh:
            json.dump({"name": self.name, "status": self.status.value}, fh)


class Service:
    def __init__(self, repo: Repository, config: Config) -> None:
        self.repo = repo
        self.config = config
        self._workers: List[Processor] = []

    def add(self, p: Processor) -> None:
        self._workers.append(p)

    async def execute(self, id: str) -> Processor:
        proc = await self.repo.get(id)
        if proc is None:
            raise KeyError(id)
        proc.process()
        await self.repo.save(proc)
        return proc

    def health(self) -> Dict[str, int]:
        return {"workers": len(self._workers), "retries": self.config.retries}


def load_config(path: str) -> Config:
    with open(path, encoding="utf-8") as fh:
        return Config(**json.load(fh))
