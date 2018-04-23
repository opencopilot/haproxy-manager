FROM golang:1.10

WORKDIR /go/src/manager
COPY . .

RUN go get -d -v ./...
RUN go install -v ./...
RUN go build -o bin/manager

CMD ["bin/manager"]