This folder contains 2 sysextensions

work.raw contains a simple script called `hello.sh` that prints "Hello World" to the console.
hello-broke.raw contains a simple script called `hello.sh` that prints "Hello World" to the console, but it is NOT signed or verity checked.

Both extensions need to have a `extension-release.NAME` with `ID=_any` for it to be identified as a sysextension.
Full path on the image should be `usr/lib/extension-release.d/extension-release.NAME`

work.raw is verity+signed with the db.key and db.crt test keys found under tests/assets/keys

The idea is to copy them into the Kairos iso overlay folder and test the verity+signed sysextension loading.

immucore should only copy the valid ones and ignore the invalid ones. A warning should be logged int he immucore log.

The test idea is as follows:
1. Copy the sysextensions to the overlay folder on test preparation
2. Build the uki iso with the overlay files on it and sign it with the same test keys
3. Boot the uki iso and check if the sysextensions are loaded correctly
4. Check if we got a warning for hello-broke.raw
5. Check if the work.raw extension was moved onto /run/extensions and loaded correctly
6. Check if the hello.sh script is executed correctly, as it should be loaded
7. Check if the sysext service is running with the override from kairos with the policy



The sysextensions are really stupid, its just a /usr/local/bin/ dir with a hello.sh script on them.
work.raw was built with systemd-repart so it would be verity+signed
```bash
systemd-repart -S -s SOURCE_DIR OUTPUT_FILE --private-key=tests/assets/keys/db.key --certificate=tests/assets/keys/db.pem
```

The other one was built with [sysext-bakery](https://github.com/flatcar/sysext-bakery) which makes it wasy to build sysextensions, but doesnt have support for signing or verity yet. So its simple to generate images with it but they wont work on UKI.
```bash
bake.sh SOURCE_DIR
```
