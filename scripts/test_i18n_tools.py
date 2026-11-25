#!/usr/bin/env python3
"""Tests for scripts/i18n_remediate.py and the keep-English waiver of i18n_review_chunked.py. Run: python3 scripts/test_i18n_tools.py"""
import importlib.util, os, re, sys, unittest
D = os.path.dirname(os.path.abspath(__file__))
def load(n):
    s = importlib.util.spec_from_file_location(n, f"{D}/{n}.py"); m = importlib.util.module_from_spec(s); s.loader.exec_module(m); return m
R = load("i18n_remediate")
EN = {"a.one": "One", "a.two": "Approves", "b.text": "Posting text", "c.login": "Sign in with Passkey", "z.last": "Last"}
TR = {"a.one": "Uno", "a.two": "Aproba", "b.text": "libro electrónico", "c.login": "Iniciar con Passkey", "z.last": "Último"}
class T(unittest.TestCase):
    def test_range_endpoints_are_not_named(self):
        self.assertEqual(R.named_keys(["[a.one..z.last] mixed tú y usted"], EN, TR), [])
    def test_named_key_in_body(self):
        self.assertEqual(R.named_keys(["[a.one..z.last] b.text mistranslated"], EN, TR), ["b.text"])
    def test_quoted_string_finds_key_in_slice(self):
        self.assertEqual(R.named_keys(["[a.one..b.text] 'Aproba' es forma incorrecta"], EN, TR), ["a.two"])
    def test_quoted_string_outside_slice_ignored(self):
        self.assertEqual(R.named_keys(["[a.one..a.two] 'libro electrónico' wrong"], EN, TR), [])
    def test_keep_english_term_never_matched(self):
        self.assertEqual(R.named_keys(["[a.one..z.last] 'Passkey' no traducido"], EN, TR, keep={"Passkey"}), [])
    def test_waiver(self):
        with open(f"{D}/i18n_review_chunked.py") as f: src = f.read()
        ns = {"json": __import__("json"), "os": os, "re": re}; exec(src[src.index("KEEP ="):src.index("ap = argparse")].replace("os.path.dirname(os.path.abspath(__file__))", repr(D)), ns)
        w = ns["waived"]
        self.assertTrue(w("'Passkey' no traducido en login.button"))
        self.assertTrue(w("Untranslated English word 'Token' in login.bootstrap.token"))
        self.assertFalse(w("Untranslated words 'Token' and 'Contact' in key X"))
        self.assertFalse(w("'Contact' not translated in the Token message"))
        self.assertFalse(w("Passkey untranslated in login.button"))   # no quoted string: not waived
        self.assertFalse(w("'Passkey' not translated and the verb in login.button is wrong"))
        self.assertFalse(w("'Token' untranslated; also 'Passkey' mistranslated as 'Claves'"))
        self.assertFalse(w("'Token' untranslated and the register is inconsistent"))
        self.assertTrue(w("'Passkey' no traducido en login.button"))
        self.assertFalse(w("'Aproba' es forma incorrecta"))
        self.assertFalse(w("Untranslated English word 'Contact' in freemail message"))
        self.assertFalse(w("Passkey mistranslated as 'Claves de acceso'"))   # a complaint other than "untranslated" is never waived
    def test_delta_keys_must_exist(self):
        import subprocess, json, tempfile
        with tempfile.TemporaryDirectory() as td:
            for n in ("en", "xx"): json.dump({"a": "A"}, open(f"{td}/{n}.json", "w"))
            r = subprocess.run([sys.executable, f"{D}/i18n_review_chunked.py", "--reviewer", "/nonexistent", "--src", f"{td}/en.json", "--translated", f"{td}/xx.json",
                                "--lang", "xx", "--provider", "p", "--out", f"{td}/o.json", "--keys", "nope"], capture_output=True, text=True)
        self.assertEqual(r.returncode, 2); self.assertIn("not in the catalogue", r.stdout)
if __name__ == "__main__": unittest.main()
