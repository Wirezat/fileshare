#!/bin/bash

GOOS=linux GOARCH=amd64 go build -o fileshare-backend internal/fileshare_internal.go
GOOS=linux GOARCH=amd64 go build -o fileshare-interface json_interface/fileshare.go
echo "done compile"
sudo rm /opt/fileshare/fileshare-backend
sudo rm /opt/fileshare/fileshare-interface
sudo rm /opt/fileshare/template.html
echo "done rm"
sudo mv fileshare-backend /opt/fileshare/fileshare-backend
sudo mv fileshare-interface /opt/fileshare/fileshare-interface
sudo cp internal/template.html /opt/fileshare/template.html
echo "done mv"
sudo restorecon -v /opt/fileshare/fileshare-backend
sudo restorecon -v /opt/fileshare/fileshare-interface
echo "done sel"
sudo systemctl restart fileshare.service
echo "done restart"