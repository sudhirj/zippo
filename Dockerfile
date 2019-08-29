FROM golang:1.12

# Copy the local package files to the container's workspace.
ADD . /go/src/zippo

# Build the qw-inbox command inside the container.
# (You may fetch or manage dependencies here,
# either manually or with a tool like "godep".)
RUN go install zippo

# Run the qw-inbox command by default when the container starts.
ENTRYPOINT /go/bin/zippo