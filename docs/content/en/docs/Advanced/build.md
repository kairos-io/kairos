---
title: "Build Kairos appliances"
linkTitle: "Build"
weight: 5
description: >
---

{{% alert title="Note" %}}

This page is a work in progress!
The feature is experimental and API is likely going to be subject to changes, don't rely on it yet!
{{% /alert %}}


This documentation section describes how the Kairos Kubernetes Native API extensions can be used to build custom appliances or booting medium for Kairos.

While it's possible to just run Kairos from the artifacts provided by our release process, there are specific use-cases which needs extended customization, for example when
additional kernel modules, or custom, user-defined logics that you might want to embed in the media used for installations.

Note the same can be achieved by using advanced configuration and actually modify the images during installation phase by leveraging the `chroot` stages that takes place in the image - this is discouraged - as it goes in opposite with the "Single Image", "No infrastructure drift" approach of Kairos. The idea here is to create a system from "scratch" and applying it to our nodes - not to run any specifc logic in the node itself.

For such purposes Kairos provides a set of Kubernetes Native Extensions that allows to programmatically generate Installable mediums, Cloud Images and Netboot artifacts to provide on-demand customization to further exploit Kubernetes patterns to automatically provision nodes using control-plane management clusters - however, the same toolset can be used manually to build appliances in order to develop and debug locally.

The [automated](/docs/installation/automated) section already shows some examples of how to leverage the Kubernetes Native Extensions and use the Kairos images to build appliances, in this section we will cover and describe in detail how to leverage the CRDs and the Kairos factory to build custom appliances.

## Prerequisites

When building locally, only `docker` is required installed in the system, for building using the Kubernetes Native extensions, a Kubernetes cluster is required and `helm` and `kubectl` installed locally. Note [kind](https://github.com/kubernetes-sigs/kind) can be used as well. The Native extension don't require any special permission, and runs completely unprivileged.

### Kubernetes

If running on Kubernetes, we install the Kairos `osbuilder` controller.

The chart depends on cert-manager. You can install the latest version of cert-manager by running the following commands:

```bash
kubectl apply -f https://github.com/jetstack/cert-manager/releases/latest/download/cert-manager.yaml
kubectl wait --for=condition=Available deployment --timeout=2m -n cert-manager --all
```

Install the Kubernetes charts with `helm`:

```bash
helm repo add kairos https://kairos-io.github.io/helm-charts
helm repo update
helm install kairos-crd kairos/kairos-crds
helm install kairos-osbuilder kairos/osbuilder
```


## Build an ISO

To build an ISO, consider the following spec, which provides a hybrid bootable ISO (UEFI/MBR), with the `core` kairos image, adding `helm`:

```yaml
kind: OSArtifact
apiVersion: build.kairos.io/v1alpha1
metadata:
  name: hello-kairos
spec:
  imageName: "quay.io/kairos/core-opensuse:latest"
  iso: true
  bundles:
  # Bundles available at: https://packages.kairos.io/Kairos/
  - quay.io/kairos/packages:helm-utils-3.10.1
  cloudConfig: |
            #cloud-config
            users:
            - name: "kairos"
              passwd: "kairos"
            install:
              device: "auto"
              reboot: true
              poweroff: false
              auto: true # Required, for automated installations
```

Apply the manifest with `kubectl apply`.

Note, the CRD allows to specify a custom Cloud config file, [check out the full configuration reference](/docs/reference/configuration).

The controller will create a pod that builds the ISO ( we can follow the process by tailing to the containers log ) and later makes it accessible to its own dedicated service (nodeport by default):

```bash
$ PORT=$(kubectl get svc hello-kairos -o json | jq '.spec.ports[0].nodePort')
$ curl http://<node-ip>:$PORT/hello-kairos.iso -o output.iso
```
## Netboot artifacts

It is possible to use the CRD to prepare artifacts required for netbooting, by enabling `netboot: true` for instance:

```yaml
kind: OSArtifact
metadata:
  name: hello-kairos
spec:
  imageName: "quay.io/kairos/core-opensuse:latest"
  netboot: true
  netbootURL: ...
  bundles: ...
  cloudConfig: ...
```

## Build a Cloud Image

Cloud images are images that automatically boots into recovery mode and can be used to deploy whatever image you want to the VM. 
Custom user-data from the Cloud provider is automatically retrieved, additionally the CRD allows to embed a custom cloudConfig so that we can use to make configuration permanent also for VM images running outside a cloud provider.

A Cloud Image boots in QEMU and also in AWS, consider:

```yaml
apiVersion: build.kairos.io/v1alpha1
kind: OSArtifact
metadata:
  name: hello-kairos
spec:
  imageName: "quay.io/kairos/core-opensuse:latest"
  cloudImage: true
  cloudConfig: |
            #cloud-config
            users:
            - name: "kairos"
              passwd: "kairos"
            name: "Default deployment"
            stages:
              boot:
              - name: "Repart image"
                layout:
                  device:
                    label: COS_RECOVERY
                  add_partitions:
                    - fsLabel: COS_STATE
                      size: 16240 # At least 16gb
                      pLabel: state
              - name: "Repart image"
                layout:
                  device:
                    label: COS_RECOVERY
                  add_partitions:
                    - fsLabel: COS_PERSISTENT
                      pLabel: persistent
                      size: 0 # all space
              - if: '[ -f "/run/cos/recovery_mode" ] && [ ! -e /usr/local/.deployed ]'
                name: "Deploy cos-system"
                commands:
                  - |
                      # Use `elemental reset --system.uri docker:<img-ref>` to deploy a custom image
                      elemental reset && \
                      touch /usr/local/.deployed && \
                      reboot
```

Note: Since the image come with only the `recovery` system populated, we need to apply a cloud-config similar to this one which tells which container image we want to deploy.
The first steps when the machine boots into is to actually create the partitions needed to boot the active and the passive images, and its populated during the first boot.

After applying the spec, the controller will create a pod which runs the build process and create a `hello-kairos.raw` file, which is an EFI bootable raw disk, bootable in QEMU and compatible with AWS which automatically provision the node:

```bash
$ PORT=$(kubectl get svc hello-kairos -o json | jq '.spec.ports[0].nodePort')
$ curl http://<node-ip>:$PORT/hello-kairos.raw -o output.raw
```

Note, in order to use the image with QEMU, we need to resize the disk at least to 32GB, this can be done with the CRD by setting `diskSize: 32000` or by truncating the file after downloading:

```bash
truncate -s "+$((32000*1024*1024))" hello-kairos.raw 
```

This is not required if running the image in the Cloud as providers usually resize the disk during import or creation of new instances.

To run the image locally with QEMU we need `qemu` installed in the system, and we need to be able to run VMs with EFI, for example:

```bash
qemu-system-x86_64 -m 2048 -bios /usr/share/qemu/ovmf-x86_64.bin -drive if=virtio,media=disk,file=output.raw
```

### Use the Image in AWS


To consume the image, copy it into an s3 bucket:

```bash
aws s3 cp <cos-raw-image> s3://<your_s3_bucket>
```

Create a `container.json` file refering to it:

```json
{
"Description": "Kairos custom image",
"Format": "raw",
"UserBucket": {
  "S3Bucket": "<your_s3_bucket>",
  "S3Key": "<cos-raw-image>"
}
}
```

Import the image:

```bash
aws ec2 import-snapshot --description "Kairos custom image" --disk-container file://container.json
```

Follow the procedure described in [AWS docs](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/creating-an-ami-ebs.html#creating-launching-ami-from-snapshot) to register an AMI from snapshot. Use all default settings except for the firmware, set to force to UEFI boot.

## Build a Cloud Image for Azure

Similarly we can build images for Azure, consider:

```yaml
apiVersion: build.kairos.io/v1alpha1
kind: OSArtifact
metadata:
  name: hello-kairos
spec:
  imageName: "quay.io/kairos/core-opensuse:latest"
  azureImage: true
  ...
```

Will generate a compressed disk `hello-kairos-azure.vhd` ready to be used in GCE.

```bash
$ PORT=$(kubectl get svc hello-kairos -o json | jq '.spec.ports[0].nodePort')
$ curl http://<node-ip>:$PORT/hello-kairos-azure.vhd -o output.vhd
```

### How to use the image in Azure

Upload the Azure Cloud VHD disk in  `.vhda`  format to your bucket:

```bash
az storage copy --source <cos-azure-image> --destination https://<account>.blob.core.windows.net/<container>/<destination-azure-image>
```

Import the disk:

```bash
az image create --resource-group <resource-group> --source https://<account>.blob.core.windows.net/<container>/<destination-azure-image> --os-type linux --hyper-v-generation v2 --name <image-name>
```

Note:  There is currently no way of altering the boot disk of an Azure VM via GUI, use the `az` to launch the VM with an expanded OS disk if needed

## Build a Cloud Image for GCE


Similarly we can build images for GCE, consider:

```yaml
apiVersion: build.kairos.io/v1alpha1
kind: OSArtifact
metadata:
  name: hello-kairos
spec:
  imageName: "quay.io/kairos/core-opensuse:latest"
  gceImage: true
  ...
```

Will generate a compressed disk `hello-kairos.gce.raw.tar.gz` ready to be used in GCE.

```bash
$ PORT=$(kubectl get svc hello-kairos -o json | jq '.spec.ports[0].nodePort')
$ curl http://<node-ip>:$PORT/hello-kairos.gce.raw.tar.gz -o output.gce.raw.tar.gz
```

### How to use the image in GCE

To upload the image in GCE (compressed):

```bash
gsutil cp <cos-gce-image> gs://<your_bucket>/
```

Import the disk:

```bash
gcloud compute images create <new_image_name> --source-uri=<your_bucket>/<cos-gce-image> --guest-os-features=UEFI_COMPATIBLE
```

See [here how to use a cloud-init with Google cloud](https://cloud.google.com/container-optimized-os/docs/how-to/create-configure-instance#using_cloud-init_with_the_cloud_config_format).
