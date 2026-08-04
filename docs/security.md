# Security and secret handling

The renderer processes untrusted repository content and may receive Secret objects as Flux substitution inputs.

## Source confinement

Every controller source must map to an existing local path. Render requests are confined to that source after symlink resolution. Kustomize can reference bases elsewhere within the source but cannot read or write outside it. The raw renderer does not follow symlinks.

Do not weaken confinement to accommodate a repository layout. Map the actual artifact root instead.

## Secrets

Flux seed objects may include Kubernetes Secrets. Their decoded values are held in memory for substitution. The executable:

- does not print substitution values in diagnostics;
- does not write intermediate decrypted manifests;
- does not read missing values from the process environment;
- writes final rendered output only when the caller requests it.

Rendered output can contain substituted secrets. Protect stdout, output files, CI logs, and uploaded artifacts accordingly. Do not commit real Secret seed files or rendered secret output.

Semantic comparison reports changes beneath Secret `data` and `stringData`, but replaces both values with redaction markers in summary and JSON output.

SOPS decryption is not currently implemented. Encrypted Secret data therefore fails normal Secret decoding rather than invoking an implicit external command.

## Network and execution

Current adapters and renderers perform no source fetches and launch no external configuration-management commands. Future source providers or external renderers must be explicit plan capabilities with documented network and execution boundaries.

Comparison reads the two caller-provided workspaces and does not invoke Git or execute preparation commands. Callers are responsible for creating and securing those workspaces before rendering.
