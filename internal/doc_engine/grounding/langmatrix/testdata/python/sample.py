"""Grounding fixture for the depth matrix."""
import asyncio
import os


# Service settings loaded from the environment.
class Config:
    """Service settings loaded from the environment."""

    def __init__(self):
        self.addr = os.environ.get("SAMPLE_ADDR", "localhost")
        self.port = int(os.getenv("SAMPLE_PORT", "8080"))


# Format a greeting for name; raises on empty input.
def format_greeting(name):
    """Format a greeting for name."""
    if not name:
        raise ValueError("greet: empty name")
    return "hello " + name


# Greet one name; keeps a call edge from greet_all.
async def greet_one(name):
    """Greet one name."""
    return format_greeting(name)


# Greet every name concurrently with asyncio.
async def greet_all(names):
    """Greet every name."""
    return await asyncio.gather(*[greet_one(n) for n in names])


# Test-looking function exercising format_greeting.
def test_format_greeting():
    """Exercise format_greeting."""
    assert format_greeting("ada") == "hello ada"
