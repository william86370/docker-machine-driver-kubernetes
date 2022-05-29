go build -o ./machine/docker-machine-driver-kubernetes
docker build -t ghcr.io/william86370/rke2ink:machine -f ./machine/Dockerfile ./machine
docker push ghcr.io/william86370/rke2ink:machine