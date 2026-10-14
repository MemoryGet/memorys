"""Independent graph cleanup schema/hash reference; no current authorization."""
import copy
import hashlib
import json
from pathlib import Path
from jsonschema import Draft202012Validator
from canonical_content import canonical, pairs, reject_number

ROOT=Path(__file__).resolve().parent


def schema():
    identifier={"type":"string","minLength":1,"maxLength":200}
    digest={"type":"string","pattern":"^[0-9a-f]{64}$"}
    def obj(properties,required=None):
        return {"type":"object","additionalProperties":False,"properties":properties,"required":list(properties) if required is None else required}
    target=obj({**{k:identifier for k in ["space_id","kb_id","backend_epoch_id","generation_id"]},
                **{k:digest for k in ["index_profile_digest","extraction_profile_digest","ontology_digest","source_snapshot_digest"]}})
    operation=obj({"operation_id":identifier,"subject_id":identifier,"kind":{"enum":["source_invalidation","generation_deletion"]},"target":target,
                   "document_id":identifier,"revision_id":identifier,"publication_epoch":{"type":"integer","minimum":1,"maximum":18446744073709551615},"retention_permit_id":identifier},
                  ["operation_id","subject_id","kind","target"])
    operation["oneOf"]=[
        {"properties":{"kind":{"const":"source_invalidation"}},"required":["document_id","revision_id","publication_epoch"],"not":{"required":["retention_permit_id"]}},
        {"properties":{"kind":{"const":"generation_deletion"}},"required":["retention_permit_id"],"not":{"anyOf":[{"required":[k]} for k in ["document_id","revision_id","publication_epoch"]]}}
    ]
    return {"$schema":"https://json-schema.org/draft/2020-12/schema",**obj({"schema_version":{"const":"1.0"},"operation":operation})}


def validate(value):
    Draft202012Validator(schema()).validate(value)
    epoch=value["operation"].get("publication_epoch")
    if epoch is not None and type(epoch) is not int:raise ValueError("Exact integer epoch required")
    canonical(json.dumps(value,ensure_ascii=False).encode("utf-8"))


def request_digest(raw):
    canonical(raw)  # Size/depth, duplicate keys and Unicode checks first.
    value=json.loads(raw.decode("utf-8"),object_pairs_hook=pairs,parse_float=reject_number,parse_constant=reject_number)
    validate(value)
    suffix="source-invalidation" if value["operation"]["kind"]=="source_invalidation" else "generation-deletion"
    tag=("akos.graph-cleanup/"+suffix+"/1\0").encode("ascii")
    return hashlib.sha256(tag+canonical(raw)).hexdigest()


def golden():
    source={"schema_version":"1.0","operation":{"operation_id":"operation","subject_id":"用户甲","kind":"source_invalidation","document_id":"document","revision_id":"revision","publication_epoch":18446744073709551615,
             "target":{**{k:"target-"+k for k in ["space_id","kb_id","backend_epoch_id","generation_id"]},**{k:"a"*64 for k in ["index_profile_digest","extraction_profile_digest","ontology_digest","source_snapshot_digest"]}}}}
    deletion=copy.deepcopy(source);o=deletion["operation"];o["kind"]="generation_deletion";o["retention_permit_id"]="permit"
    for k in ["document_id","revision_id","publication_epoch"]:del o[k]
    cases=[]
    for name,value in [("source",source),("delete",deletion)]:
        raw=json.dumps(value,ensure_ascii=False).encode()
        cases.append({"name":name,"input_json":raw.decode(),"canonical_json":canonical(raw).decode(),"sha256":request_digest(raw)})
    return {"format":"akos.graph-cleanup-golden/1","cases":cases,"source_authenticated":False}


if __name__=="__main__":
    for path,value in [(ROOT/"schemas/graph-cleanup-semantic.v1.schema.json",schema()),(ROOT/"examples/graph-cleanup-golden.v1.json",golden())]:
        path.write_text(json.dumps(value,ensure_ascii=False,indent=2)+"\n",encoding="utf-8")
