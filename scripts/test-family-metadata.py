#!/usr/bin/env python3
"""Exercise real package metadata rendering without building its container image."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]

class FamilyMetadata(unittest.TestCase):
    def render(self, group, minimum=None):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            cli = directory / 'crossplane'
            cli.write_text('''#!/usr/bin/env python3
import os, pathlib, shutil, sys
root = pathlib.Path(sys.argv[sys.argv.index('--package-root')+1])
shutil.copyfile(root/'crossplane.yaml', os.environ['CAPTURE_METADATA'])
''')
            cli.chmod(0o755)
            env = dict(os.environ, CAPTURE_METADATA=str(directory/'metadata.yaml'))
            env.pop('FAMILY_MIN_VERSION', None)
            if minimum is not None:
                env['FAMILY_MIN_VERSION'] = minimum
            subprocess.run(['bash', str(ROOT/'scripts/build-family.sh'), group, 'v9.9.9', str(directory),
                            str(cli), 'registry.example', 'amd64', 'linux_amd64'],
                           cwd=ROOT, env=env, check=True, capture_output=True)
            return (directory/'metadata.yaml').read_text()

    def test_default_release_keeps_its_own_family_minimum(self):
        self.assertIn('version: ">= v9.9.9"', self.render('workers'))

    def test_compatible_candidate_uses_explicit_family_minimum(self):
        metadata = self.render('workers', 'v0.4.2')
        self.assertIn('version: ">= v0.4.2"', metadata)
        self.assertIn('pkg.crossplane.io/provider-family: provider-family-cloudflare', metadata)
        self.assertIn('provider: xpkg.upbound.io/wildbitca/provider-family-cloudflare', metadata)

    def test_family_package_does_not_depend_on_itself(self):
        self.assertNotIn('dependsOn:', self.render('config', 'v0.4.2'))

if __name__ == '__main__':
    unittest.main()
