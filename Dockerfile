FROM golang:1.9
RUN go install github.com/sudhirj/zippo
ENTRYPOINT /go/bin/zippo
