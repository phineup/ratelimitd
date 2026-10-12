"""Client for the ratelimitd rate limiting sidecar (standard library only)."""

from .client import (
    APIError,
    CheckResult,
    Client,
    Policy,
    Rule,
    RuleResult,
)

__all__ = ["APIError", "CheckResult", "Client", "Policy", "Rule", "RuleResult"]
__version__ = "0.1.0"
