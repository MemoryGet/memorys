"""Independent Python reference for AKOS-CJSON/1; domain validation is separate."""
import hashlib
import json

from check_config import check_depth


class Number(str):
    pass


def pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError("duplicate canonical JSON key")
        result[key] = value
    return result


def reject_number(_value):
    raise ValueError("invalid canonical JSON number")


def number(value):
    if len(value) > 1024:
        raise ValueError("number token exceeds limit")
    negative = value.startswith("-")
    value = value.lstrip("-")
    components = value.lower().split("e")
    exponent = int(components[1]) if len(components) == 2 else 0
    if abs(exponent) > 10000:
        raise ValueError("number exponent exceeds limit")
    coefficient = components[0]
    if "." in coefficient:
        exponent -= len(coefficient.split(".")[1])
        coefficient = coefficient.replace(".", "")
    if len(coefficient) > 256:
        raise ValueError("number mantissa exceeds limit")
    coefficient = coefficient.lstrip("0")
    if not coefficient:
        return "0"
    trimmed = coefficient.rstrip("0")
    exponent += len(coefficient) - len(trimmed)
    if abs(exponent) > 10000:
        raise ValueError("normalized exponent exceeds limit")
    return ("-" if negative else "") + trimmed + "e" + str(exponent)


def encode(value):
    if isinstance(value, Number):
        return number(value)
    if isinstance(value, str):
        value.encode("utf-8", errors="strict")
        return json.dumps(value, ensure_ascii=False)
    if isinstance(value, dict):
        return "{" + ",".join(encode(key) + ":" + encode(value[key]) for key in sorted(value)) + "}"
    if isinstance(value, list):
        return "[" + ",".join(encode(item) for item in value) + "]"
    if value is None:
        return "null"
    return "true" if value else "false"


def canonical(data):
    if len(data) > 4 << 20:
        raise ValueError("canonical JSON input exceeds limit")
    text = data.decode("utf-8", errors="strict")
    check_depth(text)
    value = json.loads(text, object_pairs_hook=pairs, parse_int=Number,
                       parse_float=Number, parse_constant=reject_number)
    output = encode(value).encode("utf-8")
    if len(output) > 4 << 20:
        raise ValueError("canonical JSON output exceeds limit")
    return output


def digest(prefix, data):
    return hashlib.sha256(prefix.encode("ascii") + data).hexdigest()


def frozen(data):
    encoded = canonical(data)
    payload = json.loads(encoded, object_pairs_hook=pairs, parse_int=Number, parse_float=Number)
    if set(payload) != {"format", "blocks", "blobs"} or payload["format"] != "akos.freeze/1":
        raise ValueError("invalid frozen envelope")
    if not isinstance(payload["blocks"], list) or not payload["blocks"] or not isinstance(payload["blobs"], list):
        raise ValueError("invalid frozen arrays")
    blocks_hash = digest("AKOS-BLOCKS/1\n", encode(payload["blocks"]).encode("utf-8"))
    header = {"format": "akos.freeze/1", "blocks_hash": blocks_hash, "blobs": payload["blobs"]}
    return {"format": "akos.freeze/1", "blocks_hash": blocks_hash,
            "content_hash": digest("AKOS-CONTENT/1\n", encode(header).encode("utf-8"))}
