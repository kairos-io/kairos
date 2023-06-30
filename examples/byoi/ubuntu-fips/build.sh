DOCKER_BUILDKIT=1 docker build . --secret id=pro-attach-config,src=pro-attach-config.yaml -t ubuntu-focal-fips
docker run -v "$PWD"/build:/tmp/auroraboot -v /var/run/docker.sock:/var/run/docker.sock --rm -ti quay.io/kairos/auroraboot --set container_image=docker://ubuntu-focal-fips --set "disable_http_server=true" --set "disable_netboot=true" --set "state_dir=/tmp/auroraboot"
