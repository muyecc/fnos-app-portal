"""Offline helper behavior tests. Never connect to a NAS."""
import hashlib
import http.server
import json
import pathlib
import tempfile
import threading
import unittest

import check_entry
import render_project

class RenderTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.dashboard = self.root / "dashboard"
        (self.dashboard / "assets").mkdir(parents=True)
        (self.dashboard / "index.html").write_text('<script src="/app/demo-portal/assets/main.js"></script>', encoding="utf8")
        (self.dashboard / "assets/main.js").write_text("console.log('example');", encoding="utf8")
        self.manifest = {"schema": 1, "upstream_version": "1.0.0", "prefix": "/app/demo-portal/", "assets": {name: hashlib.sha256((self.dashboard / name).read_bytes()).hexdigest() for name in ["index.html", "assets/main.js"]}}
        self.save_manifest()
        self.plan = json.loads((render_project.ROOT / "references/plan.example.json").read_text(encoding="utf8"))
        self.plan.update(app_id="demo-portal", display_name="Example", docker_network="demo_default", peer_uid_verified=True)
        self.output = self.root / "project"

    def save_manifest(self):
        (self.dashboard / "portal-build.json").write_text(json.dumps(self.manifest), encoding="utf8")

    def test_dry_run_creates_nothing(self):
        answer = render_project.render(self.plan, self.dashboard, self.output, True)
        self.assertTrue(answer["dry_run"])
        self.assertFalse(self.output.exists())

    def test_only_verified_frontend_files_are_copied(self):
        (self.dashboard / ".env").write_text("unrelated-private-data", encoding="utf8")
        render_project.render(self.plan, self.dashboard, self.output)
        self.assertFalse((self.output / "fpk/app/dashboard/.env").exists())
        cfg = json.loads((self.output / "fpk/app/config.json").read_text(encoding="utf8"))
        self.assertEqual(cfg["prefix"], "/app/demo-portal")
        self.assertTrue(cfg["secure_cookie"])
        compose = (self.output / "fpk/app/docker/docker-compose.yaml").read_text(encoding="utf8")
        self.assertIn("name: demo_default", compose)
        self.assertIn("demo-portal-local:0.1.0", compose)
        self.assertNotIn("example-portal", compose)

    def test_existing_output_is_preserved(self):
        self.output.mkdir()
        marker = self.output / "keep.txt"
        marker.write_text("keep", encoding="utf8")
        with self.assertRaises(ValueError):
            render_project.render(self.plan, self.dashboard, self.output)
        self.assertEqual(marker.read_text(), "keep")

    def test_tampered_frontend_is_refused_before_output(self):
        (self.dashboard / "assets/main.js").write_text("tampered", encoding="utf8")
        with self.assertRaises(ValueError):
            render_project.render(self.plan, self.dashboard, self.output)
        self.assertFalse(self.output.exists())

    def test_parent_path_asset_is_refused(self):
        self.manifest["assets"]["../private.txt"] = "0" * 64
        self.save_manifest()
        with self.assertRaises(ValueError):
            render_project.validate(self.plan, self.dashboard)

    def test_verified_peer_is_required(self):
        self.plan["peer_uid_verified"] = False
        with self.assertRaises(ValueError):
            render_project.validate(self.plan, self.dashboard)

    def test_http_needs_exact_ip_exception(self):
        self.plan.update(public_hosts=["192.168.1.10", "192.168.1.10:5666"], allowed_origins=["http://192.168.1.10:5666"], http_cookie_ips=["192.168.1.10"])
        render_project.validate(self.plan, self.dashboard)
        self.plan["http_cookie_ips"] = []
        with self.assertRaises(ValueError):
            render_project.validate(self.plan, self.dashboard)

    def test_unlisted_origin_is_refused(self):
        self.plan["allowed_origins"] = ["https://other.example.com"]
        with self.assertRaises(ValueError):
            render_project.validate(self.plan, self.dashboard)

    def test_public_ip_http_exception_is_refused(self):
        self.plan["http_cookie_ips"] = ["8.8.8.8"]
        with self.assertRaises(ValueError):
            render_project.validate(self.plan, self.dashboard)

    def test_version_drift_is_refused(self):
        self.plan["upstream_version"] = "2.0.0"
        with self.assertRaises(ValueError):
            render_project.validate(self.plan, self.dashboard)

class EntryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path == "/denied":
                    status, body = 200, b"invalid token"
                elif self.path == "/forbidden":
                    status, body = 403, b"Forbidden"
                else:
                    status, body = 200, b'{"business_data":true}'
                self.send_response(status)
                self.end_headers()
                self.wfile.write(body)
            def log_message(self, *_):
                pass
        cls.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.base = "http://127.0.0.1:" + str(cls.server.server_port)

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()

    def test_200_error_marker_is_denied(self):
        answer = check_entry.probe(self.base, "/denied")
        self.assertTrue(answer["denied"])
        self.assertTrue(answer["expected_nas_marker"])

    def test_200_business_content_is_failure(self):
        self.assertFalse(check_entry.probe(self.base, "/leak")["denied"])

    def test_403_does_not_claim_expected_nas_marker(self):
        answer = check_entry.probe(self.base, "/forbidden")
        self.assertTrue(answer["denied"])
        self.assertFalse(answer["expected_nas_marker"])

if __name__ == "__main__":
    unittest.main()
