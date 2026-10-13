"""Journal transaction-chain design schema; no database commit implementation."""
import argparse
import json
from pathlib import Path

from build_export_manifest import RESOURCE_TYPES

ROOT = Path(__file__).parent


def build():
    identifier = {"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9._:-]*$","maxLength":200}
    cursor = {"type":"string","pattern":"^j1:[a-f0-9]{64}$"}
    digest = {"type":"string","pattern":"^[a-f0-9]{64}$"}
    integer = {"type":"integer","minimum":0,"maximum":18446744073709551615}
    def obj(properties):
        return {"type":"object","additionalProperties":False,"required":list(properties),"properties":properties}
    mutation = obj({"ordinal":integer,"operation_id":identifier,
                    "record_type":{"enum":[r for r in RESOURCE_TYPES if r != "migration_journal"]},
                    "record_id":identifier,"previous_version":{"anyOf":[integer,{"type":"null"}]},
                    "new_version":{**integer,"minimum":1},"effect":{"enum":["upsert","tombstone"]},
                    "payload":obj({"entry_id":identifier,"sha256":digest,"byte_length":integer})})
    transaction = obj({"transaction_id":identifier,"journal_stream_id":identifier,"previous_cursor":cursor,
                       "cursor":cursor,"mutation_count":{**integer,"minimum":1},
                       "mutations":{"type":"array","minItems":1,"items":mutation}})
    return {"$schema":"https://json-schema.org/draft/2020-12/schema",
            "title":"AI Knowledge OS journal complete-transaction bundle V1",
            "description":"Hash chain and envelope only. Trusted head, actual commit order, typed payloads and atomic replay require runtime evidence.",
            **obj({"schema_version":{"const":1},"journal_stream_id":identifier,"from_cursor":cursor,
                   "through_cursor":cursor,"hash_format":{"const":"akos.journal/1"},
                   "transactions":{"type":"array","items":transaction}})}


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument("--write",action="store_true")
    args=parser.parse_args();target=ROOT/"schemas/journal-bundle.v1.schema.json"
    text=json.dumps(build(),ensure_ascii=False,indent=2)+"\n"
    if args.write:target.write_text(text,encoding="utf-8")
    elif target.read_text(encoding="utf-8")!=text:raise SystemExit("journal schema is stale")
    else:print("PASS: journal schema generation consistency")


if __name__=="__main__":main()
