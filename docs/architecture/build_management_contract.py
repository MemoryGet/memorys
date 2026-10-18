"""Generate the management API contract; application handlers and storage remain separately wired."""
import argparse
import copy
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent
OUTPUT = ROOT / "openapi/management.v1.json"
ID = {"$ref": "#/components/schemas/ID"}
VERSION = {"type": "integer", "minimum": 1}
EPOCH = {"type": "integer", "minimum": 0}
TEXT = {"type": "string", "maxLength": 10000}
NONEMPTY = dict(TEXT, minLength=1)
DATE = {"type": "string", "format": "date-time"}
NULL_ID = {"oneOf": [ID, {"type": "null"}]}


def ref(name):
    return {"$ref": "#/components/schemas/" + name}


def obj(properties, required=None):
    return {"type": "object", "additionalProperties": False,
            "properties": properties, "required": list(properties) if required is None else required}


def schemas():
    result = copy.deepcopy(json.loads((ROOT / "openapi/partner.v1.json").read_text(encoding="utf-8"))["components"]["schemas"])
    result["KnowledgeBase"] = obj({"id": ID, "space_id": ID, "name": NONEMPTY, "active_backend_epoch_id": NULL_ID, "row_version": VERSION, "policy_version": VERSION})
    result["Document"] = obj({"id": ID, "space_id": ID, "kb_id": ID, "title": TEXT, "active_revision_id": NULL_ID,
                              "active_generation_set_id": NULL_ID, "publication_epoch": EPOCH, "row_version": VERSION, "deleted": {"type": "boolean"}})
    draft_block = copy.deepcopy(result["Block"])
    draft_block["properties"].pop("revision_id")
    draft_block["required"].remove("revision_id")
    draft_block["properties"].update(draft_id=ID, row_version=VERSION)
    draft_block["required"].extend(["draft_id", "row_version"])
    result["DraftBlock"] = draft_block
    result["Draft"] = obj({"id": ID, "document_id": ID, "base_revision_id": NULL_ID, "row_version": VERSION,
                           "state": {"enum": ["draft", "submitted"]}, "blocks": {"type": "array", "items": ref("DraftBlock")}})
    result["Revision"] = obj({"id": ID, "document_id": ID, "parent_revision_id": NULL_ID, "content_hash": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
                              "blocks_hash": {"type": "string", "pattern": "^[0-9a-f]{64}$"}, "hash_format": {"const": "akos.freeze/1"},
                              "blobs": {"type": "array", "items": ref("BlobRef")},
                              "created_by": ID, "created_at": DATE, "blocks": {"type": "array", "minItems": 1, "items": ref("Block")}})
    result["Approval"] = obj({"id": ID, "revision_id": ID, "decision": {"enum": ["approve", "reject", "revoke"]}, "actor_id": ID,
                              "reason": TEXT, "content_hash": {"type": "string", "pattern": "^[0-9a-f]{64}$"}, "policy_version": VERSION, "row_version": VERSION, "decided_at": DATE})
    result["ApprovalRequest"] = obj({"id": ID, "revision_id": ID, "document_id": ID, "submitted_by": ID, "submitted_at": DATE,
                                     "state": {"enum": ["submitted", "approved", "rejected", "revoked"]}, "row_version": EPOCH,
                                     "content_hash": {"type": "string", "pattern": "^[0-9a-f]{64}$"}, "policy_version": VERSION})
    result["Job"] = obj({"id": ID, "space_id": ID, "resource_id": ID, "state": {"enum": ["queued", "running", "completed", "failed", "cancelled"]},
                         "stage": NONEMPTY, "attempt": EPOCH, "row_version": VERSION, "progress": {"type": "number", "minimum": 0, "maximum": 1}, "error": ref("Error")},
                        ["id", "space_id", "resource_id", "state", "stage", "attempt", "row_version", "progress"])
    result["PublicationOperation"] = obj({"operation_id": ID, "document_id": ID, "revision_id": ID, "approval_id": ID, "expected_epoch": EPOCH,
                                          "state": {"enum": ["preparing", "ready", "committed", "failed", "superseded"]}, "generation_set_id": NULL_ID, "job_id": ID},
                                         ["operation_id", "document_id", "revision_id", "approval_id", "expected_epoch", "state", "generation_set_id"])
    result["PublicationHistory"] = obj({"id": ID, "document_id": ID, "publication_epoch": EPOCH, "revision_id": NULL_ID,
                                        "action": {"enum": ["publish", "withdraw", "delete"]}, "actor_id": ID, "reason": TEXT, "occurred_at": DATE})
    result["Grant"] = obj({"kb_id": ID, "subject_id": ID, "role": {"enum": ["Admin", "Contributor", "Viewer"]}, "policy_version": VERSION})
    result["Feedback"] = obj({"id": ID, "space_id": ID, "kb_id": ID, "kind": result["FeedbackRequest"]["properties"]["kind"],
                              "comment": NONEMPTY, "state": result["FeedbackResult"]["properties"]["state"], "created_at": DATE})
    for name in ["KnowledgeBase", "Document", "PublicationHistory", "Feedback", "ApprovalRequest"]:
        result[name + "Page"] = obj({"items": {"type": "array", "items": ref(name)}, "next_cursor": NONEMPTY}, ["items"])
    requests = {
        "CreateKB": {"name": NONEMPTY},
        "SetGrant": {"role": {"enum": ["Admin", "Contributor", "Viewer", "none"]}, "expected_policy_version": VERSION},
        "CreateDraft": {"base_revision_id": NULL_ID, "expected_document_version": VERSION},
        "EditDraft": {"expected_version": VERSION, "blocks": {"type": "array", "maxItems": 100000, "items": ref("DraftBlock")}},
        "ParseDraft": {"expected_version": VERSION, "pipeline_profile_id": ID, "pipeline_profile_version": VERSION},
        "SubmitDraft": {"expected_version": VERSION},
        "DecideApproval": {"decision": {"enum": ["approve", "reject", "revoke"]}, "reason": TEXT, "expected_approval_version": EPOCH},
        "PublishDocument": {"revision_id": ID, "approval_id": ID, "expected_publication_epoch": EPOCH},
        "WithdrawDocument": {"reason": NONEMPTY, "expected_publication_epoch": EPOCH},
        "JobCommand": {"expected_job_version": VERSION}, "ProposeDraft": {"comment": NONEMPTY},
    }
    result.update({name: obj(fields) for name, fields in requests.items()})
    result["JobCommandResult"] = obj({"job_id": ID, "state": {"enum": ["accepted", "already_terminal", "not_cancellable"]}})
    result["UploadAccepted"] = obj({"document": ref("Document"), "draft": ref("Draft"), "job": ref("Job")})
    result["Submission"] = obj({"revision": ref("Revision"), "approval_request_id": ID})
    result["WithdrawResult"] = obj({"document_id": ID, "publication_epoch": EPOCH, "history_id": ID})
    result["DeleteAccepted"] = obj({"document_id": ID, "job_id": ID, "publication_epoch": EPOCH})
    result["GrantResult"] = obj({"kb_id": ID, "subject_id": ID, "role": {"enum": ["Admin", "Contributor", "Viewer", "none"]}, "policy_version": VERSION})
    result["ProposalAccepted"] = obj({"feedback_id": ID, "job_id": ID})
    return result


ROUTES = [
    ("get", "/spaces/{space_id}/knowledge-bases", "list_knowledge_bases", None, "KnowledgeBasePage", "200", "space.read"),
    ("post", "/spaces/{space_id}/knowledge-bases", "create_knowledge_base", "CreateKB", "KnowledgeBase", "201", "space.manage"),
    ("put", "/knowledge-bases/{kb_id}/grants/{subject_id}", "set_kb_grant", "SetGrant", "GrantResult", "200", "kb.manage_access"),
    ("post", "/knowledge-bases/{kb_id}/documents", "upload_document", "multipart-upload", "UploadAccepted", "202", "kb.edit"),
    ("get", "/knowledge-bases/{kb_id}/documents", "list_documents", None, "DocumentPage", "200", "kb.read"),
    ("get", "/documents/{id}", "get_document", None, "Document", "200", "document.view_metadata"),
    ("post", "/documents/{id}/drafts", "create_draft", "CreateDraft", "Draft", "201", "kb.edit"),
    ("get", "/drafts/{draft_id}", "get_draft", None, "Draft", "200", "kb.edit"),
    ("patch", "/drafts/{draft_id}", "edit_draft", "EditDraft", "Draft", "200", "kb.edit"),
    ("post", "/drafts/{draft_id}/source", "replace_draft_source", "multipart-replace", "Job", "202", "kb.edit"),
    ("post", "/drafts/{draft_id}/parse", "parse_draft", "ParseDraft", "Job", "202", "kb.edit"),
    ("post", "/drafts/{draft_id}/submit", "submit_draft", "SubmitDraft", "Submission", "201", "kb.edit"),
    ("get", "/knowledge-bases/{kb_id}/approval-requests", "list_approval_requests", None, "ApprovalRequestPage", "200", "kb.approve"),
    ("get", "/revisions/{revision_id}", "get_revision_for_review", None, "Revision", "200", "revision.review"),
    ("post", "/revisions/{revision_id}/approval-decisions", "decide_approval", "DecideApproval", "Approval", "201", "kb.approve"),
    ("post", "/documents/{id}/publications", "publish_document", "PublishDocument", "PublicationOperation", "202", "kb.publish"),
    ("post", "/documents/{id}/withdraw", "withdraw_document", "WithdrawDocument", "WithdrawResult", "200", "kb.publish"),
    ("delete", "/documents/{id}", "delete_document", None, "DeleteAccepted", "202", "kb.delete"),
    ("get", "/documents/{id}/history", "list_document_history", None, "PublicationHistoryPage", "200", "kb.read_history"),
    ("get", "/blobs/{blob_id}/content", "download_original", None, "binary", "200", "kb.download_original"),
    ("get", "/jobs/{id}", "get_job", None, "Job", "200", "resource.view_job"),
    ("get", "/publication-operations/{id}", "get_publication_operation", None, "PublicationOperation", "200", "kb.publish"),
    ("post", "/jobs/{id}/cancel", "cancel_job", "JobCommand", "JobCommandResult", "200", "resource.manage_job"),
    ("post", "/jobs/{id}/retry", "retry_job", "JobCommand", "JobCommandResult", "202", "resource.manage_job"),
    ("get", "/knowledge-bases/{kb_id}/feedback", "list_feedback", None, "FeedbackPage", "200", "kb.manage_feedback"),
    ("post", "/feedback/{id}/proposals", "create_feedback_proposal", "ProposeDraft", "ProposalAccepted", "202", "kb.manage_feedback"),
]


def operation(route):
    method, path, name, request, response, status, permission = route
    parameters = [{"name": key, "in": "path", "required": True, "schema": ID} for key in re.findall(r"\{([^}]+)\}", path)]
    if method != "get":
        parameters.append({"name": "Idempotency-Key", "in": "header", "required": True, "schema": {"type": "string", "minLength": 1, "maxLength": 200}})
    if method == "delete":
        parameters.append({"name": "expected_document_version", "in": "query", "required": True, "schema": VERSION})
    if response.endswith("Page"):
        parameters.extend([{"name": "cursor", "in": "query", "schema": {"type": "string", "minLength": 1, "maxLength": 4096}},
                           {"name": "page_size", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 100, "default": 20}}])
    content = {"application/octet-stream": {"schema": {"type": "string", "format": "binary"}}} if response == "binary" else {"application/json": {"schema": ref(response)}}
    result = {"operationId": name, "x-required-permission": permission, "parameters": parameters,
              "description": "Permission is enforced by the application service, not by this annotation. Resource ownership, CAS, publication and delegation checks remain mandatory.",
              "responses": {status: {"description": "Command/query result", "content": content,
                                    "headers": {"X-Request-ID": {"description": "Platform request tracing ID", "schema": ID}}},
                            "default": {"$ref": "#/components/responses/Error"}}}
    if request:
        if request.startswith("multipart-"):
            fields = {"file": {"type": "string", "format": "binary"}}
            fields.update({"title": TEXT} if request == "multipart-upload" else {"expected_version": VERSION})
            body = {"multipart/form-data": {"schema": obj(fields, ["file"] if request == "multipart-upload" else ["file", "expected_version"])}}
        else:
            body = {"application/json": {"schema": ref(request)}}
        result["requestBody"] = {"required": True, "content": body}
    return result


def generate():
    partner = json.loads((ROOT / "openapi/partner.v1.json").read_text(encoding="utf-8"))
    paths = {}
    for route in ROUTES:
        paths.setdefault(route[1], {})[route[0]] = operation(route)
    return {"openapi": "3.1.1", "jsonSchemaDialect": partner["jsonSchemaDialect"],
            "info": {"title": "AI Knowledge OS management API design", "version": "1.0.0", "description": "Generated architecture contract, not an implemented server. Read access is not administrative access."},
            "servers": partner["servers"], "security": partner["security"], "paths": paths,
            "components": {"securitySchemes": partner["components"]["securitySchemes"], "responses": partner["components"]["responses"], "schemas": schemas()}}


def serialized():
    spec = generate()
    return json.dumps(spec, ensure_ascii=False, sort_keys=True, separators=(",", ":")) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    expected = serialized()
    if args.check:
        if not OUTPUT.exists() or OUTPUT.read_text(encoding="utf-8") != expected:
            raise SystemExit("Management API design artifact is missing or stale")
        print("PASS: management contract matches design source")
    else:
        OUTPUT.write_text(expected, encoding="utf-8")
        print(f"Generated management contract: {len(ROUTES)} operations")


if __name__ == "__main__":
    main()
