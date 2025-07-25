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
    - [ ] Check if Kairos-docs has any open PRs that need to be merged
    - [ ] Check if osbuilder is in the wanted version/latest
    - [ ] Check if k3s versions are correct (latest 3 versions should be available)
    - [ ] Check if any kairos/packages were bumped. Ensure they were merged and repo updated (https://github.com/kairos-io/kairos-framework/issues/2)
      - [ ] Cut a new release of the kairos-framework images
      - [ ] Bump the [kairos-framework image in kairos](https://github.com/kairos-io/kairos/blob/b334bb013c0b3ad63740e5da27d896d5d5fea81e/Earthfile#L12)
    - [ ] CI tests are passing
    - [ ] There are [no critical CVEs in our internal components](https://github.com/kairos-io/security)
    - [ ] Consider cutting an `rc`, `alpha`, ... based on changes on the CI
- [ ] **Stage 1 - Manual testing**
  - How: Using the assets from master, make sure that test scenarios not covered by automatic tests are passing, and that docs are still aligned
    - [ ] Generic hardware install
      - [ ] Manual upgrade
      - [ ] Interactive install
      - [ ] Manual recovery reset
      - [ ] Automatic reset
      - [ ] Provider decentralized test ([like we used to run automatically](https://github.com/kairos-io/kairos/issues/2709))
    - [ ] RPi Standard Install (helps validate that partition expansion is working)
      - [ ] Manual upgrade
      - [ ] Passive booting
      - [ ] Recovery booting
      - [ ] Manual recovery reset
      - [ ] Automatic reset
      - [ ] /oem exists
      - [ ] k3s is running
    - [ ] Go through any of the known issues https://kairos.io/docs/
- [ ] **Stage 3 - Release**
  - [ ] Tag the release on master
  - [ ] Update the release with any known issues
- [ ] **Stage 4 - Announcement**
  - [ ] Merge docs updates for kairos and k3s version updates
  - [ ] Create a branch `release/vX.Y.Z` on the docs (not tagging), so the new release can be built and displayed on the menu. Ideally open a PR so we can review and add/remove some commits if needed (in case we have documented WIP which is not available on the given release)
  - [ ] Blog post announcement
