"""Actual Go safe-error projection; source error text is deliberately not preserved."""
import copy
import json
from pathlib import Path
import subprocess
import tempfile

from build_remote_contract import build
from check_parser_wire_mapping import invoke,PROJECT
from remote_wire_validation import validate


def run():
    spec=build()
    fixture={"operation":"remote_error","expectation":{"request_id":"r","invocation_id":"i"},
             "response":{"request_id":"r","invocation_id":"i","error":{"code":"dependency_unavailable","safe_message":"SECRET provider detail","retryable":True,"request_id":"r"}}}
    with tempfile.TemporaryDirectory() as directory:
        executable=Path(directory)/"error-inspect.exe"
        subprocess.run([str(PROJECT/".tools/go/bin/go.exe"),"build","-o",str(executable),"./cmd/architecture-parser-wire"],cwd=PROJECT,check=True,capture_output=True,timeout=60)
        for code,message,retryable in (("dependency_unavailable","Dependency unavailable",True),("forbidden","Access denied",False)):
            case=copy.deepcopy(fixture);case["response"]["error"]["code"]=code
            validate(spec,"RemoteError",case["response"])
            result=invoke(executable,case)
            if result.returncode:raise AssertionError("Go safe-error mapping failed")
            response=json.loads(result.stdout)["response"];validate(spec,"RemoteError",response)
            if response!={"request_id":"r","invocation_id":"i","error":{"code":code,"safe_message":message,"retryable":retryable,"request_id":"r"}}:
                raise AssertionError("Safe projection or error identity changed")
            if "SECRET" in result.stdout:raise AssertionError("Provider detail leaked")
        codes=spec["components"]["schemas"]["Error"]["properties"]["code"]["enum"]
        retry_advisory={"conflict","publication_changed","cursor_expired","rate_limited","deadline_exceeded","dependency_unavailable"}
        for code in codes:
            case=copy.deepcopy(fixture);case["response"]["error"]["code"]=code
            validate(spec,"RemoteError",case["response"])
            result=invoke(executable,case)
            if result.returncode:raise AssertionError("Schema error code missing Go mapping: "+code)
            response=json.loads(result.stdout)["response"];validate(spec,"RemoteError",response)
            if response["request_id"]!="r" or response["invocation_id"]!="i" or response["error"]["request_id"]!="r" or response["error"]["code"]!=code:
                raise AssertionError("Error identity changed: "+code)
            if not response["error"]["safe_message"] or "SECRET" in result.stdout or response["error"]["retryable"]!=(code in retry_advisory):
                raise AssertionError("Unsafe projection or retry advisory: "+code)
        for mutate in (lambda x:x["response"].update(invocation_id="other"),lambda x:x["response"]["error"].update(request_id="other"),
                       lambda x:x["response"]["error"].update(cause="SECRET"),lambda x:x["response"]["error"].update(code="vendor_failure")):
            case=copy.deepcopy(fixture);mutate(case)
            result=invoke(executable,case)
            if result.returncode==0 or "SECRET" in result.stderr:raise AssertionError("Invalid or unsafe error accepted")
    print("PASS: actual Go safe-error projection for all 15 schema codes, independent before/after RemoteError schemas, 2 exact safe templates and 4 invalid identity/cause/code rejections")
    print("No provider classification, status ledger, HTTP policy, retry execution or authorization proof")


if __name__=="__main__":run()
