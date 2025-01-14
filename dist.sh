#!/bin/bash

# 1. commit to bump the version and update the changelog/readme
# 2. tag that commit
# 3. use dist.sh to produce tar.gz for all platforms
# 4. aws s3 cp dist s3://bitly-downloads/xmq/ --recursive --include "xmq-1.2.1*" --profile bitly --acl public-read
# 5. docker manifest push xmqio/xmq:latest
# 6. push to xmqio/master
# 7. update the release metadata on github / upload the binaries
# 8. update xmqio/xmqio.github.io/_posts/2014-03-01-installing.md
# 9. send release announcement emails
# 10. update IRC channel topic
# 11. tweet

set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
rm -rf   $DIR/dist/docker
mkdir -p $DIR/dist/docker

GOFLAGS='-ldflags="-s -w"'
version=$(awk '/const Binary/ {print $NF}' < $DIR/internal/version/binary.go | sed 's/"//g')
goversion=$(go version | awk '{print $3}')

echo "... running tests"
./test.sh

export GO111MODULE=on
for target in "linux/amd64" "linux/arm64" "darwin/amd64" "darwin/arm64" "freebsd/amd64" "windows/amd64"; do
    os=${target%/*}
    arch=${target##*/}
    echo "... building v$version for $os/$arch"
    BUILD=$(mktemp -d ${TMPDIR:-/tmp}/xmq-XXXXX)
    TARGET="xmq-$version.$os-$arch.$goversion"
    GOOS=$os GOARCH=$arch CGO_ENABLED=0 \
        make DESTDIR=$BUILD PREFIX=/$TARGET BLDFLAGS="$GOFLAGS" install
    pushd $BUILD
    sudo chown -R 0:0 $TARGET
    tar czvf $TARGET.tar.gz $TARGET
    mv $TARGET.tar.gz $DIR/dist
    popd
    make clean
    sudo rm -r $BUILD
done

rnd=$(LC_ALL=C tr -dc 'a-zA-Z0-9' < /dev/urandom | head -c10)
docker buildx create --use --name xmq-$rnd
docker buildx build --tag xmqio/xmq:v$version --platform linux/amd64,linux/arm64 .
if [[ ! $version == *"-"* ]]; then
    echo "Tagging xmqio/xmq:v$version as the latest release"
    shas=$(docker manifest inspect xmqio/xmq:$version |\
        grep digest | awk '{print $2}' | sed 's/[",]//g' | sed 's/^/xmqio\/xmq@/')
    docker manifest create xmqio/xmq:latest $shas
fi
