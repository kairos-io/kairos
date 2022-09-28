+++
title = "Image support matrix"
date = 2022-02-09T17:56:26+01:00
weight = 7
chapter = false
+++

Kairos builds several artifacts for user convenience which are based on openSUSE, Alpine Linux and Ubuntu.

The images are pushed over quay.io, and are available for installation and upgrading. 

The installable mediums part of the releases are generated with the methods described in the [automated installation reference](/installation/automated/#iso-remastering) from the images sources listed below, and the images can be used to point over for upgrades as well.


| **Flavor/Variant**                                  	| **Core (no k3s)**                                       	| **Standard(k3s+opt.full-mesh)**                           	|
|-----------------------------------------------------	|---------------------------------------------------------	|-----------------------------------------------------------	|
| **openSUSE based**                                  	| https://quay.io/repository/kairos/core-opensuse         	| https://quay.io/repository/kairos/kairos-opensuse         	|
| **Ubuntu based**                                    	| https://quay.io/repository/kairos/core-ubuntu           	| https://quay.io/repository/kairos/kairos-ubuntu           	|
| **Alpine Linux based**                              	| https://quay.io/repository/kairos/core-alpine           	| https://quay.io/repository/kairos/kairos-alpine           	|
| **openSUSE based (RaspberryPi 3 and 4, arm64)**     	| https://quay.io/repository/kairos/core-opensuse-arm-rpi 	| https://quay.io/repository/kairos/kairos-opensuse-arm-rpi 	|
| **Alpine Linux based (RaspberryPi 3 and 4, arm64)** 	| https://quay.io/repository/kairos/core-alpine-arm-rpi   	| https://quay.io/repository/kairos/kairos-alpine-arm-rpi   	|


