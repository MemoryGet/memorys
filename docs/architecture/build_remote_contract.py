"""Generate provider-neutral remote module HTTP design; not a running service."""
import argparse
import copy
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent
OUTPUT = ROOT / "openapi/modules.v1.json"
TEXT = {"type": "string", "minLength": 1}
DATE = {"type": "string", "format": "date-time", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]{0,8}[1-9])?Z$"}
UINT = {"type": "integer", "minimum": 0}
POSITIVE = {"type": "integer", "minimum": 1}
DIGEST = {"type": "string", "pattern": "^[0-9a-f]{64}$"}


def ref(name):
    return {"$ref": "#/components/schemas/" + name}


def obj(properties, required=None):
    return {"type": "object", "additionalProperties": False, "properties": properties,
            "required": list(properties) if required is None else required}


def array(name, minimum=0, maximum=100000):
    return {"type": "array", "minItems": minimum, "maxItems": maximum, "items": ref(name)}


def schemas():
    result = copy.deepcopy(json.loads((ROOT / "openapi/partner.v1.json").read_text(encoding="utf-8"))["components"]["schemas"])
    result["Scope"] = obj({"scope_id": ref("ID"), "subject_id": ref("ID"), "action": TEXT, "space_id": ref("ID"),
                           "kb_ids": array("ID"), "policy_version": POSITIVE, "expires_at": DATE})
    result["ModuleContext"] = obj({"request_id": ref("ID"), "invocation_id": ref("ID"), "module_instance_id": ref("ID"),
                                  "contract_version": {"const": "1.0"}, "deadline": DATE, "profile_digest": DIGEST, "scope": ref("Scope")})
    result["Diagnostic"] = obj({"code": TEXT, "safe_message": {"type": "string"}, "severity": {"enum": ["info", "warning", "error"]}})
    result["BoundingBox"]["items"]["x-akos-numeric-representation"] = "float64-shortest-roundtrip"
    block = copy.deepcopy(result["Block"])
    block["properties"].pop("revision_id"); block["required"].remove("revision_id")
    block["properties"]["order"] = UINT; block["required"].append("order")
    result["ParsedBlock"] = block
    result["ParseInput"] = obj({"draft_id": ref("ID"), "input": ref("BlobRef"), "format": TEXT,
                                "options": obj({"languages": {"type": "array", "items": TEXT}, "enable_ocr": {"type": "boolean"}})})
    result["ParseResult"] = obj({"blocks": array("ParsedBlock"), "diagnostics": array("Diagnostic"), "parser_version": TEXT})
    result["BlobPutInput"] = obj({"expected_sha256": DIGEST, "size": UINT, "media_type": TEXT, "idempotency_key": TEXT})
    result["BlobReadInput"] = obj({"blob": ref("BlobRef")})
    result["EmbeddingSpec"] = copy.deepcopy(json.loads((ROOT / "schemas/deployment-config.v1.schema.json").read_text(encoding="utf-8"))["$defs"]["indexPolicy"]["properties"]["embedding_spec"])
    result["Vector"] = {"type": "array", "minItems": 1, "maxItems": 65536,
                         "items": {"type": "number", "minimum": -3.4028234663852886e38, "maximum": 3.4028234663852886e38,
                                   "x-akos-numeric-representation": "float32-round-nearest-ties-even"}}
    result["EmbedInput"] = obj({"texts": {"type": "array", "minItems": 1, "maxItems": 10000, "items": {"type": "string"}}, "spec": ref("EmbeddingSpec")})
    result["EmbedResult"] = obj({"vectors": array("Vector", 1, 10000), "spec": ref("EmbeddingSpec")})
    result["BlockSpan"] = obj({"block_id": ref("ID"), "char_start": UINT, "char_end": UINT}, ["block_id"])
    result["BlockSpan"]["dependentRequired"] = {"char_start": ["char_end"], "char_end": ["char_start"]}
    result["Chunk"] = obj({"id": ref("ID"), "revision_id": ref("ID"), "spans": array("BlockSpan", 1), "text": {"type": "string"},
                           "profile_digest": DIGEST, "content_hash": DIGEST})
    result["Generation"] = obj({"id": ref("ID"), "kb_id": ref("ID"), "revision_id": ref("ID"), "channel": {"enum": ["keyword", "vector", "graph"]},
                                "profile_digest": DIGEST, "backend_epoch_id": ref("ID"), "embedding_spec": ref("EmbeddingSpec")},
                               ["id", "kb_id", "revision_id", "channel", "profile_digest", "backend_epoch_id"])
    result["Generation"]["allOf"] = [{"if": {"properties": {"channel": {"const": "vector"}}}, "then": {"required": ["embedding_spec"]}}]
    target = {"generation_set_id": ref("ID"), "generations": array("Generation", 1), "operation_id": ref("ID"),
              "attempt_id": ref("ID"), "fencing_token": POSITIVE, "chunks": array("Chunk", 1)}
    result["IndexPrepareInput"] = obj(dict(target, vectors=array("Vector")))
    result["CompositePrepareInput"] = obj(dict(target, projection_format_version={"type": "string", "pattern": "^[1-9][0-9]*\\.[0-9]+$"}))
    result_schemas(result)
    return result


def result_schemas(result):
    result["GenerationReadiness"] = obj({"generation_id": ref("ID"), "channel": {"enum": ["keyword", "vector", "graph"]},
                                         "backend_epoch_id": ref("ID"), "chunk_count": UINT, "embedding_spec_digest": DIGEST},
                                        ["generation_id", "channel", "backend_epoch_id", "chunk_count"])
    result["GenerationReadiness"]["allOf"] = [{"if": {"properties": {"channel": {"const": "vector"}}}, "then": {"required": ["embedding_spec_digest"]}}]
    result["ReadinessProof"] = obj({"generation_set_id": ref("ID"), "attempt_id": ref("ID"), "content_digest": DIGEST,
                                    "profile_digest": DIGEST, "generations": array("GenerationReadiness", 1)})
    result["RemoteGenerationBinding"] = obj({"generation_id": ref("ID"), "opaque_backend_ref": TEXT})
    result["BackendBinding"] = obj({"platform_resource_type": TEXT, "platform_resource_id": ref("ID"), "module_instance_id": ref("ID"),
                                    "backend_id": TEXT, "generation_id": ref("ID"), "purpose": TEXT},
                                   ["platform_resource_type", "platform_resource_id", "module_instance_id", "backend_id", "purpose"])
    result["Generation"]["properties"].update(state={"type": "string"}, backend_binding=ref("BackendBinding"), readiness=ref("ReadinessProof"))
    result["Generation"]["required"].append("state")
    result["PrepareResult"] = obj({"proof": ref("ReadinessProof"), "bindings": array("RemoteGenerationBinding", 1)})
    result["VerifyInput"] = obj({"proof": ref("ReadinessProof")})
    result["DeleteInput"] = obj({"generation_id": ref("ID"), "chunk_ids": array("ID"), "mode": {"enum": ["generation", "chunks"]}})
    result["DeleteInput"]["allOf"] = [{"if": {"properties": {"mode": {"const": "generation"}}},
        "then": {"properties": {"chunk_ids": {"maxItems": 0}}}, "else": {"properties": {"chunk_ids": {"minItems": 1}}}}]
    result["Ack"] = obj({"completed": {"const": True}})
    result["PublicationFilter"] = obj({"space_id": ref("ID"), "kb_ids": array("ID"), "revision_ids": array("ID"), "generation_ids": array("ID"), "snapshot_digest": DIGEST})
    result["Query"] = obj({"text": TEXT, "vector": ref("Vector"), "embedding_spec": ref("EmbeddingSpec")}, ["text"])
    result["Query"]["dependentRequired"] = {"vector": ["embedding_spec"], "embedding_spec": ["vector"]}
    result["RetrieveInput"] = obj({"query": ref("Query"), "publication": ref("PublicationFilter"), "generations": array("Generation", 1),
                                   "backend_epoch_id": ref("ID"), "route_id": ref("ID"), "channels": {"type": "array", "minItems": 1, "uniqueItems": True,
                                     "items": {"enum": ["keyword", "vector", "graph"]}}, "limit": {"type": "integer", "minimum": 1, "maximum": 1000}})
    result["FusionOrigin"] = obj({"route_id":ref("ID"),"retriever_id":ref("ID"),"rank":{"type":"integer","minimum":1,"maximum":1000},"generation_ids":array("ID",1)})
    result["Candidate"] = obj({"citation": ref("Citation"), "chunk_id": ref("ID"), "route_id": ref("ID"), "retriever_id": ref("ID"), "kb_id": ref("ID"),
                               "rank": POSITIVE, "score": {"type": "number", "x-akos-numeric-representation": "float64-shortest-roundtrip"}, "score_kind": TEXT, "generation_ids": array("ID", 1), "text": {"type": "string"}, "fusion_origins":array("FusionOrigin",1,32)}, ["citation","chunk_id","route_id","retriever_id","kb_id","rank","score","score_kind","generation_ids","text"])
    result["RawCandidate"] = {"allOf":[ref("Candidate"),{"not":{"required":["fusion_origins"]}}]}
    result["RetrieveResult"] = obj({"candidates": array("RawCandidate", 0, 1000)})
    result["RerankInput"] = obj({"query": TEXT, "candidates": array("Candidate", 0, 1000), "limit": {"type": "integer", "minimum": 1, "maximum": 1000}})
    result["RerankResult"] = obj({"evidence": array("Evidence", 0, 1000)})
    result["GenerateInput"] = obj({"question": TEXT, "evidence": array("Evidence", 1, 1000),
                                   "budget": obj({"deadline": DATE, "max_output_tokens": {"type": "integer", "minimum": 1, "maximum": 32768},
                                                  "max_extra_rounds": {"type": "integer", "minimum": 0, "maximum": 2}})})
    result["RemoteError"] = obj({"request_id": ref("ID"), "invocation_id": ref("ID"), "error": ref("Error")})
    result["BlobTransferSummary"] = obj({"blob": ref("BlobRef")})


def operations():
    return [("parser", "parse", "ParseInput", "ParseResult", False),
            ("blob_store", "put", "BlobPutInput", "BlobRef", True), ("blob_store", "open", "BlobReadInput", None, False),
            ("blob_store", "delete", "BlobReadInput", "Ack", True), ("embedder", "embed", "EmbedInput", "EmbedResult", False),
            ("index_writer", "prepare", "IndexPrepareInput", "PrepareResult", True), ("index_writer", "verify", "VerifyInput", "Ack", False),
            ("index_writer", "delete", "DeleteInput", "Ack", True), ("retriever", "search", "RetrieveInput", "RetrieveResult", False),
            ("composite_ingest_search", "prepare_projection", "CompositePrepareInput", "PrepareResult", True),
            ("composite_ingest_search", "verify", "VerifyInput", "Ack", False), ("composite_ingest_search", "delete", "DeleteInput", "Ack", True),
            ("composite_ingest_search", "search", "RetrieveInput", "RetrieveResult", False),
            ("reranker", "rerank", "RerankInput", "RerankResult", False), ("generator", "generate", "GenerateInput", "AnswerEvent", False)]


def build():
    definitions, paths, invocation_results = schemas(), {}, []
    headers = [{"name": "X-AKOS-Access-Grant", "in": "header", "required": True, "schema": TEXT}]
    for capability, operation, input_type, output_type, mutation in operations():
        name = capability + "_" + operation
        request = name + "_Request"
        definitions[request] = obj({"context": ref("ModuleContext"), "input": ref(input_type)})
        body = {"application/json": {"schema": ref(request)}}
        if name == "blob_store_put":
            body = {"multipart/form-data": {"schema": obj({"metadata": ref(request), "file": {"type": "string", "format": "binary"}}),
                    "encoding": {"metadata": {"contentType": "application/json"}, "file": {"contentType": "application/octet-stream"}}}}
        if output_type is None:
            response = {"application/octet-stream": {"schema": {"type": "string", "format": "binary"}}}
        elif capability == "generator":
            response = {"text/event-stream": {"schema": {"type": "string"}, "x-event-data-schema": ref("AnswerEvent")}}
        else:
            definitions[name + "_Response"] = obj({"request_id": ref("ID"), "invocation_id": ref("ID"), "result": ref(output_type)})
            response = {"application/json": {"schema": ref(name + "_Response")}}
        status_type = "BlobTransferSummary" if output_type is None else ("AskResult" if capability == "generator" else output_type)
        invocation_results.append(obj({"operation": {"const": name}, "value": ref(status_type)}))
        paths[f"/modules/v1/{capability}/{operation}"] = {"post": {"operationId": name, "parameters": headers,
            "x-mutation": mutation, "requestBody": {"required": True, "content": body},
            "responses": {"200": {"description": "Completed operation; stream terminal rules apply", "content": response,
                                    "headers": {"X-AKOS-Request-Id": {"schema": ref("ID")}, "X-AKOS-Invocation-Id": {"schema": ref("ID")}}},
                          "default": {"description": "Safe module error; never a raw provider error", "content": {"application/json": {"schema": ref("RemoteError")}}}}}}
    definitions["InvocationResult"] = {"oneOf": invocation_results}
    definitions["InvocationStatus"] = obj({"target_invocation_id": ref("ID"), "state": {"enum": ["running", "uncertain", "succeeded", "failed", "cancel_requested", "cancelled"]},
                                           "result": ref("InvocationResult"), "error": ref("Error")}, ["target_invocation_id", "state"])
    definitions["InvocationStatus"]["allOf"] = [
        {"if": {"properties": {"state": {"const": "succeeded"}}}, "then": {"required": ["result"], "not": {"required": ["error"]}},
         "else": {"not": {"required": ["result"]}}},
        {"if": {"properties": {"state": {"const": "failed"}}}, "then": {"required": ["error"]}, "else": {"not": {"required": ["error"]}}}]
    definitions["CancelResult"] = obj({"target_invocation_id": ref("ID"), "state": {"enum": ["cancel_requested", "already_terminal"]}})
    for operation, output in [("status", "InvocationStatus"), ("cancel", "CancelResult")]:
        name = "control_" + operation
        definitions[name + "_Request"] = obj({"context": ref("ModuleContext"), "input": obj({"target_invocation_id": ref("ID")})})
        definitions[name + "_Response"] = obj({"request_id": ref("ID"), "invocation_id": ref("ID"), "result": ref(output)})
        paths["/modules/v1/control/" + operation] = {"post": {"operationId": name, "parameters": headers,
            "requestBody": {"required": True, "content": {"application/json": {"schema": ref(name + "_Request")}}},
            "responses": {"200": {"description": "Authorized invocation state", "content": {"application/json": {"schema": ref(name + "_Response")}}},
                          "default": {"description": "Unknown or unauthorized invocation is hidden", "content": {"application/json": {"schema": ref("RemoteError")}}}}}}
    return {"openapi": "3.1.1", "info": {"title": "AKOS remote module contracts", "version": "1.0.0",
             "description": "Architecture contract; no handlers, service authentication or grant verifier implemented."},
            "jsonSchemaDialect": "https://json-schema.org/draft/2020-12/schema", "paths": paths,
            "security": [{"ServiceAuth": []}], "components": {"securitySchemes": {"ServiceAuth": {"type": "http", "scheme": "bearer"}}, "schemas": definitions}}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    content = json.dumps(build(), ensure_ascii=False, indent=2) + "\n"
    if args.check:
        if not OUTPUT.exists() or OUTPUT.read_text(encoding="utf-8") != content:
            raise SystemExit("Remote module contract is stale")
        print("PASS: remote contract matches design source")
    else:
        OUTPUT.write_text(content, encoding="utf-8")
        print("Generated", len(build()["paths"]), "remote operations")
