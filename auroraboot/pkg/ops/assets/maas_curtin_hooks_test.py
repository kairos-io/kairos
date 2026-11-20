import importlib.util
import os
import unittest

HOOK_PATH = os.path.join(os.path.dirname(__file__), "maas-curtin-hooks")


def load_hook():
    import importlib.machinery
    loader = importlib.machinery.SourceFileLoader("maashook", HOOK_PATH)
    spec = importlib.util.spec_from_file_location("maashook", HOOK_PATH, loader=loader)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class ExtractDatasourceTest(unittest.TestCase):
    def setUp(self):
        self.h = load_hook()

    def test_extracts_maas_cloud_config_content(self):
        content = "#cloud-config\ndatasource:\n  MAAS:\n    metadata_url: http://maas:5248/MAAS/metadata/\n    consumer_key: ck\n    token_key: tk\n    token_secret: ts\n"
        cfg = {"cloudconfig": {"maas-cloud-config": {"content": content, "path": "x"}}}
        self.assertEqual(self.h.extract_datasource(cfg), content)

    def test_returns_none_when_absent(self):
        self.assertIsNone(self.h.extract_datasource({"cloudconfig": {"maas-datasource": {"content": "datasource_list: [ MAAS ]"}}}))
        self.assertIsNone(self.h.extract_datasource({}))


class TranslateNetworkTest(unittest.TestCase):
    def setUp(self):
        self.h = load_hook()

    def _only_file(self, doc):
        self.assertIsNotNone(doc)
        files = doc["stages"]["initramfs"][0]["files"]
        self.assertEqual(len(files), 1)
        return files[0]

    def test_v2_static(self):
        netcfg = {"version": 2, "ethernets": {"enp1s0": {
            "addresses": ["172.16.0.5/24"],
            "routes": [{"to": "default", "via": "172.16.0.1"}],
            "nameservers": {"addresses": ["1.1.1.1"]}}}}
        f = self._only_file(self.h.translate_network(netcfg))
        self.assertEqual(f["path"], "/etc/systemd/network/10-maas.network")
        self.assertIn("Name=enp1s0", f["content"])
        self.assertIn("Address=172.16.0.5/24", f["content"])
        self.assertIn("Gateway=172.16.0.1", f["content"])
        self.assertIn("DNS=1.1.1.1", f["content"])
        self.assertNotIn("DHCP=yes", f["content"])

    def test_v2_static_maas_wrapped(self):
        # MAAS/curtin hand the netplan config wrapped in a top-level "network:"
        # key (this is the exact shape seen on a real deploy). translate_network
        # must unwrap it; otherwise the static config is silently dropped and
        # the node falls back to DHCP.
        netcfg = {"network": {"version": 2, "ethernets": {"ens3": {
            "addresses": ["172.16.0.207/24"],
            "gateway4": "172.16.0.1",
            "match": {"macaddress": "52:54:00:ef:89:5c"},
            "mtu": 1500,
            "nameservers": {"addresses": ["172.16.0.1"], "search": ["maas"]},
            "set-name": "ens3"}}}}
        f = self._only_file(self.h.translate_network(netcfg))
        self.assertIn("Name=ens3", f["content"])
        self.assertIn("Address=172.16.0.207/24", f["content"])
        self.assertIn("Gateway=172.16.0.1", f["content"])
        self.assertIn("DNS=172.16.0.1", f["content"])
        self.assertIn("Domains=maas", f["content"])
        self.assertNotIn("DHCP=yes", f["content"])

    def test_v2_dhcp(self):
        netcfg = {"version": 2, "ethernets": {"enp1s0": {"dhcp4": True}}}
        f = self._only_file(self.h.translate_network(netcfg))
        self.assertIn("DHCP=yes", f["content"])
        self.assertNotIn("Address=", f["content"])
        self.assertNotIn("Gateway=", f["content"])
        self.assertNotIn("DNS=", f["content"])

    def test_v1_dhcp(self):
        netcfg = {"version": 1, "config": [{
            "type": "physical", "name": "eth0",
            "subnets": [{"type": "dhcp4"}]}]}
        f = self._only_file(self.h.translate_network(netcfg))
        self.assertIn("DHCP=yes", f["content"])
        self.assertNotIn("Address=", f["content"])
        self.assertNotIn("Gateway=", f["content"])

    def test_v1_static(self):
        netcfg = {"version": 1, "config": [{
            "type": "physical", "name": "eth0",
            "subnets": [{"type": "static", "address": "10.0.0.5/24", "gateway": "10.0.0.1"}],
            "dns_nameservers": ["8.8.8.8"]}]}
        f = self._only_file(self.h.translate_network(netcfg))
        self.assertIn("Name=eth0", f["content"])
        self.assertIn("Address=10.0.0.5/24", f["content"])
        self.assertIn("Gateway=10.0.0.1", f["content"])
        self.assertIn("DNS=8.8.8.8", f["content"])

    def test_empty_returns_none(self):
        self.assertIsNone(self.h.translate_network({}))
        self.assertIsNone(self.h.translate_network({"version": 2, "ethernets": {}}))


if __name__ == "__main__":
    unittest.main()
