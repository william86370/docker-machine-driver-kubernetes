go build -o ./machine/docker-machine-driver-nd-6hgmt
docker build -t ghcr.io/william86370/rke2ink:machine -f ./machine/Dockerfile ./machine
docker push ghcr.io/william86370/rke2ink:machine