"""Actual Go request/event-data mappings and sequence rules; not SSE or generation."""
import copy
import json
from pathlib import Path
import subprocess
import tempfile

from build_remote_contract import build
from canonical_content import canonical
from check_parser_wire_mapping import invoke,PROJECT
from check_retrieval_wire_mapping import fixtures as retrieval_fixtures
from remote_wire_validation import validate


def fixtures():
    base=retrieval_fixtures()[2];context=copy.deepcopy(base["request"]["context"])
    context["module_instance_id"]="generator-a";context["scope"]["action"]="knowledge.ask"
    evidence=copy.deepcopy(base["response"]["result"]["evidence"])
    request={"context":context,"input":{"question":"设备怎么修？","evidence":evidence,"budget":{"deadline":context["deadline"],"max_output_tokens":1000,"max_extra_rounds":2}}}
    result={"request_id":"r","answer":"检查电源😀","citations":[evidence[0]["citation"]],"status":"answered","profile_version":9007199254740993,
            "profile_digest":context["profile_digest"],"timings":{"generate":1.25},"degraded":[{"capability":"graph","reason":"policy_disabled"}]}
    meta={"type":"meta","request_id":"r"}
    expectation={"profile_version":result["profile_version"],"max_events":20,"max_text_bytes":65536}
    answered=[meta,{"type":"answer_delta","text":result["answer"]},{"type":"citation","citation":evidence[0]["citation"]},{"type":"done","result":result}]
    insufficient=copy.deepcopy(result);insufficient.update(answer="证据不足",status="insufficient_evidence",citations=[])
    failed=[meta,{"type":"error","error":{"code":"dependency_unavailable","safe_message":"Dependency unavailable","retryable":True,"request_id":"r"}}]
    return [{"operation":"generator_generate","request":copy.deepcopy(request),"response":copy.deepcopy(events),"expectation":copy.deepcopy(expectation)} for events in (answered,[meta,{"type":"done","result":insufficient}],failed)]


def run():
    spec=build()
    with tempfile.TemporaryDirectory() as directory:
        executable=Path(directory)/"generation-inspect.exe"
        subprocess.run([str(PROJECT/".tools/go/bin/go.exe"),"build","-o",str(executable),"./cmd/architecture-parser-wire"],cwd=PROJECT,check=True,capture_output=True,timeout=60)
        for case in fixtures():
            validate(spec,"generator_generate_Request",case["request"])
            for event in case["response"]:validate(spec,"AnswerEvent",event)
            result=invoke(executable,case)
            if result.returncode:raise AssertionError("Go generation mapping failed: "+result.stderr)
            output=json.loads(result.stdout);validate(spec,"generator_generate_Request",output["request"])
            for event in output["response"]:validate(spec,"AnswerEvent",event)
            expected={"request":case["request"],"response":case["response"],"stream_outcome":"error" if case["response"][-1]["type"]=="error" else "done"}
            if canonical(result.stdout.encode())!=canonical(json.dumps(expected,ensure_ascii=False).encode()):raise AssertionError("Generation fields changed")
        mutations=[lambda x:x["response"].pop(),lambda x:x["response"].pop(0),lambda x:x["response"].append(copy.deepcopy(x["response"][-1])),
                   lambda x:x["response"][2]["citation"].update(block_id="outside-evidence"),
                   lambda x:x["response"][-1]["result"].update(answer="contradicting answer"),
                   lambda x:x["response"][-1]["result"].update(profile_version=1),
                   lambda x:x["request"]["input"]["budget"].update(deadline="2026-10-13T12:00:00Z")]
        for mutate in mutations:
            case=copy.deepcopy(fixtures()[0]);mutate(case)
            if invoke(executable,case).returncode==0:raise AssertionError("Invalid generation sequence/request accepted")
    print("PASS: actual Go generation request and all 5 event variants, 3 stream outcomes, exact >2^53 profile version, independent schemas and 7 rejected budget/sequence/reference/trace mutations")
    print("No model, SSE framing, current authorization, deadline enforcement, tokenizer or sink/network cancellation proof")


if __name__=="__main__":run()
