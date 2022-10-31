# :book: Kairos documentation

The Kairos documentation uses [docsy](https://docsy.dev).

## Test your changes

After cloning the repo (with submodules), just run `make serve` to test the website locally.

```
$> git clone --recurse-submodule https://github.com/kairos-io/kairos
$> cd kairos/docs
$> make serve
```

If you have a local copy already checked out, sync the submodules:

```
$> git submodule update --init --recursive --depth 1
```

To run the website locally in other platforms, e.g. MacOS:

```
$> HUGO_PLATFORM=macOS-64bit make serve
```
