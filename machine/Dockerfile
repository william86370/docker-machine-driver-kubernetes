FROM ubuntu:20.04 

RUN set -x \
    && apt-get update \
	&& apt-get upgrade -y \
	&& apt-get install -y --no-install-recommends --no-install-suggests \
		wget \
		ca-certificates \
        curl unzip mkisofs gzip sed tar \
    && apt-get clean autoclean \
	&& apt-get autoremove -y \
	&& rm -rf /var/lib/apt/lists/* 

# install Dockermachine 
RUN mkdir -p /machine && curl -L https://github.com/docker/machine/releases/download/v0.16.2/docker-machine-`uname -s`-`uname -m` >/machine/docker-machine && \
    chmod +x /machine/docker-machine

COPY ./docker-machine-driver-kubernetes /usr/local/bin/docker-machine-driver-kubernetes
RUN chmod +x /usr/local/bin/docker-machine-driver-kubernetes
RUN mkdir -p /.kube
ENV KUBECONFIG=/.kube/config
# ENV POD_K8TOKEN=

ENTRYPOINT [ "/machine/docker-machine" ]