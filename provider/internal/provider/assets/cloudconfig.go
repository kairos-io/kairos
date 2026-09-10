package assets

const LocalDNS = `
name: DNS Configuration
stages:
    initramfs:
        - files:
            - path: /etc/systemd/resolved.conf
              permissions: 0644
              owner: 0
              group: 0
              content: |
                [Resolve]
                DNS=127.0.0.1
        - dns:
            nameservers:
                - 127.0.0.1
`
