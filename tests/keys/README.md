This are TEST keys, used for development purposes.

You can install this keys on a VM EFI and test secureboot.

They are pregenerated so you can iterate building Kairos UKI EFI and use the same signature without generating keys
all the time.

They should never be installed anywhere different than a VM.


Sets of keys:

*.key - Private key
*.crt - Certificate
*.der - Public certificate in DER format. Can be used to manually add the entries to the EFI database.
*.esl - EFI Signature List.
*.auth - SIGNED EFI Signature List. Can be used by systemd-boot to automatically add the entries to the EFI database.


So for a EFI firmware to trust Kairos UKI EFI, you need to add the following entries to the EFI database depending of its state.

Setup mode (No keys installed, no PK key installed) systemd-boot will auto-add the following keys on the first boot and reset the system to continue booting:
 - PK: PK.auth
 - KEK: KEK.auth
 - DB: DB.auth

Adding secureboot keys manually to edk2 firmware:
[![Adding secureboot keys manually to edk2 firmware](https://img.youtube.com/vi/ITlxqQkFbwk/0.jpg)](https://www.youtube.com/watch?v=ITlxqQkFbwk "Adding secureboot keys manually to edk2 firmware")

User mode (PK key installed, other certs already in there) you need to manually add the following keys in the firmware:
 - KEK: KEK.der
 - DB: DB.der

Auto secureBoot key enrollment via systemd-boot:
[![Auto secureBoot key enrollment via systemd-boot](https://img.youtube.com/vi/zmxDNQ56P7s/0.jpg)](https://www.youtube.com/watch?v=zmxDNQ56P7s "Auto secureBoot key enrollment via systemd-boot")

## Generate keys from scratch (key+pem+der+esl)

```bash
uuid=$(uuidgen -N kairos --namespace @dns --sha1)
for key in PK KEK DB; do
  openssl req -new -x509 -subj "/CN=${key}/" -keyout "${key}.key" -out "${key}.pem"
  openssl x509 -outform DER -in "${key}.pem" -out "${key}.der"
  sbsiglist --owner "${uuid}" --type x509 --output "${key}.esl" "${key}.der"
done
```


## Generate auth files for systemd-boot auto-enrollment

```bash
## Generate the auth files from the esl files by signing them.
attr=NON_VOLATILE,RUNTIME_ACCESS,BOOTSERVICE_ACCESS,TIME_BASED_AUTHENTICATED_WRITE_ACCESS
sbvarsign --attr "${attr}" --key PK.key --cert PK.crt --output PK.auth PK PK.esl
sbvarsign --attr "${attr}" --key PK.key --cert PK.crt --output KEK.auth KEK KEK.esl
sbvarsign --attr "${attr}" --key KEK.key --cert KEK.crt --output DB.auth DB DB.esl
```