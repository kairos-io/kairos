#!/bin/bash

set -e

imageName=$1

imageID=$(aws --region "$AWS_REGION" ec2 describe-images \
  --filters "Name=name,Values=$imageName" \
  --query 'Images[0].ImageId' --output text)

echo "generating a temporary ssh key"
ssh-keygen -t rsa -b 4096 -N "" -f my-key

# TODO: Reset to something not hardcoded? Maybe the same version as the image but different flavor?
aws ec2 run-instances \
    --image-id "$imageID" \
    --count 1 \
    --instance-type t3.micro \
    --block-device-mappings '[
        {
            "DeviceName": "/dev/xvda",
            "Ebs": {
                "VolumeSize": 30,
                "DeleteOnTermination": true,
                "VolumeType": "gp3"
            }
        }
    ]' \
    --user-data file://<(echo -e "#!/bin/bash\n$(cat <<EOF
#cloud-config
users:
- name: kairos
  ssh_authorized_keys:
  - "$(cat my-key.pub)"
  groups:
    - admin

reset:
  system:
    uri: "quay.io/kairos/opensuse:leap-15.6-standard-amd64-generic-master-k3sv1.32.1-rc2-k3s1"
EOF
)") \
--tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=KairosTestAWSVM}]'
