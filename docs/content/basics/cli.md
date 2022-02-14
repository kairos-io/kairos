+++
title = "CLI"
date = 2022-02-09T17:56:26+01:00
weight = 3
pre = "<b>- </b>"
+++

A CLI is provided as part of releases associated to each `c3os` version. 

The CLI can be used from an external machine to generate network tokens and pair nodes on first-boot.

```
./c3os --help                                                         
NAME:
   c3os - c3os (register|install)

USAGE:
    [global options] command [command options] [arguments...]

VERSION:
   0.1

DESCRIPTION:
   c3os registers and installs c3os boxes

AUTHOR:
   Ettore Di Giacinto

COMMANDS:
   register           
   create-config, c   
   generate-token, g  
   setup, s           
   get-kubeconfig     
   install, i         
   help, h            Shows a list of commands or help for one command
```

## `create-config`

Generates a new `c3os` configuration file which can be used as cloud-init, with a new unique network token:

```
$ ./c3os create-config  
c3os:
  network_token: b3RwOgogIGRodDoKICAgIGludGVydmFsOiA5MjIzMzcyMDM2ODU0Nzc1ODA3CiAgICBrZXk6IEVCMzJJMlNXTjJCNFBHNEtCWTNBUVBBS0FWRTY0Q0VLVUlDTktTUFVWVU5BWTM0QklEQ0EKICAgIGxlbmd0aDogMzIKICBjcnlwdG86CiAgICBpbnRlcnZhbDogOTIyMzM3MjAzNjg1NDc3NTgwNwogICAga2V5OiBDMk1RRk5DWEFVRElPWjVHM1pZUUIzVEVHTzVXVEdQR1pZSEVQQkY3SFEyVUROUlZCTkxRCiAgICBsZW5ndGg6IDMyCnJvb206IGp6Q29kQVVOWUZSUklQU3JISmx4d1BVUnVxTGJQQnh4CnJlbmRlenZvdXM6IG5NckRCbllyVVBMdnFPV0Z2dWZvTktXek1adEJIRmpzCm1kbnM6IGpQUUhIbVZza2x6V29xbWNkeVlnbVhMSVFjTE1HUFN6Cm1heF9tZXNzYWdlX3NpemU6IDIwOTcxNTIwCg==
  offline: false
  reboot: false
  device: ""
  poweroff: false
```

Now you can use this in your configuration file to create new c3os nodes:

```yaml
c3os:
  network_token: b3RwOgogIGRodDoKICAgIGludGVydmFsOiA5MjIzMzcyMDM2ODU0Nzc1ODA3CiAgICBrZXk6IEVCMzJJMlNXTjJCNFBHNEtCWTNBUVBBS0FWRTY0Q0VLVUlDTktTUFVWVU5BWTM0QklEQ0EKICAgIGxlbmd0aDogMzIKICBjcnlwdG86CiAgICBpbnRlcnZhbDogOTIyMzM3MjAzNjg1NDc3NTgwNwogICAga2V5OiBDMk1RRk5DWEFVRElPWjVHM1pZUUIzVEVHTzVXVEdQR1pZSEVQQkY3SFEyVUROUlZCTkxRCiAgICBsZW5ndGg6IDMyCnJvb206IGp6Q29kQVVOWUZSUklQU3JISmx4d1BVUnVxTGJQQnh4CnJlbmRlenZvdXM6IG5NckRCbllyVVBMdnFPV0Z2dWZvTktXek1adEJIRmpzCm1kbnM6IGpQUUhIbVZza2x6V29xbWNkeVlnbVhMSVFjTE1HUFN6Cm1heF9tZXNzYWdlX3NpemU6IDIwOTcxNTIwCg==
  offline: false
  reboot: false
  device: ""
  poweroff: false

# Cloud init syntax to setup users. 
# See https://rancher-sandbox.github.io/cos-toolkit-docs/docs/reference/cloud_init/
stages:
   network:
     - name: "Setup users"
       authorized_keys:
        c3os: 
        - github:yourhandle!
```


## `generate-token`

Generates a new network token which can be used in a configuration file:

```
$ ./c3os generate-token
b3RwOgogIGRodDoKICAgIGludGVydmFsOiA5MjIzMzcyMDM2ODU0Nzc1ODA3CiAgICBrZXk6IFhMMjRYUk1MTlFOQ1pJQTU0SVFLQ1laMk83SENQWEFBU1ZKN0tZSTQ3MzVaUkpKSktRSEEKICAgIGxlbmd0aDogMzIKICBjcnlwdG86CiAgICBpbnRlcnZhbDogOTIyMzM3MjAzNjg1NDc3NTgwNwogICAga2V5OiBMR1dMWFBTUllaU0ZERDdOT0pBNzdKV0ZWQjRHVkZBMjJIWlZPWU1VT0lNSFVYNFZXUURRCiAgICBsZW5ndGg6IDMyCnJvb206IFRtcUt5VnFHQ1ZZam9TRm9CTEVNRGVEdmJzelBkVEdoCnJlbmRlenZvdXM6IGttb3J4Q21sY2NjVVppWmdkSW5xTERvTGJtS3ZGdm9mCm1kbnM6IEZkWVdQc2R4aHdvWHZlb0VzSXNnVHRXbEJUbE9IVHJmCm1heF9tZXNzYWdlX3NpemU6IDIwOTcxNTIwCg==
```

And now:

```yaml
c3os:
  network_token: b3RwOgogIGRodDoKICAgIGludGVydmFsOiA5MjIzMzcyMDM2ODU0Nzc1ODA3CiAgICBrZXk6IFhMMjRYUk1MTlFOQ1pJQTU0SVFLQ1laMk83SENQWEFBU1ZKN0tZSTQ3MzVaUkpKSktRSEEKICAgIGxlbmd0aDogMzIKICBjcnlwdG86CiAgICBpbnRlcnZhbDogOTIyMzM3MjAzNjg1NDc3NTgwNwogICAga2V5OiBMR1dMWFBTUllaU0ZERDdOT0pBNzdKV0ZWQjRHVkZBMjJIWlZPWU1VT0lNSFVYNFZXUURRCiAgICBsZW5ndGg6IDMyCnJvb206IFRtcUt5VnFHQ1ZZam9TRm9CTEVNRGVEdmJzelBkVEdoCnJlbmRlenZvdXM6IGttb3J4Q21sY2NjVVppWmdkSW5xTERvTGJtS3ZGdm9mCm1kbnM6IEZkWVdQc2R4aHdvWHZlb0VzSXNnVHRXbEJUbE9IVHJmCm1heF9tZXNzYWdlX3NpemU6IDIwOTcxNTIwCg==
  offline: false
  reboot: false
  device: ""
  poweroff: false

# Cloud init syntax to setup users. 
# See https://rancher-sandbox.github.io/cos-toolkit-docs/docs/reference/cloud_init/
stages:
   network:
     - name: "Setup users"
       authorized_keys:
        c3os: 
        - github:yourhandle!
```

## `register` 

The **register** command can be used to register and drive installation of nodes via QR code with a cloud-init config file ( with `--config`).

```
NAME:
    register - 

USAGE:
    register [command options] [arguments...]

OPTIONS:
   --config value  
   --device value  
   --reboot        
   --poweroff
```

When booting `c3os` via ISO, the boot process ends up in displaying a QR code which can be parsed by `c3os register` from another machine.

### Taking a screenshot

`register` by default takes a screenshot and tries to find a QR code in it:

```
c3os register
```

### Providing a qrcode image/screenshot manually

It can be also be specified an image:

```
c3os register <file.png>
```

After the pairing is done, the node will start installation with the provided options. 

A `--device` and a `--config` file are required in order to have a functional installation.

## `install` 

Called by c3os nodes on boot and not meant to be used manually. It kicks in the installation and the QR pairing process.

## `setup` 

Called by c3os nodes on boot and not meant to be used manually. It prepares `edgevpn` and `k3s` bootstrapping the node and the VPN.

