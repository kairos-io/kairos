FROM alpine as build
WORKDIR /build
RUN wget https://github.com/k3s-io/k3s/releases/download/v1.23.16%2Bk3s1/k3s-airgap-images-amd64.tar.gz

FROM scratch
COPY ./run.sh /
COPY --from=build /build/k3s-airgap-images-amd64.tar.gz /k3s-airgap-images-amd64.tar.gz
