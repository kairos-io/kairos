## Connect to the cluster network

Network tokens can be used to connect to the VPN created by the cluster. They are indeed tokens of [edgevpn](https://github.com/mudler/edgevpn) networks, and thus can be used to connect to with its CLI. 

The `kairos` CLI can be used to connect as well, with the `bridge` command:

```bash
sudo kairos bridge --network-token <TOKEN>
```

{{% notice note %}}
The command needs root permissions as it sets up a local tun interface to connect to the VPN.
{{% /notice %}}

Afterward you can connect to [localhost:8080](http://localhost:8080) to access the network API and verify machines are connected.

See [edgeVPN](https://mudler.github.io/edgevpn/docs/getting-started/cli/) documentation on how to connect to the VPN with the edgeVPN cli, which is similar:

```bash
EDGEVPNTOKEN=<network_token> edgevpn --dhcp
```