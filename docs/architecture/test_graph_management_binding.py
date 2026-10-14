import copy
import unittest

from check_assembly_semantics import check_dependencies


def samples():
    modules = {"query": {"dependency_bindings": {"projection_manager": {
        "instance_id": "manager", "capability": "graph_projection_manager", "contract": "1.0", "required_features": []}}},
        "manager": {"dependency_bindings": {}}}
    manifests = {"query": {"capabilities": [{"name":"graph", "version":"1.0", "features":[]}], "dependencies":[]},
                 "manager": {"capabilities": [{"name":"graph_projection_manager", "version":"1.0", "features":[]}], "dependencies":[]}}
    return modules, manifests


class GraphManagementBindingTests(unittest.TestCase):
    def test_separate_manager_orders_before_query(self):
        modules, manifests = samples()
        self.assertEqual(check_dependencies(modules, manifests), ["manager", "query"])

    def test_missing_wrong_or_optional_manager_cannot_bypass(self):
        for mutation in [lambda m,p: m["query"]["dependency_bindings"].clear(),
                         lambda m,p: p["manager"]["capabilities"][0].update(name="graph"),
                         lambda m,p: p["query"]["dependencies"].append({"slot":"projection_manager", "capability":"parser", "major":1, "minimum_minor":0, "required_features":[], "optional":True}),
                         lambda m,p: (m["query"]["dependency_bindings"].clear(), p["query"]["dependencies"].append({"slot":"projection_manager", "capability":"graph_projection_manager", "major":1, "minimum_minor":0, "required_features":[], "optional":True}))]:
            modules, manifests = samples(); mutation(modules, manifests)
            with self.assertRaises(ValueError): check_dependencies(modules, manifests)

    def test_composite_manager_needs_no_self_dependency(self):
        modules, manifests = samples()
        modules["query"]["dependency_bindings"].clear()
        manifests["query"]["capabilities"].append(copy.deepcopy(manifests["manager"]["capabilities"][0]))
        del modules["manager"]; del manifests["manager"]
        self.assertEqual(check_dependencies(modules,manifests), ["query"])
