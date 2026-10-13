"""Offline wire validation only; never treat a typed Scope as authorization proof."""
from datetime import datetime
import math
import re

from jsonschema import Draft202012Validator, FormatChecker


def formats():
    checker = FormatChecker()

    @checker.checks("date-time")
    def timestamp(value):
        if not isinstance(value, str):
            return True
        if not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{0,8}[1-9])?Z", value):
            return False
        try:
            datetime.fromisoformat(value.replace("Z", "+00:00"))
            return True
        except ValueError:
            return False
    return checker


def validator(spec, name):
    return Draft202012Validator({"$ref": "#/components/schemas/" + name, "components": spec["components"]},
                               format_checker=formats())


def finite_values(value):
    if isinstance(value, float) and not math.isfinite(value):
        raise ValueError("Non-finite remote number")
    if isinstance(value, dict):
        for child in value.values():
            finite_values(child)
    elif isinstance(value, list):
        for child in value:
            finite_values(child)


def validate(spec, name, value):
    finite_values(value)
    validator(spec, name).validate(value)
    context = value.get("context") if isinstance(value, dict) else None
    if context:
        deadline = datetime.fromisoformat(context["deadline"].replace("Z", "+00:00"))
        expiry = datetime.fromisoformat(context["scope"]["expires_at"].replace("Z", "+00:00"))
        if deadline > expiry:
            raise ValueError("Invocation deadline exceeds access scope expiry")
