# Setting up the keylime agent in Kairos

Most of the steps are already covered in the [Keylime documentation](https://keylime-docs.readthedocs.io/en/latest/). Here we will cover the steps that are specific to Kairos.


We provide the keylime agent as a luet package for ease of installation.
For it to be installeed you need to create your own derivative of the Kairos image and add the keylime-agent package to it.


```Dockerfile
FROM quay.io/kairos/ubuntu:24.04-core-amd64-generic-v3.2.1 AS base
COPY luet.yaml /etc/luet/luet.yaml
RUN luet install -y --relax utils/keylime-agent
```

Then you can build your image with the agent on it:

```bash
docker build -t kairos-keylime .
```


That will generate an artifact based on the Kairos image with the keylime-agent installed.

Then you need at a minimum the follow configuration in your cloud config:

```yaml
install:
  bind_mounts:
    - /var/lib/keylime
  grub_options:
    extra_cmdline: "ima_appraise=fix ima_template=ima-sig ima_policy=tcb"

stages:
  initramfs:
    - name: "Set keylime user and password"
      users:
        kairos:
          passwd: "kairos"
          groups:
            - "admin"
        keylime:
          groups:
            - "tss"
  boot:
    - name: "Set Keylime config"
      files:
        - path: /var/lib/keylime/cv_ca/cacert.crt
          content: |
            -----BEGIN CERTIFICATE-----
            MIID8zCCAtugAwIBAgIBATANBgkqhkiG9w0BAQsFADBzMQswCQYDVQQGEwJVUzEm
            MCQGA1UEAwwdS2V5bGltZSBDZXJ0aWZpY2F0ZSBBdXRob3JpdHkxCzAJBgNVBAgM
            Ak1BMRIwEAYDVQQHDAlMZXhpbmd0b24xDjAMBgNVBAoMBU1JVExMMQswCQYDVQQL
            DAI1MzAeFw0yNDEwMzAxMTQyNDNaFw0zNDEwMjgxMTQyNDNaMHMxCzAJBgNVBAYT
            AlVTMSYwJAYDVQQDDB1LZXlsaW1lIENlcnRpZmljYXRlIEF1dGhvcml0eTELMAkG
            A1UECAwCTUExEjAQBgNVBAcMCUxleGluZ3RvbjEOMAwGA1UECgwFTUlUTEwxCzAJ
            BgNVBAsMAjUzMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAjiRxfpyt
            ro1FSEprtrDOUo66AmobNO4j2oNeFBbwG31a4bZqHcD7Tjke9V9cwFRM8TtBrg0r
            L5dlZZyM5betmGbgZTwGtPFZthbPvusEOHUrNrwR0imTJtYbqUk5nsRtyyxDJdec
            kh4ibfugyYJu1gEKkZe4BiUisAp5tNifaEdfs9uTz4Ijr4jSniveL1Kio6ngARvM
            xpQgYj4M7fn5q1rIVeZyTFNWFBUY13rViQkvK69b2oz+RwARPgDYkl6kRW/7Z07f
            T7CrEzhbxfbAlPKpfAhcgusHUcajQXfh8T8OtlTNNbTedlFS4dHWkEUKRfoUA09h
            p2ZNCIaGPqQ34QIDAQABo4GRMIGOMA8GA1UdEwEB/wQFMAMBAf8wHQYDVR0OBBYE
            FHxXU4zLckC2WtgM6kxL4c1nxmB1MCsGA1UdHwQkMCIwIKAeoByGGmh0dHA6Ly9s
            b2NhbGhvc3Q6MzgwODAvY3JsMA4GA1UdDwEB/wQEAwIBBjAfBgNVHSMEGDAWgBR8
            V1OMy3JAtlrYDOpMS+HNZ8ZgdTANBgkqhkiG9w0BAQsFAAOCAQEAb9ZyuWPLQDd+
            H2MHr4VEADuXY/EXlBKf+YH9tfWfiWkUkOVPFanX9+dO/EDcOMKItTd6u8FI05SL
            UCjLsjLSwufxC8SpCo3XgkL/1q2wRlZ0IZcHPZV+7qATkqBl54k/ImZwENs0oXuT
            uDcfdJ4FgP/M47HnJaP9/8IRxOgLn370zhxrjx56+A1BPiRAYfWyqCYOEHbFd+Cf
            q9pFQQOHdmarzF/EScq6UvndtXRAthu1I1ArqzSisLV55O5eu6L+5h2ZAoBHlCD6
            Imgvg/m5BbmUo3G5QlfGpU1H7edNsn+OPfC9SDI9jYSKJ8lbyb/fn1QRnjEEnzqs
            AV0t3VsfgQ==
            -----END CERTIFICATE-----
          owner_string: "keylime"
          permissions: 0640
        - path: /etc/keylime/agent.conf.d/10-config.conf
          content: |
            [agent]
            ip = '0.0.0.0'
            registrar_ip = '192.168.100.184'
            uuid = '61388a67-baa4-4f2b-8221-d539b7b4d98b'
          owner_string: "keylime"
          permissions: 0640
    - name: "Set keylime owner to /var/lib/keylime"
      commands:
        - chown -R keylime:keylime /var/lib/keylime
    - name: "Set default IMA policy"
      path: /etc/ima/ima-policy
      permissions: 0644
      content: |
        # PROC_SUPER_MAGIC
        dont_measure fsmagic=0x9fa0
        # SYSFS_MAGIC
        dont_measure fsmagic=0x62656572
        # DEBUGFS_MAGIC
        dont_measure fsmagic=0x64626720
        # TMPFS_MAGIC
        dont_measure fsmagic=0x01021994
        # RAMFS_MAGIC
        dont_measure fsmagic=0x858458f6
        # SECURITYFS_MAGIC
        dont_measure fsmagic=0x73636673
        # SELINUX_MAGIC
        dont_measure fsmagic=0xf97cff8c
        # CGROUP_SUPER_MAGIC
        dont_measure fsmagic=0x27e0eb
        # OVERLAYFS_MAGIC
        # when containers are used we almost always want to ignore them
        dont_measure fsmagic=0x794c7630
        # Don't measure log, audit or tmp files
        dont_measure obj_type=var_log_t
        dont_measure obj_type=auditd_log_t
        dont_measure obj_type=tmp_t
        # MEASUREMENTS
        measure func=BPRM_CHECK
        measure func=FILE_MMAP mask=MAY_EXEC
        measure func=MODULE_CHECK uid=0
    - name: "Enable keylime-agent service"
      systemctl:
        enable:
        - keylime-agent
        start:
        - keylime-agent
```


Lets go a bit into detail of some of the options.

 - `bind_mounts`: This is required for the keylime-agent to store the keys and certificates. It needs to be persisted across reboots.
 - `extra_cmdline`: This is required to enable the IMA appraisal in the kernel. This is required for keylime to work if you expect to use runtime attestation.
 - `users`: We add the keylime user as the default keylime agent service will drop privileges to this user. Has to have the `tss` group as well.
 - `/etc/ima/ima-policy`: This is the default IMA policy that the kernel will use. The one provided is just a generic example.
 You have to make sure that this is deployed properly, otherwise the agent will not be able to communicate with the verifier/tenant/registrar correctly.
 - Ownership of /var/lib/keylime: The keylime agent will need to write to this directory. It is important to set the correct ownership. We do it at the end so all the written files are owned by the keylime user.
 - `systemctl`: We want to enable and start the keylime-agent service so it starts on boot and is running.
 - `/etc/keylime/agent.conf.d/10-config.conf`: This is the keylime agent configuration. Keylime agent provides a default config and we use this to override those default values. Minimal values that need configuring here are as follows: 
   - `ip`: The IP address the agent will listen on. This should be set to `0.0.0.0` to listen on all interfaces or to the specific interface IP address if you know it on advance. Otherwise it will only listen on the loopback interface and won't be reachable from the outside.
   - `registrar_ip`: The IP address of the keylime registrar server. Otherwise the agent will not be able to communicate with the registrar.
   - `uuid`: The UUID of the agent. This is used to identify the agent in the registrar. This can be any UUID as long as it is unique in the registrar server. If you set it to 'generate' it will generate a random UUID for you.


With this values, building a derivative image and installing it should be enough to have the keylime agent running in Kairos.
You can verify it under the registrar as it should auto register itself.


Now from the tenant you can apply any policy you want to the agent.

As an example, we add a policy that will only allow the agent to boot if the PCR 15 is equal to a specific value (in this case empty value as we haven't measured anything into PCR15):
    
```bash
$ keylime_tenant -c update --uuid UID_OF_AGENT -t IP_OF_AGENT  --tpm_policy '{"15":["0000000000000000000000000000000000000000","0000000000000000000000000000000000000000000000000000000000000000","000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"]}'
```

Then the agent will start the attestation process, which you can see both in the agent logs and in the verifier logs. As well as checking with the tenant that the agent is in the correct state.

Then to test the revocation you can extend the PCR15 manually:

```bash
$ tpm2_pcrextend 15:sha256=f1d2d2f924e986ac86fdf7b36c94bcdf32beec15324234324234234333333333
```

Then the verifier will see that the agent is not in the correct state and will revoke it. You can see this in the verifier logs:

```bash
# {"61388a67-baa4-4f2b-8221-d539b7b4d98b": {"operational_state": "Invalid Quote", "v": null, "ip": "192.168.100.164", "port": 9002, "tpm_policy": "{ \"15\": [\"0000000000000000000000000000000000000000\", \"0000000000000000000000000000000000000000000000000000000000000000\", \"000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000\"], \"mask\": \"0x408000\"}", "meta_data": "{}", "has_mb_refstate": 0, "has_runtime_policy": 0, "accept_tpm_hash_algs": ["sha512", "sha384", "sha256"], "accept_tpm_encryption_algs": ["ecc", "rsa"], "accept_tpm_signing_algs": ["ecschnorr", "rsassa"], "hash_alg": "sha256", "enc_alg": "rsa", "sign_alg": "rsassa", "verifier_id": "default", "verifier_ip": "127.0.0.1", "verifier_port": 8881, "severity_level": 6, "last_event_id": "pcr_validation.invalid_pcr_15", "attestation_count": 158, "last_received_quote": 1730388195, "last_successful_attestation": 1730388193}}
```

You will also see on the agent logs that it has been revoked:

```bash
# INFO  keylime_agent::notifications_handler > Received revocation
```

This is a very basic example of how to use keylime in Kairos. You can extend this to use more complex policies and more complex attestation mechanisms. 
As Keylime is a very flexible tool, you can use it in many different ways to secure your infrastructure. Here are some more links to the Keylime documentation to get you started:
 - [Keylime documentation](https://keylime-docs.readthedocs.io/en/latest/)
 - [Red Hat Keylime documentation](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/security_hardening/assembly_ensuring-system-integrity-with-keylime_security-hardening)
 - [Suse Keylime documentation](https://documentation.suse.com/sle-micro/6.0/html/Micro-keylime/index.html)