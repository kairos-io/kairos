#!/bin/sh

for n in $(k3s kubectl get namespace -A | tr -s ' ' | cut -f1 -d' ' | tail -n +2); do
    for p in $(k3s kubectl get pods -n "$n" | tr -s ' ' | cut -f1 -d' ' | tail -n +2); do
        echo ---------------------------
        echo "$n" - "$p"
        echo ---------------------------
        k3s kubectl logs "$p" -n "$n"
    done
done
