---
title: "Access your home-lab Kairos cluster over a Wireguard VPN"
date: 2023-03-29T10:53:13+01:00
author: Dimitris Karakasilis([Personal page](https://dimitris.karakasilis.me)) ([GitHub](https://github.com/jimmykarily)) ([Codeberg](https://codeberg.org/dkarakasilis/))
---

## The problem

You got yourself a Rabserry Pi (or more), and you want to put them to good use.
You decide to make a Kubernetes cluster out of them, so that you can utilise the resources better, use familiar tools and implement infrastructure-as-code.

Up to this point, kudos to you for demanding no less than a real cloud from your home infra.

Like a smart person you are, you probably used [Kairos](https://kairos.io/) to create your cluster and it's now  up and running.
It's now time to run some workloads.

Here is my list if you need some ideas:

- A self-hosted Dropbox alternative (e.g. [Seafile](https://www.seafile.com/en/home/), [NextCloud](https://nextcloud.com/) or other)
- [Pihole](https://pi-hole.net/)
- An [mqtt](https://mqtt.org/) broker for your IoT projects
- Your own [Gitea](https://gitea.io/en-us/) instance
- Your own ChatGPT alternative (e.g. using [lama-cli](https://github.com/go-skynet/llama-cli) or [serge](https://github.com/nsarrazin/serge))

None of these workloads is intended for public access. There are ways to expose the cluster to the world (e.g. like I described [in another post](https://dimitris.karakasilis.me/2022/12/26/self-hosted-ci.html))
but it would be better if only devices within a VPN would have access to it.

Once again, there are many VPN solutions out there, but for this blog post, we'll go with [Wireguard](https://www.wireguard.com/).

So here is the problem in one sentence:

> "How do we expose our (possibly behind NAT) cluster, to machines inside the same Wireguard VPN?"

_"NAT" is the main part of the problem because otherwise this would simply be a blog post on how to create a Wireguard VPN. There are many nice tutorials already out there for that._

## A Solution

While trying to solve the problem, I learned 2 things about Wireguard that I didn't know:

1. Wireguard doesn't distinguish between a "server" and a "client". All peers are made equal.
2. Wireguard doesn't provide a solution for NAT traversal. How you access nodes behind NAT, is up to you.

So imagine you have your cluster behind your home router (NAT) and your mobile phone on another network (behind NAT too) trying to access a service on the cluster.
That's not possible, unless there is some public IP address that somehow forwards requests to the cluster.

And that's the idea this solution is based on.

### High level view

![Image describing the solution](/images/kairos-over-wireguard.svg)

The idea is almost similar to the one I described [in another post](https://dimitris.karakasilis.me/2022/12/26/self-hosted-ci.html).
The only difference is, that this time we expose the cluster only to machines inside the VPN.

Prerequisites:

- A VM with a public IP address and SSH access (as small as it gets, it's good enough)
- `kubectl` access to the cluster we want to expose (it doesn't have to be Kairos, even [`k3d`](https://k3d.io) and [`kind`](https://kind.sigs.k8s.io/) will do)
- A machine to test the result (a smartphone where Wireguard can be installed is fine)

### Step by step

From this point on, we will use the IP address `1.2.3.4` as the public IP address of the VM in the cloud.
Replace it with the one matching your VM. We also assume, that the user with SSH access is `root`. Replace if necessary.

#### Setup the cloud VM

SSH to the machine:

```bash
$ ssh root@1.2.3.4
```

Create Wireguard keys:

```bash
$ wg genkey | tee privatekey | wg pubkey > publickey
```

Create Wireguard config:

```bash
$ cat << EOF > /etc/wireguard/wg0.conf
[Interface]
Address = 192.168.6.1/24
PrivateKey = $(cat privatekey)
ListenPort = 41194

# Mobile client
[Peer]
PublicKey = <public key from next step>
AllowedIPs = 192.168.6.2/32
EOF
```

Start and enable the Wireguard service:

```
$ sudo systemctl enable --now wg-quick@wg0
```

Allow binding non-loopback interfaces when creating an SSH reverse tunnel
by setting `GatewayPorts clientspecified` in `/etc/ssh/sshd_config`.

#### Setup the test machine (mobile?)

On some computer with `wg` installed, generate the keys:

```bash
$ wg genkey | tee privatekey | wg pubkey > publickey
```

Create the Wireguard configuration. Follow the instructions for your favorite application.
For Android, you can use this: https://play.google.com/store/apps/details?id=com.wireguard.android

If setting up a Linux machine, you can create the configuration like this:

```bash
$ cat << EOF > /etc/wireguard/wg0.conf
[Interface]
Address = 192.168.6.2/24
PrivateKey = $(cat privatekey)

# The cloud VM
[Peer]
PublicKey = <public key from the previous step>
AllowedIPs = 192.168.6.1/32
Endpoint = 1.2.3.4:41194
EOF
```

Start and enable the Wireguard service. If on a Linux machine, something like this will do:

```
$ sudo systemctl enable --now wg-quick@wg0
```

On a mobile, follow the instructions of your application.

After a while, your client should be able to ping the IP address of the VM: `192.168.6.1`.
You may find the output of `wg show` useful, while waiting for the peers to connect.

#### Setup the cluster

Deploy the helper Pod. We will use an image created [with this Dockerfile](https://codeberg.org/dkarakasilis/self-hosted-ci/src/branch/main/image) and
published [here](https://quay.io/repository/jimmykarily/nginx-ssh-reverse-proxy). The image's entrypoint works with a config
described [here](https://codeberg.org/dkarakasilis/self-hosted-ci/src/commit/20d7c6cbf70cd5318309362b0897e6aeb9842b82/image/start.sh#L5-L27).
The image is not multiarch, but there is one suitable for RasberryPi 4 (see the comment in the file).

If you are are going to create a fresh Kairos cluster, you can use a config like the following to automatically set up the helper Pod (make sure you replace the `id_rsa` and `id_rsa.pub` keys).
If you prefer to not have the keys stored on your Kairos host filesystem, you can simply create the same resources using `kubectl apply -f` after your cluster is up an running.

```
#cloud-config

users:
- name: kairos
passwd: kairos

stages:
after-install-chroot:
    - files:
        - path: /var/lib/rancher/k3s/server/manifests/rproxy-pod.yaml
        content: |
        ---
        apiVersion: v1
        data:
            id_rsa: the_vms_private_key_in_base64
            id_rsa.pub: the_vms_public_key_in_base64
        kind: Secret
        metadata:
            name: jumpbox-ssh-key
        type: Opaque

        ---
        apiVersion: v1
        kind: ConfigMap
        metadata:
            name: proxy-config
        data:
            config.json: |
            {
                "services": [
                    {
                    "bindIP": "192.168.6.1",
                    "bindPort": "443",
                    "proxyAddress": "traefik.kube-system.svc",
                    "proxyPort": "443"
                    },
                    {
                    "bindIP": "192.168.6.1",
                    "bindPort": "80",
                    "proxyAddress": "traefik.kube-system.svc",
                    "proxyPort": "80"
                    }
                ],
                "jumpbox": {
                    "url": "1.2.3.4",
                    "user": "root",
                    "sshKeyFile": "/ssh/id_rsa"
                }
            }

        ---
        apiVersion: apps/v1
        kind: Deployment
        metadata:
            annotations:
            name: nginx-ssh-reverse-proxy
        spec:
            replicas: 1
            selector:
            matchLabels:
                app.kubernetes.io/instance: nginx-ssh-reverse-proxy
                app.kubernetes.io/name: nginx-ssh-reverse-proxy
            template:
            metadata:
                labels:
                    app.kubernetes.io/instance: nginx-ssh-reverse-proxy
                    app.kubernetes.io/name: nginx-ssh-reverse-proxy
            spec:
                containers:
                - name: proxy
                    # Change to quay.io/jimmykarily/nginx-ssh-reverse-proxy-arm64:latest
                    # if you are running on a RasberryPi 4
                    image: quay.io/jimmykarily/nginx-ssh-reverse-proxy:latest
                    command: ["/start.sh", "/proxy-config/config.json"]
                    imagePullPolicy: Always
                    volumeMounts:
                    - name: ssh-key
                    mountPath: /ssh
                    - name: config-volume
                    mountPath: /proxy-config/
                volumes:
                - name: ssh-key
                    secret:
                    secretName: jumpbox-ssh-key
                    defaultMode: 0400
                - name: proxy-config
                - name: config-volume
                    configMap:
                    name: proxy-config

```

In a nutshell, the config above is creating a reverse SSH tunnel from the VM
to the Pod. Inside the Pod, nginx redirects traffic to the traefik load balancer running
on the cluster. This has the effect, that any request landing on the VM on ports 80 and 443
will eventually reach the Traefik instance inside the cluster on ports 80 and 443.
As a result, you can point any domain you want to the VM and it will reach the corresponding Ingress defined on your cluster.

{{% alert color="info" %}}

**NOTE:** The SSH tunnel will only bind the IP address `192.168.6.1` on the VM, which means, anyone trying to access the VM using its public IP address, will not be able to access the cluster. Only machines that can talk to `192.168.6.1` have access, in other words, machines inside the VPN.

{{% /alert %}}

#### Test the connection

- Try to access the cluster with the VPN IP address (should work).
  From your test peer, open `http://192.168.6.1`. You should see a 404 message from Traefik.
  You can also verify it is a response from Traefik in your cluster, by calling curl
  on the `https` endpoint (on a "default" k3s installation):

  ```bash
  $ curl -k -v https://192.168.6.1 2>&1 | grep TRAEFIK
  *  subject: CN=TRAEFIK DEFAULT CERT
  *  issuer: CN=TRAEFIK DEFAULT CERT
  ```

- Try to access the cluster with domain pointing to the VPN IP address (should work)
  You can create a wildcard DNS record and point it to the VPN IP address if
  you want to make it easier for people to access the services you are running.
  E.g. by creating an A record like this: `*.mydomainhere.org -> 192.168.6.1`
  you will be able create Ingresses for your applications like:
  `app1.mydomainhere.org`, `app2.mydomainhere.org`.

- Try to access the cluster using the public IP address (should not work)

  ```bash
  $ curl http://1.2.3.4
  ```
  This command should fail to connect to your cluster


### Conclusion

For non-critical workloads, when 100% uptime is not a hard requirement, the solution we described allows one to use services that would otherwise cost multiple times more by hosting
those on their own hardware. It does so, without exposing the home network to the public.

If you liked this solution or if you have comments, questions or recommendations for improvements, please reach out!

### Useful links

- [Kairos documentation](https://kairos.io/docs/)
- [WireGuard documentation](https://www.wireguard.com/quickstart/)
