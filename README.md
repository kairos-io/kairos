# cOS + k3s = c3OS

A dead simple [cOS](https://github.com/rancher-sandbox/cOS-toolkit) derivative with k3s.

## Build

Needs only docker.

Run `build.sh`

## Cloud-init examples

Examples for k3s running as agent or server are in `examples/`. Install the ISO with `cos-install --config <config-file>` or either place it in `/oem` after install.

## Default user

user: c3os
pass: c3os