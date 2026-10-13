"""File-copy inventory schema for stable platform BlobRefs."""
import argparse
import json
from pathlib import Path

ROOT=Path(__file__).parent


def build():
    identifier={"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9._:-]*$","maxLength":200}
    digest={"type":"string","pattern":"^[a-f0-9]{64}$"}
    version={"type":"integer","minimum":1,"maximum":18446744073709551615}
    size={"type":"integer","minimum":0,"maximum":9223372036854775807}
    def obj(properties):return {"type":"object","additionalProperties":False,"required":list(properties),"properties":properties}
    blob=obj({"id":identifier,"sha256":digest,"size":size,"media_type":{"type":"string","minLength":1,"maxLength":255}})
    return {"$schema":"https://json-schema.org/draft/2020-12/schema","title":"AI Knowledge OS Blob copy manifest V1",
            "description":"Byte-copy inventory only; source authority, actual storage bindings and atomic cutover require runtime verification.",
            **obj({"schema_version":{"const":1},"migration_id":identifier,"kind":{"const":"blob_store_copy"},
                   "source_mapping_id":identifier,"target_mapping_id":identifier,"source_lock_sha256":digest,"target_lock_sha256":digest,
                   "retention_hold_id":identifier,"snapshot":obj({"snapshot_id":identifier,"journal_stream_id":identifier,
                       "watermark":{"type":"string","pattern":"^j1:[a-f0-9]{64}$"},"blob_writes":{"const":"paused"}}),
                   "objects":{"type":"array","items":obj({"entry_id":identifier,"space_id":identifier,"blob":blob,
                       "source_binding_version":version,"target_binding_version":version,"target_binding_ref":identifier})}})}


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument("--write",action="store_true")
    args=parser.parse_args();path=ROOT/"schemas/blob-copy.v1.schema.json";text=json.dumps(build(),ensure_ascii=False,indent=2)+"\n"
    if args.write:path.write_text(text,encoding="utf-8")
    elif path.read_text(encoding="utf-8")!=text:raise SystemExit("blob copy schema is stale")
    else:print("PASS: blob copy schema generation consistency")


if __name__=="__main__":main()
