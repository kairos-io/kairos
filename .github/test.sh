#!/bin/bash

((count = 100))                        
while [[ $count -ne 0 ]] ; do
    echo "Checking if kubeconfig is present"
    ./kairos get-kubeconfig > kube
    grep "http" < kube
    rc=$?
    if [[ $rc -eq 0 ]] ; then
        ((count = 1))
        break
    fi
    ((count = count - 1))
    sleep 5
done

if cat kube; then
    echo "Test succeeded"
else
    echo "Test failed"
    exit 1
fi