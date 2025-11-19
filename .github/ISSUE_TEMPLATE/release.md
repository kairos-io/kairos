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
    - [ ] Check for [critical CVEs in our internal components](https://github.com/kairos-io/security). Do any necessary bumps or PR merges to get all components to good state.
    - [ ] Check if the desired versions of the binaries are referenced in [the kairos-init Makefile](https://github.com/kairos-io/kairos-init/blob/fea3a17d511f70b66a4972f43f601ba6cc9105f3/Makefile#L2-L6) (check for renovate bot PRs)
    - [ ] Bump versions if needed and cut a new release of kairos-init
    - [ ] Bump the kairos-init version [on the kairos Dockerfile](https://github.com/kairos-io/kairos/blob/6deaa69ead774ee052de894a9c56b952952a68d3/images/Dockerfile#L2)
    - [ ] CI tests are passing
    - [ ] Check if [Kairos docs](https://github.com/kairos-io/kairos-docs/) has any open PRs that need to be merged
    - [ ] Check if osbuilder is in the wanted version/latest
    - [ ] Check if k3s versions are correct (latest 3 versions should be available)
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
  - [ ] Perform a manual commit on the `docs` repo to trigger CI (`git commit --allow-empty -m "Trigger Build`)
- [ ] **Stage 4 - Announcement**
  - [ ] Blog post announcement
