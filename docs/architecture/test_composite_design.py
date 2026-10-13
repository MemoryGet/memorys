"""Composite projection assembly acceptance; providers remain hypothetical."""
import copy
import unittest

from check_assembly_semantics import fixtures, validate


def composite_fixture():
    config, providers = fixtures()
    manifest = copy.deepcopy(next(iter(providers.values())))
    manifest.update(module_id="reference-composite", state_model="derived", migration_modes=["rebuild"])
    capability = copy.deepcopy(manifest["capabilities"][0])
    capability["name"] = "composite_ingest_search"
    manifest["capabilities"] = [capability]
    manifest["composite_groups"] = [{"id": "engine", "capabilities": ["composite_ingest_search"],
                                       "replacement_unit": "whole_group", "reason": "Coupled projection engine"}]
    providers[("reference-composite", "0.1.0")] = manifest
    module = copy.deepcopy(config["modules"][0])
    module.update(instance_id="composite", provider="reference-composite")
    config["modules"].append(module)
    binding = {"instance_id": "composite", "capability": "composite_ingest_search", "contract": "1.0", "required_features": []}
    index = config["index_profiles"][0]
    index.update(mode="composite_projection", bindings={"composite_ingest_search": copy.deepcopy(binding)},
                 index_routes=[{"route_id": "engine-index", "channels": ["keyword", "vector"], "binding": copy.deepcopy(binding)}])
    config["query_profiles"][0]["retrieval_routes"] = [
        {"route_id": "engine-recall", "channels": ["keyword", "vector"], "binding": copy.deepcopy(binding), "required": True, "candidate_limit": 100}]
    return config, providers


class CompositeDesignTests(unittest.TestCase):
    def test_full_composite_projection_is_valid(self):
        config, providers = composite_fixture()
        self.assertEqual(validate(config, providers), ["composite", "design-fixture"])

    def test_independent_extension_channel_remains_replaceable(self):
        config, providers = composite_fixture()
        for capability in providers[("unimplemented-reference", "0.1.0")]["capabilities"]:
            if capability["name"] in {"index_writer", "retriever"}:
                capability["features"].append("channel.graph")
        independent = {"instance_id": "design-fixture", "capability": "index_writer", "contract": "1.0", "required_features": []}
        config["index_profiles"][0]["index_routes"].append(
            {"route_id": "graph-index", "channels": ["graph"], "binding": independent})
        query = config["query_profiles"][0]
        query["retrieval_routes"][0]["candidate_limit"] = 90
        query["retrieval_routes"].append({"route_id": "graph-recall", "channels": ["graph"],
                                          "binding": dict(independent, capability="retriever"), "candidate_limit": 10, "required": False})
        self.assertEqual(validate(config, providers), ["composite", "design-fixture"])

    def test_separate_writer_is_not_a_composite_ingest_route(self):
        config, providers = composite_fixture()
        config["index_profiles"][0]["index_routes"][0]["binding"].update(
            instance_id="design-fixture", capability="index_writer")
        with self.assertRaises(ValueError):
            validate(config, providers)

    def test_separate_retriever_cannot_split_a_composite_projection(self):
        config, providers = composite_fixture()
        config["query_profiles"][0]["retrieval_routes"][0]["binding"].update(
            instance_id="design-fixture", capability="retriever")
        with self.assertRaises(ValueError):
            validate(config, providers)

    def test_same_composite_result_cannot_contribute_twice_to_fusion(self):
        config, providers = composite_fixture()
        routes = config["query_profiles"][0]["retrieval_routes"]
        routes[0]["candidate_limit"] = 50
        duplicate = copy.deepcopy(routes[0])
        duplicate["route_id"] = "duplicate-engine-recall"
        routes.append(duplicate)
        with self.assertRaises(ValueError):
            validate(config, providers)


if __name__ == "__main__":
    unittest.main()
