# Build stage 
FROM golang:1.26 AS build 
WORKDIR /src

# copy go.mod/go.sum to cache this layer 
copy go.mod go.sum ./ 
run go mod download 

copy . . 
# static 
run CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /sis ./cmd/server/

# as a writable data dir owned by distroless nonroot uid 65532 
run mkdir -p /data && chown 65532:65532 /data

# runtime stage 
from gcr.io/distroless/static-debian12 
copy --from=build /sis /sis 
copy --from=build --chown=65532:65532 /data /data

user 65532:65532 
env PORT=8080 DATABASE_PATH=/data/sis.db
expose 8080 
entrypoint ["/sis"]  


