# Ubuntu non-HWE image

Our Ubuntu based images, will use HWE kernels. If you need to use a non-HWE one, you can build an image of your own with the kernel of your choice, and then use it as your `BASE_IMAGE`. Here's an example:

We are going to assume that you start the process at the root of the Kairos repo and that the non-HWE image is the one in the Dockerfile within the same directory as this README.md file.

Let's start by building the base image.

```
$ cd examples/byoi/ubuntu-non-hwe
$ docker build -t ubuntu-non-hwe:22.04 .
[+] Building 58.7s (13/13) FINISHED                                                      docker:default
 => [internal] load build definition from Dockerfile                                               0.0s
 => => transferring dockerfile: 577B                                                               0.0s
 => [internal] load metadata for docker.io/library/ubuntu:22.04                                    0.4s
 => [internal] load metadata for quay.io/kairos/kairos-init:v0.2.6                                 0.5s
 => [internal] load .dockerignore                                                                  0.0s
 => => transferring context: 2B                                                                    0.0s
 => [kairos-init 1/1] FROM quay.io/kairos/kairos-init:v0.2.6@sha256:35f581dbc480385b21f7a22317fc5  0.0s
 => [base-kairos 1/7] FROM docker.io/library/ubuntu:22.04@sha256:ed1544e454989078f5dec1bfdabd8c5c  0.0s
 => CACHED [base-kairos 2/7] COPY --from=kairos-init /kairos-init /kairos-init                     0.0s
 => CACHED [base-kairos 3/7] RUN /kairos-init -l debug -s install --version "v0.0.1"               0.0s
 => [base-kairos 4/7] RUN apt-get remove -y linux-base linux-image-generic-hwe-22.04 && apt-get a  2.3s
 => [base-kairos 5/7] RUN apt-get install -y --no-install-recommends linux-image-generic          18.4s
 => [base-kairos 6/7] RUN /kairos-init -l debug -s init --version "v0.0.1"                        34.1s 
 => [base-kairos 7/7] RUN rm /kairos-init                                                          0.2s 
 => exporting to image                                                                             3.3s 
 => => exporting layers                                                                            3.3s 
 => => writing image sha256:eea47e62c3238b7f51301ce7ab99bbe43036b401d288dd27b7f1eb6f4193a5fa       0.0s 
 => => naming to docker.io/library/ubuntu-non-hwe:22.04   
```

You should now be able to use your container image `ubuntu-non-hwe:22.04` as base artifact to generate ISOs or raw images.Have a look at osbuilder-tools or AuroraBoot in the kairos documentation for how to build those.