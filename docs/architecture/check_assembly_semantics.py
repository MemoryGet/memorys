"""Offline architecture assembly checks using hypothetical manifests, no live factories."""
import copy
import json
from pathlib import Path

from jsonschema import Draft202012Validator
from jsonschema.exceptions import ValidationError

from check_deployment_design import binding, fixture
from check_manifest_design import validate_manifest

ROOT = Path(__file__).resolve().parent


def require(condition, message):
    if not condition:
        raise ValueError(message)


def unique(values, key):
    result = {key(value): value for value in values}
    require(len(result) == len(values), "Duplicate identifiers")
    return result


def version(value):
    return tuple(map(int, value.split(".")))


def local_target(reference, root):
    target = root
    for token in reference[2:].split("/"):
        index = 0
        while index < len(token):
            if token[index] == "~":
                require(index + 1 < len(token) and token[index + 1] in "01", "Invalid local schema reference")
                index += 1
            index += 1
        key = token.replace("~1", "/").replace("~0", "~")
        if isinstance(target, dict):
            require(key in target, "Missing local schema reference target")
            target = target[key]
        elif isinstance(target, list):
            require(key.isdigit() and str(int(key)) == key and int(key) < len(target), "Invalid local schema reference index")
            target = target[int(key)]
        else:
            raise ValueError("Invalid local schema reference target")
    require(isinstance(target, (dict, bool)), "Local schema reference target is not a schema")


def local_schema(value, root=None):
    if root is None:
        root = value
    if isinstance(value, dict):
        for keyword in ("$ref", "$dynamicRef"):
            if keyword in value:
                require(isinstance(value[keyword], str) and value[keyword].startswith("#/"), "External schema reference is forbidden")
                local_target(value[keyword], root)
        require("$id" not in value, "Schema resource reference rebasing is forbidden in offline checks")
        if "$schema" in value:
            require(value["$schema"].rstrip("#") == "https://json-schema.org/draft/2020-12/schema", "Unsupported schema dialect")
        for child in value.values():
            local_schema(child, root)
    elif isinstance(value, list):
        for child in value:
            local_schema(child, root)


def check_binding(value, manifests, allowed=None):
    require(value["instance_id"] in manifests, "Unknown module instance")
    manifest = manifests[value["instance_id"]]
    capabilities = unique(manifest["capabilities"], lambda c: c["name"])
    name = value["capability"]
    require(name in capabilities, "Capability not provided")
    if allowed is not None:
        require(name in allowed, "Capability does not match role")
    supplied, requested = version(capabilities[name]["version"]), version(value["contract"])
    require(supplied[0] == requested[0] and supplied[1] >= requested[1], "Contract version incompatible")
    require(set(value["required_features"]) <= set(capabilities[name]["features"]), "Required features missing")
    return capabilities[name]


def check_dependencies(modules, manifests):
    graph = {name: set() for name in modules}
    for name, module in modules.items():
        declarations = unique(manifests[name]["dependencies"], lambda d: d["slot"])
        bindings = module["dependency_bindings"]
        require(set(bindings) <= set(declarations), "Undeclared dependency slot")
        for slot, dep in declarations.items():
            if slot not in bindings:
                require(dep["optional"], "Missing required dependency")
                continue
            provided = check_binding(bindings[slot], manifests, {dep["capability"]})
            supplied = version(provided["version"])
            require(supplied[0] == dep["major"] and supplied[1] >= dep["minimum_minor"], "Dependency contract incompatible")
            require(set(dep["required_features"]) <= set(provided["features"]), "Dependency features missing")
            graph[name].add(bindings[slot]["instance_id"])
    order = []
    while graph:
        ready = sorted(name for name, deps in graph.items() if not deps)
        require(bool(ready), "Dependency cycle")
        order.extend(ready)
        graph = {name: deps - set(ready) for name, deps in graph.items() if name not in ready}
    return order


def check_routes(profile, manifests, query=False):
    field = "retrieval_routes" if query else "index_routes"
    unique(profile[field], lambda r: r["route_id"])
    covered = set()
    mandatory = set()
    composite_instances = set()
    for route in profile[field]:
        allowed = {"retriever", "composite_ingest_search"} if query else {"index_writer", "composite_ingest_search"}
        cap = check_binding(route["binding"], manifests, allowed)
        if query and route["binding"]["capability"] == "composite_ingest_search":
            instance = route["binding"]["instance_id"]
            require(instance not in composite_instances, "Composite recall result counted more than once")
            composite_instances.add(instance)
        channels = set(route["channels"])
        require({"channel." + name for name in channels} <= set(cap["features"]), "Route channel unsupported")
        if query:
            filters = {"prefilter.space", "prefilter.kb", "prefilter.revision", "prefilter.generation"}
            require(filters <= set(cap["features"]), "Mandatory retrieval filters missing")
            if route["required"]:
                mandatory |= channels
        else:
            require(not covered & channels, "Overlapping index channels")
        covered |= channels
    return covered, mandatory


def check_projection_binding(index, query=None):
    if index["mode"] != "composite_projection":
        return
    require(set(index["bindings"]) == {"composite_ingest_search"}, "Composite projection cannot claim independent processing bindings")
    binding = index["bindings"]["composite_ingest_search"]
    identity = (binding["instance_id"], binding["capability"])
    owned = set()
    for route in index["index_routes"]:
        selected = route["binding"]
        if selected["capability"] == "composite_ingest_search":
            require((selected["instance_id"], selected["capability"]) == identity, "Composite projection write binding split")
            owned.update(route["channels"])
    require(bool(owned), "Composite projection has no composite write route")
    if query is not None:
        for route in query["retrieval_routes"]:
            selected = route["binding"]
            if owned & set(route["channels"]):
                require((selected["instance_id"], selected["capability"]) == identity, "Composite projection recall binding split")


def check_composite_groups(bindings, manifests):
    selected = {}
    used = set()
    for value in bindings:
        selected.setdefault(value["capability"], set()).add(value["instance_id"])
        used.add(value["instance_id"])
    for instance in used:
        for group in manifests[instance]["composite_groups"]:
            members = set(group["capabilities"])
            if not any(instance in selected.get(name, set()) for name in members):
                continue
            require(all(instance in selected.get(name, set()) for name in members), "Composite capability group split")


def check_profiles(config, manifests):
    indexes = unique(config["index_profiles"], lambda p: (p["id"], p["version"]))
    queries = unique(config["query_profiles"], lambda p: (p["id"], p["version"]))
    unique(config["pipeline_profiles"], lambda p: (p["id"], p["version"]))
    for profile in [*indexes.values(), *queries.values()]:
        for role, value in profile["bindings"].items():
            check_binding(value, manifests, {role})
    for profile in indexes.values():
        roles = {"parser", "splitter", "embedder"} if profile["mode"] == "independent" else {"composite_ingest_search"}
        require(roles <= set(profile["bindings"]), "Missing index processing role")
        check_projection_binding(profile)
        covered, _ = check_routes(profile, manifests)
        require(set(profile["policy"]["required_channels"]) <= covered, "Missing required index channel")
        chunk = profile["policy"]["chunking"]
        require(chunk["overlap_codepoints"] < chunk["max_codepoints"], "Overlap must be smaller than chunk")
        require(chunk.get("parent_max_codepoints", chunk["max_codepoints"]) >= chunk["max_codepoints"], "Parent chunk smaller than child")
    for profile in queries.values():
        check_routes(profile, manifests, query=True)
        policy = profile["policy"]
        require(policy["answer_evidence_limit"] <= policy["candidate_limit"], "Answer evidence budget exceeds candidates")
        require(sum(r["candidate_limit"] for r in profile["retrieval_routes"]) <= policy["candidate_limit"], "Route budgets exceed total")
        for name, enabled in [("generator", policy["generation"]["enabled"]), ("reranker", policy["rerank"]["enabled"]), ("reflection", profile["reflection"]["enabled"])]:
            require(not enabled or name in profile["bindings"], "Enabled capability not bound")
    for profile in config["pipeline_profiles"]:
        i, q = profile["index_profile_ref"], profile["query_profile_ref"]
        require((i["id"], i["version"]) in indexes and (q["id"], q["version"]) in queries, "Missing profile version")
        index, query = indexes[(i["id"], i["version"])], queries[(q["id"], q["version"])]
        check_projection_binding(index, query)
        available, _ = check_routes(index, manifests)
        requested, mandatory = check_routes(query, manifests, query=True)
        required = set(index["policy"]["required_channels"])
        require(requested <= available and required <= mandatory, "Index and required recall channel coverage mismatch")
        require(not required & set(query["policy"]["optional_capabilities"]), "Required channel cannot be optional")
        selected = [*config["bindings"].values(), *index["bindings"].values(), *query["bindings"].values()]
        selected += [route["binding"] for route in index["index_routes"] + query["retrieval_routes"]]
        check_composite_groups(selected, manifests)


def validate(config, providers):
    schema = json.loads((ROOT / "schemas/deployment-config.v1.schema.json").read_text())
    manifest_schema = json.loads((ROOT / "schemas/module-manifest.v1.schema.json").read_text())
    Draft202012Validator(schema).validate(config)
    modules = unique(config["modules"], lambda m: m["instance_id"])
    manifests = {}
    for name, module in modules.items():
        key = (module["provider"], module["implementation_version"])
        require(key in providers, "Provider release not available")
        manifest = providers[key]
        validate_manifest(manifest, Draft202012Validator(manifest_schema))
        require(manifest["module_id"] == module["provider"], "Provider ID mismatch")
        require(manifest["implementation_version"] == module["implementation_version"], "Provider version mismatch")
        supplied, requested = version(manifest["contract_version"]), version(module["contract"])
        require(supplied[0] == requested[0] and supplied[1] >= requested[1], "Lifecycle contract incompatible")
        local_schema(manifest["config_schema"])
        Draft202012Validator(manifest["config_schema"]).validate(module["settings"])
        manifests[name] = manifest
    for role, value in config["bindings"].items():
        check_binding(value, manifests, {role})
    transaction_roles = ["metadata_uow", "task_repository", "audit"]
    require(len({config["bindings"][role]["instance_id"] for role in transaction_roles}) == 1, "Atomic transaction package split")
    order = check_dependencies(modules, manifests)
    check_profiles(config, manifests)
    return order


def fixtures():
    config = fixture()
    names = [*config["bindings"], "parser", "splitter", "embedder", "index_writer", "retriever", "fuser", "generator", "reranker", "reflection"]
    manifest = {
        "schema_version": 1, "module_id": "unimplemented-reference", "implementation_version": "0.1.0", "contract_version": "1.0",
        "capabilities": [{"name": name, "version": "1.0", "features": ["channel.keyword", "channel.vector", "prefilter.space", "prefilter.kb", "prefilter.revision", "prefilter.generation", "stream.read"]} for name in names],
        "dependencies": [], "config_schema": {"type": "object", "additionalProperties": False},
        "health": {"startup_timeout_ms": 1000, "runtime_timeout_ms": 500, "supports_readiness": True},
        "state_model": "authoritative", "migration_modes": ["export_import"], "composite_groups": [],
    }
    return config, {("unimplemented-reference", "0.1.0"): manifest}


def dependency_fixture(config, providers):
    dependency_config, dependency_providers = copy.deepcopy(config), copy.deepcopy(providers)
    helper = copy.deepcopy(config["modules"][0])
    helper.update(instance_id="parser-helper", provider="reference-parser", dependency_bindings={"source": binding("blob_store")})
    helper_manifest = copy.deepcopy(providers[("unimplemented-reference", "0.1.0")])
    helper_manifest["capabilities"] = [item for item in helper_manifest["capabilities"] if item["name"] == "parser"]
    helper_manifest.update(module_id="reference-parser", dependencies=[{"slot": "source", "capability": "blob_store", "major": 1,
                                                                       "minimum_minor": 0, "required_features": ["stream.read"], "optional": False}])
    dependency_config["modules"].append(helper)
    dependency_config["index_profiles"][0]["bindings"]["parser"].update(instance_id="parser-helper")
    dependency_providers[("reference-parser", "0.1.0")] = helper_manifest
    return dependency_config, dependency_providers


def check_cycle(config, providers):
    cycle_config, cycle_providers = copy.deepcopy(config), copy.deepcopy(providers)
    cycle_config["modules"][0]["dependency_bindings"]["parser"] = dict(binding("parser"), instance_id="parser-helper")
    cycle_providers[("unimplemented-reference", "0.1.0")]["dependencies"] = [
        {"slot": "parser", "capability": "parser", "major": 1, "minimum_minor": 0, "required_features": [], "optional": False}]
    try:
        validate(cycle_config, cycle_providers)
    except ValueError as error:
        require(str(error) == "Dependency cycle", "Cycle rejected for unrelated reason")
    else:
        raise AssertionError("Dependency cycle accepted")


def check_composite_fixture(config, providers):
    group_providers = copy.deepcopy(providers)
    group_providers[("unimplemented-reference", "0.1.0")]["composite_groups"] = [
        {"id": "engine", "capabilities": ["index_writer", "retriever"], "replacement_unit": "whole_group", "reason": "hypothetical coupled engine"}]
    validate(config, group_providers)
    split = copy.deepcopy(config)
    helper = dict(copy.deepcopy(split["modules"][0]), instance_id="retrieval-helper", provider="independent-retriever")
    split["modules"].append(helper)
    manifest = copy.deepcopy(providers[("unimplemented-reference", "0.1.0")])
    manifest.update(module_id="independent-retriever", state_model="stateless", migration_modes=[], composite_groups=[])
    manifest["capabilities"] = [item for item in manifest["capabilities"] if item["name"] == "retriever"]
    group_providers[("independent-retriever", "0.1.0")] = manifest
    for route in split["query_profiles"][0]["retrieval_routes"]:
        route["binding"]["instance_id"] = "retrieval-helper"
    try:
        validate(split, group_providers)
    except ValueError as error:
        require(str(error) == "Composite capability group split", "Composite split rejected for unrelated reason")
    else:
        raise AssertionError("Composite group split accepted")


def run():
    config, providers = fixtures()
    require(validate(config, providers) == ["design-fixture"], "Unexpected dependency order")
    dependency_config, dependency_providers = dependency_fixture(config, providers)
    require(validate(dependency_config, dependency_providers) == ["design-fixture", "parser-helper"], "Incorrect dependency-first ordering")
    edits = [
        lambda c, p: c["modules"].append(copy.deepcopy(c["modules"][0])),
        lambda c, p: c["bindings"]["authorizer"].update(instance_id="missing"),
        lambda c, p: c["bindings"]["authorizer"].update(capability="authenticator"),
        lambda c, p: c["bindings"]["authorizer"].update(contract="2.0"),
        lambda c, p: c["bindings"]["authorizer"].update(required_features=["not.supported"]),
        lambda c, p: c["modules"][0]["settings"].update(unknown=True),
        lambda c, p: c["pipeline_profiles"][0]["query_profile_ref"].update(version=2),
        lambda c, p: c["index_profiles"][0]["index_routes"][1].update(channels=["keyword"]),
        lambda c, p: c["query_profiles"][0]["retrieval_routes"][1].update(required=False),
        lambda c, p: c["query_profiles"][0]["retrieval_routes"][1].update(candidate_limit=51),
        lambda c, p: c["index_profiles"][0]["policy"]["chunking"].update(overlap_codepoints=1000),
        lambda c, p: c["query_profiles"][0]["policy"]["generation"].update(enabled=True),
        lambda c, p: c["modules"][0]["dependency_bindings"].update(undeclared=binding("blob_store")),
    ]
    for edit in edits:
        candidate, catalogue = copy.deepcopy(config), copy.deepcopy(providers)
        edit(candidate, catalogue)
        try:
            validate(candidate, catalogue)
        except (ValueError, ValidationError):
            continue
        raise AssertionError("Invalid design assembly accepted")
    check_cycle(dependency_config, dependency_providers)
    check_composite_fixture(config, providers)
    print(f"PASS: 3 hypothetical assemblies, dependency order, {len(edits)} invalid assemblies, dependency cycle and composite split rejected")
    print("Not covered: artifact locks, live health, backend layout, KB fanout, runtime identity or migrations.")


if __name__ == "__main__":
    run()
