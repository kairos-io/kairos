+++
title = "Meta-Distribution"
date = 2022-02-09T17:56:26+01:00
weight = 1
pre = "<b>- </b>"
+++

We like to define c3os as a meta Linux Distribution as its goal is to convert any other distro to an immutable layout with Kubernetes Native components.

## Components

The c3OS stack is composed of the following:

- A core OS image release for each flavor in ISO, qcow2, and similar format (currently can pick from openSUSE and Alpine based) - provided for user convenience
- A release with k3s embedded
- A set of Kubernetes Native API components (CRDs) to install into the control-plane node, to manage deployment, artifacts creation, and lifecycle (WIP)
- A set of Kubernetes Native API components (CRDs) to install into the target nodes to manage and control the node after deployment (WIP)
- An agent installed into the nodes to be compliant with Kubernetes Native API components mentioned above

Every component is extensible and modular such as it can be customized and replaced in the stack, and built off either locally or with Kubernetes

## 