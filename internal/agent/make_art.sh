#!/bin/sh

if [ -z "${GOPATH}" ]; then
    echo GOPATH environment variable not set
    exit
fi

if [ ! -e "${GOPATH}/bin/2goarray" ]; then
    echo "Installing 2goarray..."
    if ! go get github.com/cratonica/2goarray; then
        echo Failure executing go get github.com/cratonica/2goarray
        exit
    fi
fi

if [ -z "$1" ]; then
    echo Please specify a PNG file
    exit
fi

if [ ! -f "$1" ]; then
    echo "${1} is not a valid file"
    exit
fi    

OUTPUT=iconunix.go
echo "Generating ${OUTPUT}"
echo "//+build linux darwin" > "${OUTPUT}"
echo >> "${OUTPUT}"
if ! "${GOPATH}"/bin/2goarray DefaultBanner agent < "${1}" >> "${OUTPUT}"; then
    echo Failure generating "${OUTPUT}"
    exit
fi
echo Finished
