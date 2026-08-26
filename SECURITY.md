# Security policy

bastion runs with your cloud credentials and root access to your dev box, so
security reports get priority.

## Reporting

Please report vulnerabilities privately via
[GitHub security advisories](https://github.com/sanketsaurav/bastion/security/advisories/new)
rather than public issues. You should hear back within a few days.

## Scope notes

The trust model (SPEC.md §11): the box definition and the VM are trusted;
bastion defends against accidental exposure, injection through remote argv,
and secret leakage into state, logs, or digests. Reports about a compromised
VM reaching things you deliberately forwarded to it are working as designed;
reports about bastion widening exposure beyond the definition are exactly
what we want to hear about.

## Verifying releases

Release archives ship with `checksums.txt`, signed keyless via Sigstore:

```console
$ cosign verify-blob \
    --certificate checksums.txt.pem --signature checksums.txt.sig \
    --certificate-identity-regexp 'github.com/sanketsaurav/bastion' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    checksums.txt
$ shasum -a 256 -c checksums.txt --ignore-missing
```
