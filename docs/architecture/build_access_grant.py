"""Internal GrantVerifier output shape; never a bearer token or verified credential."""
import argparse
import json
from pathlib import Path


def build():
    ident={"type":"string","minLength":1,"maxLength":200}
    digest={"type":"string","pattern":"^[a-f0-9]{64}$"}
    time={"type":"integer","minimum":0,"maximum":9223372036854775807}
    props={"format":{"const":"akos.grant-bindings/1"},"issuer_id":ident,"audience_module_id":ident,
           "caller_service_id":ident,"subject_id":ident,"action":ident,"method":{"enum":["POST","PUT","GET","DELETE"]},
           "path":{"type":"string","pattern":"^/modules/v1/[a-z_]+/[a-z_]+$","maxLength":200},
           "invocation_id":ident,"request_digest":digest,"scope_digest":digest,"profile_digest":digest,
           "issued_at_ns":time,"not_before_ns":time,"expires_at_ns":time}
    return {"$schema":"https://json-schema.org/draft/2020-12/schema","title":"Internal access grant bindings V1",
            "description":"Shape of a trusted verifier result, not proof of signature or authorization. Cannot be supplied as an HTTP body.",
            "type":"object","additionalProperties":False,"required":list(props),"properties":props}


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--write',action='store_true');args=p.parse_args()
    path=Path(__file__).parent/'schemas/access-grant-bindings.v1.schema.json';text=json.dumps(build(),ensure_ascii=False,indent=2)+'\n'
    if args.write:path.write_text(text,encoding='utf-8')
    elif path.read_text(encoding='utf-8')!=text:raise SystemExit('grant bindings schema is stale')
    else:print('PASS: grant bindings schema generation consistency')
