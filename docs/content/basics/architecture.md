+++
title = "Architecture"
date = 2022-02-09T17:56:26+01:00
weight = 2
pre = "<b>-. </b>"
+++

c3OS uses [edgevpn](https://github.com/mudler/edgevpn) to coordinate, automatically discover/configure peers and establish a p2p vpn network between the cluster nodes.

The initial pairing is done via QR code to ease out deployment on situations where sending files or connecting remotely is inconvienent, or either with automatic configuration via cloud-init.