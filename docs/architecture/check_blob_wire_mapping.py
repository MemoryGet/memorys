"""Actual Go Blob DTO checks; no multipart, stream transport, storage or authorization."""
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
    context=copy.deepcopy(retrieval_fixtures()[1]["request"]["context"])
    context["module_instance_id"]="blob-a";context["scope"]["action"]="blob.put"
    blob={"id":"blob-1","sha256":"b"*64,"size":9007199254740993,"media_type":"application/pdf"}
    request={"context":context,"input":{"expected_sha256":blob["sha256"],"size":blob["size"],"media_type":blob["media_type"],"idempotency_key":"key-1"}}
    cases=[{"operation":"blob_store_put","request":request,"response":{"request_id":"r","invocation_id":"i","result":blob}}]
    for operation in ("open","delete"):
        read_context=copy.deepcopy(context);read_context["scope"]["action"]="blob."+operation
        case={"operation":"blob_store_"+operation,"request":{"context":read_context,"input":{"blob":copy.deepcopy(blob)}}}
        if operation=="delete":case["response"]={"request_id":"r","invocation_id":"i","result":{"completed":True}}
        cases.append(case)
    return cases


def run():
    spec=build()
    with tempfile.TemporaryDirectory() as directory:
        executable=Path(directory)/"blob-inspect.exe"
        subprocess.run([str(PROJECT/".tools/go/bin/go.exe"),"build","-o",str(executable),"./cmd/architecture-parser-wire"],cwd=PROJECT,check=True,capture_output=True,timeout=60)
        for case in fixtures():
            operation=case["operation"]
            validate(spec,operation+"_Request",case["request"])
            if "response" in case:validate(spec,operation+"_Response",case["response"])
            result=invoke(executable,case)
            if result.returncode:raise AssertionError("Go Blob mapping failed: "+operation)
            output=json.loads(result.stdout)
            validate(spec,operation+"_Request",output["request"])
            if "response" in case:validate(spec,operation+"_Response",output["response"])
            expected={key:case[key] for key in ("request","response") if key in case}
            if canonical(result.stdout.encode())!=canonical(json.dumps(expected,ensure_ascii=False).encode()):
                raise AssertionError("Blob fields changed: "+operation)
        for mutation in (lambda c:c["response"]["result"].update(size=100),
                         lambda c:c["response"].update(invocation_id="outside-call"),
                         lambda c:c["request"]["input"].update(download_url="https://invalid.example/private")):
            bad=copy.deepcopy(fixtures()[0]);mutation(bad)
            if invoke(executable,bad).returncode==0:raise AssertionError("Invalid Blob request/result accepted")
    print("PASS: 3 actual Go Blob put/open/delete metadata mappings, exact >2^53 size, independent before/after schemas and 3 invalid request/result rejections")
    print("No multipart or binary transport, storage, grant, current authorization, cancellation or deletion proof")


if __name__=="__main__":run()
