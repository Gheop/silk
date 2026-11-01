FROM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /silk ./cmd/silk

FROM scratch
COPY --from=build /silk /silk
# The binary reads stdin or a file and writes stdout: no reason to be root.
USER 65534:65534
ENTRYPOINT ["/silk"]
