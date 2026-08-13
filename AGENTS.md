# inari-plugin-sdk — Agent Guide

Go SDK for Inari backend extensions: handshake, lifecycle, auth context, helpers (plan §6 #6, §5.8).

Stack: Go, hashicorp go-plugin

## Key architecture constraints
- Plugins run as sidecar containers/subprocesses: handshake cookie, protocol version, checksum verification, crash isolation (§5.8).
- Versioned gRPC contract defined in the `inari-api` repo — pin its packages (§6).
- Plugin HTTP endpoints surface through `/api/extensions/<name>/*` (control plane authenticates, enforces RBAC, strips sensitive headers, reverse-proxies) (§5.8).
- First-party extensions (inari-ext-argocd) build on this SDK — keep the API minimal and stable.

## Conventions
- Conventional Commits; SemVer releases; container images/artifacts cosign-signed (once CI exists).
- Write tests for new behavior; keep changes minimal and focused.
- Canonical architecture & development plan: https://github.com/7K-Inari/inari-docs/blob/main/docs/architecture/inari-platform-plan.md (section references below point into it).

## Platform design principles (apply everywhere)
1. Tenant-aware to the core — every object carries a tenant ID; every API decision is tenant-scoped.
2. Zero tenant credentials on the hub — no tenant kubeconfigs or cloud keys in the control plane.
3. Pull, never push — agents dial out; the control plane never initiates connections into tenant networks.
4. Desired state, eventually reconciled — GitOps/CR-based mutations, not imperative RPCs.
5. The catalog is a projection of reality — capabilities are discovered, not declared.
6. Small kernel, everything else extension.
7. Modular monolith first — strict internal module boundaries.
