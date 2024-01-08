# Ubuntu non-HWE image

Our Ubuntu based images, will use HWE kernels. If you need to use a non-HWE one, you can build an image of your own with the kernel of your choice, and then use it as your `BASE_IMAGE`. Here's an example:

We are going to asume that you start the process at the root of the Kairos repo and that the non-HWE image is the one in the Dockerfile within the same directory as this README.md file.

Let's start by building the base image.

```
$ cd examples/byoi/ubuntu-non-hwe
$ docker build -t ubuntu-non-hwe:22.04 .
[+] Building 42.8s (6/6) FINISHED                                                                                                                            docker:default
 => [internal] load build definition from Dockerfile                                                                                                                   0.0s
 => => transferring dockerfile: 156B                                                                                                                                   0.0s
 => [internal] load .dockerignore                                                                                                                                      0.0s
 => => transferring context: 2B                                                                                                                                        0.0s
 => [internal] load metadata for docker.io/library/ubuntu:22.04                                                                                                        0.0s
 => CACHED [1/2] FROM docker.io/library/ubuntu:22.04                                                                                                                   0.0s
 => [2/2] RUN apt-get update &&       apt-get install -y --no-install-recommends       linux-image-generic                                                            37.9s
 => exporting to image                                                                                                                                                 4.9s
 => => exporting layers                                                                                                                                                4.9s
 => => writing image sha256:e68595542681417870bf3f0a2e51eafa898c3e43ee5c895f7c82d5f4e25df8db                                                                           0.0s 
 => => naming to docker.io/library/ubuntu-non-hwe:22.04                                                                                                                0.0s
```

Now, let's go to the images directory and build an Ubuntu Kairos image, based on `ubuntu-non-hwe:22.04`

```
cd ../../../images/
docker build --build-arg="BASE_IMAGE=ubuntu-non-hwe:22.04" \
             --build-arg="FAMILY=ubuntu" \
             --build-arg="FLAVOR=ubuntu" \
             --build-arg="FLAVOR_RELEASE=22.04" \
             --build-arg="VARIANT=core" \
             --build-arg="MODEL=generic" \
             --build-arg="FRAMEWORK_VERSION=v2.5.5" \
             --build-arg="RELEASE=v0.0.1" 
             -t kairos-ubuntu-non-hwe:22.04 \
             -f Dockerfile.kairos-ubuntu .
```

The `Dockerfile.kairos-ubuntu` Dockerfile will install all kairos dependencies if they are missing. When it comes to the kernel it will only install one if there's no existing kernel on your base image, then it will proceed to install the karios agent and do the rest of the process to convert the base image into a kairos image.

You should now be able to use your container image `kairos-ubuntu-non-hwe:22.04`. If you need an iso or other type of artifact, have a look at osbuilder-tools or AuroraBoot in the kairos documentation.