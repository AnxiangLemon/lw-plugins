"""Public method catalog generation without host source checkouts."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "tools"))
from build_wechat_panel_catalog import build


def swagger():
    return {
        "paths": {
            "/api/Group/SetName": {
                "post": {
                    "parameters": [{"in": "body", "schema": {"$ref": "#/definitions/Name"}}],
                    "responses": {"200": {"description": "ok"}},
                }
            },
            "/api/Login": {"post": {}},
        },
        "definitions": {
            "Name": {
                "type": "object",
                "required": ["chatRoomId", "authorization"],
                "properties": {
                    "chatRoomId": {"type": "string"},
                    "content": {"type": "string"},
                    "authorization": {"type": "string"},
                    "access_token": {"type": "string"},
                },
            }
        },
    }


METHODS = [{"name": "group.set_name", "path": "/api/Group/SetName"}]


class CatalogBuilderTests(unittest.TestCase):
    def test_public_methods_limit_scope_and_remove_host_credentials(self):
        result = build(METHODS, swagger(), "swagger.json")
        self.assertEqual(result["counts"], {"business": 1, "total": 1})
        method = result["methods"][0]
        self.assertEqual(method["name"], "group.set_name")
        self.assertEqual(set(method["schema"]["properties"]), {"chatRoomId", "content"})
        self.assertEqual(method["schema"]["required"], ["chatRoomId"])
        self.assertFalse(method["schema"]["additionalProperties"])
        self.assertEqual(method["example"]["chatRoomId"], "example@chatroom")

    def test_missing_documentation_identifies_the_method(self):
        with self.assertRaisesRegex(ValueError, "Missing POST documentation for group.set_name"):
            build(METHODS, {"paths": {}}, "swagger.json")

    def test_duplicate_method_names_are_rejected(self):
        with self.assertRaisesRegex(ValueError, "Duplicate public method: group.set_name"):
            build(METHODS * 2, swagger(), "swagger.json")

    def test_cli_works_with_only_public_json_inputs(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            methods = work / "methods.json"
            spec = work / "swagger.json"
            output = work / "catalog.json"
            methods.write_text(json.dumps({"methods": METHODS}), encoding="utf-8")
            spec.write_text(json.dumps(swagger()), encoding="utf-8")
            subprocess.run([
                sys.executable, str(ROOT / "tools/build_wechat_panel_catalog.py"),
                "--methods-file", str(methods), "--docs-file", str(spec), "--output", str(output),
            ], cwd=work, check=True, capture_output=True)
            result = json.loads(output.read_text(encoding="utf-8"))
            self.assertEqual(result["source"], "swagger.json")
            self.assertEqual(result["methods"][0]["name"], "group.set_name")


if __name__ == "__main__":
    unittest.main()
