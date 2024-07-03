@echo off
D:\Qt\Qt5.14.2\Tools\mingw730_64\bin\protoc.exe -I ./ --go_out=plugins=grpc:. --plugin=protoc-gen-go=D:\go_work\gopath\bin\protoc-gen-go.exe *.proto

pause