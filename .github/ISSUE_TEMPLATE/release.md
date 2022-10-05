---
name: 'Kairos Release'
about: 'Start a new kairos release.'
labels: release
title: 'Kairos release v'
assignees: mudler
---

## 🗺 What's left for release

<List of items with remaining PRs and/or Issues to be considered for this release>

## 🔦 Highlights

< top highlights for this release notes >

## ✅ Release Checklist

- [ ] **Stage 0 - Finishing Touches**
    - [ ] Check kairos/packages, and for any needed update
    - [ ] Make sure CI tests are passing.
- [ ] **Stage 1 - Infrastructure Testing**
  - How: Using the testing version, make sure that manual and k8s upgrades are working from the latest release, and that docs are still aligned
  - Where:
    - [ ] Two kairos nodes
      - [ ] Deploy latest release with automatic node setup
      - [ ] Upgrade to testing release
      - [ ] Analyze
        - [ ] Create deployments
        - [ ] Keep cluster running overnight
        - [ ] Run upgrades and verify workload is still running
        - [ ] Keep cluster running overnight
- [ ] **Stage 3 - Release**
  - [ ] Tag the release on master.
    - [ ] Run `NO_PUSH=true go run ./.github/tag.go <tag>` to check that the correct tag will be created
    - [ ] Run `go run ./.github/tag.go <tag>` to tag a new release
- [ ] **Stage 4 - Update Upstream**
  - [ ] Update the examples to the final release
  - [ ] Update the upstream testing branches to the final release and create PRs.
- [ ] Make required changes to the release process.
