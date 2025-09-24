CGO_ENABLED=1 \
    CGO_CFLAGS="$(php-config --includes) -I/home/linuxbrew/.linuxbrew/Cellar/watcher/0.13.8/include/ -I/home/linuxbrew/.linuxbrew/Cellar/php-zts/8.4.12/include/php/" \
    CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs) -L/home/linuxbrew/.linuxbrew/lib/ -L/usr/lib" \
    go build .


npx esbuild client.js --bundle --outfile=bundle.js

sudo LD_LIBRARY_PATH=/home/linuxbrew/.linuxbrew/lib:$LD_LIBRARY_PATH ./caddy-grpc-test run

protoc -I=. helloworld.proto \
        --js_out=import_style=commonjs,binary:. \
        --grpc-web_out=import_style=commonjs,mode=grpcwebtext:.

