# Hugging Face Model Strategy (Practical Version)

## What we are trying to do

We want a simple and reliable way for teams to use models from Hugging Face without pulling random artifacts from anywhere.

The goal is:

- easy model consumption for teams,
- strong security checks on what we download,
- and predictable, repeatable model versions in production.

## Why this matters

If teams pull models ad hoc, we get:

- unknown model sources,
- mutable versions (hard to reproduce),
- weak traceability when things go wrong.

If we add a trusted path, we get:

- safer model usage,
- faster onboarding for teams,
- fewer production surprises.

## Value we bring

- **Reduce risk:** block unapproved or tampered model files.
- **Increase speed:** one standard path for model usage instead of custom team workflows.
- **Improve reliability:** pinned revisions mean reproducible behavior.
- **Improve traceability:** every accepted download has verification evidence.

## Security approach (without ClamAV)

Model files are large, so AV scanning is not the best primary control here.

Instead we focus on controls that scale:

1. **Trusted source only**
   - Only allow model repos from our approved HF org namespace.

2. **Pin exact revision**
   - Use commit SHA, not floating branch or tag references.

3. **Allowlisted files**
   - Only download expected files for each model/revision.

4. **Digest verification**
   - Verify SHA256 for each required file.
   - Fail closed on mismatch.

5. **Manifest requirement**
   - Require a model manifest with expected files and digests.

## How we use Sigstore

Sigstore gives us a strong trust signal for model artifacts.

- **At publish time**
  - Sign model bundles or manifests.
  - Bind signatures to workload/developer identity through OIDC.

- **At consume time**
  - Verify signature and identity.
  - Verify transparency log inclusion.
  - Reject artifacts that fail verification.

This gives us provenance we can trust and audit later.

## Lightweight lifecycle (not heavy process)

Keep states simple:

- `under_review`: candidate model
- `supported`: approved to use
- `recommended`: default choice

This is enough to guide teams without adding too much process overhead.

## What teams get

- A clear way to ask for and consume approved models.
- Fewer one-off security reviews.
- Better confidence in what goes to production.

## What platform/security gets

- Consistent controls for model downloads.
- Better incident response with verification evidence.
- Clear metrics on adoption and blocked risky downloads.

## How we measure success

- % downloads that pass policy and verification.
- Number of blocked unapproved/tampered downloads.
- Time to move a model from review to supported.
- % production deployments with revision + digest evidence.

## Bottom line

This is not about making things bureaucratic.

It is about giving teams a fast default that is also safe:

- trusted source,
- pinned version,
- verified files,
- signed provenance.
# Governed Hugging Face Model Supply Chain Strategy

## Purpose

Define a high-level organizational strategy for consuming Hugging Face models with strong security, verifiable provenance, and practical governance at enterprise scale.

This strategy is intentionally non-implementation and focused on business value, policy, and operating model decisions.

## Executive Value Proposition

By governing model intake and distribution through a single trusted path, the organization gains:

- Reduced supply chain risk from unvetted and mutable model artifacts.
- Faster AI delivery through standardized policy gates instead of ad hoc approvals.
- Stronger compliance posture with auditable evidence for approvals and provenance.
- Reproducible deployments by pinning immutable revisions and verifying integrity.
- Clear ownership and accountability across model producers, approvers, and consumers.

## What Problem This Solves

Without governance, model usage tends to be fragmented:

- Teams fetch models from mixed sources and mutable references.
- Security checks vary by team and are difficult to verify later.
- Compliance metadata may be incomplete or inconsistent.
- Incident response is slow because lineage and approval records are scattered.

This strategy turns model hosting into a governed supply chain capability.

## Scope and Trust Boundary

### In Scope

- Hugging Face model artifacts consumed by internal teams.
- Policy-based approval, promotion, and consumption controls.
- Cryptographic integrity and provenance verification.
- Audit evidence and reporting.

### Out of Scope

- Malware scanning of very large model binaries with ClamAV.

Large model size and low scanning signal make AV scanning an inefficient primary control. Integrity, provenance, and policy gates provide stronger and more scalable security assurances for this use case.

## Governance Charter

### Roles and Responsibilities

- **Model Producer**
  - Publishes candidate model artifacts and metadata.
  - Ensures training and packaging evidence is complete.
- **Governance Reviewer**
  - Validates policy, risk, and compliance requirements.
  - Approves state transitions.
- **Platform Owner**
  - Owns policy definitions, control enforcement, and reporting.
  - Maintains trust roots, signing identity policy, and exceptions.
- **Model Consumer**
  - Uses only approved model references and supported lifecycle states.

### Approval Authority

- Only designated governance reviewers can promote models to production-eligible states.
- Emergency exceptions require documented approval and expiry date.

### Policy Ownership

- Platform and security jointly own global control policy.
- Domain teams own model-specific metadata within approved policy schema.

## Security and Trust Strategy

### 1) Source Control and Access

- Allow model consumption only from approved org-controlled Hugging Face repositories.
- Enforce least-privilege tokens and organization identity controls.
- Restrict write/publish permissions to authorized CI or publisher identities.

### 2) Immutable and Deterministic Intake

- Require revision pinning with commit SHA (no floating tag or branch references).
- Allow only policy-approved file names and file types.
- Require a model manifest that defines expected artifacts and digests.

### 3) Mandatory Integrity Verification

- Verify each artifact digest before acceptance.
- Reject downloads on any mismatch or missing required file.
- Persist verification evidence for audit and incident response.

### 4) Governance Metadata Validation

- Require model card metadata for license, intended use, limitations, ownership, and risk tier.
- Reject promotion when mandatory metadata is missing or policy-invalid.

## Sigstore Strategy for Provenance and Trust

Sigstore model signing is the recommended provenance backbone for this program.

### Signing Policy (Publisher Side)

- Sign approved model bundles or manifests at publish time.
- Bind signatures to workload or developer identity through OIDC-issued certificates.
- Store signature bundle and transparency proof with model release evidence.

### Verification Policy (Consumer Side)

- Verify signature validity before download acceptance.
- Verify certificate identity against approved issuer and identity patterns.
- Verify transparency log inclusion to support tamper detection and non-repudiation.
- Reject artifacts that fail identity, signature, or provenance checks.

### Trust Root Policy

- Maintain approved signer identity patterns and issuers centrally.
- Rotate and review trust policy on a defined cadence.
- Record all policy changes with approver and rationale.

## Model Lifecycle Governance

Use explicit model states:

- `under_review`: candidate model; restricted usage.
- `supported`: approved for production use.
- `recommended`: preferred default for new workloads.

Promotion criteria should include:

- Required metadata present and valid.
- Policy checks passed.
- Digest and provenance checks passed.
- Governance approval recorded.

## Secure Download and Consumption Requirements

All production-bound model downloads must satisfy:

- Source repo is org allowlisted.
- Revision is immutable and policy-approved.
- Manifest and file digests match policy.
- Signature/provenance checks pass.
- Audit record is emitted and retained.

If any required control fails, the download is denied.

## Operating Model

1. **Submission**
   - Producer submits candidate model reference and metadata.
2. **Automated Validation**
   - Policy, metadata, digest, and provenance checks run.
3. **Governance Review**
   - Reviewer validates risk and compliance context.
4. **Promotion**
   - Model transitions lifecycle state when approved.
5. **Consumption**
   - Consumers can only use approved references and states.
6. **Monitoring**
   - Continuous reporting on policy adherence and exceptions.

## KPI Framework

Track outcomes that tie governance to business value:

- Percentage of downloads that are policy-approved.
- Number of blocked unapproved or tampered download attempts.
- Time to promote from candidate to supported.
- Percentage of production deployments with complete provenance evidence.
- Number of exception approvals and exception expiry compliance.
- Reduction in model-related security and compliance incidents.

## KPI Reporting Model

- **Weekly operational report**
  - Policy pass/fail rates, blocked download attempts, and open exceptions.
- **Monthly governance review**
  - Promotion lead time, provenance coverage, and exception aging by domain.
- **Quarterly leadership review**
  - Trend analysis, risk reduction outcomes, and policy updates required.
- **Incident-triggered report**
  - End-to-end traceability for impacted model artifacts, approvals, and signer identities.

Reporting owners:

- Platform owner publishes operational and monthly governance reports.
- Security and compliance co-sign quarterly outcomes and control effectiveness.

## Phased Rollout

### Phase 1: Foundation

- Establish org allowlist, immutable revision pinning, manifest and digest requirements.
- Launch baseline governance workflow and audit reporting.

### Phase 2: Provenance Hardening

- Introduce Sigstore signing and verification policies.
- Enforce trusted identity patterns and transparency evidence checks.

### Phase 3: Governance Maturity

- Standardize lifecycle promotion criteria and exception handling.
- Publish leadership dashboards for risk and adoption metrics.

### Phase 4: Continuous Assurance

- Perform periodic policy reviews and trust root updates.
- Run tabletop incident response exercises using recorded audit evidence.

## External Alignment

This strategy aligns with patterns used by mature platforms and standards:

- Hugging Face enterprise controls for organization and repository governance.
- Stage-based model approval models used by managed ML registries.
- Sigstore model-signing recommendations for model integrity.
- SLSA-style provenance verification and trust-root based policy enforcement.

## Decision Statement

The organization should treat model hosting as a governed supply chain, not a storage problem.

The primary controls are policy, immutable references, digest validation, and Sigstore-backed provenance checks, enforced through a clear lifecycle governance model and measurable KPI outcomes.
