"""Check design catalogue schemas and REST/MCP shape mapping, not a running SDK."""
import json

from jsonschema import Draft202012Validator

from build_mcp_contract import OUTPUT, generate, serialized


def check_local_refs(schema):
    def walk(value):
        if isinstance(value, dict):
            if "$ref" in value:
                reference = value["$ref"]
                assert reference.startswith("#/$defs/"), reference
                assert reference[len("#/$defs/"):] in schema.get("$defs", {}), reference
            for child in value.values():
                walk(child)
        elif isinstance(value, list):
            for child in value:
                walk(child)
    walk(schema)


def run():
    assert OUTPUT.read_text(encoding="utf-8") == serialized(), "Stale MCP contract"
    catalogue = json.loads(OUTPUT.read_text(encoding="utf-8"))
    assert catalogue == generate()
    tools = {tool["name"]: tool for tool in catalogue["tools"]}
    assert len(tools) == len(catalogue["tools"]) == 4
    for tool in tools.values():
        for direction in ["inputSchema", "outputSchema"]:
            schema = tool[direction]
            Draft202012Validator.check_schema(schema)
            check_local_refs(schema)
    request = {"question": "产品怎么使用", "space_id": "space-1"}
    ask = Draft202012Validator(tools["knowledge_ask"]["inputSchema"])
    ask.validate(request)
    assert not ask.is_valid(dict(request, stream=True))
    assert not ask.is_valid(dict(request, user_id="spoofed"))
    error = {"code": "forbidden", "safe_message": "没有访问权限", "retryable": False, "request_id": "request-1"}
    for tool in tools.values():
        result_validator = Draft202012Validator(tool["outputSchema"])
        result_validator.validate(error)
        assert not result_validator.is_valid(dict(error, backend_id="private"))
    trace = {"request_id": "request-1", "profile_version": 1, "profile_digest": "a" * 64, "timings": {}, "degraded": []}
    successes = {
        "knowledge_search": dict(trace, evidence=[]),
        "knowledge_read": dict(trace, document_id="doc-1", revision_id="rev-1", blocks=[], citations=[]),
        "knowledge_ask": dict(trace, answer="证据不足", citations=[], status="insufficient_evidence"),
        "knowledge_feedback": {"request_id": "request-1", "feedback_id": "feedback-1", "state": "received"},
    }
    for name, result in successes.items():
        Draft202012Validator(tools[name]["outputSchema"]).validate(result)
        envelope = {"isError": False, "structuredContent": result,
                    "content": [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]}
        assert json.loads(envelope["content"][0]["text"]) == envelope["structuredContent"]
    assert tools["knowledge_feedback"]["annotations"]["readOnlyHint"] is False
    print("PASS: 4 generated tools, 8 bundled schemas, 4 success/error DTOs, rejected private error fields, text/structured fixtures, ask parameter restrictions")
    print("No MCP transport, OAuth, SDK behavior or live authorization was tested.")


if __name__ == "__main__":
    run()
