"""Actual control DTO union checks; never query a ledger or cancel backend work."""
import copy
import json
from pathlib import Path
import subprocess
import tempfile

from build_remote_contract import build,operations
from canonical_content import canonical
from check_parser_wire_mapping import invoke,PROJECT,fixture_cases
from check_retrieval_wire_mapping import fixtures as retrieval_fixtures
from check_index_wire_mapping import fixtures as index_fixtures
from check_blob_wire_mapping import fixtures as blob_fixtures
from check_generation_wire_mapping import fixtures as generation_fixtures
from remote_wire_validation import validate


def result_values():
    values={"parser_parse":fixture_cases()[0]["response"]["result"]}
    for case in retrieval_fixtures()+index_fixtures():values[case["operation"]]=copy.deepcopy(case["response"]["result"])
    blobs=blob_fixtures();values["blob_store_put"]=copy.deepcopy(blobs[0]["response"]["result"])
    values["blob_store_open"]={"blob":copy.deepcopy(values["blob_store_put"])}
    values["blob_store_delete"]={"completed":True}
    values["composite_ingest_search_search"]=copy.deepcopy(values["retriever_search"])
    values["generator_generate"]=copy.deepcopy(generation_fixtures()[0]["response"][-1]["result"])
    if set(values)!={capability+"_"+operation for capability,operation,*_ in operations()}:raise AssertionError("Incomplete business result inventory")
    return values


def fixtures():
    context=copy.deepcopy(retrieval_fixtures()[0]["request"]["context"])
    context.update(request_id="control-r",invocation_id="control-i",module_instance_id="module-a")
    context["scope"]["action"]="invocation.status"
    request={"context":context,"input":{"target_invocation_id":"target-i"}}
    cases=[]
    for operation,value in result_values().items():
        expected={"operation":operation,"target_request_id":"r"}
        if operation=="generator_generate":expected.update(target_profile_digest=value["profile_digest"],target_profile_version=value["profile_version"])
        result={"target_invocation_id":"target-i","state":"succeeded","result":{"operation":operation,"value":value}}
        cases.append({"operation":"control_status","request":copy.deepcopy(request),"response":{"request_id":"control-r","invocation_id":"control-i","result":result},"expectation":expected})
    for state in ("running","uncertain","cancel_requested","cancelled","failed"):
        result={"target_invocation_id":"target-i","state":state}
        if state=="failed":result["error"]={"code":"internal","safe_message":"Internal failure","retryable":False,"request_id":"r"}
        cases.append({"operation":"control_status","request":copy.deepcopy(request),"response":{"request_id":"control-r","invocation_id":"control-i","result":result},"expectation":{"operation":"parser_parse","target_request_id":"r"}})
    for state in ("cancel_requested","already_terminal"):
        cancel=copy.deepcopy(request);cancel["context"]["scope"]["action"]="invocation.cancel"
        cases.append({"operation":"control_cancel","request":cancel,"response":{"request_id":"control-r","invocation_id":"control-i","result":{"target_invocation_id":"target-i","state":state}}})
    return cases


def run():
    spec=build();cases=fixtures()
    with tempfile.TemporaryDirectory() as directory:
        executable=Path(directory)/"control-inspect.exe"
        subprocess.run([str(PROJECT/".tools/go/bin/go.exe"),"build","-o",str(executable),"./cmd/architecture-parser-wire"],cwd=PROJECT,check=True,capture_output=True,timeout=60)
        for case in cases:
            name=case["operation"];validate(spec,name+"_Request",case["request"]);validate(spec,name+"_Response",case["response"])
            result=invoke(executable,case)
            if result.returncode:raise AssertionError("Control mapping failed: "+name+": "+result.stderr)
            output=json.loads(result.stdout);validate(spec,name+"_Request",output["request"]);validate(spec,name+"_Response",output["response"])
            expected={key:case[key] for key in ("request","response")}
            if canonical(result.stdout.encode())!=canonical(json.dumps(expected,ensure_ascii=False).encode()):raise AssertionError("Control fields changed")
        negatives=[]
        for mutate in (lambda c:c["response"]["result"].update(target_invocation_id="foreign"),
                       lambda c:c["response"]["result"]["result"].update(operation="generator_generate"),
                       lambda c:c["request"]["input"].update(target_invocation_id="control-i"),
                       lambda c:c["response"]["result"].update(state="running")):
            case=copy.deepcopy(cases[0]);mutate(case);negatives.append(case)
        failed=copy.deepcopy(next(c for c in cases if c["response"]["result"]["state"]=="failed"))
        failed["response"]["result"]["error"]["request_id"]="control-r";negatives.append(failed)
        cancelled=copy.deepcopy(cases[-1]);cancelled["response"]["result"]["state"]="cancelled";negatives.append(cancelled)
        generated=copy.deepcopy(next(c for c in cases if c.get("expectation",{}).get("operation")=="generator_generate"))
        generated["expectation"]["target_profile_version"]=1;negatives.append(generated)
        for case in negatives:
            if invoke(executable,case).returncode==0:raise AssertionError("Invalid control correlation/state/result accepted")
    print("PASS: 22 actual control status/cancel cases covering all 15 business result variants, 6 states, 2 cancellation responses, independent schemas and 7 invalid target/type/state/trace rejections")
    print("No ledger, current authorization, operation-specific result validation, real cancellation or confirmed backend termination proof")


if __name__=="__main__":run()
