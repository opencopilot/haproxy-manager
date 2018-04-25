FROM golang:1.10

RUN apt-get update && apt-get -y install unzip && apt-get clean


# install protobuf
ENV PB_VER 3.5.1
ENV PB_URL https://github.com/google/protobuf/releases/download/v${PB_VER}/protoc-${PB_VER}-linux-x86_64.zip
RUN mkdir -p /tmp/protoc && \
    curl -L ${PB_URL} > /tmp/protoc/protoc.zip && \
    cd /tmp/protoc && \
    unzip protoc.zip && \
    cp /tmp/protoc/bin/protoc /usr/local/bin && \
    cp -R /tmp/protoc/include/* /usr/local/include && \
    chmod go+rx /usr/local/bin/protoc && \
    cd /tmp && \
    rm -r /tmp/protoc

RUN go get github.com/golang/protobuf/protoc-gen-go

WORKDIR /go/src/github.com/opencopilot/haproxy-manager
COPY . .

# generate gRPC
RUN protoc -I ./manager ./manager/Manager.proto --go_out=plugins=grpc:./manager

RUN go get -d -v ./...
RUN go install -v ./...
RUN go build -o bin/manager

EXPOSE 50052

ENTRYPOINT [ "bin/manager" ]