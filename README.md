# CAPTuring

**Cluster API Provider Turing Pi.** Infrastructure provider that drives a [Turing Pi 2](https://turingpi.com/) BMC (`tpi` REST API) from Cluster API.

`CAPT` is Tinkerbell. This project is **CAPTuring**.

## Status

v1 talks to a BMC for power and optional eMMC flash. It does not PXE-boot, talk Redfish, or install Flux. Pair it later with Talos CABPT/CACPPT.

| Kind | Role |
|------|------|
| `TuringPiCluster` | BMC URL + credentials Secret. Ready when the BMC answers. |
| `TuringPiMachine` | Slot `node: 1–4`. Create powers on (optional `imageURL` flash). Delete powers off. |
| `TuringPiMachineTemplate` | Template for MachineDeployment / TalosControlPlane. |

`providerID` format: `turingpi://<bmc-host>/node/<n>`.

## Fake BMC

Tests use `pkg/bmc/fake`: an `httptest` server with the firmware ≥ 2.0 wire format (authenticate, power get/set, flash upload/progress). No hardware required.

```bash
make test   # includes a ≥90% coverage gate on pkg/bmc and internal/controller
```

## Example

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TuringPiCluster
metadata:
  name: turos
spec:
  controlPlaneEndpoint:
    host: 10.88.0.21
    port: 6443
  bmc:
    url: https://10.88.0.11
    insecureSkipVerify: true
    credentialsRef:
      name: turingpi-bmc
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TuringPiMachine
metadata:
  name: turos1
spec:
  node: 1
```

Secret keys: `username`, `password`.

## Adopt an existing node

Set `capturing.infrastructure.cluster.x-k8s.io/adopted: "true"` on the `TuringPiMachine`. CAPTuring will not flash, will power the slot on if it is off, and **will not power off** when the object is deleted.

Install in-cluster:

```bash
kubectl apply -k config/
```

Image: `ghcr.io/ferrix/capturing:1.0.0` (linux/arm64 + linux/amd64). Arm64 builds on `runs-on: turos-arm64-capturing`.

Versions are **semantic-release** on `main` from conventional commits (current baseline **v1.0.0**):

| Commit | Release |
|--------|---------|
| `fix:` | patch (`1.0.1`) |
| `feat:` | minor (`1.1.0`) |
| `feat!:` / `BREAKING CHANGE:` | major (`2.0.0`) |

CI writes `CHANGELOG.md`, tags `vX.Y.Z`, GitHub Release, GHCR `:X.Y.Z` / `:X.Y` / `:X`, and bumps GitOps `images.newTag`. `chore(release):` commits use `[skip ci]`.

## License

MIT
