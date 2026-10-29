"""Offline regression tests for release selection and minimal go.mod edits."""

import unittest

from update_go import update_module


def release(version, stable=True):
    return {"version": f"go{version}", "stable": stable}


class UpdateGoTests(unittest.TestCase):
    def test_numeric_order_and_prereleases(self):
        updated, version = update_module(
            b"module example.com/test\n\ngo 1.27.3\n",
            [release("1.27.9"), release("1.28.0rc1", False), release("1.27.10")],
        )
        self.assertEqual(version, "1.27.10")
        self.assertIn(b"go 1.27.10\n", updated)

    def test_new_minor_preserves_other_content_and_line_endings(self):
        original = b"module example.com/test\r\n\r\ngo 1.27.3 // minimum\r\nrequire example.com/lib v1.0.0\r\n"
        updated, version = update_module(original, [release("1.28.0"), release("1.27.9")])
        self.assertEqual(version, "1.28.0")
        self.assertEqual(updated, original.replace(b"1.27.3", b"1.28.0"))

    def test_current_or_newer_module_is_unchanged(self):
        for version in ("1.27.3", "1.28.0"):
            with self.subTest(version=version):
                original = f"go {version}\n".encode()
                self.assertEqual(update_module(original, [release("1.27.3")]), (original, version))

    def test_two_component_directive(self):
        self.assertEqual(update_module(b"go 1.27\n", [release("1.27.3")]), (b"go 1.27.3\n", "1.27.3"))

    def test_invalid_feed_fails(self):
        for feed in ([], {}, [release("1.28.0rc1", False)], [release("1.28.0rc1")]):
            with self.subTest(feed=feed), self.assertRaises(ValueError):
                update_module(b"go 1.27.3\n", feed)

    def test_missing_or_duplicate_directive_fails(self):
        for content in (b"module example.com/test\n", b"go 1.27.3\ngo 1.27.4\n"):
            with self.subTest(content=content), self.assertRaises(ValueError):
                update_module(content, [release("1.27.4")])


if __name__ == "__main__":
    unittest.main()
