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
    - [ ] Check if Kairos-docs were updated and consider tagging them with the same version as Kairos
    - [ ] Check if osbuilder is in the wanted version/latest
    - [ ] Check if any kairos/packages was bumped and they were merged and repo updated (https://github.com/kairos-io/packages)
    - [ ] Check latest repository update was merged, otherwise trigger its job (https://github.com/kairos-io/kairos/actions/workflows/bump_repos.yml)
    - [ ] Make sure CI tests are passing.
    - [ ] Consider cutting an `rc`, `alpha`, ... based on changes on the CI
- [ ] **Stage 1 - Manual testing**
  - How: Using the assets from master, make sure that test scenarios not covered by automatic tests are passing, and that docs are still aligned
    - [ ] Fedora flavor install, and manual upgrade works
    - [ ] Any flavor interactive install
    - [ ] Any flavor recovery reset
    - [ ] ARM images (openSUSE, alpine) boots and manual upgrade works
    - [ ] ARM images passive and recovery booting
    - [ ] ARM images reset works
    - [ ] ARM images /oem exists
- [ ] **Stage 3 - Release**
  - [ ] Tag the release on master.
- [ ] **Stage 4 - Update provider-kairos**
  - [ ] Update go mod to consume `kairos-io/kairos`.
  - [ ] Check if any changes on the pipelines and building pieces are required
    - [ ] Flavor changes
    - [ ] `osbuilder` version bumps
  - [ ] Update the `CORE_VERSION` file of `kairos-io/provider` to match the release tag of `kairos-io/kairos`
  - [ ] Tag the release on `provider-kairos`
- [ ] **Stage 5 - Announcement**
  - [ ] Blog post announcement
