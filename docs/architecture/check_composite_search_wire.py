"""Composite search uses the same typed retrieval DTO; no composite backend executed."""
import copy
import json
from pathlib import Path
import subprocess
import tempfile

from build_remote_contract import build
from canonical_content import canonical
from check_parser_wire_mapping import invoke,PROJECT
from check_retrieval_wire_mapping import fixtures
from remote_wire_validation import validate


def run():
    case=copy.deepcopy(fixtures()[1]);case["operation"]="composite_ingest_search_search"
    case["request"]["context"]["module_instance_id"]="composite-a"
    case["response"]["result"]["candidates"][0]["retriever_id"]="composite-a"
    spec=build();name=case["operation"]
    validate(spec,name+"_Request",case["request"]);validate(spec,name+"_Response",case["response"])
    with tempfile.TemporaryDirectory() as directory:
        executable=Path(directory)/"composite-search.exe"
        subprocess.run([str(PROJECT/".tools/go/bin/go.exe"),"build","-o",str(executable),"./cmd/architecture-parser-wire"],cwd=PROJECT,check=True,capture_output=True,timeout=60)
        result=invoke(executable,case)
        if result.returncode:raise AssertionError("Composite search typed mapping failed")
        output=json.loads(result.stdout);validate(spec,name+"_Request",output["request"]);validate(spec,name+"_Response",output["response"])
        expected={key:case[key] for key in ("request","response")}
        if canonical(result.stdout.encode())!=canonical(json.dumps(expected,ensure_ascii=False).encode()):raise AssertionError("Composite search fields changed")
        bad=copy.deepcopy(case);bad["response"]["result"]["candidates"][0]["generation_ids"]=["foreign-generation"]
        if invoke(executable,bad).returncode==0:raise AssertionError("Foreign generation accepted")
    print("PASS: composite search actual shared Go retrieval mapping, independent request/result schemas and foreign generation rejection")
    print("No composite backend, hybrid score behavior, filtering, grant, permission or replacement proof")


if __name__=="__main__":run()
