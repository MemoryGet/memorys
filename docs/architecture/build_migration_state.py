"""KB backend migration record schema; metadata/file migration use separate contracts."""
import argparse
import json
from pathlib import Path

ROOT=Path(__file__).parent
PHASES=("planned","quiescing","building","verifying","ready","cutover_committed","restart_verified",
        "serving_retained","finalized","aborting","aborted","rollback_quiescing","rollback_ready",
        "rollback_committed","rollback_restart_verified","rolled_back")


def build():
    identifier={"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9._:-]*$","maxLength":200}
    digest={"type":"string","pattern":"^[a-f0-9]{64}$"}
    cursor={"type":"string","pattern":"^j1:[a-f0-9]{64}$"}
    integer={"type":"integer","minimum":1,"maximum":18446744073709551615}
    seconds={"type":"integer","minimum":0,"maximum":253402300799}
    nullable=lambda schema:{"anyOf":[schema,{"type":"null"}]}
    properties={"schema_version":{"const":1},"kind":{"const":"kb_backend"},"migration_id":identifier,
                "space_id":identifier,"kb_id":identifier,"source_epoch_id":identifier,"target_epoch_id":identifier,
                "source_lock_sha256":digest,"target_lock_sha256":digest,"target_index_profile_digest":digest,
                "journal_stream_id":identifier,"retention_hold_id":identifier,"retention_policy_id":identifier,
                "retention_policy_version":integer,"row_version":integer,"fencing_token":integer,
                "phase":{"enum":list(PHASES)},"content_writes_paused":{"type":"boolean"},
                "snapshot_cursor":nullable(cursor),"forward_checkpoint":nullable(cursor),"cutover_cursor":nullable(cursor),
                "rollback_checkpoint":nullable(cursor),"rollback_cutover_cursor":nullable(cursor),
                "resumed_at_seconds":nullable(seconds),"rollback_until_seconds":nullable(seconds),
                "retain_until_seconds":nullable(seconds)}
    return {"$schema":"https://json-schema.org/draft/2020-12/schema","title":"AI Knowledge OS KB backend migration record V1",
            "description":"Persisted state design. CAS, current authorization, proof provenance and actual cutover require runtime verification.",
            "type":"object","additionalProperties":False,"required":list(properties),"properties":properties}


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument("--write",action="store_true")
    args=parser.parse_args();path=ROOT/"schemas/migration-state.v1.schema.json";text=json.dumps(build(),ensure_ascii=False,indent=2)+"\n"
    if args.write:path.write_text(text,encoding="utf-8")
    elif path.read_text(encoding="utf-8")!=text:raise SystemExit("migration state schema is stale")
    else:print("PASS: migration state schema generation consistency")


if __name__=="__main__":main()
