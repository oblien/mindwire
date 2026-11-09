# Publish the Python SDK

The distribution is named `mindwire`, with import namespace `mindwire`. A name
becomes claimed only after the first successful PyPI upload. Pending publishers
do not reserve names; check availability before the first release.

## One-time PyPI account setup

1. [Sign in or register](https://pypi.org/account/login/) on PyPI, verify your email
   and complete its two-factor authentication setup.
2. Open [Publishing](https://pypi.org/manage/account/publishing/) and add a pending
   **GitHub** publisher with these exact values:

   | Field | Value |
   | --- | --- |
   | PyPI project name | `mindwire` |
   | Owner | `oblien` |
   | Repository name | `mindwire` |
   | Workflow name | `publish-python.yml` |
   | Environment name | `pypi` |

3. The matching GitHub environment is `pypi`. Configure its deployment permissions
   under the [repository environments](https://github.com/oblien/mindwire/settings/environments).

Publishing uses GitHub's OIDC identity, so no PyPI password or persistent API token
is needed in GitHub secrets or source files. The first upload creates the PyPI
project in the account that configured the pending publisher.

## Publish

1. Update `__version__` in `src/mindwire/_version.py`. Package metadata, the public
   version and the User-Agent all use that value.
2. Commit and push the reviewed SDK changes to `main`.
3. Open [Python SDK workflow](https://github.com/oblien/mindwire/actions/workflows/publish-python.yml).
   Choose **Run workflow**, select `main`, and enable **Publish to PyPI**.

The workflow tests Python 3.10 and 3.14, builds a source archive and universal
Python wheel, validates their metadata, installs the built wheel and source
archive in clean environments, and tests against a fresh Go runtime. Only the
publish job has OIDC permission. It uploads the already-tested artifacts to PyPI
and then checks that a clean `pip install mindwire==VERSION` succeeds from PyPI.

Ordinary pushes and pull requests run checks without publishing. Manual runs with
the publish option disabled also only validate. Publication is restricted to the
repository's `main` branch. Bump the Python version for subsequent releases; PyPI
does not allow replacing previously uploaded distributions.

```sh
gh workflow run publish-python.yml --ref main -f publish=true
```

See [PyPI's Trusted Publishing documentation](https://docs.pypi.org/trusted-publishers/creating-a-project-through-oidc/).
