#!/bin/bash

echo "Listing top largest packages"
pkgs=$(dpkg-query -Wf '${Installed-Size}\t${Package}\t${Status}\n' | awk '$NF == "installed"{print $1 "\t" $2}' | sort -nr)
head -n 30 <<< "${pkgs}"
echo
df -h
echo
sudo apt-get remove -y '^llvm-.*|^libllvm.*' || true
sudo apt-get remove --auto-remove android-sdk-platform-tools || true
sudo apt-get purge --auto-remove android-sdk-platform-tools || true
sudo rm -rf /usr/local/lib/android
sudo apt-get remove -y '^dotnet-.*|^aspnetcore-.*' || true
sudo rm -rf /usr/share/dotnet
sudo apt-get remove -y '^mono-.*' || true
sudo apt-get remove -y '^ghc-.*' || true
sudo apt-get remove -y '.*jdk.*|.*jre.*' || true
sudo apt-get remove -y 'php.*' || true
sudo apt-get remove -y hhvm || true
sudo apt-get remove -y powershell || true
sudo apt-get remove -y firefox || true
sudo apt-get remove -y monodoc-manual || true
sudo apt-get remove -y msbuild || true
sudo apt-get remove -y microsoft-edge-stable || true
sudo apt-get remove -y '^google-.*' || true
sudo apt-get remove -y azure-cli || true
sudo apt-get remove -y '^mongo.*-.*|^postgresql-.*|^mysql-.*|^mssql-.*' || true
sudo apt-get remove -y '^gfortran-.*' || true
sudo apt-get remove -y '^gcc-*' || true
sudo apt-get remove -y '^g++-*' || true
sudo apt-get remove -y '^cpp-*' || true
sudo apt-get autoremove -y
sudo apt-get clean
echo
echo "Listing top largest packages"
pkgs=$(dpkg-query -Wf '${Installed-Size}\t${Package}\t${Status}\n' | awk '$NF == "installed"{print $1 "\t" $2}' | sort -nr)
head -n 30 <<< "${pkgs}"
echo
sudo rm -rfv build || true

sudo rm -rf /usr/local/lib/android # will release about 10 GB if you don't need Android
sudo rm -rf /usr/share/dotnet # will release about 20GB if you don't need .NET

df -h


